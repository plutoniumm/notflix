<script lang="ts">
  import { onMount } from "svelte";
  import { hashKey } from "../core/video";
  import { blurbs } from "../core/blurbs.svelte";
  import { parseName, epTag } from "../library/torrentName";

  // "What is this again?" — answered while paused, never over moving picture.
  // Netflix's pause screen: the show at full size, the episode small beneath.
  let {
    name,
    title,
    paused,
    embed = false,
  }: { name: string; title: string; paused: boolean; embed?: boolean } = $props();

  const text = $derived(blurbs.get(hashKey(name)));
  const parsed = $derived(parseName(name));
  const show = $derived(parsed.title || title);
  const tag = $derived(epTag(parsed));

  onMount(() => {
    blurbs.load();
  });
</script>

{#if !embed}
  <aside class="synopsis pl-enter" class:out={!paused}>
    <h1 class="ttl">{show}</h1>
    {#if tag}<div class="tag">{tag}</div>{/if}
    {#if text}<p class="body">{text}</p>{/if}
  </aside>
{/if}

<style>
  /* Full height so the scrim behind the words reaches the frame's edges;
     pointer-events off so a click still lands on the video. */
  .synopsis {
    position: fixed;
    inset: 0 auto 0 0;
    z-index: 3;
    width: min(62vw, 900px);
    display: flex;
    flex-direction: column;
    justify-content: center;
    padding: 0 0 6vh calc(72px + env(safe-area-inset-left));
    pointer-events: none;
    background: linear-gradient(
      to right,
      rgba(3, 2, 6, 0.78) 0%,
      rgba(3, 2, 6, 0.42) 55%,
      transparent 100%
    );
    /* A pause to look at the frame shouldn't be covered at once; the title
       waits, and leaves the instant play resumes. */
    transition-delay: 1.4s;
  }

  .synopsis.out,
  :global(.pl:has(.rail.open)) .synopsis {
    opacity: 0;
    transform: translateX(-12px);
    transition-delay: 0s;
  }

  .ttl {
    margin: 0;
    max-width: min(16ch, calc(50vw - 136px));
    font-family: var(--font-display);
    font-size: clamp(34px, 5.2vw, 84px);
    line-height: 0.95;
    letter-spacing: -0.035em;
    font-weight: 700;
    text-wrap: balance;
    color: var(--tx-5);
    text-shadow: 0 2px 24px rgba(0, 0, 0, 0.5);
  }

  .tag {
    align-self: start;
    margin-top: 14px;
    font-variant-numeric: tabular-nums;
    letter-spacing: 0.1em;
    font-size: 12px;
    font-weight: 700;
    padding: 3px 9px;
    border-radius: 6px;
    color: var(--tx-4);
    background: color-mix(in oklab, var(--tx-5) 11%, transparent);
  }

  .body {
    margin: 16px 0 0;
    max-width: min(46ch, calc(50vw - 136px));
    font-size: clamp(13px, 1.05vw, 15px);
    line-height: 1.55;
    color: var(--tx-3);
    text-wrap: pretty;
  }

  /* Phones give the picture the whole screen; the play button sits mid-frame
     and the words would land on it. */
  @media (max-width: 700px) {
    .synopsis {
      display: none;
    }
  }
</style>
