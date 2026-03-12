<script lang="ts">
  import { onMount } from "svelte";
  import videojs from "video.js";
  import "video.js/dist/video-js.css";
  import { cleanName, subPath, parseRaw, vidURL } from "./lib/video";
  import { Tracker } from "./lib/tracker";
  import { initSubs, searchSubs, startWhisper, reloadTrack } from "./lib/subs";
  import { addHotkeys } from "./lib/hotkeys";
  import SubsPicker from "./SubsPicker.svelte";

  let { videoParam }: { videoParam: string } = $props();

  const { dir, name } = parseRaw(videoParam);
  const title = cleanName(name);
  const sub = subPath(videoParam);

  let videoEl = $state<HTMLVideoElement | undefined>(undefined);
  let player: any = null;
  let subs = $state<any[] | null>(null);
  let wMsg = $state("");
  let nextURL = $state<string | null>(null);
  let rows = $state<[string, any[]][]>([]);
  let paused = $state(true);
  let hideUI = $state(false);
  let timer: ReturnType<typeof setTimeout>;

  const tracker = new Tracker();
  const autoplay =
    new URLSearchParams(window.location.search).get("autoplay") === "1";
  const embed = window.location.pathname === "/embed";

  function showUI() {
    hideUI = false;
    clearTimeout(timer);

    if (!paused)
      timer = setTimeout(() => {
        hideUI = true;
      }, 3000);
  }

  onMount(() => {
    document.title = `${title} | Notflix`;

    player = videojs(videoEl!, {
      controls: true,
      preload: "auto",
      fill: true,
      poster: "/assets/home.png",
      playbackRates: [0.5, 1, 1.25, 1.5, 2],
    });

    player.src({ src: `/video/${videoParam}`, type: "video/mp4" });

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

    player.on("play", () => {
      paused = false;
      showUI();
    });

    player.on("pause", () => {
      paused = true;
      hideUI = false;
      clearTimeout(timer);
    });

    player.ready(async () => {
      if (autoplay) player.play();
      const saved = tracker.get(videoParam);
      if (saved > 0) player.currentTime(saved);

      addHotkeys(
        player,
        () => {
          if (nextURL) window.location.href = nextURL;
        },
        () => {
          const tracks = player.textTracks();
          for (let i = 0; i < tracks.length; i++) {
            const t = tracks[i];
            if (
              (t.kind === "captions" || t.kind === "subtitles") &&
              t.mode === "showing"
            ) {
              t.mode = "hidden";
            }
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

    initSubs(player, videoParam, sub);

    fetch("/list/video")
      .then((r) => r.json())
      .then((data: VideoData) => {
        rows = Object.entries(data).filter(([, files]) => files?.length > 0);
        nextURL = nextVid(data);
      })
      .catch(() => {});

    return () => {
      clearTimeout(timer);
      player?.dispose();
    };
  });

  function nextVid(data: VideoData): string | null {
    const files = data[dir] ?? [];
    const idx = files.findIndex((f) => f.name === name);

    if (idx === -1 || idx === files.length - 1) return null;

    return vidURL(dir, files[idx + 1].name, autoplay);
  }

  async function fetchSubs() {
    const results = await searchSubs(videoParam);

    if (!results) {
      alert("No subtitles found on OpenSubtitles.");
      return;
    }

    subs = results;
  }

  async function selectSub(fid: number) {
    const res = await fetch("/api/subs/download", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ file_id: fid, file: videoParam }),
    })
      .then((r) => r.json())
      .catch(() => ({ ok: false }));

    if (res.ok) {
      subs = null;
      reloadTrack(player, `/subs/${sub}`, "English");
    }
  }

  async function runWhisper() {
    await startWhisper(
      videoParam,
      (msg) => (wMsg = msg),
      () => {
        wMsg = "";
        reloadTrack(
          player,
          `/subs/${sub.replace(/\.vtt$/, ".whisper.vtt")}`,
          "Whisper",
          true,
        );
      },
    );
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="player-page"
  class:hide-ui={hideUI}
  class:embed
  onmousemove={showUI}
>
  {#if !embed}
    <div class="player-bar f al-ct g10">
      <a href="/" class="back fs-base sh-0">← Back</a>
      <h1 class="title fw5 m0 trunc">
        {dir !== "." ? `${dir} / ` : ""}{title}
      </h1>

      <div class="f g5 sh-0">
        <button class="btn-ghost" onclick={fetchSubs}> Fetch Subs </button>
        <button class="btn-ghost" onclick={runWhisper}> Whisper </button>
      </div>
    </div>
  {/if}

  <div class="video-wrap p-abs">
    <video
      bind:this={videoEl}
      class="video-js vjs-default-skin vjs-big-play-centered"
    ></video>
  </div>

  {#if wMsg}
    <div class="whisper-msg p-abs fs-base c-red rx5">
      {wMsg}
    </div>
  {/if}

  {#if !embed && rows.length > 0 && paused}
    <div class="related p-abs">
      {#each rows as [rowDir, files]}
        {#if rowDir === dir && files.length > 1}
          <h2 class="fs-sm c-muted m0 fw4" style="margin-bottom:10px">
            {rowDir === "." ? "More Movies" : rowDir}
          </h2>

          <div class="related-list f flow-x-s g10">
            {#each files as f (f.key)}
              <a
                href={vidURL(rowDir, f.name)}
                class="serie sh-0 rx2 flow-h"
                class:active={f.name === name}
              >
                <img
                  src="/images/{f.key}.jpg"
                  alt=""
                  loading="lazy"
                  class="w-100"
                />
                <span class="d-b fs-xs c-muted trunc">
                  {cleanName(f.name)}
                </span>
              </a>
            {/each}
          </div>
        {/if}
      {/each}
    </div>
  {/if}
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

  .player-bar {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    z-index: 10;
    padding: 20px 32px 48px;
    background: linear-gradient(
      to bottom,
      rgba(0, 0, 0, 0.85) 0%,
      transparent 100%
    );
    transition: opacity 0.3s;
  }
  .player-page.hide-ui .player-bar {
    opacity: 0;
    pointer-events: none;
  }

  .back {
    color: #ddd;
    white-space: nowrap;
    transition: color 0.15s;
  }
  .back:hover {
    color: #fff;
  }

  .title {
    flex: 1;
    font-size: 1rem;
    color: #e5e5e5;
  }

  .whisper-msg {
    bottom: 80px;
    left: 32px;
    z-index: 10;
    background: rgba(0, 0, 0, 0.6);
    padding: 6px 12px;
  }

  .related {
    bottom: 0;
    left: 0;
    right: 0;
    z-index: 10;
    padding: 16px 32px 24px;
    background: linear-gradient(
      to top,
      rgba(0, 0, 0, 0.9) 0%,
      transparent 100%
    );
  }

  .related-list {
    padding-bottom: 4px;
    scrollbar-width: none;
  }
  .related-list::-webkit-scrollbar {
    display: none;
  }

  .serie {
    width: 140px;
    border: 2px solid transparent;
    transition:
      border-color 0.15s,
      transform 0.15s;
  }
  .serie:hover {
    transform: scale(1.04);
  }
  .serie.active {
    border-color: #e50914;
  }
  .serie.active span {
    color: #fff;
  }

  .serie img {
    aspect-ratio: 16/9;
    background: #222;
  }
  .serie span {
    padding: 4px 4px 6px;
    background: #1a1a1a;
  }
</style>
