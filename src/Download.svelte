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
      <a class="btn-ghost" href="/video/{videoParam}" download={title}>
        ⬇ Download
      </a>
    {:else if state === "idle" || state === "error"}
      <button class="btn-ghost" onclick={download} title={storageHint}>
        {state === "error" ? "Retry ⬇" : "⬇ Download"}
      </button>
    {:else if state === "downloading"}
      <div class="f al-ct g5">
        <div class="prog-bar rx2">
          <div class="prog-fill" style="width:{progress}%"></div>
        </div>

        <span class="prog-pct">{progress}%</span>
        <button
          class="btn-ghost"
          onclick={remove}
          style="padding:2px 6px;font-size:12px">✕</button
        >
      </div>
    {:else if state === "done"}
      <span class="offline-pill">✓ Offline</span>
      <button
        class="btn-ghost"
        onclick={remove}
        title="Remove offline copy"
        style="padding:2px 8px;font-size:13px">🗑</button
      >
    {/if}
  </div>
{/if}

<style>
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
    font-size: 11px;
    color: #999;
    min-width: 28px;
  }

  .offline-pill {
    font-size: 12px;
    color: #4ade80;
    padding: 2px 8px;
    border: 1px solid #4ade80;
    border-radius: 12px;
    white-space: nowrap;
  }
</style>
