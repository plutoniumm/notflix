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
    onPip,
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
    onPip?: () => void;
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
  // barEl IS the drawn bar: its box excludes the track's touch padding, so
  // the fill width, the head offset and the click mapping share one basis.
  let barEl = $state<HTMLDivElement>();
  let dragged = false;

  const pct = $derived(ps.pct);
  const dur = $derived(ps.duration);
  const cur = $derived(ps.currentTime);
  const headPct = $derived(
    s.scrubbing && dur > 0 ? (s.hoverSec / dur) * 100 : pct,
  );

  function track(e: MouseEvent): number {
    if (!barEl || dur <= 0) return 0;
    const r = barEl.getBoundingClientRect();
    const x = Math.max(0, Math.min(r.width, e.clientX - r.left));
    s.hoverX = x;
    s.hoverSec = (x / r.width) * dur;

    return s.hoverSec;
  }

  // Pointer events cover mouse and touch in one path; the mouse-only version
  // left dragging dead on every touch device (a tap seeked via the synthesized
  // compatibility click, a drag did nothing) and never showed the preview
  // tile. Capture keeps the move stream on the track once the drag starts.
  function onDown(e: PointerEvent) {
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
    s.scrubbing = true;
    s.hovering = true;
    dragged = false;
    onSeek(track(e));
  }
  function onMove(e: PointerEvent) {
    s.hovering = true;
    if (s.scrubbing) dragged = true;
    track(e);
  }

  // Two seeks per drag (press + release). Seeking on every move aborts and
  // restarts the in-flight HLS segment fetch dozens of times a second.
  function onUp(e: PointerEvent) {
    if (!s.scrubbing) return;
    if (dragged) onSeek(s.hoverSec);
    s.scrubbing = false;
    dragged = false;
    (e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId);
    if (e.pointerType !== "mouse") s.hovering = false;
  }
  function onLeave() {
    if (!s.scrubbing) s.hovering = false;
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
  const fmtSpeed = (n: number) => parseFloat(n.toFixed(2)).toString();
</script>

{#if dur > 0}
  <div class="dock p-fix" class:hidden={ps.hideUI} style="--pct:{pct}%">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="track ptr"
      onpointerdown={onDown}
      onpointermove={onMove}
      onpointerup={onUp}
      onpointercancel={onUp}
      onpointerleave={onLeave}
    >
      <div class="bar" bind:this={barEl}>
        <div class="bg"></div>
        <div class="fill" style="width:{headPct}%"></div>
        <div class="head" style="left:{headPct}%"></div>

        {#if s.hovering}
          <div class="scrub-line" style="left:{s.hoverX}px"></div>
          <div class="tip" style="left:{s.hoverX}px" class:withtile={!!tile}>
            {#if tile}
              <div class="tile" style={tile}></div>
            {/if}
            <div class="tip-time">{fmtTime(s.hoverSec)}</div>
          </div>
        {/if}
      </div>
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

      {#if onPip}
        <button class="ctrl ptr cc" aria-label="Picture in picture" onclick={onPip}>
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="4" width="20" height="16" rx="2" /><rect x="12" y="11" width="8" height="7" rx="1" fill="currentColor" /></svg>
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
    /* No padding of its own, so the inset can be the padding — and it insets
       the scrub track too, which keeps the track box and its hit-test
       aligned. There is no --safe-l/r token, so those stay raw env(). */
    padding-bottom: var(--safe-b, env(safe-area-inset-bottom));
    padding-left: env(safe-area-inset-left);
    padding-right: env(safe-area-inset-right);
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
    user-select: none;
    touch-action: none;
  }
  .bar {
    position: absolute;
    inset: 0 16px;
  }
  .bg,
  .fill {
    position: absolute;
    top: 50%;
    transform: translateY(-50%);
    left: 0;
    height: 3px;
    border-radius: 99px;
    transition: height 0.18s var(--ease-out);
  }
  .bg { background: rgba(255, 255, 255, 0.18); right: 0; }
  .fill { background: var(--red); }
  .track:hover .bg,
  .track:hover .fill { height: 6px; }

  .head {
    position: absolute;
    top: 50%;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--red);
    transform: translate(-50%, -50%);
    opacity: 0;
    transition: opacity 0.18s var(--ease-out);
    box-shadow: 0 0 6px var(--red-glow);
  }
  .track:hover .head { opacity: 1; }

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
    touch-action: manipulation;
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
