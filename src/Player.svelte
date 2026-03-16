<script lang="ts">
  import { onMount } from "svelte";

  import videojs from "video.js";
  import "video.js/dist/video-js.css";
  import SubsPicker from "./Subs.svelte";
  import DownloadButton from "./Download.svelte";

  import { clean, parseRaw, vidURL, nextVid } from "./lib/video";
  import { Quality, Subs, GET, POST, Tracker } from "./lib";
  import { Touch, Hotkeys } from "./lib/ux";

  let { videoParam }: any = $props();

  const { dir, name } = parseRaw(videoParam);
  const title = clean(name);
  const sub = videoParam.replace(/\.mp4$/i, ".vtt");

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
  let quality = $state(localStorage.getItem(Quality.key) ?? "auto");
  let maxHeight = $state<number | null>(null);
  let autoQ = $state<string>("original");
  let speedMbps = $state<number | null>(null);
  let videoDuration = $state(0);

  let player_ready = false;
  let switching = false;
  let stalling = false;
  let streamStart = 0;
  let uiTimer: ReturnType<typeof setTimeout>;
  let waitingDebounce: ReturnType<typeof setTimeout>;
  let idleTimer: ReturnType<typeof setTimeout>;
  let speedWorker: Worker | null = null;
  let streamKilled = false;
  let killedAt = 0;

  let availableLevels = $derived(
    Quality.levels.filter((l) => maxHeight === null || l.h <= maxHeight),
  );
  let autoLabel = $derived(
    Quality.levels.find((l) => l.q === autoQ)?.label ?? "Original",
  );

  function applyQualityAt(q: string, seek = 0) {
    if (!player || !player_ready) return;

    switching = true;
    streamStart = q === "original" ? 0 : seek;
    player.src({
      src: Quality.src(videoParam, q, seek),
      type: Quality.type(q),
    });

    const done = () => {
      switching = false;
      if (player.isDisposed()) return;
      if (q === "original" && seek > 0) player.currentTime(seek);
      if (!paused) player.play().catch(() => {});
    };
    const swTimeout = setTimeout(done, 8000);
    player.one("loadedmetadata", () => {
      clearTimeout(swTimeout);
      done();
    });
  }

  function tryAutoSwitch() {
    if (quality !== "auto" || speedMbps === null || maxHeight === null) return;

    const aq = Quality.auto(speedMbps, maxHeight);
    if (aq === autoQ) return;

    const wasOriginal = autoQ === "original";
    if (wasOriginal && !stalling) return;

    autoQ = aq;
    if (!paused) applyQualityAt(aq, player?.currentTime() ?? 0);
  }

  function setQuality(q: string) {
    quality = q;
    localStorage.setItem(Quality.key, q);
    if (q === "auto") {
      tryAutoSwitch();
      return;
    }

    applyQualityAt(q, player?.currentTime() ?? 0);
  }

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

    const initQ = quality === "auto" ? "original" : quality;
    player.src({
      src: Quality.src(videoParam, initQ),
      type: Quality.type(initQ),
    });

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
        const q = quality === "auto" ? autoQ : quality;
        applyQualityAt(q, killedAt);
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

    player.on("loadedmetadata", () => {
      if (videoDuration > 0 && player.duration() === Infinity)
        player.duration(videoDuration);
    });

    player.on("seeking", () => {
      const effectiveQ = quality === "auto" ? autoQ : quality;
      if (switching || effectiveQ === "original") return;

      const t = player.currentTime() ?? 0;

      if (t < streamStart - 1) {
        applyQualityAt(effectiveQ, t);
        return;
      }

      const buf = player.buffered();
      let maxBuf = streamStart;
      for (let i = 0; i < buf.length; i++) {
        if (buf.start(i) <= t + 1) maxBuf = Math.max(maxBuf, buf.end(i));
      }
      if (t > maxBuf + 2) {
        applyQualityAt(effectiveQ, t);
      }
    });

    player.on("waiting", () => {
      stalling = true;
      if (switching) return;
      clearTimeout(waitingDebounce);
      waitingDebounce = setTimeout(
        () => speedWorker?.postMessage({ type: "measure" }),
        3000,
      );
    });

    player.on("playing", () => {
      stalling = false;
      clearTimeout(waitingDebounce);
    });

    player.ready(() => {
      player_ready = true;
      tryAutoSwitch();
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

    const heightFallback = setTimeout(() => {
      if (maxHeight === null) {
        maxHeight = 1080;
        tryAutoSwitch();
      }
    }, 5000);

    GET(`/api/video/info?file=${encodeURIComponent(videoParam)}`)
      .then((info: { height: number; duration: number }) => {
        clearTimeout(heightFallback);

        maxHeight = info.height > 0 ? info.height : 1080;
        if (info.duration > 0) videoDuration = info.duration;

        tryAutoSwitch();
      })
      .catch(() => {
        clearTimeout(heightFallback);
        maxHeight = 1080;
        tryAutoSwitch();
      });

    speedWorker = new Worker(new URL("./lib/speedWorker.ts", import.meta.url), {
      type: "module",
    });
    speedWorker.onmessage = (e: MessageEvent<number>) => {
      speedMbps = e.data;
      tryAutoSwitch();
    };

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
      clearTimeout(waitingDebounce);
      clearTimeout(idleTimer);
      speedWorker?.terminate();
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
        <div
          class="icon-btn gear-wrap"
          title="Quality: {quality === 'auto'
            ? `Auto (${autoLabel})`
            : quality}"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="currentColor"
            aria-hidden="true"
          >
            <path
              d="M19.14 12.94c.04-.3.06-.61.06-.94s-.02-.64-.07-.94l2.03-1.58a.49.49 0 0 0 .12-.64l-1.92-3.32a.49.49 0 0 0-.6-.22l-2.39.96a7.02 7.02 0 0 0-1.62-.94l-.36-2.54A.484.484 0 0 0 14 2h-4c-.25 0-.46.18-.49.42l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96a.48.48 0 0 0-.6.22L2.62 8.72a.48.48 0 0 0 .12.64l2.03 1.58c-.05.3-.07.62-.07.94s.02.64.07.94l-2.03 1.58a.49.49 0 0 0-.12.64l1.92 3.32c.12.22.37.29.6.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.03.24.24.42.49.42h4c.25 0 .46-.18.49-.42l.36-2.54a6.9 6.9 0 0 0 1.62-.94l2.39.96c.23.08.48 0 .6-.22l1.92-3.32a.49.49 0 0 0-.12-.64l-2.01-1.58zM12 15.6A3.6 3.6 0 1 1 12 8.4a3.6 3.6 0 0 1 0 7.2z"
            />
          </svg>

          <select
            class="gear-select"
            onchange={(e) => setQuality(e.currentTarget.value)}
          >
            <option value="auto" selected={quality === "auto"}>
              Auto ({autoLabel})
            </option>

            <option value="original" selected={quality === "original"}>
              Original
            </option>

            {#each availableLevels as lvl (lvl.q)}
              <option value={lvl.q} selected={quality === lvl.q}>
                {lvl.label}
              </option>
            {/each}
          </select>
        </div>

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

  .gear-wrap {
    position: relative;
  }
  .gear-select {
    position: absolute;
    inset: 0;
    opacity: 0;
    cursor: pointer;
    width: 100%;
    height: 100%;
  }
  .gear-select option {
    background: #141414;
    color: #ddd;
  }
</style>
