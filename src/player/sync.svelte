<script lang="ts">
  let { ms, visible }: { ms: number; visible: boolean } = $props();
</script>

<div class="pill p-fix f-row al-ct g10 p10 rx20 o-0" class:visible>
  <span class="lbl">A/V</span>
  <span class="val">{ms > 0 ? "+" : ""}{ms}ms</span>
</div>

<style>
  .pill {
    right: 20px;
    top: 50%;
    transform: translateY(-50%);
    z-index: 10;
    background: #0009;
    backdrop-filter: blur(6px);
    color: var(--tx-5);
    pointer-events: none;
    white-space: nowrap;
    transition: opacity 0.5s ease;
  }
  .pill.visible {
    opacity: 1;
    transition: opacity 0s;
  }
  .lbl {
    opacity: 0.5;
    font-size: 10px;
    letter-spacing: 0.05em;
  }
  .val {
    font-size: 13px;
    font-variant-numeric: tabular-nums;
  }
</style>
