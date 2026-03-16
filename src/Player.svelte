<script lang="ts">
  import { onMount } from "svelte";

  import videojs from "video.js";
  import "video.js/dist/video-js.css";
  import SubsPicker from "./Subs.svelte";
  import DownloadButton from "./Download.svelte";

  import { clean, parseRaw, vidURL, nextVid } from "./lib/video";
  import { Subs, GET, POST, Tracker } from "./lib";
  import { Touch, Hotkeys } from "./lib/ux";

  let { videoParam }: any = $props();

  const { dir, name } = parseRaw(videoParam);
  const title = clean(name);
  const sub = videoParam.replace(/\.mp4$/i, ".vtt");
  const masterSrc = `/api/hls/master?file=${encodeURIComponent(videoParam)}`;

  const autoplay =
    new URLSearchParams(window.location.search).get("autoplay") === "1";
  const embed = window.location.pathname === "/embed";
  const tracker = new Tracker();

  let videoEl: HTMLVideoElement | undefined;
  let pageEl: HTMLElement | undefined;
  let player: any = null;
  let subs = $state<any[] | null>(null);
  let wMsg = $state("");
  let nextURL = $state<string | null>(null);
  let rows = $state<[string, any[]][]>([]);
  let videoKey = $state("");
  let paused = $state(true);
  let hideUI = $state(false);

  let uiTimer: ReturnType<typeof setTimeout>;
  let idleTimer: ReturnType<typeof setTimeout>;
  let streamKilled = false;
  let killedAt = 0;

  function showUI() {
    hideUI = false;
    clearTimeout(uiTimer);
    if (!paused)
      uiTimer = setTimeout(() => {
        hideUI = true;
      }, 3000);
  }

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
      (msg) => (wMsg = msg),
      () => {
        wMsg = "";
        Subs.reload(
          player,
          `/subs/${sub.replace(/\.vtt$/, ".whisper.vtt")}`,
          "Whisper",
          true,
        );
      },
    );
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

    player.src({ src: masterSrc, type: "application/vnd.apple.mpegurl" });

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
      clearTimeout(idleTimer);
      if (streamKilled) {
        streamKilled = false;
        paused = false;
        player.src({ src: masterSrc, type: "application/vnd.apple.mpegurl" });
        player.one("loadedmetadata", () => {
          player.currentTime(killedAt);
          player.play().catch(() => {});
        });
        return;
      }

      paused = false;
      showUI();
    });

    player.on("pause", () => {
      paused = true;
      hideUI = false;
      clearTimeout(uiTimer);
      clearTimeout(idleTimer);
      idleTimer = setTimeout(() => {
        killedAt = player.currentTime() ?? 0;
        streamKilled = true;

        const vid = player.el().querySelector("video");
        vid.removeAttribute("src");
        vid.load();
      }, 60_000);
    });

    player.ready(() => {
      if (autoplay) player.play();
      const saved = tracker.get(videoParam);
      if (saved > 0) player.currentTime(saved);

      Touch(player, pageEl!);
      Hotkeys(
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
        rows = Object.entries(data).filter(([, files]) => files?.length);

        nextURL = nextVid(data, dir, name, autoplay);
        videoKey = data[dir]?.find((f) => f.name === name)?.key ?? "";

        if (nextURL) {
          const nextParam = new URLSearchParams(
            nextURL.slice(nextURL.indexOf("?")),
          ).get("video");
          if (nextParam) {
            fetch(`/video/${encodeURIComponent(nextParam)}`, {
              headers: { Range: "bytes=0-1048575" },
              priority: "low" as any,
            }).catch(() => {});
          }
        }
      })
      .catch(() => {});

    return () => {
      clearTimeout(uiTimer);
      clearTimeout(idleTimer);
      player?.dispose();
    };
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={pageEl}
  class="player-page"
  class:hide-ui={hideUI}
  class:embed
  onmousemove={showUI}
  ontouchstart={showUI}
>
  {#if !embed}
    <div class="player-bar f al-ct g10">
      <a href="/" class="back fs-base sh-0">←</a>
      <h1 class="title fw5 m0 trunc">
        {title}
      </h1>

      <div class="f g5 sh-0 al-ct">
        <!-- Subtitles -->
        <button class="icon-btn" onclick={fetchSubs} title="Fetch subtitles">
          <svg
            width="20"
            height="15"
            viewBox="0 0 20 15"
            fill="none"
            aria-hidden="true"
          >
            <rect
              x="0.75"
              y="0.75"
              width="18.5"
              height="13.5"
              rx="2"
              stroke="currentColor"
              stroke-width="1.5"
            />
            <rect
              x="2.5"
              y="9"
              width="6"
              height="2"
              rx="1"
              fill="currentColor"
            />
            <rect
              x="10.5"
              y="9"
              width="7"
              height="2"
              rx="1"
              fill="currentColor"
            />
          </svg>
        </button>

        <button
          class="icon-btn"
          onclick={runWhisper}
          title="Whisper transcription"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <rect x="9" y="2" width="6" height="12" rx="3" />
            <path d="M5 10a7 7 0 0 0 14 0" />
            <line x1="12" y1="17" x2="12" y2="21" />
            <line x1="8" y1="21" x2="16" y2="21" />
          </svg>
        </button>

        {#if videoKey}
          <DownloadButton {videoParam} {title} key={videoKey} />
        {/if}
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
    <div class="whisper-msg p-abs fs-base c-red rx5">{wMsg}</div>
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
                  {clean(f.name)}
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
    animation: slide-down 0.3s ease;
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
    animation: slide-up 0.25s ease;
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

  .icon-btn {
    background: none;
    border: none;
    color: #ccc;
    cursor: pointer;
    padding: 5px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    transition:
      color 0.15s,
      background 0.15s;
  }
  .icon-btn:hover {
    color: #fff;
    background: rgba(255, 255, 255, 0.12);
  }
</style>
