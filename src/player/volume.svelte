<script lang="ts">
  let { level, visible }: any = $props();
</script>

<div class="pill p-fix f-col al-ct g10 p10 rx20 o-0" class:visible>
  <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
    <path
      d="M3 9v6h4l5 5V4L7 9H3zm13.5 3c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z"
    />
  </svg>

  <div class="track rx2 flow-h p-rel">
    <div class="fill p-fix rx2" style="height:{level * 100}%"></div>
  </div>
</div>

<style>
  .pill {
    left: 20px;
    top: 50%;
    transform: translateY(-50%);
    z-index: 10;
    background: #0009;
    backdrop-filter: blur(6px);
    color: var(--tx-5);
    pointer-events: none;
    transition: opacity 0.5s ease;
  }
  .pill.visible {
    opacity: 1;
    transition: opacity 0s;
  }

  .track {
    width: 4px;
    height: 80px;
    background: #fff4;
  }
  .fill {
    bottom: 0;
    left: 0;
    right: 0;
    background: var(--tx-5);
    transition: height 0.1s;
  }
</style>
