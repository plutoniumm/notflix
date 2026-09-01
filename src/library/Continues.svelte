<script lang="ts">
  import { ago, durWords } from "../core/fmt";
  import { join } from "../core/path";
  import MediaCard from "../components/MediaCard.svelte";
  import ScrollRow from "../components/ScrollRow.svelte";

  type Item = {
    dir: string;
    name: string;
    key: string;
    t: number;
    at: number;
    dur?: number;
  };

  let {
    items,
    downloadedSet,
    onRemove,
  }: {
    items: Item[];
    downloadedSet: Set<string>;
    onRemove: (e: Event, dir: string, name: string) => void;
  } = $props();

  // The one you'd actually resume gets the room; the rest stay a rail.
  const lede = $derived(items[0]);
  const rest = $derived(items.slice(1));

  const done = (i: Item) =>
    i.dur && i.dur > 0 ? Math.min(100, (i.t / i.dur) * 100) : 0;

  const left = (i: Item) =>
    i.dur && i.dur > i.t
      ? `${durWords(i.dur - i.t)} left`
      : `${durWords(i.t)} in`;
</script>

{#if items.length > 0}
  <section class="row">
    <div class="head f al-ct g10">
      <h2 class="m0">Continue watching</h2>
      <span class="count fs-xs tx-1">{items.length}</span>
    </div>

    <div class="lede" class:solo={rest.length === 0}>
      <div class="hero spotlight">
        <MediaCard
          dir={lede.dir}
          name={lede.name}
          key={lede.key}
          idx={0}
          downloaded={downloadedSet.has(join(lede.dir, lede.name))}
        >
          {#snippet overlay()}
            <!-- svelte-ignore a11y_consider_explicit_label -->
            <button
              class="cw-remove p-abs cc o-0 ptr"
              onclick={(e) => onRemove(e, lede.dir, lede.name)}>✕</button
            >
            <div class="cw-track p-abs">
              <div class="cw-fill h-100" style="--p:{done(lede)}%"></div>
            </div>
          {/snippet}

          {#snippet footer()}
            <div class="cw-meta fs-xs f al-ct g5">
              <span class="resume">Resume</span>
              <span>{left(lede)}</span>
              <span class="cw-ago">{ago(lede.at)}</span>
            </div>
          {/snippet}
        </MediaCard>
      </div>

      {#if rest.length > 0}
        <div class="more p-rel">
          <ScrollRow>
            <div class="cards spotlight f flow-x-s g10">
              {#each rest as item, idx (join(item.dir, item.name))}
                <MediaCard
                  dir={item.dir}
                  name={item.name}
                  key={item.key}
                  idx={idx + 1}
                  downloaded={downloadedSet.has(join(item.dir, item.name))}
                >
                  {#snippet overlay()}
                    <!-- svelte-ignore a11y_consider_explicit_label -->
                    <button
                      class="cw-remove p-abs cc o-0 ptr"
                      onclick={(e) => onRemove(e, item.dir, item.name)}
                      >✕</button
                    >
                    <div class="cw-track p-abs">
                      <div class="cw-fill h-100" style="--p:{done(item)}%"></div>
                    </div>
                  {/snippet}

                  {#snippet footer()}
                    <div class="cw-meta fs-xs f al-ct g5">
                      <span>{left(item)}</span>
                      <span class="cw-ago">{ago(item.at)}</span>
                    </div>
                  {/snippet}
                </MediaCard>
              {/each}
            </div>
          </ScrollRow>
        </div>
      {/if}
    </div>
  </section>
{/if}

<style>
  .row {
    margin-bottom: 40px;
  }
  .head {
    padding: 0 48px;
    margin-bottom: 14px;
  }
  h2 {
    color: var(--tx-5);
    font-size: 22px;
    font-weight: 600;
  }
  .count {
    font-variant-numeric: tabular-nums;
  }
  .head::after {
    content: "";
    flex: 1;
    height: 1px;
    background: linear-gradient(to right, var(--glass-bd), transparent);
  }

  .lede {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 20px;
    padding: 0 48px;
    align-items: start;
  }
  .more {
    min-width: 0;
    margin: 0 -48px;
  }
  .cards {
    padding: 14px 48px 18px;
    scrollbar-width: none;
  }
  .cards :global(.tile) {
    width: 190px;
  }

  /* Wide: the resume target holds the left column at hero size (the tile's own
     container query flips it), the rest run as a rail beside it. */
  @media (min-width: 980px) {
    .lede {
      grid-template-columns: minmax(360px, 38%) minmax(0, 1fr);
    }
    .lede.solo {
      grid-template-columns: minmax(360px, 62%);
    }
    .more {
      margin: 0 -48px 0 0;
      --fade-s: 0px;
    }
    .cards {
      padding: 14px 48px 18px 0;
    }
  }

  .cw-remove {
    top: 6px;
    right: 6px;
    width: 26px;
    height: 26px;
    font-size: 12px;
    background: rgb(0 0 0 / 0.55);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    color: var(--tx-4);
    border-radius: 50%;
    border: 1px solid var(--glass-bd);
    z-index: 3;
    transition:
      opacity var(--motion-fast, 0.16s),
      background var(--motion-fast, 0.18s) var(--ease-out),
      color var(--motion-fast, 0.18s) var(--ease-out),
      scale var(--motion-fast, 0.16s) var(--ease-snap);
  }
  :global(.tile):hover .cw-remove,
  :global(.tile):focus-within .cw-remove {
    opacity: 1;
  }
  .cw-remove:hover {
    background: var(--red);
    color: #fff;
    border-color: var(--red);
  }
  .cw-remove:active {
    scale: 0.88;
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

  .cw-track {
    bottom: 0;
    left: 0;
    right: 0;
    height: 3px;
    background: rgb(0 0 0 / 0.45);
    z-index: 2;
  }
  .cw-fill {
    width: var(--p, 100%);
    min-width: 3px;
    background: linear-gradient(
      90deg,
      color-mix(in oklab, var(--art-1) 70%, var(--red)),
      var(--red)
    );
    box-shadow: 0 0 10px var(--red-glow);
    transition: width var(--motion-slow, 0.5s) var(--ease-out);
  }

  .cw-meta {
    color: var(--tx-2);
    padding-top: 2px;
    white-space: nowrap;
  }
  .cw-ago {
    color: var(--tx-1);
    margin-left: auto;
  }
  .resume {
    color: var(--tx-5);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    font-size: 10px;
    padding: 2px 8px;
    border-radius: 999px;
    background: color-mix(in oklab, var(--art-1) 40%, transparent);
    border: 1px solid color-mix(in oklab, var(--art-1) 55%, transparent);
  }

  @media (max-width: 640px) {
    .head {
      padding: 0 16px;
      margin-bottom: 8px;
    }
    h2 {
      font-size: 17px;
    }
    .lede {
      padding: 0 16px;
      gap: 14px;
    }
    .more {
      margin: 0 -16px;
    }
    .cards {
      padding: 4px 16px 12px;
    }
    .cards :global(.tile) {
      width: 148px;
    }
  }
</style>
