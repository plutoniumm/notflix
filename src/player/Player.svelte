<script lang="ts">
  import { onMount } from "svelte";
  import videojs from "video.js";
  import "video.js/dist/video-js.css";

  const Vhs = (videojs as any).Vhs;
  if (Vhs) {
    Vhs.GOAL_BUFFER_LENGTH = 30;
    Vhs.MAX_GOAL_BUFFER_LENGTH = 90;
    Vhs.GOAL_BUFFER_LENGTH_RATE = 3;
    // Bias first-segment ABR to a high rung on a fat pipe. Without this VHS
    // starts at the lowest rendition and climbs only after measuring.
    Vhs.INITIAL_BANDWIDTH = 20_000_000;
  }

  import Bar from "./bar.svelte";
  import Dock from "./dock.svelte";
  import Volume from "./volume.svelte";
  import Sync from "./sync.svelte";
  import Related from "./related.svelte";
  import Whisper from "./whisper.svelte";
  import Skip, { activeSkip } from "./skip.svelte";
  import Tune from "./tune.svelte";

  import { clean, parseRaw, nextVid } from "../core/video";
  import { api, HEAD } from "../core/api";
  import { toast } from "../core/toast.svelte";
  import Subs from "./subs";
  import WatchProgress from "./watch";
  import { Down, isSupported } from "../library/dl";
  import { local } from "../core/store";
  import { Touch, Hotkeys, togglePip, pipOk, av1Ok } from "./ux";
  import { SAVE_INTERVAL_MS } from "../core/events.svelte";
  import { AVSync } from "./avsync";
  import { PlayerView } from "./view.svelte";
  import { Sprite } from "./sprite.svelte";
  import { Ambient } from "./ambient";
  import Chrome from "./chrome.svelte";
  import type { WhisperCue } from "./subs";

  let { videoParam: initialParam }: any = $props();

  let videoParam = $state(initialParam);
  const parsed = $derived(parseRaw(videoParam));
  const dir = $derived(parsed.dir);
  const name = $derived(parsed.name);
  const title = $derived(clean(name));
  // Any extension the server calls playable (.mp4/.mkv/.mov) reaches here, so
  // swap whatever is present — matching the server's own `base` computation.
  const sub = $derived(videoParam.replace(/\.[^./]+$/, ".vtt"));
  // Probed once per mount — it's a UA capability, not element state. Sent so
  // the server can pick the h264 ladder over the AV1-only master rather than
  // guessing from the User-Agent.
  const av1 = av1Ok() ? 1 : 0;
  const masterSrc = $derived(
    `/api/hls/master?file=${encodeURIComponent(videoParam)}&av1=${av1}`,
  );
  const fileSrc = $derived(`/video/${videoParam}`);
  let autoplay =
    new URLSearchParams(window.location.search).get("autoplay") === "1";
  const embed = window.location.pathname === "/embed";
  const speed = local<number>("speed", 1);
  const watch = new WatchProgress();
  const view = new PlayerView();
  const ps = view.state;
  const sprite = new Sprite();

  let videoEl: HTMLVideoElement | undefined;
  let pageEl: HTMLElement | undefined;
  let player: any = null;
  let avSync: AVSync | null = null;
  let ambient: Ambient | null = null;
  let buffered = $state<[number, number][]>([]);
  let cleanups: (() => void)[] = [];
  let playerError = $state("");
  let listData: any[] | null = null;
  let isOffline = false;
  let swapping = false;
  let dead = false;
  let whisperStop: (() => void) | null = null;
  let fellBack = false;
  let resumeSeek: (() => boolean) | null = null;
  let audioInfo: any[] = [];
  let skipped = { intro: false, outro: false };

  function safePlay() {
    const p = player?.play();
    if (p && typeof p.then === "function") {
      p.catch((err: any) => {
        if (err?.name === "NotAllowedError") {
          toast.info("Tap the video to start playback");
        } else if (err?.name !== "AbortError") {
          toast.err(`Playback blocked: ${err?.message ?? err}`);
        }
      });
    }
  }

  // Seamless next: swap the source in the same document. A full navigation
  // drops Chrome's user-gesture autoplay allowance, so whole seasons would
  // stall on every episode boundary. Offline records still play via direct
  // MP4 + a real navigation.
  function goNext() {
    if (!ps.nextURL || swapping) return;
    if (isOffline) {
      watch.clear(videoParam);
      window.location.href = ps.nextURL;
      return;
    }
    const url = new URL(ps.nextURL, window.location.origin);
    const param = url.searchParams.get("video");
    if (!param) {
      window.location.href = ps.nextURL;
      return;
    }
    watch.clear(videoParam);
    autoplay = true;
    url.searchParams.set("autoplay", "1");
    history.pushState({}, "", url.pathname + url.search);
    loadEpisode(param, true);
  }

  function setMediaMeta() {
    if (!("mediaSession" in navigator)) return;
    navigator.mediaSession.metadata = new MediaMetadata({
      title,
      artist: dir === "." ? "Notflix" : dir,
      artwork: ps.videoKey
        ? [{ src: `/images/${ps.videoKey}.jpg`, type: "image/jpeg" }]
        : [{ src: "/assets/icon.svg", type: "image/svg+xml" }],
    });
  }

  function refreshMeta() {
    if (!listData) return;
    ps.rows = listData
      .filter((g: any) => g.files?.length)
      .map((g: any) => [g.dir, g.files]) as [string, any[]][];
    ps.nextURL = nextVid(listData, dir, name, autoplay);
    ps.videoKey =
      listData
        .find((g: any) => g.dir === dir)
        ?.files?.find((f: any) => f.name === name)?.key ?? "";
    if (ps.videoKey) sprite.load(ps.videoKey);
    setMediaMeta();
  }

  // Resume guard: only seek to a saved position once duration is known
  // and the position lands sanely inside the media. An earlier broken
  // playback timeline could have persisted a drifted/huge currentTime;
  // without this, the player yanks a fresh start to a bogus timestamp
  // ("just started but shows ~2h, jumped back"). Out-of-range → start at 0.
  const applyResume = (param: string, t: number) => {
    if (param !== videoParam || !Number.isFinite(t) || t <= 0) return;
    const trySeek = () => {
      if (param !== videoParam) return true;
      const d = player.duration();
      if (!Number.isFinite(d) || d <= 0) return false;
      if (t < d - 2) player.currentTime(t);
      resumeSeek = null;
      return true;
    };
    if (trySeek()) return;

    resumeSeek = trySeek;
    player.one("loadedmetadata", trySeek);
  };

  // One-shot per episode: guards consumed on trigger, so seeking back into
  // a range deliberately never auto-skips again (manual button + `x` still
  // do). Paused/seeking scrubs through a range don't count — only actual
  // playback entering it.
  function autoSkipTick() {
    if (swapping || player.paused() || player.seeking()) return;
    const which = activeSkip(player.currentTime() ?? 0, introRange, outroRange);
    if (which === "intro" && !skipped.intro) {
      skipped.intro = true;
      player.currentTime(introRange![1]);
      toast.info("Skipped intro");
    } else if (which === "outro" && !skipped.outro && ps.nextURL) {
      skipped.outro = true;
      toast.info("Next episode");
      goNext();
    }
  }

  // ffprobe sees every audio stream, but the picker can only switch what HLS
  // actually shipped: the AV1 demuxed-CMAF master emits ONE #EXT-X-MEDIA
  // rendition, so advertising the probe list there sells switches that don't
  // exist. Re-runs on `addtrack` because the manifest is parsed after the
  // probe call resolves.
  function applyAudio() {
    const atl = player?.audioTracks?.();
    const n = atl?.length ?? 0;
    if (n < 2 || audioInfo.length < 2) {
      view.audio.setTracks([]);
      return;
    }

    view.audio.setTracks(audioInfo);
    for (let i = 0; i < n; i++) {
      if (atl[i].enabled) {
        view.audio.select(i);
        break;
      }
    }
  }

  async function loadEpisode(param: string, swap: boolean) {
    videoParam = param;
    document.title = `${title} | Notflix`;
    playerError = "";
    fellBack = false;
    introRange = null;
    outroRange = null;
    skipped = { intro: false, outro: false };
    whisperStop?.();
    whisperStop = null;
    whisperCues = [];
    ps.wMsg = "";

    if (swap) {
      swapping = true;
      if (resumeSeek) {
        player.off("loadedmetadata", resumeSeek);
        resumeSeek = null;
      }
      const rt = player.remoteTextTracks();
      for (let i = rt.length - 1; i >= 0; i--)
        player.removeRemoteTextTrack(rt[i]);
      view.subs.setInfo(null);
      view.subs.markActive(null);
      view.subs.setResults(null);
      view.subs.close();
      audioInfo = [];
      view.audio.setTracks([]);
      view.audio.select(0);
      view.audio.close();
      sprite.manifest = null;
      sprite.url = "";
      buffered = [];
      avSync?.set(0);
      player.src({ src: masterSrc, type: "application/vnd.apple.mpegurl" });
      const clearSwap = () => (swapping = false);
      player.one("loadedmetadata", clearSwap);
      player.one("error", clearSwap);
      safePlay();
    }

    refreshMeta();
    watch.resume(param).then((t) => applyResume(param, t));

    if (isOffline) {
      const subUrl = `/subs/${sub}`;
      if (await HEAD(subUrl)) {
        Subs.reload(player, subUrl, "Subtitles", true);
      }
    } else {
      const info = await Subs.start(player, param, sub);
      if (param !== videoParam) return;
      view.subs.setInfo(info);
      if (info?.local?.length) {
        const eng = info.local.findIndex((t) =>
          ["eng", "en", "english", "sdh"].includes(
            t.language.replace(/\d+$/, ""),
          ),
        );
        view.subs.markActive(info.local[eng >= 0 ? eng : 0]?.file ?? null);
      }
    }

    api
      .skip(param, { silent: true })
      .then((res: any) => {
        if (param !== videoParam) return;
        introRange = asRange(res?.intro);
        outroRange = asRange(res?.outro);
      })
      .catch((err: any) => console.warn("[skip]", err));

    if (!isOffline) {
      api.hls
        .avoffset(param, { silent: true })
        .then((res: any) => {
          if (param !== videoParam) return;
          if (res?.offset_ms > 0 && avSync?.on) {
            ps.showSync(avSync.set(res.offset_ms));
          }
        })
        .catch((err: any) => console.warn("[avoffset]", err));

      api.audio
        .info(param, { silent: true })
        .then((tracks: any) => {
          if (param !== videoParam) return;
          audioInfo = Array.isArray(tracks) ? tracks : [];
          applyAudio();
        })
        .catch((err: any) => console.warn("[audio info]", err));
    }
  }

  // iPhone Safari exposes no Fullscreen API on non-video ELEMENTS — neither
  // requestFullscreen nor webkitRequestFullscreen exists there, and
  // document.fullscreenElement is always undefined. Only the video itself can
  // go fullscreen, at the cost of the custom chrome.
  function isFull(): boolean {
    return !!(
      document.fullscreenElement ||
      (document as any).webkitFullscreenElement ||
      (videoEl as any)?.webkitDisplayingFullscreen
    );
  }

  function enterFull() {
    const el = pageEl as any;
    const req = el?.requestFullscreen ?? el?.webkitRequestFullscreen;
    if (req) {
      req.call(el)?.catch?.(() => {});
      (screen.orientation as any)?.lock?.("landscape").catch(() => {});
      return;
    }

    const v = videoEl as any;
    if (v?.webkitEnterFullscreen) v.webkitEnterFullscreen();
    else v?.webkitSetPresentationMode?.("fullscreen");
  }

  function exitFull() {
    const d = document as any;
    if (d.fullscreenElement && d.exitFullscreen) {
      d.exitFullscreen()?.catch?.(() => {});
      (screen.orientation as any)?.unlock?.();
      return;
    }
    if (d.webkitFullscreenElement && d.webkitExitFullscreen) {
      d.webkitExitFullscreen();
      return;
    }

    (videoEl as any)?.webkitExitFullscreen?.();
  }

  function toggleFullscreen() {
    isFull() ? exitFull() : enterFull();
  }

  let whisperCues = $state<WhisperCue[]>([]);
  let tuneOpen = $state(false);
  let canPip = $state(false);
  let canSync = $state(false);

  let introRange = $state<[number, number] | null>(null);
  let outroRange = $state<[number, number] | null>(null);

  const asRange = (v: any): [number, number] | null =>
    Array.isArray(v) && v.length === 2 && v.every((n) => Number.isFinite(n))
      ? [v[0], v[1]]
      : null;

  async function searchSubs() {
    view.subs.setSearching(true);
    view.subs.setResults(await Subs.search(videoParam));
    view.subs.setSearching(false);
  }

  function toggleSubs() {
    view.subs.toggle();
    if (view.subs.open && view.subs.onlineResults === null && !view.subs.searching) {
      searchSubs();
    }
  }

  function selectLocal(file: string, label: string) {
    const slash = videoParam.lastIndexOf("/");
    const dir = slash >= 0 ? videoParam.slice(0, slash + 1) : "";
    Subs.reload(player, `/subs/${dir}${file}`, label, true);
    view.subs.markActive(file);
    view.subs.close();
  }

  async function selectEmbedded(idx: number, lang: string) {
    const file = await Subs.extractEmbedded(player, videoParam, idx, lang);
    if (file) {
      view.subs.markActive(file);
      view.subs.setInfo(await api.subs.info(videoParam));
    }
    view.subs.close();
  }

  async function selectOnline(pick: any) {
    const res = await Subs.downloadOnline(player, videoParam, pick);
    if ("file" in res) {
      view.subs.markActive(res.file);
      view.subs.setInfo(await api.subs.info(videoParam));
    } else {
      toast.err(res.error || "Failed to download subtitle");
    }
    view.subs.close();
  }

  function subsOff() {
    const tracks = player.textTracks();
    for (let i = 0; i < tracks.length; i++) {
      if (tracks[i].mode === "showing") tracks[i].mode = "hidden";
    }
    view.subs.markActive(null);
    view.subs.close();
  }

  function selectAudio(track: number) {
    const atl = player?.audioTracks();
    if (!atl || atl.length === 0) {
      toast.err("Audio tracks not available (HLS not parsed yet?)");
      view.audio.close();
      return;
    }
    if (track < 0 || track >= atl.length) {
      toast.err(`Audio track ${track} out of range (have ${atl.length})`);
      view.audio.close();
      return;
    }
    for (let i = 0; i < atl.length; i++) {
      atl[i].enabled = i === track;
    }
    view.audio.select(track);
    view.audio.close();
  }

  function runWhisper() {
    if (view.subs.info?.whisper) {
      const wsub = sub.replace(/\.vtt$/, ".whisper.vtt");
      Subs.reload(player, `/subs/${wsub}`, "Whisper", true);
      return;
    }
    whisperCues = [];
    const wsub = sub;
    whisperStop = Subs.whisperStream(
      videoParam,
      wsub,
      (msg) => (ps.wMsg = msg),
      (cue) => {
        whisperCues = [...whisperCues, cue];
      },
      () => {
        ps.wMsg = "";
        whisperStop = null;
        Subs.reload(
          player,
          `/subs/${wsub.replace(/\.vtt$/, ".whisper.vtt")}`,
          "Whisper",
        );
        whisperCues = [];
      },
    );
  }

  async function init() {
    let initSrc = masterSrc;
    let initType = "application/vnd.apple.mpegurl";
    if (videoParam.endsWith(".mp4") && isSupported()) {
      isOffline = await Down.has(videoParam);
      if (isOffline) {
        initSrc = fileSrc;
        initType = "video/mp4";
      }
    }
    if (dead) return;

    player = videojs(videoEl!, {
      controls: true,
      preload: "auto",
      fill: true,
      playsinline: true,
      playbackRates: [0.5, 1, 1.25, 1.5, 2],
      html5: {
        vhs: {
          limitRenditionByPlayerDimensions: false,
        },
      },
    });

    // MEDIA_ERR_DECODE / MEDIA_ERR_SRC_NOT_SUPPORTED means the device can't
    // play what HLS offered — most often an AV1 master on hardware with no AV1
    // decoder. The stored file is served with byte ranges and faststart, so one
    // retry there rescues the session instead of dead-ending on the overlay.
    player.on("error", () => {
      const err = player.error();
      if (!err) return;
      if (!fellBack && !isOffline && (err.code === 3 || err.code === 4)) {
        fellBack = true;
        const at = player.currentTime() ?? 0;
        player.src({ src: fileSrc, type: "video/mp4" });
        if (at > 0) player.one("loadedmetadata", () => player.currentTime(at));
        safePlay();
        return;
      }
      playerError = err.message || "Playback error";
    });

    player.src({ src: initSrc, type: initType });

    const atl = player.audioTracks?.();
    if (atl?.addEventListener) {
      atl.addEventListener("addtrack", applyAudio);
      atl.addEventListener("removetrack", applyAudio);
      cleanups.push(() => {
        atl.removeEventListener("addtrack", applyAudio);
        atl.removeEventListener("removetrack", applyAudio);
      });
    }

    ps.bind(player);

    // The ambilight samples the tech element, which video.js may swap for the
    // node bound here. Same-origin media, so the canvas never taints — the
    // sampler still guards the read and gives up quietly if it ever does.
    const tech = (player.el()?.querySelector("video") as HTMLVideoElement) ?? videoEl!;
    ambient = new Ambient((a, b) => {
      pageEl?.style.setProperty("--art-1", a);
      pageEl?.style.setProperty("--art-2", b);
    }).attach(tech);

    const readBuffered = () => {
      const b = player?.buffered?.();
      const out: [number, number][] = [];
      for (let i = 0; i < (b?.length ?? 0); i++) out.push([b.start(i), b.end(i)]);
      buffered = out;
    };
    player.on("progress", readBuffered);
    player.on("seeked", readBuffered);
    player.on("loadedmetadata", readBuffered);

    player.on("timeupdate", autoSkipTick);
    player.on("ended", () => {
      if (ps.nextURL) goNext();
    });

    player.requestFullscreen = () => {
      enterFull();
      return player;
    };
    player.exitFullscreen = () => {
      exitFull();
      return player;
    };
    player.isFullscreen = () => isFull();

    player.ready(() => {
      if (autoplay) safePlay();

      const savedRate = speed.get();
      if (savedRate >= 0.25 && savedRate <= 4 && savedRate !== 1)
        player.playbackRate(savedRate);
      player.on("ratechange", () => speed.set(player.playbackRate() || 1));

      cleanups.push(Touch(player, pageEl!, toggleFullscreen));
      cleanups.push(
        Hotkeys(
          player,
          toggleFullscreen,
          goNext,
          () => {
            const tracks = player.textTracks();
            for (let i = 0; i < tracks.length; i++) {
              const t = tracks[i];
              if (
                (t.kind === "captions" || t.kind === "subtitles") &&
                t.mode === "showing"
              )
                t.mode = "hidden";
            }
            runWhisper();
          },
          (deltaMs) => {
            if (canSync) ps.showSync(avSync!.adjust(deltaMs));
          },
          () => {
            const which = activeSkip(
              player.currentTime() ?? 0,
              introRange,
              outroRange,
            );
            if (which === "intro") player.currentTime(introRange![1]);
            else if (which === "outro")
              ps.nextURL ? goNext() : player.currentTime(outroRange![1]);
          },
          () => (tuneOpen = !tuneOpen),
        ),
      );

      const saveProgress = () => {
        if (swapping) return;
        watch.save(videoParam, player.currentTime() ?? 0, player.duration() ?? 0);
      };
      const trackTimer = setInterval(() => {
        if (document.hidden) return;
        saveProgress();
      }, SAVE_INTERVAL_MS);
      player.on("pause", saveProgress);
      player.on("seeked", saveProgress);

      // iOS Safari does not reliably fire beforeunload on app switch, screen
      // lock, tab discard or PWA swipe-away, and the 2s loop stops saving the
      // moment the page hides — so the last write has to happen on pagehide and
      // on visibilitychange, over sendBeacon, which a suspending page keeps.
      const saveOnLeave = () => {
        watch.flushOnLeave(videoParam, player.currentTime() ?? 0, player.duration() ?? 0);
      };
      const onHide = () => {
        if (document.visibilityState === "hidden") saveOnLeave();
      };
      window.addEventListener("pagehide", saveOnLeave);
      window.addEventListener("beforeunload", saveOnLeave);
      document.addEventListener("visibilitychange", onHide);

      cleanups.push(() => {
        clearInterval(trackTimer);
        window.removeEventListener("pagehide", saveOnLeave);
        window.removeEventListener("beforeunload", saveOnLeave);
        document.removeEventListener("visibilitychange", onHide);
      });
    });

    avSync = new AVSync().init(videoEl!);
    canSync = avSync.on;
    canPip = pipOk(player);

    if ("mediaSession" in navigator) {
      const ms = navigator.mediaSession;
      setMediaMeta();
      ms.setActionHandler("play", () => safePlay());
      ms.setActionHandler("pause", () => player.pause());
      ms.setActionHandler("seekbackward", () =>
        player.currentTime(Math.max(0, player.currentTime() - 10)),
      );
      ms.setActionHandler("seekforward", () =>
        player.currentTime(
          Math.min(player.duration(), player.currentTime() + 10),
        ),
      );
      ms.setActionHandler("seekto", (d) => {
        if (d.seekTime != null) player.currentTime(d.seekTime);
      });
      ms.setActionHandler("nexttrack", () => goNext());
      ms.setActionHandler("previoustrack", null);

      const updatePosition = () => {
        try {
          ms.setPositionState({
            duration: player.duration() || 0,
            playbackRate: player.playbackRate() || 1,
            position: Math.min(
              player.currentTime() || 0,
              player.duration() || 0,
            ),
          });
        } catch {}
      };
      player.on("timeupdate", updatePosition);
      player.on("ratechange", updatePosition);
      cleanups.push(() => {
        ms.metadata = null;
        ms.setActionHandler("play", null);
        ms.setActionHandler("pause", null);
        ms.setActionHandler("seekbackward", null);
        ms.setActionHandler("seekforward", null);
        ms.setActionHandler("seekto", null);
        ms.setActionHandler("nexttrack", null);
      });
    }

    api
      .videoList({ silent: true })
      .then((data: any) => {
        if (!Array.isArray(data)) return;
        listData = data;
        refreshMeta();
      })
      .catch((err: any) => console.warn("[videoList]", err));

    const onPop = () => {
      const p = new URLSearchParams(window.location.search).get("video");
      if (!p) {
        window.location.reload();
        return;
      }
      if (p !== videoParam) loadEpisode(p, true);
    };
    window.addEventListener("popstate", onPop);
    cleanups.push(() => window.removeEventListener("popstate", onPop));
    cleanups.push(() => whisperStop?.());

    loadEpisode(videoParam, false);
  }

  // onMount must stay synchronous: Svelte only honours a returned FUNCTION,
  // so an async callback's promise silently discards the teardown.
  onMount(() => {
    init().catch((err: any) => console.warn("[player init]", err));

    return () => {
      dead = true;
      cleanups.forEach((fn) => fn());
      cleanups = [];
      view.destroy();
      ambient?.detach();
      avSync?.destroy();
      player?.dispose();
      player = null;
    };
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={pageEl}
  class="page pl p-fix flow-h"
  class:hide-ui={ps.hideUI}
  class:embed
  onmousemove={() => ps.showUI(ps.paused)}
  ontouchstart={() => ps.showUI(ps.paused)}
>
  <Bar
    {view}
    {title}
    {embed}
    {videoParam}
    {runWhisper}
    {tuneOpen}
    onTune={() => (tuneOpen = !tuneOpen)}
    onToggleSubs={toggleSubs}
    onSelectLocal={selectLocal}
    onSelectEmbedded={selectEmbedded}
    onSelectOnline={selectOnline}
    onSubsOff={subsOff}
    onSelectAudio={selectAudio}
  />

  <Chrome />

  <!-- Shared-element target: the library card's thumbnail carries the same
       `art-<key>` name, so the tile grows into the picture across the
       navigation instead of the page cutting. -->
  <div
    class="video-wrap p-abs"
    style:view-transition-name={ps.videoKey ? `art-${ps.videoKey}` : undefined}
  >
    <!-- Without playsinline iPhone Safari hijacks playback into the native
         fullscreen player and every piece of custom chrome disappears. -->
    <video
      bind:this={videoEl}
      playsinline
      class="video-js vjs-default-skin vjs-big-play-centered"
    ></video>
  </div>

  {#if playerError}
    <div class="error-overlay p-abs cc f-col g10">
      <span class="err-icon">⚠</span>
      <span class="err-msg">{playerError}</span>
      <button
        class="err-retry rx5 ptr"
        onclick={() => {
          playerError = "";
          player?.load();
        }}>Retry</button
      >
    </div>
  {/if}

  <Whisper msg={ps.wMsg} cues={whisperCues} currentTime={ps.currentTime} />

  <Dock
    {ps}
    {title}
    {buffered}
    intro={introRange}
    outro={outroRange}
    speed={ps.speed}
    onSeek={(t) => player?.currentTime(t)}
    onPlayPause={() => (player?.paused() ? safePlay() : player.pause())}
    onSpeedDown={() =>
      player?.playbackRate(
        Math.max(0.25, Math.round((player.playbackRate() - 0.25) * 4) / 4),
      )}
    onSpeedUp={() =>
      player?.playbackRate(
        Math.min(4, Math.round((player.playbackRate() + 0.25) * 4) / 4),
      )}
    onNext={ps.nextURL ? goNext : undefined}
    onPip={canPip ? () => togglePip(player) : undefined}
    onFullscreen={toggleFullscreen}
    trickplay={(sec) => sprite.tile(sec)}
  />
  <Skip
    {ps}
    intro={introRange}
    outro={outroRange}
    onSeek={(t) => player?.currentTime(t)}
    onNext={goNext}
  />
  <Tune
    open={tuneOpen}
    videoEl={() => videoEl ?? null}
    {canSync}
    syncMs={ps.syncMs}
    onSync={(ms) => ps.showSync(avSync?.set(ms) ?? 0)}
    speed={ps.speed}
    onSpeed={(r) => player?.playbackRate(r)}
    onClose={() => (tuneOpen = false)}
  />
  <Volume level={ps.volLevel} visible={ps.volVisible} />
  <Sync ms={ps.syncMs} visible={ps.syncVisible} />
  <Related rows={ps.rows} {dir} {name} {embed} paused={ps.paused} />
</div>

<style>
  .page {
    inset: 0;
    background: #000;
    /* touch-action isn't inherited, but hit-testing walks up to the nearest
       non-auto ancestor — so this kills double-tap zoom for the whole player
       without a global rule. The scrub track opts out with `none`. */
    touch-action: manipulation;
  }
  .page.hide-ui {
    cursor: none;
  }

  .error-overlay {
    inset: 0;
    z-index: 20;
    background: rgba(3, 2, 6, 0.82);
    backdrop-filter: blur(18px) saturate(120%);
    -webkit-backdrop-filter: blur(18px) saturate(120%);
    color: var(--tx-4);
    text-align: center;
  }
  .err-icon {
    font-size: 2rem;
    color: var(--red);
  }
  .err-msg {
    font-family: var(--font-display);
    font-size: 15px;
    letter-spacing: -0.01em;
    max-width: 34ch;
    text-wrap: balance;
  }
  .err-retry {
    background: var(--glass-2);
    border: 1px solid var(--glass-bd);
    color: var(--tx-5);
    padding: 9px 22px;
    font-family: var(--font-display);
    font-size: 14px;
    font-weight: 500;
    transition:
      background var(--m-fast, 130ms) var(--ease-out),
      transform var(--m-fast, 130ms) var(--ease-snap);
  }
  .err-retry:hover {
    background: var(--red);
  }
  .err-retry:active {
    transform: scale(0.96);
  }

  .video-wrap {
    inset: 0;
    z-index: 1;
  }
</style>
