<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { isSupported, Down } from "./lib/dl";

  let {
    videoParam,
    title,
    key,
  }: {
    videoParam: string;
    title: string;
    key: string;
  } = $props();

  type State = "idle" | "downloading" | "done" | "error";

  const bgfetch = isSupported();
  const show = videoParam.endsWith(".mp4");

  let state = $state<State>("idle");
  let progress = $state(0);
  let storageHint = $state("");
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let unsub: (() => void) | null = null;

  async function init() {
    if (!bgfetch) return;

    const record = await Down.get(videoParam);
    if (record) {
      state =
        record.status === "done"
          ? "done"
          : record.status === "error"
            ? "error"
            : "downloading";
      if (state === "downloading") startPolling(record.bgFetchId!);
    }

    const est = await Down.storage();
    if (est.quota > 0) {
      storageHint = `${((est.quota - est.used) / 1e9).toFixed(1)} GB free`;
    }
  }

  function startPolling(bgFetchId: string) {
    if (pollTimer) return;
    pollTimer = setInterval(async () => {
      progress = await Down.progress(bgFetchId);
    }, 500);
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  async function download() {
    try {
      state = "downloading";
      progress = 0;
      await Down.start(videoParam, title, key);

      const record = await Down.get(videoParam);
      if (record?.bgFetchId) startPolling(record.bgFetchId);
    } catch (e) {
      console.error("Download failed:", e);
      state = "error";
    }
  }

  async function remove() {
    stopPolling();
    await Down.del(videoParam);
    state = "idle";
    progress = 0;
  }

  onMount(() => {
    init();
    unsub = Down.on((vp, record) => {
      if (vp !== videoParam) return;
      stopPolling();
      state = record ? (record.status === "done" ? "done" : "error") : "idle";
    });
  });

  onDestroy(() => {
    stopPolling();
    unsub?.();
  });
</script>

{#if show}
  <div class="dl-wrap f al-ct g5">
    {#if !bgfetch}
      <a
        class="icon-btn"
        href="/video/{videoParam}"
        download={title}
        title="Download"
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
          <path d="M12 3v13M5 12l7 7 7-7" /><line
            x1="4"
            y1="21"
            x2="20"
            y2="21"
          />
        </svg>
      </a>
    {:else if state === "idle" || state === "error"}
      <button
        class="icon-btn"
        onclick={download}
        title={state === "error"
          ? `Retry — ${storageHint}`
          : storageHint || "Download"}
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke={state === "error" ? "#f87171" : "currentColor"}
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M12 3v13M5 12l7 7 7-7" /><line
            x1="4"
            y1="21"
            x2="20"
            y2="21"
          />
        </svg>
      </button>
    {:else if state === "downloading"}
      <div class="f al-ct g5">
        <div class="prog-bar rx2">
          <div class="prog-fill" style="width:{progress}%"></div>
        </div>

        <span class="prog-pct fs-xs">{progress}%</span>
        <button
          class="btn-ghost fs-xs"
          onclick={remove}
          style="padding:2px 6px;">✕</button
        >
      </div>
    {:else if state === "done"}
      <span class="offline-pill rx10 fs-xs">✓ Offline</span>
      <button
        class="btn-ghost fs-xs"
        onclick={remove}
        title="Remove offline copy"
        style="padding:2px 8px;">🗑</button
      >
    {/if}
  </div>
{/if}

<style>
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
    text-decoration: none;
  }
  .icon-btn:hover {
    color: #fff;
    background: rgba(255, 255, 255, 0.12);
  }

  .prog-bar {
    width: 80px;
    height: 4px;
    background: #444;
    overflow: hidden;
  }

  .prog-fill {
    height: 100%;
    background: #e50914;
    transition: width 0.3s;
    min-width: 2px;
  }

  .prog-pct {
    color: #999;
    min-width: 28px;
  }

  .offline-pill {
    color: #4ade80;
    padding: 2px 8px;
    border: 1px solid #4ade80;
    white-space: nowrap;
  }
</style>
