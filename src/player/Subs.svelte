<script lang="ts">
  import Dropdown from "./Dropdown.svelte";
  import Badge from "../components/Badge.svelte";
  import { langLabel } from "./subs";
  import type { SubsInfo } from "./subs";

  let {
    info,
    onlineResults,
    searching,
    activeSub,
    onSelectLocal,
    onSelectEmbedded,
    onSelectOnline,
    onSubsOff,
    onClose,
  }: {
    info: SubsInfo | null;
    onlineResults: any[] | null;
    searching: boolean;
    activeSub: string | null;
    onSelectLocal: (file: string, label: string) => void;
    onSelectEmbedded: (idx: number, lang: string) => Promise<void>;
    onSelectOnline: (pick: any) => Promise<void>;
    onSubsOff: () => void;
    onClose: () => void;
  } = $props();

  let busy = $state<string | null>(null);

  function pickKey(r: any): string {
    return r.provider === "subdl" ? "u:" + r.url : "o:" + r.file_id;
  }

  async function pickEmbedded(idx: number, lang: string) {
    busy = `e${idx}`;
    await onSelectEmbedded(idx, lang);
    busy = null;
  }

  async function pickOnline(r: any) {
    busy = pickKey(r);
    await onSelectOnline(r);
    busy = null;
  }
</script>

<Dropdown {onClose} width="300px" maxHeight="320px">
  <button class="item" class:active={!activeSub} onclick={onSubsOff}>
    <span class="bullet">{!activeSub ? "●" : "○"}</span>
    Off
  </button>

  {#if info?.local?.length}
    <div class="divider"></div>
    <div class="section-hd">Local</div>
    {#each info.local as track}
      {@const label = langLabel(track.language)}
      <button
        class="item"
        class:active={activeSub === track.file}
        onclick={() => onSelectLocal(track.file, label)}
      >
        <span class="bullet">{activeSub === track.file ? "●" : "○"}</span>
        {label}
      </button>
    {/each}
  {/if}

  {#if info?.embedded?.length}
    <div class="divider"></div>
    <div class="section-hd">Embedded</div>
    {#each info.embedded as track}
      <button
        class="item"
        class:busy={busy === `e${track.index}`}
        onclick={() => pickEmbedded(track.index, track.language)}
      >
        <span class="bullet">◇</span>
        {track.language || "Unknown"} (track {track.index})
      </button>
    {/each}
  {/if}

  <div class="divider"></div>
  <div class="section-hd row">
    Online
    {#if searching}<span class="spin">↻</span>{/if}
  </div>

  {#if searching && !onlineResults}
    <div class="empty">Searching…</div>
  {:else if onlineResults !== null && onlineResults.length === 0}
    <div class="empty">No results</div>
  {:else if onlineResults?.length}
    {#each onlineResults as r (pickKey(r))}
      <button
        class="item"
        class:busy={busy === pickKey(r)}
        onclick={() => pickOnline(r)}
      >
        {#if r.hash_match}<span class="check">✓</span>{/if}
        <span class="release trunc">{r.release || "Unknown"}</span>
        {#if r.provider === "subdl"}
          <Badge variant="accent">subdl</Badge>
        {:else if r.download_count}
          <span class="dl-count">{r.download_count.toLocaleString()}</span>
        {/if}
      </button>
    {/each}
  {/if}
</Dropdown>

<style>
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
</style>
