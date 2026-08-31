<script lang="ts">
  import { ago, durWords } from "../core/fmt";
  import { join } from "../core/path";
  import MediaCard from "../components/MediaCard.svelte";
  import ScrollRow from "../components/ScrollRow.svelte";

  type Item = { dir: string; name: string; key: string; t: number; at: number };

  let {
    items,
    downloadedSet,
    onRemove,
  }: {
    items: Item[];
    downloadedSet: Set<string>;
    onRemove: (e: Event, dir: string, name: string) => void;
  } = $props();
</script>

{#if items.length > 0}
  <section class="row">
    <h2>Continue Watching</h2>
    <ScrollRow>
      <div class="cards f flow-x-s g5">
        {#each items as item, idx (join(item.dir, item.name))}
          <MediaCard
            dir={item.dir}
            name={item.name}
            key={item.key}
            {idx}
            downloaded={downloadedSet.has(join(item.dir, item.name))}
          >
            {#snippet overlay()}
              <!-- svelte-ignore a11y_consider_explicit_label -->
              <button
                class="cw-remove p-abs cc o-0 ptr"
                onclick={(e) => onRemove(e, item.dir, item.name)}>✕</button
              >
              <div class="cw-bar p-abs"></div>
            {/snippet}

            {#snippet footer()}
              <div class="cw-time fs-xs f al-ct j-bw g5">
                <span>{durWords(item.t)}</span>
                <span class="cw-ago">{ago(item.at)}</span>
              </div>
            {/snippet}
          </MediaCard>
        {/each}
      </div>
    </ScrollRow>
  </section>
{/if}

<style>
  .row {
    margin-bottom: 36px;
  }
  .row h2 {
    padding: 0 48px;
    margin: 0 0 14px;
    color: var(--tx-5);
    font-size: 22px;
    font-weight: 600;
  }
  @media (max-width: 640px) {
    .row h2 {
      padding: 0 16px;
      font-size: 17px;
    }
    .cards {
      padding: 6px 16px 12px;
    }
  }

  .cards {
    padding: 8px 48px 16px;
    scrollbar-width: none;
  }
  :global(.media-card) {
    width: 220px;
  }

  .cw-remove {
    top: 6px;
    right: 6px;
    width: 24px;
    height: 24px;
    font-size: 12px;
    background: rgba(13, 11, 18, 0.7);
    backdrop-filter: blur(8px);
    color: var(--tx-3);
    border-radius: 50%;
    border: 1px solid var(--glass-bd);
    z-index: 2;
    transition: opacity 0.15s, background 0.18s var(--ease-out),
      color 0.18s var(--ease-out), transform 0.16s var(--ease-snap);
  }
  :global(.media-card):hover .cw-remove {
    opacity: 1;
  }
  .cw-remove:hover {
    background: var(--red);
    color: #fff;
    border-color: var(--red);
  }
  .cw-remove:active {
    transform: scale(0.88);
  }
  /* No hover to reveal it on touch: opacity 0 still takes taps, so the card
     carried an invisible delete button where a thumb naturally lands. */
  @media (hover: none) {
    .cw-remove {
      opacity: 0.85;
      top: 2px;
      right: 2px;
      width: 44px;
      height: 44px;
      font-size: 16px;
    }
  }

  .cw-bar {
    bottom: 0;
    left: 0;
    right: 0;
    height: 3px;
    background: linear-gradient(90deg, var(--red) 0%, #ff7849 100%);
    box-shadow: 0 0 8px var(--red-glow);
  }

  .cw-time {
    color: var(--tx-2);
    padding: 0 10px 8px;
    background: var(--bg-3);
    border-radius: 0 0 var(--r-lg) var(--r-lg);
  }
  .cw-ago {
    color: var(--tx-1);
  }
</style>
