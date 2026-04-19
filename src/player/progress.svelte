<script lang="ts">
  import { fmtTime } from "../core/events.svelte";

  let {
    pct,
    currentTime = 0,
    duration,
    hidden,
    speed = 1,
    paused = true,
    onSpeedDown,
    onSpeedUp,
    onPlayPause,
    onNext,
  }: {
    pct: number;
    currentTime?: number;
    duration: number;
    hidden: boolean;
    speed?: number;
    paused?: boolean;
    onSpeedDown?: () => void;
    onSpeedUp?: () => void;
    onPlayPause?: () => void;
    onNext?: () => void;
  } = $props();

  let flip = $state(0);
  let touchY = 0;

  function speedUp() {
    onSpeedUp?.();
    animate(1);
  }
  function speedDown() {
    onSpeedDown?.();
    animate(-1);
  }

  function animate(dir: number) {
    flip = dir;
    setTimeout(() => (flip = 0), 250);
  }

  function onWheel(e: WheelEvent) {
    e.preventDefault();
    if (e.deltaY < 0) speedDown();
    else if (e.deltaY > 0) speedUp();
  }

  function onTouchStart(e: TouchEvent) {
    touchY = e.touches[0].clientY;
  }

  function onTouchEnd(e: TouchEvent) {
    const dy = e.changedTouches[0].clientY - touchY;
    if (dy < -25) speedDown();
    else if (dy > 25) speedUp();
  }

  const fmtSpeed = (s: number) => parseFloat(s.toFixed(2)).toString();
</script>

{#if duration > 0}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="pill p-fix rx20"
    class:hidden
    style="--pct:{pct}%"
    onwheel={onWheel}
    ontouchstart={onTouchStart}
    ontouchend={onTouchEnd}
  >
    <div class="inner" class:flip-up={flip === 1} class:flip-down={flip === -1}>
      {pct}% · {fmtTime(duration / speed)} · {fmtSpeed(speed)}x
    </div>
    {#if onNext}
      <button class="ctrl next" onclick={onNext}>⏭</button>
    {/if}
  </div>
{/if}

<style>
  .pill {
    bottom: 20px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 10;
    background: linear-gradient(to right, #fff6 var(--pct), #0008 var(--pct));
    backdrop-filter: blur(8px);
    color: #fffd;
    font-variant-numeric: tabular-nums;
    font-size: 15px;
    padding: 8px 18px;
    white-space: nowrap;
    pointer-events: auto;
    transition: opacity 0.3s;
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: ns-resize;
    overflow: hidden;
    perspective: 800px;
  }

  .pill.hidden {
    opacity: 0;
    pointer-events: none;
  }

  .inner {
    pointer-events: none;
  }

  .inner.flip-up {
    animation: roll-up 0.25s ease;
  }
  .inner.flip-down {
    animation: roll-down 0.25s ease;
  }

  @keyframes roll-up {
    0% {
      transform: rotateX(0);
      opacity: 1;
    }
    40% {
      transform: rotateX(-90deg);
      opacity: 0;
    }
    60% {
      transform: rotateX(90deg);
      opacity: 0;
    }
    100% {
      transform: rotateX(0);
      opacity: 1;
    }
  }
  @keyframes roll-down {
    0% {
      transform: rotateX(0);
      opacity: 1;
    }
    40% {
      transform: rotateX(90deg);
      opacity: 0;
    }
    60% {
      transform: rotateX(-90deg);
      opacity: 0;
    }
    100% {
      transform: rotateX(0);
      opacity: 1;
    }
  }

  .ctrl {
    background: none;
    border: none;
    color: #fffa;
    font-size: 16px;
    padding: 4px 6px;
    line-height: 1;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
  }
  .ctrl:active {
    color: #fff;
  }
</style>
