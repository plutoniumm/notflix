<script lang="ts">
  import { onMount } from "svelte";
  import videojs from "video.js";
  import "video.js/dist/video-js.css";

  const Vhs = (videojs as any).Vhs;
  if (Vhs) {
    Vhs.GOAL_BUFFER_LENGTH = 30;
    Vhs.MAX_GOAL_BUFFER_LENGTH = 300;
    Vhs.GOAL_BUFFER_LENGTH_RATE = 3;
  }

  import Bar from "./bar.svelte";
  import Progress from "./progress.svelte";
  import Volume from "./volume.svelte";
  import Sync from "./sync.svelte";
  import Related from "./related.svelte";
  import Whisper from "./whisper.svelte";

  import { clean, parseRaw, nextVid } from "../core/video";
  import { api } from "../core/api";
  import { kv } from "../core/kv";
  import Subs from "./subs";
  import Tracker from "./tracker";
  import { Down, isSupported } from "../library/dl";
  import { Touch, Hotkeys } from "./ux";
  import {
    PlayerState,
    SAVE_INTERVAL_MS,
    RESUME_THRESHOLD_S,
    END_CUTOFF_S,
  } from "../core/events.svelte";
  import { AVSync } from "./avsync";
  import type { SubsInfo, WhisperCue } from "./subs";
  import type { AudioTrack } from "./AudioPicker.svelte";

  let { videoParam }: any = $props();

  const { dir, name } = parseRaw(videoParam);
  const title = clean(name);
  const sub = videoParam.replace(/\.mp4$/i, ".vtt");
  const masterSrc = `/api/hls/master?file=${encodeURIComponent(videoParam)}`;
  const autoplay =
    new URLSearchParams(window.location.search).get("autoplay") === "1";
  const embed = window.location.pathname === "/embed";
  const tracker = new Tracker();
  const ps = new PlayerState();

  let videoEl: HTMLVideoElement | undefined;
  let pageEl: HTMLElement | undefined;
  let player: any = null;
  let avSync: AVSync | null = null;
  let cleanups: (() => void)[] = [];
  let playerError = $state("");

  function goNext() {
    if (!ps.nextURL) return;
    tracker.del(videoParam);
    navigator.sendBeacon("/kv/set",
      new Blob([JSON.stringify({ key: `watched:${videoParam}`, value: null })],
      { type: "application/json" }));
    window.location.href = ps.nextURL;
  }

  function toggleFullscreen() {
    if (document.fullscreenElement) {
      document.exitFullscreen();
      (screen.orientation as any)?.unlock?.();
    } else {
      pageEl?.requestFullscreen();
      (screen.orientation as any)?.lock?.('landscape').catch(() => {});
    }
  }

  let subsInfo = $state<SubsInfo | null>(null);
  let whisperCues = $state<WhisperCue[]>([]);
  let subsOpen = $state(false);
  let onlineResults = $state<any[] | null>(null);
  let searchingSubs = $state(false);
  let activeSub = $state<string | null>(null);

  let audioTracks = $state<AudioTrack[]>([]);
  let audioTrack = $state(0);
  let audioOpen = $state(false);

  async function searchSubs() {
    searchingSubs = true;
    onlineResults = await Subs.search(videoParam);
    searchingSubs = false;
  }

  function toggleSubs() {
    subsOpen = !subsOpen;
    if (subsOpen && onlineResults === null && !searchingSubs) {
      searchSubs();
    }
  }

  function selectLocal(file: string, label: string) {
    const dir = videoParam.lastIndexOf('/') >= 0 ? videoParam.slice(0, videoParam.lastIndexOf('/') + 1) : '';
    Subs.reload(player, `/subs/${dir}${file}`, label, true);
    activeSub = file;
    subsOpen = false;
  }

  async function selectEmbedded(idx: number, lang: string) {
    const file = await Subs.extractEmbedded(player, videoParam, idx, lang);
    if (file) {
      activeSub = file;
      subsInfo = await api.subs.info(videoParam);
    }
    subsOpen = false;
  }

  async function selectOnline(pick: any) {
    const res = await Subs.downloadOnline(player, videoParam, pick);
    if ('file' in res) {
      activeSub = res.file;
      subsInfo = await api.subs.info(videoParam);
    } else {
      alert(res.error);
    }
    subsOpen = false;
  }

  function subsOff() {
    const tracks = player.textTracks();
    for (let i = 0; i < tracks.length; i++) {
      if (tracks[i].mode === 'showing') tracks[i].mode = 'hidden';
    }
    activeSub = null;
    subsOpen = false;
  }

  function selectAudio(track: number) {
    const atl = player.audioTracks();
    for (let i = 0; i < atl.length; i++) {
      atl[i].enabled = (i === track);
    }
    audioTrack = track;
    audioOpen = false;
  }

  function runWhisper() {
    if (subsInfo?.whisper) {
      const wsub = sub.replace('.vtt', '.whisper.vtt');
      Subs.reload(player, `/subs/${wsub}`, 'Whisper', true);
      return;
    }
    whisperCues = [];
    const stopWhisper = Subs.whisperStream(
      videoParam,
      sub,
      (msg) => (ps.wMsg = msg),
      (cue) => { whisperCues = [...whisperCues, cue]; },
      () => {
        ps.wMsg = '';
        const wsub = sub.replace('.vtt', '.whisper.vtt');
        Subs.reload(player, `/subs/${wsub}`, 'Whisper');
        whisperCues = [];
      },
    );
    cleanups.push(stopWhisper);
  }

  onMount(async () => {
    document.title = `${title} | Notflix`;

    let initSrc = masterSrc;
    let initType = "application/vnd.apple.mpegurl";
    if (isSupported() && videoParam.endsWith(".mp4")) {
      const record = await Down.get(videoParam);
      if (record?.status === "done") {
        initSrc = `/video/${videoParam}`;
        initType = "video/mp4";
      }
    }

    player = videojs(videoEl!, {
      controls: true,
      preload: "auto",
      fill: true,
      playbackRates: [0.5, 1, 1.25, 1.5, 2],
    });

    player.on("error", () => {
      const err = player.error();
      if (err) playerError = err.message || "Playback error";
    });

    player.on("loadedmetadata", () => {
      const tech = player.tech_ as any;
      tech?.on?.("bandwidthupdate", () => {
        const vhs = tech?.vhs;
        if (!vhs || !Vhs) return;
        const bw = vhs.systemBandwidth || vhs.bandwidth || 0;
        const req = vhs.playlists?.media?.()?.attributes?.BANDWIDTH || 0;
        if (bw <= 0 || req <= 0) return;
        const spare = bw / req;
        Vhs.MAX_GOAL_BUFFER_LENGTH = Math.min(600, Math.max(60, 60 * spare));
      });
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

    const isOffline = initType === "video/mp4" && initSrc.startsWith("/video/");

    player.ready(() => {
      if (autoplay) player.play()?.catch(() => {});
      const saved = tracker.get(videoParam);
      if (saved > 0) {
        player.currentTime(saved);
      } else {
        kv.get("watched:" + videoParam).then((res: any) => {
          const t = res?.value?.t;
          if (t > RESUME_THRESHOLD_S) player.currentTime(Math.max(0, t - RESUME_THRESHOLD_S));
        });
      }

      cleanups.push(Touch(player, pageEl!, toggleFullscreen));
      cleanups.push(Hotkeys(
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
      ));

      const saveProgress = () => {
        const t = player.currentTime() ?? 0;
        const d = player.duration() ?? 0;
        tracker.set(videoParam, t);

        if (d > 0 && d - t < END_CUTOFF_S) {
          tracker.del(videoParam);
          kv.set(`watched:${videoParam}`, null);
        } else if (t > RESUME_THRESHOLD_S) {
          kv.set(`watched:${videoParam}`, { t, at: Date.now() });
        }
      };
      const trackTimer = setInterval(() => {
        if (document.hidden) return;
        saveProgress();
      }, SAVE_INTERVAL_MS);
      player.on('pause', saveProgress);
      player.on('seeked', saveProgress);

      const saveOnLeave = () => {
        const t = player.currentTime() ?? 0;
        const d = player.duration() ?? 0;
        if (d > 0 && d - t < END_CUTOFF_S) {
          tracker.del(videoParam);
          navigator.sendBeacon("/kv/set",
            new Blob([JSON.stringify({ key: `watched:${videoParam}`, value: null })],
            { type: "application/json" }));
        } else if (t > RESUME_THRESHOLD_S) {
          tracker.set(videoParam, t);
          navigator.sendBeacon("/kv/set",
            new Blob([JSON.stringify({ key: `watched:${videoParam}`, value: { t, at: Date.now() } })],
            { type: "application/json" }));
        }
        tracker.flush();
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
      document.addEventListener('visibilitychange', onVisChange);

      cleanups.push(() => {
        clearInterval(trackTimer);
        window.removeEventListener("beforeunload", saveOnLeave);
        document.removeEventListener('visibilitychange', onVisChange);
      });
    });

    if (isOffline) {
      try {
        const subUrl = `/subs/${sub}`;
        const res = await fetch(subUrl, { method: 'HEAD' });
        if (res.ok) {
          Subs.reload(player, subUrl, 'Subtitles', true);
        }
      } catch {}
    } else {
      subsInfo = await Subs.start(player, videoParam, sub);
      if (subsInfo?.local?.length) {
        const eng = subsInfo.local.findIndex((t) =>
          ['eng', 'en', 'english', 'sdh'].includes(t.language.replace(/\d+$/, '')));
        activeSub = subsInfo.local[eng >= 0 ? eng : 0]?.file ?? null;
      }
    }

    avSync = new AVSync().init(videoEl!);
    if (!isOffline) {
      api.hls.avoffset(videoParam).then((res: any) => {
        if (res?.offset_ms > 0) {
          ps.showSync(avSync!.set(res.offset_ms));
        }
      });

      api.audio.info(videoParam).then((tracks: any) => {
        if (Array.isArray(tracks) && tracks.length > 1) {
          audioTracks = tracks;
          const atl = player.audioTracks();
          for (let i = 0; i < atl.length; i++) {
            if (atl[i].enabled) { audioTrack = i; break; }
          }
        }
      });
    }

    if ('mediaSession' in navigator) {
      const ms = navigator.mediaSession;
      ms.metadata = new MediaMetadata({
        title,
        artist: dir === '.' ? 'Notflix' : dir,
        artwork: [{ src: '/assets/icon.svg', type: 'image/svg+xml' }],
      });
      ms.setActionHandler('play', () => player.play());
      ms.setActionHandler('pause', () => player.pause());
      ms.setActionHandler('seekbackward', () => player.currentTime(Math.max(0, player.currentTime() - 10)));
      ms.setActionHandler('seekforward', () => player.currentTime(Math.min(player.duration(), player.currentTime() + 10)));
      ms.setActionHandler('seekto', (d) => { if (d.seekTime != null) player.currentTime(d.seekTime); });
      ms.setActionHandler('nexttrack', ps.nextURL ? goNext : null);
      ms.setActionHandler('previoustrack', null);

      const updatePosition = () => {
        try {
          ms.setPositionState({
            duration: player.duration() || 0,
            playbackRate: player.playbackRate() || 1,
            position: Math.min(player.currentTime() || 0, player.duration() || 0),
          });
        } catch {}
      };
      player.on('timeupdate', updatePosition);
      player.on('ratechange', updatePosition);
      cleanups.push(() => {
        ms.metadata = null;
        ms.setActionHandler('play', null);
        ms.setActionHandler('pause', null);
        ms.setActionHandler('seekbackward', null);
        ms.setActionHandler('seekforward', null);
        ms.setActionHandler('seekto', null);
        ms.setActionHandler('nexttrack', null);
      });
    }

    api.videoList()
      .then((data: any) => {
        if (!data) return;
        ps.rows = Object.entries(data).filter(([, files]) => files?.length) as [
          string,
          any[],
        ][];
        ps.nextURL = nextVid(data, dir, name, autoplay);
        ps.videoKey = data[dir]?.find((f: any) => f.name === name)?.key ?? "";
        if (ps.videoKey && 'mediaSession' in navigator) {
          navigator.mediaSession.metadata = new MediaMetadata({
            title,
            artist: dir === '.' ? 'Notflix' : dir,
            artwork: [{ src: `/images/${ps.videoKey}.jpg`, type: 'image/jpeg' }],
          });
        }

        if (ps.nextURL) {
          const nextParam = new URLSearchParams(
            ps.nextURL.slice(ps.nextURL.indexOf("?")),
          ).get("video");
          if (nextParam)
            fetch(`/video/${encodeURIComponent(nextParam)}`, {
              headers: { Range: "bytes=0-1048575" },
              priority: "low" as any,
            }).catch(() => {});
        }
      })
      .catch(() => {});

    return () => {
      cleanups.forEach((fn) => fn());
      ps.destroy();
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
    {title}
    {embed}
    hidden={ps.hideUI}
    videoKey={ps.videoKey}
    {videoParam}
    {runWhisper}
    subsOpen={subsOpen}
    subsInfo={subsInfo}
    onlineResults={onlineResults}
    searching={searchingSubs}
    {activeSub}
    onToggleSubs={toggleSubs}
    onSelectLocal={selectLocal}
    onSelectEmbedded={selectEmbedded}
    onSelectOnline={selectOnline}
    onSubsOff={subsOff}
    onCloseSubs={() => (subsOpen = false)}
    {audioTracks}
    {audioTrack}
    {audioOpen}
    onToggleAudio={() => (audioOpen = !audioOpen)}
    onSelectAudio={selectAudio}
    onCloseAudio={() => (audioOpen = false)}
    hasSubs={ps.hasSubs}
    whisperActive={!!ps.wMsg}
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
      <button class="err-retry rx5 ptr" onclick={() => { playerError = ""; player?.load(); }}>Retry</button>
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
    onSpeedDown={() => player?.playbackRate(Math.max(0.25, Math.round((player.playbackRate() - 0.25) * 4) / 4))}
    onSpeedUp={() => player?.playbackRate(Math.min(4, Math.round((player.playbackRate() + 0.25) * 4) / 4))}
    onPlayPause={() => player?.paused() ? player.play() : player.pause()}
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
  .err-icon { font-size: 2rem; }
  .err-msg { font-size: 14px; max-width: 320px; }
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
