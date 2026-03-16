<script lang="ts">
  import { fmtTime } from "../lib/events.svelte";

  let {
    pct,
    duration,
    hideUI,
  }: {
    pct: number;
    duration: number;
    hideUI: boolean;
  } = $props();
</script>

{#if duration > 0}
  <div class="pill p-fix rx20" class:hidden={hideUI} style="--pct:{pct}%">
    {pct}% · {fmtTime(duration)}
  </div>
{/if}

<style>
  .pill {
    bottom: 20px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 10;
    background: linear-gradient(to right, #f004 var(--pct), #0008 var(--pct));
    backdrop-filter: blur(8px);
    color: #fffd;
    font-variant-numeric: tabular-nums;
    padding: 4px 12px;
    white-space: nowrap;
    pointer-events: none;
    transition: opacity 0.3s;
  }

  .pill.hidden {
    opacity: 0;
  }
</style>
