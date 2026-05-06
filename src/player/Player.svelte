<script lang="ts">
  import { onMount } from "svelte";
  import videojs from "video.js";
  import "video.js/dist/video-js.css";

  const Vhs = (videojs as any).Vhs;
  if (Vhs) {
    Vhs.GOAL_BUFFER_LENGTH = 60;
    Vhs.MAX_GOAL_BUFFER_LENGTH = 600;
    Vhs.GOAL_BUFFER_LENGTH_RATE = 3;
    // Bias first-segment ABR to a high rung on a fat pipe. Without this VHS
    // starts at the lowest rendition and climbs only after measuring.
    Vhs.INITIAL_BANDWIDTH = 20_000_000;
  }

  import Bar from "./bar.svelte";
  import Progress from "./progress.svelte";
  import Volume from "./volume.svelte";
  import Sync from "./sync.svelte";
  import Related from "./related.svelte";
  import Whisper from "./whisper.svelte";

  import { clean, parseRaw, nextVid } from "../core/video";
  import { api, HEAD, prefetchRange } from "../core/api";
  import { toast } from "../core/toast.svelte";
  import Subs from "./subs";
  import WatchProgress from "./watch";
  import { Down, isSupported } from "../library/dl";
  import { Touch, Hotkeys } from "./ux";
  import { SAVE_INTERVAL_MS } from "../core/events.svelte";
  import { AVSync } from "./avsync";
  import { PlayerView } from "./view.svelte";
  import type { WhisperCue } from "./subs";

  let { videoParam }: any = $props();

  const parsed = $derived(parseRaw(videoParam));
  const dir = $derived(parsed.dir);
  const name = $derived(parsed.name);
  const title = $derived(clean(name));
  const sub = $derived(videoParam.replace(/\.mp4$/i, ".vtt"));
  const masterSrc = $derived(
    `/api/hls/master?file=${encodeURIComponent(videoParam)}`,
  );
  const autoplay =
    new URLSearchParams(window.location.search).get("autoplay") === "1";
  const embed = window.location.pathname === "/embed";
  const watch = new WatchProgress();
  const view = new PlayerView();
  const ps = view.state;

  let videoEl: HTMLVideoElement | undefined;
  let pageEl: HTMLElement | undefined;
  let player: any = null;
  let avSync: AVSync | null = null;
  let cleanups: (() => void)[] = [];
  let playerError = $state("");

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

  function goNext() {
    if (!ps.nextURL) return;
    watch.clear(videoParam);
    window.location.href = ps.nextURL;
  }

  function toggleFullscreen() {
    if (document.fullscreenElement) {
      document.exitFullscreen();
      (screen.orientation as any)?.unlock?.();
    } else {
      pageEl?.requestFullscreen();
      (screen.orientation as any)?.lock?.("landscape").catch(() => {});
    }
  }

  let whisperCues = $state<WhisperCue[]>([]);

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
      toast.err("Audio tracks not available");
      view.audio.close();
      return;
    }
    if (track < 0 || track >= atl.length) {
      toast.err(`Audio track ${track} out of range`);
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
      const wsub = sub.replace(".vtt", ".whisper.vtt");
      Subs.reload(player, `/subs/${wsub}`, "Whisper", true);
      return;
    }
    whisperCues = [];
    const stopWhisper = Subs.whisperStream(
      videoParam,
      sub,
      (msg) => (ps.wMsg = msg),
      (cue) => {
        whisperCues = [...whisperCues, cue];
      },
      () => {
        ps.wMsg = "";
        const wsub = sub.replace(".vtt", ".whisper.vtt");
        Subs.reload(player, `/subs/${wsub}`, "Whisper");
        whisperCues = [];
      },
    );
    cleanups.push(stopWhisper);
  }

  onMount(async () => {
    document.title = `${title} | Notflix`;

    let initSrc = masterSrc;
    let initType = "application/vnd.apple.mpegurl";
    let isOffline = false;
    if (videoParam.endsWith(".mp4")) {
      // Direct MP4 range-serve preserves source quality and skips HLS
      // repackaging. Works for both streaming and SW-cached offline copies.
      initSrc = `/video/${videoParam}`;
      initType = "video/mp4";
      if (isSupported()) {
        const record = await Down.get(videoParam);
        isOffline = record?.status === "done";
      }
    }

    player = videojs(videoEl!, {
      controls: true,
      preload: "auto",
      fill: true,
      playbackRates: [0.5, 1, 1.25, 1.5, 2],
      html5: {
        vhs: {
          limitRenditionByPlayerDimensions: false,
          useBandwidthFromLocalStorage: true,
        },
      },
    });

    player.on("error", () => {
      const err = player.error();
      if (err) playerError = err.message || "Playback error";
    });

    player.src({ src: initSrc, type: initType });

    ps.bind(player);

    player.requestFullscreen = () => {
      pageEl!.requestFullscreen();
      return player;
    };
    player.exitFullscreen = () => {
      document.exitFullscreen();
      return player;
    };
    player.isFullscreen = () => !!document.fullscreenElement;

    player.ready(() => {
      if (autoplay) safePlay();
      const saved = watch.localResume(videoParam);
      if (saved > 0) {
        player.currentTime(saved);
      } else {
        watch.serverResume(videoParam).then((t) => {
          if (t > 0) player.currentTime(t);
        });
      }

      cleanups.push(Touch(player, pageEl!, toggleFullscreen));
      cleanups.push(
        Hotkeys(
          player,
          pageEl!,
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
            const ms = avSync?.adjust(deltaMs) ?? 0;
            ps.showSync(ms);
          },
        ),
      );

      const saveProgress = () => {
        watch.save(videoParam, player.currentTime() ?? 0, player.duration() ?? 0);
      };
      const trackTimer = setInterval(() => {
        if (document.hidden) return;
        saveProgress();
      }, SAVE_INTERVAL_MS);
      player.on("pause", saveProgress);
      player.on("seeked", saveProgress);

      const saveOnLeave = () => {
        watch.flushOnLeave(videoParam, player.currentTime() ?? 0, player.duration() ?? 0);
      };
      window.addEventListener("beforeunload", saveOnLeave);

      const onVisChange = () => {
        const vid = player.tech_?.el_ as HTMLVideoElement | undefined;
        if (!vid || player.paused()) return;
        if (document.hidden) {
          vid.requestPictureInPicture?.().catch(() => {});
        } else if (document.pictureInPictureElement === vid) {
          document.exitPictureInPicture().catch(() => {});
        }
      };
      document.addEventListener("visibilitychange", onVisChange);

      cleanups.push(() => {
        clearInterval(trackTimer);
        window.removeEventListener("beforeunload", saveOnLeave);
        document.removeEventListener("visibilitychange", onVisChange);
      });
    });

    if (isOffline) {
      const subUrl = `/subs/${sub}`;
      if (await HEAD(subUrl)) {
        Subs.reload(player, subUrl, "Subtitles", true);
      }
    } else {
      const info = await Subs.start(player, videoParam, sub);
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

    avSync = new AVSync().init(videoEl!);
    if (!isOffline) {
      api.hls
        .avoffset(videoParam, { silent: true })
        .then((res: any) => {
          if (res?.offset_ms > 0) {
            ps.showSync(avSync!.set(res.offset_ms));
          }
        })
        .catch((err: any) => console.warn("[avoffset]", err));

      api.audio
        .info(videoParam)
        .then((tracks: any) => {
          if (Array.isArray(tracks) && tracks.length > 1) {
            view.audio.setTracks(tracks);
            const atl = player.audioTracks();
            if (!atl) return;
            for (let i = 0; i < atl.length; i++) {
              if (atl[i].enabled) {
                view.audio.select(i);
                break;
              }
            }
          }
        })
        .catch((err: any) => console.warn("[audio info]", err));
    }

    if ("mediaSession" in navigator) {
      const ms = navigator.mediaSession;
      ms.metadata = new MediaMetadata({
        title,
        artist: dir === "." ? "Notflix" : dir,
        artwork: [{ src: "/assets/icon.svg", type: "image/svg+xml" }],
      });
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
      ms.setActionHandler("nexttrack", ps.nextURL ? goNext : null);
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
      .videoList()
      .then((data: any) => {
        if (!data) return;
        ps.rows = Object.entries(data).filter(([, files]) => files?.length) as [
          string,
          any[],
        ][];
        ps.nextURL = nextVid(data, dir, name, autoplay);
        ps.videoKey = data[dir]?.find((f: any) => f.name === name)?.key ?? "";
        if (ps.videoKey && "mediaSession" in navigator) {
          navigator.mediaSession.metadata = new MediaMetadata({
            title,
            artist: dir === "." ? "Notflix" : dir,
            artwork: [
              { src: `/images/${ps.videoKey}.jpg`, type: "image/jpeg" },
            ],
          });
        }

        if (ps.nextURL) {
          const nextParam = new URLSearchParams(
            ps.nextURL.slice(ps.nextURL.indexOf("?")),
          ).get("video");
          if (nextParam) prefetchRange(`/video/${encodeURIComponent(nextParam)}`);
        }
      })
      .catch((err: any) => console.warn("[videoList]", err));

    return () => {
      cleanups.forEach((fn) => fn());
      view.destroy();
      avSync?.destroy();
      player?.dispose();
    };
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={pageEl}
  class="page p-fix flow-h"
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
    onToggleSubs={toggleSubs}
    onSelectLocal={selectLocal}
    onSelectEmbedded={selectEmbedded}
    onSelectOnline={selectOnline}
    onSubsOff={subsOff}
    onSelectAudio={selectAudio}
  />

  <div class="video-wrap p-abs">
    <video
      bind:this={videoEl}
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

  <Progress
    pct={ps.pct}
    currentTime={ps.currentTime}
    duration={ps.duration}
    hidden={ps.hideUI}
    speed={ps.speed}
    paused={ps.paused}
    onSpeedDown={() =>
      player?.playbackRate(
        Math.max(0.25, Math.round((player.playbackRate() - 0.25) * 4) / 4),
      )}
    onSpeedUp={() =>
      player?.playbackRate(
        Math.min(4, Math.round((player.playbackRate() + 0.25) * 4) / 4),
      )}
    onPlayPause={() => (player?.paused() ? safePlay() : player.pause())}
    onNext={ps.nextURL ? goNext : undefined}
  />
  <Volume level={ps.volLevel} visible={ps.volVisible} />
  <Sync ms={ps.syncMs} visible={ps.syncVisible} />
  <Related rows={ps.rows} {dir} {name} {embed} paused={ps.paused} />
</div>

<style>
  .page {
    inset: 0;
    background: #000;
  }
  .page.hide-ui {
    cursor: none;
  }

  .error-overlay {
    inset: 0;
    z-index: 20;
    background: #000c;
    color: var(--tx-4);
    text-align: center;
  }
  .err-icon {
    font-size: 2rem;
  }
  .err-msg {
    font-size: 14px;
    max-width: 320px;
  }
  .err-retry {
    background: var(--bg-5);
    color: var(--tx-5);
    border: none;
    padding: 8px 20px;
    font-size: 14px;
  }

  .video-wrap {
    inset: 0;
  }
</style>
