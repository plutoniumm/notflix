<script lang="ts">
  let { volLevel, volVisible }: {
    volLevel: number;
    volVisible: boolean;
  } = $props();
</script>

<div class="vol-pill" class:visible={volVisible}>
  <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
    {#if volLevel === 0}
      <path d="M16.5 12c0-1.77-1.02-3.29-2.5-4.03v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51C20.63 14.91 21 13.5 21 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3L3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06c1.38-.31 2.63-.95 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z"/>
    {:else if volLevel < 0.5}
      <path d="M18.5 12c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM5 9v6h4l5 5V4L9 9H5z"/>
    {:else}
      <path d="M3 9v6h4l5 5V4L7 9H3zm13.5 3c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z"/>
    {/if}
  </svg>

  <div class="vol-track">
    <div class="vol-fill" style="height:{volLevel * 100}%"></div>
  </div>
</div>

<style>
  .vol-pill {
    position: fixed;
    left: 20px;
    top: 50%;
    transform: translateY(-50%);
    z-index: 10;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 10px 10px 12px;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(6px);
    color: rgba(255, 255, 255, 0.9);
    border-radius: 20px;
    pointer-events: none;
    opacity: 0;
    transition: opacity 0.5s ease;
  }
  .vol-pill.visible {
    opacity: 1;
    transition: opacity 0s;
  }

  .vol-track {
    width: 4px;
    height: 80px;
    background: rgba(255, 255, 255, 0.2);
    border-radius: 2px;
    position: relative;
    overflow: hidden;
  }
  .vol-fill {
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    background: #fff;
    border-radius: 2px;
    transition: height 0.1s;
  }
</style>
