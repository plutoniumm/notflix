<script lang="ts">
  import { onMount } from "svelte";
  import { hashKey } from "../core/video";
  import { blurbs } from "../core/blurbs.svelte";

  // A spoiler-free line about what a title actually is, generated from the
  // dialogue already on disk. Most titles have none — the component renders
  // nothing at all rather than reserving empty space in a grid.
  let { name, lines = 3 }: { name: string; lines?: number } = $props();

  const text = $derived(blurbs.get(hashKey(name)));

  onMount(() => {
    blurbs.load();
  });
</script>

{#if text}
  <p class="blurb fs-xs tx-2" style="--lines: {lines}">{text}</p>
{/if}

<style>
  .blurb {
    margin: 4px 0 0;
    line-height: 1.45;
    letter-spacing: -0.005em;
    text-wrap: pretty;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: var(--lines, 3);
    line-clamp: var(--lines, 3);
    overflow: hidden;
  }
</style>
