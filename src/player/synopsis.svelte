<script lang="ts">
  import { onMount } from "svelte";
  import { hashKey } from "../core/video";
  import { blurbs } from "../core/blurbs.svelte";

  // "What is this again?" — answered while paused, never over moving picture.
  // The up-next rail owns the left edge, so this sits opposite it.
  let {
    name,
    title,
    paused,
    embed = false,
  }: { name: string; title: string; paused: boolean; embed?: boolean } = $props();

  const text = $derived(blurbs.get(hashKey(name)));

  onMount(() => {
    blurbs.load();
  });
</script>

{#if !embed && text}
  <aside class="synopsis pl-pane pl-enter rx5 p10" class:out={!paused}>
    <div class="pl-lbl">SYNOPSIS</div>
    <div class="head trunc">{title}</div>
    <p class="body">{text}</p>
  </aside>
{/if}

<style>
  .synopsis {
    position: fixed;
    top: 64px;
    right: calc(12px + env(safe-area-inset-right));
    z-index: 9;
    width: min(340px, 42vw);
    border-radius: var(--r-lg);
  }

  .synopsis.out {
    opacity: 0;
    transform: translateY(-8px);
    pointer-events: none;
  }

  @starting-style {
    .synopsis {
      opacity: 0;
      transform: translateY(-8px);
    }
  }

  .head {
    font-family: var(--font-display);
    font-size: 13px;
    font-weight: 500;
    color: var(--tx-5);
    margin-top: 2px;
  }

  .body {
    margin: 6px 0 0;
    font-size: 12px;
    line-height: 1.5;
    color: var(--tx-3);
    text-wrap: pretty;
  }

  /* Phones give the picture the whole screen; a floating panel there covers
     the frame the user paused on. */
  @media (max-width: 700px) {
    .synopsis {
      display: none;
    }
  }
</style>
