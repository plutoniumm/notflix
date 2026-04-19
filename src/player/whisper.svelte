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
  <div class="p-fix sub-overlay">{activeCue.text}</div>
{/if}

{#if msg}
  <div class="p-fix p10 fs red rx5 status-msg">{msg}</div>
{/if}

<style>
  .sub-overlay {
    bottom: 80px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 10;
    max-width: 80%;
    text-align: center;
    font-size: 1.4rem;
    color: #fff;
    text-shadow:
      0 0 4px #000,
      0 1px 3px #000,
      1px 0 3px #000,
      -1px 0 3px #000;
    pointer-events: none;
  }

  .status-msg {
    bottom: 80px;
    left: 32px;
    z-index: 10;
    background: #0009;
  }
</style>
