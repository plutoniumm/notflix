<script lang="ts">
  import type { WhisperCue } from "./subs";

  let {
    msg,
    cues,
    currentTime,
  }: { msg: string; cues: WhisperCue[]; currentTime: number } = $props();

  const activeCue = $derived(
    cues?.findLast((c) => currentTime >= c.start && currentTime < c.end) ??
      null,
  );
</script>

{#if activeCue}
  <div class="sub-overlay">{activeCue.text}</div>
{/if}

{#if msg}
  <div class="status-msg pl-pane pl-num">
    <span class="dot"></span>
    {msg}
  </div>
{/if}

<style>
  .sub-overlay {
    position: fixed;
    /* Clears the dock and the home indicator; the dock's height is fixed, so
       this can stay a constant. */
    bottom: calc(104px + var(--safe-b, 0px));
    left: 50%;
    transform: translateX(-50%);
    z-index: 10;
    max-width: min(80%, 46ch);
    text-align: center;
    text-wrap: balance;
    font-family: var(--font-display);
    font-size: clamp(1.05rem, 2.4vw, 1.5rem);
    font-weight: 500;
    line-height: 1.32;
    letter-spacing: -0.014em;
    color: #fff;
    text-shadow:
      0 0 4px #000,
      0 1px 3px #000,
      1px 0 3px #000,
      -1px 0 3px #000;
    pointer-events: none;
  }

  .status-msg {
    position: fixed;
    bottom: calc(104px + var(--safe-b, 0px));
    left: calc(32px + env(safe-area-inset-left));
    z-index: 10;
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 8px 14px;
    border-radius: var(--r-xl);
    font-size: 12px;
    color: var(--tx-4);
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--red);
    box-shadow: 0 0 8px var(--red-glow);
    animation: breathe 1.6s var(--ease-out) infinite;
  }
  @keyframes breathe {
    0%, 100% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.35; transform: scale(0.7); }
  }
</style>
