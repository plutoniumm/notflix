<script lang="ts" module>
  export function activeSkip(
    t: number,
    intro: [number, number] | null,
    outro: [number, number] | null,
  ): "intro" | "outro" | null {
    if (intro && t >= intro[0] && t < intro[1] - 1) return "intro";
    if (outro && t >= outro[0] && t < outro[1] - 1) return "outro";
    return null;
  }
</script>

<script lang="ts">
  import type { PlayerState } from "../core/events.svelte";

  let {
    ps,
    intro,
    outro,
    onSeek,
    onNext,
  }: {
    ps: PlayerState;
    intro: [number, number] | null;
    outro: [number, number] | null;
    onSeek: (t: number) => void;
    onNext: () => void;
  } = $props();

  const which = $derived(activeSkip(ps.currentTime, intro, outro));
  const inIntro = $derived(which === "intro");
  const inOutro = $derived(which === "outro");
</script>

{#if inIntro}
  <button class="skip rx5 ptr" onclick={() => onSeek(intro![1])}>
    Skip intro
    <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor"><path d="M5 4l12 8L5 20zM19 4h2v16h-2z" /></svg>
  </button>
{:else if inOutro}
  {#if ps.nextURL}
    <button class="skip rx5 ptr" onclick={onNext}>
      Next episode
      <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor"><path d="M5 4l12 8L5 20zM19 4h2v16h-2z" /></svg>
    </button>
  {:else}
    <button class="skip rx5 ptr" onclick={() => onSeek(outro![1])}>
      Skip credits
      <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor"><path d="M5 4l12 8L5 20zM19 4h2v16h-2z" /></svg>
    </button>
  {/if}
{/if}

<style>
  .skip {
    position: fixed;
    right: 28px;
    bottom: 110px;
    z-index: 12;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 18px;
    background: rgba(7, 6, 10, 0.85);
    border: 1px solid var(--glass-bd);
    color: var(--tx-5);
    font-family: var(--font-display);
    font-size: 14px;
    font-weight: 500;
    letter-spacing: -0.01em;
    backdrop-filter: blur(10px) saturate(140%);
    -webkit-backdrop-filter: blur(10px) saturate(140%);
    box-shadow: var(--sh-2);
    animation: skip-in 0.25s var(--ease-out);
    transition: background 0.18s var(--ease-out), transform 0.16s var(--ease-snap);
  }
  .skip:hover {
    background: var(--red);
  }
  .skip:active {
    transform: scale(0.95);
  }

  @keyframes skip-in {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
</style>
