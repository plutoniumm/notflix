<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { isSupported, Down } from "../library/dl";
  import { usePoll } from "../core/poll";
  import ProgressBar from "../components/ProgressBar.svelte";
  import Icon from "../components/Icon.svelte";

  let {
    videoParam,
    title,
  }: {
    videoParam: string;
    title: string;
    key?: string;
  } = $props();

  type State = "idle" | "downloading" | "done" | "error";

  // BackgroundFetch needs a secure context (absent over plain-HTTP Tailscale)
  // and Safari lacks it entirely — badge only; the real download is <a download>.
  const bgfetch = isSupported();
  const show = $derived(videoParam.endsWith(".mp4"));
  // ?dl=1 -> Content-Disposition: attachment, so Safari saves instead of opening
  // its inline player.
  const fileURL = $derived(`/video/${videoParam}?dl=1`);

  let state = $state<State>("idle");
  let progress = $state(0);
  let stopPoll: (() => void) | null = null;
  let unsub: (() => void) | null = null;

  function endPoll() {
    stopPoll?.();
    stopPoll = null;
  }

  async function init(vp: string) {
    if (!bgfetch) return;
    const record = await Down.get(vp);
    if (!record || vp !== videoParam) return;
    state =
      record.status === "done"
        ? "done"
        : record.status === "error"
          ? "error"
          : "downloading";
    if (state === "downloading" && record.bgFetchId) {
      // usePoll skips ticks while hidden — this used to hit IndexedDB every
      // 500ms in a backgrounded tab forever.
      stopPoll = usePoll(async () => {
        // Down.progress is NaN while BackgroundFetch has no downloadTotal yet.
        const p = await Down.progress(record.bgFetchId!);
        progress = Number.isFinite(p) ? p : 0;
      }, 500);
    }
  }

  async function remove() {
    endPoll();
    await Down.del(videoParam);
    state = "idle";
    progress = 0;
  }

  // videoParam changes under this component during the in-place episode swap,
  // so the offline state must be re-read, not just read on mount.
  $effect(() => {
    const vp = videoParam;
    state = "idle";
    progress = 0;
    init(vp);

    return endPoll;
  });

  onMount(() => {
    unsub = Down.on((vp, record) => {
      if (vp !== videoParam) return;
      endPoll();
      state = record ? (record.status === "done" ? "done" : "error") : "idle";
    });
  });

  onDestroy(() => {
    endPoll();
    unsub?.();
  });
</script>

{#if show}
  <div class="dl-wrap f al-ct g5">
    {#if state === "downloading"}
      <ProgressBar value={progress} label width="120px" height="4px" />
      <button class="btn-ghost fs-xs" onclick={remove} style="padding:2px 6px;"
        >✕</button
      >
    {:else}
      <!-- svelte-ignore a11y_consider_explicit_label -->
      <a
        class="ibtn cc ptr rx5 p5"
        href={fileURL}
        download={title}
        title="Save offline"
      >
        <Icon name="download" />
      </a>
      {#if state === "done"}
        <button
          class="btn-ghost fs-xs"
          onclick={remove}
          title="Remove PWA offline copy"
          style="padding:2px 6px;">🗑</button
        >
      {/if}
    {/if}
  </div>
{/if}

<style>
  /* Matches the bar's own .ibtn — same component, different style scope. */
  .ibtn {
    color: var(--tx-3);
    border: 1px solid transparent;
    border-radius: var(--r-md) !important;
    min-width: 38px;
    min-height: 38px;
    transition:
      color var(--m-fast, 130ms) var(--ease-out),
      background var(--m-fast, 130ms) var(--ease-out),
      border-color var(--m-fast, 130ms) var(--ease-out),
      transform var(--m-fast, 130ms) var(--ease-snap);
  }
  @media (hover: hover) {
    .ibtn:hover {
      color: var(--tx-5);
      background: var(--glass);
      border-color: var(--glass-bd);
    }
  }
  @media (hover: none) {
    .ibtn { min-width: 44px; min-height: 44px; }
  }
  .ibtn:active {
    color: var(--tx-5);
    background: var(--glass-2);
    transform: scale(0.9);
  }
</style>
