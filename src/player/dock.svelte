<script lang="ts">
  import { fmtTime } from "../core/events.svelte";
  import type { PlayerState } from "../core/events.svelte";

  let {
    ps,
    title,
    speed = 1,
    onSeek,
    onPlayPause,
    onSpeedDown,
    onSpeedUp,
    onNext,
    onFullscreen,
    trickplay,
  }: {
    ps: PlayerState;
    title: string;
    speed?: number;
    onSeek: (sec: number) => void;
    onPlayPause: () => void;
    onSpeedDown: () => void;
    onSpeedUp: () => void;
    onNext?: () => void;
    onFullscreen: () => void;
    // Renderer for the trickplay preview tile. Receives the hovered
    // timestamp in seconds, returns inline CSS for a 160-wide tile (or
    // empty string when no sprite is available).
    trickplay?: (sec: number) => string;
  } = $props();

  class Scrub {
    hoverSec = $state(0);
    hoverX = $state(0);
    hovering = $state(false);
    scrubbing = $state(false);
    flipDir = $state(0);
  }
  const s = new Scrub();
  let trackEl = $state<HTMLDivElement>();

  const pct = $derived(ps.pct);
  const dur = $derived(ps.duration);
  const cur = $derived(ps.currentTime);

  function xToSec(clientX: number): number {
    if (!trackEl || dur <= 0) return 0;
    const rect = trackEl.getBoundingClientRect();
    const f = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    return f * dur;
  }

  function onMove(e: MouseEvent) {
    if (!trackEl) return;
    s.hovering = true;
    const rect = trackEl.getBoundingClientRect();
    s.hoverX = Math.max(0, Math.min(rect.width, e.clientX - rect.left));
    s.hoverSec = xToSec(e.clientX);
    if (s.scrubbing) onSeek(s.hoverSec);
  }
  function onLeave() {
    s.hovering = false;
  }
  function onDown(e: MouseEvent) {
    s.scrubbing = true;
    onSeek(xToSec(e.clientX));
    window.addEventListener("mousemove", onWinMove);
    window.addEventListener("mouseup", onUp);
  }
  function onWinMove(e: MouseEvent) {
    if (!s.scrubbing) return;
    onMove(e);
  }
  function onUp() {
    s.scrubbing = false;
    window.removeEventListener("mousemove", onWinMove);
    window.removeEventListener("mouseup", onUp);
  }

  function speedWheel(e: WheelEvent) {
    e.preventDefault();
    if (e.deltaY < 0) {
      onSpeedDown();
      s.flipDir = -1;
    } else if (e.deltaY > 0) {
      onSpeedUp();
      s.flipDir = 1;
    }
    setTimeout(() => (s.flipDir = 0), 250);
  }

  const tile = $derived(
    trickplay && s.hovering && dur > 0 ? trickplay(s.hoverSec) : "",
  );
  const tipPct = $derived(
    trackEl ? (s.hoverX / trackEl.getBoundingClientRect().width) * 100 : 0,
  );
  const fmtSpeed = (n: number) => parseFloat(n.toFixed(2)).toString();
</script>

{#if dur > 0}
  <div class="dock p-fix" class:hidden={ps.hideUI} style="--pct:{pct}%">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="track"
      bind:this={trackEl}
      onmousemove={onMove}
      onmouseleave={onLeave}
      onmousedown={onDown}
    >
      <div class="bar-bg"></div>
      <div class="bar-fill" style="width:{pct}%"></div>
      <div class="bar-head" style="left:{pct}%"></div>

      {#if s.hovering}
        <div class="scrub-line" style="left:{s.hoverX}px"></div>
        <div
          class="tip"
          style="left:{tipPct}%"
          class:withtile={!!tile}
        >
          {#if tile}
            <div class="tile" style={tile}></div>
          {/if}
          <div class="tip-time">{fmtTime(s.hoverSec)}</div>
        </div>
      {/if}
    </div>

    <div class="row f al-ct g10 p20">
      <button
        class="ctrl ptr cc"
        aria-label={ps.paused ? "Play" : "Pause"}
        onclick={onPlayPause}
      >
        {#if ps.paused}
          <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor"><path d="M6 4l14 8-14 8z" /></svg>
        {:else}
          <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor"><path d="M6 4h4v16H6zM14 4h4v16h-4z" /></svg>
        {/if}
      </button>

      <div class="time fs-num tx-4 sh-0">
        {fmtTime(cur)} <span class="tx-2">/ {fmtTime(dur)}</span>
      </div>

      <div class="title trunc fw5 tx-5">{title}</div>

      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="speed-chip cc rx5 ptr sh-0"
        title="Scroll to change speed"
        onwheel={speedWheel}
        class:flip-up={s.flipDir === 1}
        class:flip-down={s.flipDir === -1}
      >
        {fmtSpeed(speed)}×
      </div>

      {#if onNext}
        <button class="ctrl ptr cc" aria-label="Next" onclick={onNext}>
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor"><path d="M5 4l12 8L5 20zM19 4h2v16h-2z" /></svg>
        </button>
      {/if}

      <button class="ctrl ptr cc" aria-label="Fullscreen" onclick={onFullscreen}>
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" /></svg>
      </button>
    </div>
  </div>
{/if}

<style>
  .dock {
    bottom: 0;
    left: 0;
    right: 0;
    z-index: 10;
    background: linear-gradient(
      to top,
      rgba(7, 6, 10, 0.85) 0%,
      rgba(7, 6, 10, 0.55) 60%,
      transparent 100%
    );
    backdrop-filter: blur(10px) saturate(140%);
    -webkit-backdrop-filter: blur(10px) saturate(140%);
    color: var(--tx-5);
    transition: opacity 0.3s var(--ease-out), transform 0.3s var(--ease-out);
    animation: slide-up 0.25s var(--ease-out);
  }
  .dock.hidden {
    opacity: 0;
    transform: translateY(20%);
    pointer-events: none;
  }

  .track {
    position: relative;
    height: 18px;
    padding: 0 16px;
    cursor: pointer;
    user-select: none;
  }
  .bar-bg,
  .bar-fill {
    position: absolute;
    top: 50%;
    transform: translateY(-50%);
    left: 16px;
    right: 16px;
    height: 3px;
    border-radius: 99px;
    transition: height 0.18s var(--ease-out);
  }
  .bar-bg { background: rgba(255, 255, 255, 0.18); }
  .bar-fill { background: var(--red); right: auto; }
  .track:hover .bar-bg,
  .track:hover .bar-fill { height: 6px; }

  .bar-head {
    position: absolute;
    top: 50%;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--red);
    transform: translate(calc(-50% + 16px), -50%);
    opacity: 0;
    transition: opacity 0.18s var(--ease-out);
    box-shadow: 0 0 6px var(--red-glow);
  }
  .track:hover .bar-head { opacity: 1; }

  .scrub-line {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 1px;
    background: rgba(255, 255, 255, 0.4);
    pointer-events: none;
  }

  .tip {
    position: absolute;
    bottom: 24px;
    transform: translateX(-50%);
    background: rgba(7, 6, 10, 0.92);
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-sm);
    padding: 4px 8px;
    font-family: var(--font-display);
    font-variant-numeric: tabular-nums;
    font-size: 12px;
    pointer-events: none;
    box-shadow: var(--sh-2);
    white-space: nowrap;
  }
  .tip.withtile { padding: 4px; }
  .tile {
    border-radius: 4px;
    background-color: #000;
    background-repeat: no-repeat;
    margin-bottom: 4px;
  }
  .tip-time { text-align: center; color: var(--tx-4); }

  .row { padding-top: 4px; }

  .ctrl {
    background: transparent;
    color: var(--tx-4);
    border: none;
    border-radius: var(--r-md);
    min-width: 36px;
    min-height: 36px;
    transition: color 0.18s var(--ease-out), background 0.18s var(--ease-out),
      transform 0.16s var(--ease-snap);
  }
  .ctrl:hover { color: var(--tx-5); background: var(--glass); }
  .ctrl:active { transform: scale(0.92); }

  .time {
    font-family: var(--font-display);
    font-variant-numeric: tabular-nums;
    font-size: 13px;
    letter-spacing: -0.01em;
  }
  .fs-num { font-variant-numeric: tabular-nums; }

  .title {
    flex: 1;
    font-family: var(--font-display);
    font-size: 14px;
    letter-spacing: -0.012em;
    color: var(--tx-5);
  }

  .speed-chip {
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    color: var(--tx-4);
    padding: 4px 10px;
    font-family: var(--font-display);
    font-variant-numeric: tabular-nums;
    font-size: 12px;
    font-weight: 500;
    perspective: 600px;
    transition: background 0.18s var(--ease-out);
  }
  .speed-chip:hover { background: var(--glass-2); color: var(--tx-5); }
  .speed-chip.flip-up { animation: flip-u 0.25s ease; }
  .speed-chip.flip-down { animation: flip-d 0.25s ease; }
  @keyframes flip-u {
    0%, 100% { transform: rotateX(0); }
    50% { transform: rotateX(-90deg); }
  }
  @keyframes flip-d {
    0%, 100% { transform: rotateX(0); }
    50% { transform: rotateX(90deg); }
  }

  @keyframes slide-up {
    from { opacity: 0; transform: translateY(20%); }
    to   { opacity: 1; transform: translateY(0); }
  }
</style>
