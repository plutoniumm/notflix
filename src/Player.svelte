<script lang="ts">
  import { onMount } from "svelte";
  import videojs from "video.js";
  import "video.js/dist/video-js.css";

  import Bar from "./player/bar.svelte";
  import Progress from "./player/progress.svelte";
  import Volume from "./player/volume.svelte";
  import Sync from "./player/sync.svelte";
  import Related from "./player/related.svelte";
  import Whisper from "./player/whisper.svelte";

  import { clean, parseRaw, nextVid } from "./lib/video";
  import { Subs, GET, POST, Tracker } from "./lib";
  import { Down, isSupported } from "./lib/dl";
  import { Touch, Hotkeys } from "./lib/ux";
  import { PlayerState } from "./lib/events.svelte";
  import { AVSync } from "./lib/avsync";
  import type { SubsInfo, WhisperCue } from "./lib/subs";
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

  // subtitle state
  let subsInfo = $state<SubsInfo | null>(null);
  let whisperCues = $state<WhisperCue[]>([]);
  let subsOpen = $state(false);
  let onlineResults = $state<any[] | null>(null);
  let searchingSubs = $state(false);
  let activeEmbeddedIdx = $state<number | null>(null);

  // audio state
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

  async function selectEmbedded(idx: number) {
    await Subs.extractEmbedded(player, videoParam, sub, idx);
    activeEmbeddedIdx = idx;
    subsOpen = false;
  }

  async function selectOnline(fid: number) {
    const res = await POST("/api/subs/download", {
      file_id: fid,
      file: videoParam,
    });
    if (!res?.ok) {
      alert("Subtitle download failed: " + (res?.error ?? "unknown error"));
      return;
    }
    Subs.reload(player, `/subs/${sub}`, "English");
    activeEmbeddedIdx = null;
    subsOpen = false;
  }

  function selectAudio(track: number) {
    const pos = player.currentTime();
    const wasPlaying = !player.paused();
    audioTrack = track;
    audioOpen = false;
    const src = `/api/hls/master?file=${encodeURIComponent(videoParam)}&audio=${track}`;
    player.src({ src, type: 'application/vnd.apple.mpegurl' });
    player.ready(() => {
      player.currentTime(pos);
      if (wasPlaying) player.play();
    });
  }

  function runWhisper() {
    if (subsInfo?.whisper) {
      const wsub = sub.replace('.vtt', '.whisper.vtt');
      Subs.reload(player, `/subs/${wsub}`, 'Whisper', true);
      return;
    }
    whisperCues = [];
    Subs.whisperStream(
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
  }

  onMount(async () => {
    document.title = `${title} | Notflix`;

    player = videojs(videoEl!, {
      controls: true,
      preload: "auto",
      fill: true,
      playbackRates: [0.5, 1, 1.25, 1.5, 2],
    });

    let initSrc = masterSrc;
    let initType = "application/vnd.apple.mpegurl";
    if (isSupported() && videoParam.endsWith(".mp4")) {
      const record = await Down.get(videoParam);
      if (record?.status === "done") {
        initSrc = `/video/${videoParam}`;
        initType = "video/mp4";
      }
    }
    player.src({ src: initSrc, type: initType });

    player.addRemoteTextTrack(
      {
        kind: "captions",
        src: `/subs/${sub}`,
        srclang: "en",
        label: "English",
        default: true,
      },
      false,
    );

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
      if (autoplay) player.play();
      const saved = tracker.get(videoParam);
      if (saved > 0) {
        player.currentTime(saved);
      } else {
        GET(`/kv/get?key=${encodeURIComponent("watched:" + videoParam)}`).then(
          (res) => {
            const t = res?.value?.t;
            if (t > 60) player.currentTime(Math.max(0, t - 60));
          },
        );
      }

      cleanups.push(Touch(player, pageEl!));
      cleanups.push(Hotkeys(
        player,
        pageEl!,
        () => {
          if (ps.nextURL) window.location.href = ps.nextURL;
        },
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

      const trackTimer = setInterval(() => {
        const t = player.currentTime() ?? 0;
        const d = player.duration() ?? 0;
        tracker.set(videoParam, t);
        if (d > 0 && d - t < 5 * 60) {
          tracker.del(videoParam);
          POST("/kv/set", { key: `watched:${videoParam}`, value: null });
        } else if (t > 60) {
          POST("/kv/set", {
            key: `watched:${videoParam}`,
            value: { t, at: Date.now() },
          });
        }
      }, 2000);
      cleanups.push(() => clearInterval(trackTimer));
    });

    subsInfo = await Subs.start(player, videoParam, sub);

    // Set up Web Audio sync and auto-apply the source file's native A/V offset
    avSync = new AVSync().init(videoEl!);
    GET(`/api/hls/avoffset?file=${encodeURIComponent(videoParam)}`).then((res) => {
      if (res?.offset_ms > 0) {
        ps.showSync(avSync!.set(res.offset_ms));
      }
    });

    GET(`/api/audio/info?file=${encodeURIComponent(videoParam)}`).then((tracks) => {
      if (Array.isArray(tracks) && tracks.length > 1) audioTracks = tracks;
    });

    GET("/list/video")
      .then((data) => {
        ps.rows = Object.entries(data).filter(([, files]) => files?.length) as [
          string,
          any[],
        ][];
        ps.nextURL = nextVid(data, dir, name, autoplay);
        ps.videoKey = data[dir]?.find((f: any) => f.name === name)?.key ?? "";

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
    activeEmbeddedIdx={activeEmbeddedIdx}
    onToggleSubs={toggleSubs}
    onSelectEmbedded={selectEmbedded}
    onSelectOnline={selectOnline}
    onCloseSubs={() => (subsOpen = false)}
    {audioTracks}
    {audioTrack}
    {audioOpen}
    onToggleAudio={() => (audioOpen = !audioOpen)}
    onSelectAudio={selectAudio}
    onCloseAudio={() => (audioOpen = false)}
  />

  <div class="video-wrap p-abs">
    <video
      bind:this={videoEl}
      class="video-js vjs-default-skin vjs-big-play-centered"
    ></video>
  </div>

  <Whisper msg={ps.wMsg} cues={whisperCues} currentTime={ps.currentTime} />

  <Progress pct={ps.pct} duration={ps.duration} hidden={ps.hideUI} />
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

  .video-wrap {
    inset: 0;
  }
</style>
