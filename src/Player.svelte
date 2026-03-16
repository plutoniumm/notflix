<script lang="ts">
  import { onMount } from "svelte";
  import videojs from "video.js";
  import "video.js/dist/video-js.css";

  import SubsPicker from "./Subs.svelte";
  import Bar from "./player/bar.svelte";
  import Progress from "./player/progress.svelte";
  import Volume from "./player/volume.svelte";
  import Related from "./player/related.svelte";
  import Whisper from "./player/whisper.svelte";

  import { clean, parseRaw, nextVid } from "./lib/video";
  import { Subs, GET, POST, Tracker } from "./lib";
  import { Down, isSupported } from "./lib/dl";
  import { Touch, Hotkeys } from "./lib/ux";
  import { PlayerState } from "./lib/events.svelte";

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
  let subs = $state<any[] | null>(null);

  async function fetchSubs() {
    const results = await Subs.search(videoParam);
    if (!results) {
      alert("No subtitles found on OpenSubtitles.");
      return;
    }
    subs = results;
  }

  async function selectSub(fid: number) {
    const res = await POST("/api/subs/download", {
      file_id: fid,
      file: videoParam,
    });
    if (res?.ok) {
      subs = null;
      Subs.reload(player, `/subs/${sub}`, "English");
    }
  }

  async function runWhisper() {
    await Subs.whisper(
      videoParam,
      (msg) => (ps.wMsg = msg),
      () => {
        ps.wMsg = "";
        Subs.reload(
          player,
          `/subs/${sub.replace(/\.vtt$/, ".whisper.vtt")}`,
          "Whisper",
          true,
        );
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

    player.ready(() => {
      if (autoplay) player.play();
      const saved = tracker.get(videoParam);
      if (saved > 0) player.currentTime(saved);

      Touch(player, pageEl!);
      Hotkeys(
        player,
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
      );

      setInterval(() => {
        const t = player.currentTime() ?? 0;
        const d = player.duration() ?? 0;
        tracker.set(videoParam, t);
        if (d - t < 5 * 60) tracker.del(videoParam);
      }, 2000);
    });

    Subs.start(player, videoParam, sub);

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
      ps.destroy();
      player?.dispose();
    };
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={pageEl}
  class="player-page"
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
    getSubs={fetchSubs}
    {runWhisper}
  />

  <div class="video-wrap p-abs">
    <video
      bind:this={videoEl}
      class="video-js vjs-default-skin vjs-big-play-centered"
    ></video>
  </div>

  <Whisper msg={ps.wMsg} />

  <Progress pct={ps.pct} duration={ps.duration} hidden={ps.hideUI} />
  <Volume level={ps.volLevel} visible={ps.volVisible} />
  <Related rows={ps.rows} {dir} {name} {embed} paused={ps.paused} />
</div>

{#if subs}
  <SubsPicker
    results={subs}
    onSelect={selectSub}
    onClose={() => (subs = null)}
  />
{/if}

<style>
  .player-page {
    position: fixed;
    inset: 0;
    background: #000;
    overflow: hidden;
  }
  .player-page.hide-ui {
    cursor: none;
  }

  .video-wrap {
    inset: 0;
  }
</style>
