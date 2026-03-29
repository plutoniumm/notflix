<script lang="ts">
  import { onMount } from "svelte";
  import { clickOutside } from "./lib/clickOutside";
  import type { SubsInfo } from "./lib/subs";

  let {
    info,
    onlineResults,
    searching,
    activeEmbeddedIdx,
    onSelectEmbedded,
    onSelectOnline,
    onClose,
  }: {
    info: SubsInfo | null;
    onlineResults: any[] | null;
    searching: boolean;
    activeEmbeddedIdx: number | null;
    onSelectEmbedded: (idx: number) => Promise<void>;
    onSelectOnline: (fid: number) => Promise<void>;
    onClose: () => void;
  } = $props();

  let busy = $state<number | null>(null);

  async function pickEmbedded(idx: number) {
    busy = -idx - 1;
    await onSelectEmbedded(idx);
    busy = null;
  }

  async function pickOnline(fid: number) {
    busy = fid;
    await onSelectOnline(fid);
    busy = null;
  }

  onMount(() => clickOutside(onClose));
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="dropdown" onclick={(e) => e.stopPropagation()}>
  {#if info?.embedded?.length}
    <div class="section-hd">Embedded</div>
    {#each info.embedded as track}
      <button
        class="item"
        class:active={activeEmbeddedIdx === track.index}
        class:busy={busy === -track.index - 1}
        onclick={() => pickEmbedded(track.index)}
      >
        <span class="bullet"
          >{activeEmbeddedIdx === track.index ? "●" : "○"}</span
        >
        {track.language || "Unknown"} (track {track.index})
      </button>
    {/each}
    <div class="divider"></div>
  {/if}

  <div class="section-hd row">
    Online
    {#if searching}<span class="spin">↻</span>{/if}
  </div>

  {#if !searching && !onlineResults}
    <div class="empty">No results</div>
  {:else if onlineResults?.length}
    {#each onlineResults as r (r.file_id)}
      <button
        class="item"
        class:busy={busy === r.file_id}
        onclick={() => pickOnline(r.file_id)}
      >
        {#if r.hash_match}<span class="check">✓</span>{/if}
        <span class="release trunc">{r.release || "Unknown"}</span>
        <span class="dl-count">{r.download_count?.toLocaleString() ?? 0}</span>
      </button>
    {/each}
  {/if}
</div>

<style>
  .dropdown {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    width: 300px;
    max-height: 320px;
    overflow-y: auto;
    background: #111c;
    backdrop-filter: blur(8px);
    border: 1px solid #fff2;
    border-radius: 8px;
    z-index: 100;
    animation: fade-in 0.15s ease;
  }

  .section-hd {
    padding: 8px 12px 4px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--tx-3);
  }

  .section-hd.row {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .divider {
    height: 1px;
    background: #fff1;
    margin: 4px 0;
  }

  .item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 12px;
    text-align: left;
    color: var(--tx-4);
    font-size: 13px;
    transition: background 0.1s;
  }
  .item:hover {
    background: #fff1;
    color: var(--tx-5);
  }
  .item.active {
    color: var(--tx-5);
  }
  .item.busy {
    opacity: 0.6;
    pointer-events: none;
  }

  .bullet {
    width: 14px;
    text-align: center;
    flex-shrink: 0;
  }

  .check {
    color: var(--grn, #4c4);
    flex-shrink: 0;
  }

  .release {
    flex: 1;
  }

  .dl-count {
    font-size: 11px;
    color: var(--tx-2);
    flex-shrink: 0;
  }

  .empty {
    padding: 8px 12px;
    font-size: 12px;
    color: var(--tx-2);
  }

  .spin {
    animation: spin 1s linear infinite;
    display: inline-block;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @keyframes fade-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
</style>
