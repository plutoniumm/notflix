<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { isSupported, Down } from "../library/dl";
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

  // BackgroundFetch is a secure-context API. Over Tailscale HTTP (the
  // typical access pattern here) the API surface is missing, and Safari
  // doesn't ship it at all. We use it ONLY to surface "already saved as
  // PWA" badges when applicable; the actual download path is a plain
  // <a download> link that works everywhere.
  const bgfetch = isSupported();
  const show = $derived(videoParam.endsWith(".mp4"));
  // ?dl=1 makes the server send Content-Disposition: attachment so Safari
  // saves instead of opening its inline player.
  const fileURL = $derived(`/video/${videoParam}?dl=1`);

  let state = $state<State>("idle");
  let progress = $state(0);
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let unsub: (() => void) | null = null;

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
      pollTimer = setInterval(async () => {
        // Down.progress is NaN while BackgroundFetch has no downloadTotal yet.
        const p = await Down.progress(record.bgFetchId!);
        progress = Number.isFinite(p) ? p : 0;
      }, 500);
    }
  }

  async function remove() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
    await Down.del(videoParam);
    state = "idle";
    progress = 0;
  }

  // videoParam changes under this component during the in-place next-episode
  // swap, so the offline state has to be re-read, not just read on mount.
  $effect(() => {
    const vp = videoParam;
    state = "idle";
    progress = 0;
    init(vp);

    return () => {
      if (pollTimer) {
        clearInterval(pollTimer);
        pollTimer = null;
      }
    };
  });

  onMount(() => {
    unsub = Down.on((vp, record) => {
      if (vp !== videoParam) return;
      if (pollTimer) {
        clearInterval(pollTimer);
        pollTimer = null;
      }
      state = record ? (record.status === "done" ? "done" : "error") : "idle";
    });
  });

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer);
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
</style>
