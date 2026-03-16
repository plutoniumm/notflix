<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { isSupported, Down } from "../lib/dl";

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
      <!-- svelte-ignore a11y_consider_explicit_label -->
      <a class="ibtn cc ptr rx5 p5" href="/video/{videoParam}" download={title}>
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
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
        class="ibtn cc ptr rx5 p5"
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
          stroke={state === "error" ? "var(--red)" : "currentColor"}
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
        <div class="bar rx2">
          <div class="fill" style="width:{progress}%"></div>
        </div>

        <span class="pct fs-xs">{progress}%</span>
        <button
          class="btn-ghost fs-xs"
          onclick={remove}
          style="padding:2px 6px;">✕</button
        >
      </div>
    {:else if state === "done"}
      <span class="offline rx10 fs-xs">✓ Offline</span>
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
  .ibtn {
    color: var(--tx-4);
    transition:
      color 0.15s,
      background 0.15s;
  }
  .ibtn:hover {
    color: var(--tx-5);
    background: #fff2;
  }

  .bar {
    width: 80px;
    height: 4px;
    background: var(--bg-5);
    overflow: hidden;
  }

  .fill {
    height: 100%;
    background: var(--red);
    transition: width 0.3s;
    min-width: 2px;
  }

  .pct {
    color: var(--tx-2);
    min-width: 28px;
  }

  .offline {
    color: var(--grn);
    padding: 2px 8px;
    border: 1px solid var(--grn);
    white-space: nowrap;
  }
</style>
