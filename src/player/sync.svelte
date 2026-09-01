<script lang="ts">
  let { ms, visible }: { ms: number; visible: boolean } = $props();
</script>

<div class="pill pl-pane f-row al-ct g10 p10 o-0" class:visible>
  <span class="lbl pl-lbl">A/V</span>
  <span class="val pl-num">{ms > 0 ? "+" : ""}{ms}ms</span>
</div>

<style>
  .pill {
    position: fixed;
    right: calc(20px + env(safe-area-inset-right));
    top: 50%;
    transform: translateY(-50%);
    z-index: 10;
    border-radius: var(--r-xl);
    padding-inline: 14px;
    color: var(--tx-5);
    pointer-events: none;
    white-space: nowrap;
    transition: opacity var(--m-slow, 520ms) var(--ease-out);
  }
  .pill.visible {
    opacity: 1;
    transition: opacity 0s;
  }
  .val {
    font-size: 13px;
    color: var(--tx-5);
  }
</style>
