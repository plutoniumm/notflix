<script lang="ts">
  import { join } from "../core/path";
  import MediaCard from "../components/MediaCard.svelte";
  import ScrollRow from "../components/ScrollRow.svelte";
  import { ordinals } from "./series";
  import type { ContinueItem } from "./homeData.svelte";

  let {
    dir,
    files,
    continues = [],
    downloadedSet,
  }: {
    dir: string;
    files: VideoEntry[];
    continues?: ContinueItem[];
    downloadedSet: Set<string>;
  } = $props();

  const marks = $derived(ordinals(files.map((f) => f.name)));

  // The rail's one piece of live state. Newest watch wins when several
  // episodes of the same set are part-watched.
  const resume = $derived.by(() => {
    let name = "";
    let at = -1;
    for (const c of continues)
      if (c.dir === dir && c.at > at && files.some((f) => f.name === c.name)) {
        name = c.name;
        at = c.at;
      }

    return name;
  });
</script>

<ScrollRow>
  <div class="spine flow-x-s f g10">
    {#each files as f, idx (f.name)}
      <div class="ep f-col" class:here={f.name === resume}>
        <MediaCard
          {dir}
          name={f.name}
          key={f.key}
          {idx}
          downloaded={downloadedSet.has(join(dir, f.name))}
        />
        <!-- Flex, not a line box: the rail is centred on .mark, and a baseline
             would leave descender space the ordinal's mask does not cover. -->
        <div class="mark p-rel f al-ct">
          <span class="node"></span>
          <span class="ord fs-xs">{marks[idx]}</span>
        </div>
      </div>
    {/each}
  </div>
</ScrollRow>

<style>
  .spine {
    padding: 14px 48px 20px;
    scrollbar-width: none;
    transition:
      opacity var(--motion, 0.26s) var(--ease-out),
      translate var(--motion, 0.26s) var(--ease-out);
  }

  /* ShowRow swaps the strip outright on a season change, so it animates from
     @starting-style — a keyframe would have to be re-triggered. */
  @starting-style {
    .spine {
      opacity: 0;
      translate: 14px 0;
    }
  }

  .ep {
    width: 220px;
    flex-shrink: 0;
    transition:
      opacity var(--motion, 0.3s) var(--ease-out),
      filter var(--motion, 0.3s) var(--ease-out);
  }
  .ep :global(.tile) {
    width: 100%;
  }

  /* One rail, drawn per card and bled half a gap each way so the segments butt
     into a continuous line; the ends stop flush with the strip's padding. */
  .mark::before {
    content: "";
    position: absolute;
    left: -5px;
    right: -5px;
    top: 50%;
    height: 2px;
    border-radius: 2px;
    background: color-mix(in oklab, var(--red) 34%, transparent);
  }

  /* A stop at every episode: a bare line is a divider, a line with nodes on it
     is a sequence you move along. */
  .node {
    position: relative;
    width: 7px;
    height: 7px;
    margin-right: 7px;
    border-radius: 50%;
    flex: none;
    background: color-mix(in oklab, var(--red) 62%, var(--bg-2));
    box-shadow: 0 0 0 3px var(--bg-2);
  }
  .here .node {
    background: var(--red);
    box-shadow:
      0 0 0 3px var(--bg-2),
      0 0 12px var(--red-glow);
  }
  .ep:first-child .mark::before {
    left: 0;
  }
  .ep:last-child .mark::before {
    right: 0;
  }

  /* Opaque, so the rail reads as threading behind the ordinals. */
  .ord {
    position: relative;
    padding: 2px 8px 2px 0;
    background: var(--bg-2);
    color: var(--tx-4);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    letter-spacing: 0.1em;
  }

  .here .ord {
    padding-left: 8px;
    margin-left: -8px;
    border-radius: 999px;
    color: var(--tx-5);
    background: color-mix(in oklab, var(--red) 78%, transparent);
    box-shadow: 0 2px 10px -2px var(--red-glow);
  }

  /* MediaCard's own .spotlight dim keys on a direct `> .tile`, which the
     ordinal wrapper breaks; same effect, one level out. */
  @media (hover: hover) {
    .spine:has(.ep:hover) .ep:not(:hover) {
      opacity: 0.55;
      filter: saturate(0.5) brightness(0.78);
    }
  }

  @media (max-width: 640px) {
    .spine {
      padding: 12px 16px 16px;
    }
    .ep {
      width: 148px;
    }
  }

  
</style>
