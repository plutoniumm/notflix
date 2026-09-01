<script lang="ts">
  import { fmtTime } from "../core/events.svelte";
  import type { PlayerState } from "../core/events.svelte";

  let {
    ps,
    title,
    speed = 1,
    buffered = [],
    intro = null,
    outro = null,
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
    // Loaded ranges, in seconds, straight off the tech's TimeRanges.
    buffered?: [number, number][];
    intro?: [number, number] | null;
    outro?: [number, number] | null;
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

  const dur = $derived(ps.duration);
  const cur = $derived(ps.currentTime);
  // ps.pct is rounded to whole percent, which on a two-hour film is a 72s
  // stair-step. The hero control gets the unrounded figure.
  const pct = $derived(dur > 0 ? (cur / dur) * 100 : 0);
  const headPct = $derived(
    s.scrubbing && dur > 0 ? (s.hoverSec / dur) * 100 : pct,
  );
  const left = $derived(dur > 0 ? Math.max(0, dur - cur) : 0);

  const span = (r: [number, number]) => ({
    a: (Math.max(0, r[0]) / dur) * 100,
    w: (Math.min(dur, r[1] - r[0]) / dur) * 100,
  });
  const bufs = $derived(dur > 0 ? buffered.map(span) : []);
  const marks = $derived(
    dur > 0
      ? [
          { kind: "intro", r: intro },
          { kind: "outro", r: outro },
        ]
          .filter((m) => !!m.r)
          .map((m) => ({ kind: m.kind, ...span(m.r as [number, number]) }))
      : [],
  );
  const hoverPct = $derived(dur > 0 ? (s.hoverSec / dur) * 100 : 0);
  const overMark = $derived(
    s.hovering
      ? marks.find((m) => hoverPct >= m.a && hoverPct <= m.a + m.w)
      : undefined,
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
  <div
    class="dock p-fix"
    class:hidden={ps.hideUI}
    style="--pct:{headPct}%;--hx:{s.hoverX}px"
  >
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="track ptr"
      class:live={s.scrubbing}
      onpointerdown={onDown}
      onpointermove={onMove}
      onpointerup={onUp}
      onpointercancel={onUp}
      onpointerleave={onLeave}
    >
      <div class="bar" bind:this={barEl}>
        <div class="bg"></div>

        {#each bufs as b}
          <div class="buf" style="left:{b.a}%;width:{b.w}%"></div>
        {/each}

        {#each marks as m}
          <div class="mark {m.kind}" style="left:{m.a}%;width:{m.w}%"></div>
        {/each}

        <div class="fill"></div>
        <div class="head"></div>

        {#if s.hovering}
          <div class="scrub-line"></div>
          <div class="tip" class:withtile={!!tile}>
            {#if tile}
              <div class="tile" style={tile}></div>
            {/if}
            <div class="tip-time pl-num">
              {fmtTime(s.hoverSec)}
              {#if overMark}
                <span class="tip-mark"
                  >{overMark.kind === "intro" ? "Intro" : "Credits"}</span
                >
              {/if}
            </div>
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

      <div class="time pl-num sh-0" title="{fmtTime(left)} left">
        <span class="now">{fmtTime(cur)}</span>
        <span class="sep">/</span>
        <span class="tot">{fmtTime(dur)}</span>
      </div>

      <div class="title trunc fw5 tx-5">{title}</div>

      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="speed-chip pl-num cc rx5 ptr sh-0"
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
      rgba(3, 2, 6, 0.88) 0%,
      rgba(3, 2, 6, 0.5) 55%,
      transparent 100%
    );
    backdrop-filter: blur(14px) saturate(150%);
    -webkit-backdrop-filter: blur(14px) saturate(150%);
    color: var(--tx-5);
    transition:
      opacity var(--m, 260ms) var(--ease-out),
      transform var(--m, 260ms) var(--ease-out);
  }
  @starting-style {
    .dock {
      opacity: 0;
      transform: translateY(24%);
    }
  }
  .dock.hidden {
    opacity: 0;
    transform: translateY(20%);
    pointer-events: none;
  }

  .track {
    position: relative;
    height: 20px;
    padding: 0 16px;
    user-select: none;
    touch-action: none;
  }
  /* Vertical only — the horizontal padding is the shared measurement basis
     and must stay identical to .bar's inset. */
  @media (hover: none) {
    .track { height: 30px; }
  }
  .bar {
    position: absolute;
    inset: 0 16px;
  }
  .bg,
  .buf,
  .mark,
  .fill {
    position: absolute;
    top: 50%;
    height: var(--h, 3px);
    transform: translateY(-50%);
    border-radius: 99px;
    transition: height var(--m-fast, 130ms) var(--ease-out);
  }
  .bg { background: rgba(255, 255, 255, 0.16); left: 0; right: 0; }
  .buf { background: rgba(255, 255, 255, 0.3); }
  .fill {
    left: 0;
    width: var(--pct);
    background: linear-gradient(
      90deg,
      color-mix(in oklab, var(--red) 78%, var(--art-1)) 0%,
      var(--red) 100%
    );
    box-shadow: 0 0 10px -1px var(--red-glow);
  }

  /* Detected intro/credits, legible in the timeline rather than only as a
     button that appears out of nowhere. */
  .mark {
    background: repeating-linear-gradient(
      -45deg,
      rgba(255, 255, 255, 0.34) 0 3px,
      transparent 3px 6px
    );
  }

  .track:hover,
  .track.live { --h: 7px; }
  .track:hover .head,
  .track.live .head { opacity: 1; transform: translate(-50%, -50%) scale(1); }

  .head {
    position: absolute;
    top: 50%;
    left: var(--pct);
    width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--red);
    transform: translate(-50%, -50%) scale(0.4);
    opacity: 0;
    transition:
      opacity var(--m-fast, 130ms) var(--ease-out),
      transform var(--m-fast, 130ms) var(--ease-snap);
    box-shadow:
      0 0 0 3px rgba(3, 2, 6, 0.55),
      0 0 14px var(--red-glow);
  }

  .scrub-line {
    position: absolute;
    left: var(--hx);
    top: 0;
    bottom: 0;
    width: 1px;
    background: rgba(255, 255, 255, 0.35);
    pointer-events: none;
  }

  /* Deliberately not .pnl: the tip tracks the pointer, and a backdrop-filter
     that moves re-blurs its backdrop every frame. Opaque is cheaper and, over
     a preview tile, indistinguishable. */
  .tip {
    position: absolute;
    left: var(--hx);
    bottom: 26px;
    transform: translateX(-50%);
    background: rgba(6, 5, 10, 0.94);
    border: 1px solid var(--glass-bd);
    box-shadow: var(--sh-3);
    border-radius: var(--r-md);
    padding: 5px 9px;
    font-size: 12px;
    pointer-events: none;
    white-space: nowrap;
    transition:
      opacity var(--m-fast, 130ms) var(--ease-out),
      translate var(--m-fast, 130ms) var(--ease-out);
  }
  @starting-style {
    .tip { opacity: 0; translate: 0 6px; }
  }
  .tip.withtile { padding: 5px; }
  .tile {
    border-radius: var(--r-sm);
    background-color: #000;
    background-repeat: no-repeat;
    margin-bottom: 5px;
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.1);
    zoom: 1.25;
  }
  .tip-time {
    display: flex;
    align-items: baseline;
    justify-content: center;
    gap: 7px;
    color: var(--tx-5);
  }
  .tip-mark {
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--tx-2);
  }

  .row { padding-top: 6px; }

  .ctrl {
    background: transparent;
    touch-action: manipulation;
    color: var(--tx-4);
    border: none;
    border-radius: var(--r-md);
    min-width: 36px;
    min-height: 36px;
    transition:
      color var(--m-fast, 130ms) var(--ease-out),
      background var(--m-fast, 130ms) var(--ease-out),
      transform var(--m-fast, 130ms) var(--ease-snap);
  }
  @media (hover: none) {
    .ctrl { min-width: 44px; min-height: 44px; }
  }
  .ctrl:hover { color: var(--tx-5); background: var(--glass); }
  .ctrl:active { transform: scale(0.92); }

  .time {
    font-size: 13px;
    display: flex;
    align-items: baseline;
    gap: 5px;
  }
  .now { color: var(--tx-5); font-weight: 500; }
  .sep, .tot { color: var(--tx-2); }

  .title {
    flex: 1;
    font-family: var(--font-display);
    font-size: 14px;
    letter-spacing: -0.014em;
    color: var(--tx-5);
  }

  .speed-chip {
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    color: var(--tx-4);
    padding: 4px 10px;
    font-size: 12px;
    font-weight: 500;
    perspective: 600px;
    transition: background var(--m-fast, 130ms) var(--ease-out);
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
</style>
