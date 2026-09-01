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
    onSpeedSet,
    onVolume,
    onMute,
    onNext,
    onPip,
    onEpisodes,
    onFullscreen,
    trickplay,
  }: {
    ps: PlayerState;
    title: string;
    speed?: number;
    buffered?: [number, number][];
    intro?: [number, number] | null;
    outro?: [number, number] | null;
    onSeek: (sec: number) => void;
    onPlayPause: () => void;
    onSpeedDown: () => void;
    onSpeedUp: () => void;
    onSpeedSet: (rate: number) => void;
    onVolume: (level: number) => void;
    onMute: () => void;
    onNext?: () => void;
    onPip?: () => void;
    onEpisodes?: () => void;
    onFullscreen: () => void;
    // Hovered second -> inline CSS for the tile, "" when there is no sprite.
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
  // beamEl IS the drawn timeline: its box excludes the track's touch padding,
  // so the fill width, the head offset and the click mapping share one basis.
  // Not `.bar` — that is the top chrome, and chrome.svelte's player-state rules
  // are global, so the two would answer to each other's selectors.
  let beamEl = $state<HTMLDivElement>();
  let dragged = false;
  let keyTimer: ReturnType<typeof setTimeout> | undefined;
  $effect(() => () => clearTimeout(keyTimer));

  const dur = $derived(ps.duration);
  const cur = $derived(ps.currentTime);
  // ps.pct is rounded to whole percent — a 72s stair-step on a two-hour film.
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

  // One legible object: the whole runtime split into named regions at the
  // detected intro/credits boundaries, so a range reads as structure rather
  // than as a stripe laid over the bar.
  const chapters = $derived.by(() => {
    if (dur <= 0) return [];

    const marks = ([
      [intro, "Intro"],
      [outro, "Credits"],
    ] as [[number, number] | null, string][])
      .filter(([r]) => !!r)
      .map(([r, name]) => ({
        a: Math.max(0, r![0]),
        b: Math.min(dur, r![1]),
        name,
      }))
      .sort((x, y) => x.a - y.a);

    const out: { a: number; b: number; name: string; kind: string }[] = [];
    let end = 0;
    for (const c of marks) {
      // Overlapping ranges are bad data, not a layout mode: never emit a
      // segment that runs backwards.
      if (c.b <= end) continue;
      if (c.a > end) out.push({ a: end, b: c.a, name: "Feature", kind: "main" });
      out.push({
        a: Math.max(end, c.a),
        b: c.b,
        name: c.name,
        kind: c.name.toLowerCase(),
      });
      end = c.b;
    }
    if (end < dur)
      out.push({ a: end, b: dur, name: out.length ? "Feature" : "", kind: "main" });

    return out.map((c) => ({
      ...c,
      l: (c.a / dur) * 100,
      w: ((c.b - c.a) / dur) * 100,
    }));
  });

  const here = $derived(
    s.hovering
      ? chapters.find((c) => s.hoverSec >= c.a && s.hoverSec <= c.b)
      : undefined,
  );
  const mark = $derived(here && here.kind !== "main" ? here.name : "");
  const cuts = $derived(chapters.slice(1).map((c) => c.l));

  function track(e: PointerEvent): number {
    if (!beamEl || dur <= 0) return 0;
    const r = beamEl.getBoundingClientRect();
    const x = Math.max(0, Math.min(r.width, e.clientX - r.left));
    s.hoverX = x;
    s.hoverSec = (x / r.width) * dur;

    return s.hoverSec;
  }

  // The commit half of a drag, shared with the keyboard: one seek when the
  // gesture ends, never one per move — a seek per move restarts the in-flight
  // HLS segment fetch dozens of times a second.
  function release() {
    if (dragged) onSeek(s.hoverSec);
    s.scrubbing = false;
    dragged = false;
  }

  // Pointer events cover mouse and touch in one path; a mouse-only version
  // leaves dragging dead on touch. Capture keeps the move stream on the track.
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

  function onUp(e: PointerEvent) {
    if (!s.scrubbing) return;
    release();
    (e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId);
    if (e.pointerType !== "mouse") s.hovering = false;
  }
  function onLeave() {
    if (!s.scrubbing) s.hovering = false;
  }

  // Arrows preview live and commit once the keys stop, so holding one down
  // costs the same single seek a drag does.
  function onKey(e: KeyboardEvent) {
    if (dur <= 0 || e.ctrlKey || e.metaKey) return;

    const step = e.altKey ? 0.1 : e.shiftKey ? 30 : 5;
    const from = s.scrubbing ? s.hoverSec : cur;
    let to: number | null = null;
    if (e.key === "ArrowRight") to = from + step;
    else if (e.key === "ArrowLeft") to = from - step;
    else if (e.key === "PageUp") to = from + 60;
    else if (e.key === "PageDown") to = from - 60;
    else if (e.key === "Home") to = 0;
    else if (e.key === "End") to = dur - 1;
    if (to === null) return;

    e.preventDefault();
    // Without this the document-level hotkeys seek a second time on the same
    // press — the track is a div, so they don't skip it the way they skip
    // inputs and buttons.
    e.stopPropagation();

    s.hoverSec = Math.max(0, Math.min(dur - 0.1, to));
    s.hoverX = beamEl ? (s.hoverSec / dur) * beamEl.clientWidth : 0;
    s.hovering = true;
    s.scrubbing = true;
    dragged = true;
    clearTimeout(keyTimer);
    keyTimer = setTimeout(release, 160);
  }

  const RATES = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2];

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

  function cycleSpeed() {
    const i = RATES.findIndex((r) => r > speed + 0.001);
    onSpeedSet(RATES[i < 0 ? 0 : i]);
    s.flipDir = 1;
    setTimeout(() => (s.flipDir = 0), 250);
  }

  const tile = $derived(
    trickplay && s.hovering && dur > 0 ? trickplay(s.hoverSec) : "",
  );
  const gap = $derived(s.scrubbing ? s.hoverSec - cur : 0);
  const fmtSpeed = (n: number) => parseFloat(n.toFixed(2)).toString();
  const vol = $derived(ps.volLevel);
</script>

{#if dur > 0}
  <div
    class="dock p-fix"
    class:hidden={ps.hideUI}
    style="--pct:{headPct}%;--hx:{s.hoverX}px"
  >
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div
      class="track ptr"
      class:live={s.scrubbing}
      tabindex="0"
      onpointerdown={onDown}
      onpointermove={onMove}
      onpointerup={onUp}
      onpointercancel={onUp}
      onpointerleave={onLeave}
      onkeydown={onKey}
      onfocus={() => (s.hovering = true)}
      onblur={onLeave}
    >
      <div class="beam" bind:this={beamEl}>
        <div class="bg"></div>

        {#each chapters as c}
          {#if c.kind !== "main"}
            <div class="mark {c.kind}" style="left:{c.l}%;width:{c.w}%"></div>
          {/if}
        {/each}

        {#each bufs as b}
          <div class="buf" style="left:{b.a}%;width:{b.w}%"></div>
        {/each}

        <div class="fill"></div>

        {#each cuts as c}
          <div class="cut" style="left:{c}%"></div>
        {/each}

        <div class="head"></div>

        {#if s.hovering}
          <div class="scrub-line"></div>
          <div class="tip" class:cine={s.scrubbing} class:withtile={!!tile}>
            {#if tile}
              <div class="tile" style={tile}></div>
            {/if}
            <div class="tip-time pl-num">
              {fmtTime(s.hoverSec)}
              {#if mark}
                <span class="tip-mark">{mark}</span>
              {/if}
            </div>
            {#if s.scrubbing}
              <div class="gap pl-num">
                {gap >= 0 ? "+" : "−"}{fmtTime(Math.abs(gap))}
              </div>
            {/if}
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

      <div class="vol f al-ct sh-0">
        <button
          class="ctrl ptr cc"
          aria-label={vol === 0 ? "Unmute" : "Mute"}
          onclick={onMute}
        >
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor" stroke="none">
            <path d="M4 9h3.5L12 5v14L7.5 15H4z" />
            {#if vol === 0}
              <path d="M16 9.5l4 5M20 9.5l-4 5" stroke="currentColor" stroke-width="1.8" fill="none" />
            {:else}
              <path d="M15.6 8.6a5 5 0 010 6.8" stroke="currentColor" stroke-width="1.8" fill="none" />
              {#if vol > 0.5}
                <path d="M18.2 6.4a8.5 8.5 0 010 11.2" stroke="currentColor" stroke-width="1.8" fill="none" />
              {/if}
            {/if}
          </svg>
        </button>
        <input
          class="vol-range"
          type="range"
          min="0"
          max="1"
          step="0.05"
          value={vol}
          title="Volume"
          oninput={(e) => onVolume(+e.currentTarget.value)}
        />
      </div>

      <div class="time pl-num sh-0" title="{fmtTime(left)} left">
        <span class="now">{fmtTime(cur)}</span>
        <span class="sep">/</span>
        <span class="tot">{fmtTime(dur)}</span>
      </div>

      <div class="title trunc fw5 tx-5">{title}</div>

      <button
        class="speed-chip pl-num cc rx5 ptr sh-0"
        title="Playback speed — click to cycle, scroll to nudge"
        onclick={cycleSpeed}
        onwheel={speedWheel}
        class:flip-up={s.flipDir === 1}
        class:flip-down={s.flipDir === -1}
      >
        {fmtSpeed(speed)}×
      </button>

      {#if onEpisodes}
        <button class="ctrl ptr cc" aria-label="Up next" onclick={onEpisodes}>
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 6h11M4 12h11M4 18h7M18 13l3 3-3 3" /></svg>
        </button>
      {/if}

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
    /* Control density comes off the dock's own width, not the viewport: the
       same dock is right in an iPhone landscape and in a 34" window. */
    container: dock / inline-size;
    /* No padding of its own, so the inset is the padding — and it insets the
       scrub track too, keeping the track box and its hit-test aligned. */
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
  /* The chrome hides on an idle pointer, but a scrubber being driven from the
     keyboard is in use — and it can't move the pointer to say so. */
  .dock.hidden:has(.track:focus-visible),
  .dock.hidden:has(.track.live) {
    opacity: 1;
    transform: none;
    pointer-events: auto;
  }

  .track {
    position: relative;
    height: 20px;
    padding: 0 16px;
    user-select: none;
    touch-action: none;
    transition: height var(--m-fast, 130ms) var(--ease-out);
  }
  /* Vertical only — the horizontal padding is the shared measurement basis
     and must stay identical to .beam's inset. */
  @media (hover: none) {
    .track { height: 30px; }
  }
  .track.live { height: 34px; }
  .track:focus-visible { outline-offset: -3px; }

  .beam {
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

  .mark { background: color-mix(in oklab, var(--tx-5) 26%, transparent); }
  .mark.credits { background: color-mix(in oklab, var(--gold) 34%, transparent); }

  /* Above the fill: a chapter edge has to stay readable through the played
     part, which is where you actually look for it. */
  .cut {
    position: absolute;
    top: 50%;
    width: 2px;
    height: calc(var(--h, 3px) + 4px);
    transform: translate(-1px, -50%);
    background: rgba(3, 2, 6, 0.85);
    border-radius: 1px;
    pointer-events: none;
    transition: height var(--m-fast, 130ms) var(--ease-out);
  }

  .track:hover { --h: 7px; }
  .track.live { --h: 9px; }
  .track:hover .head,
  .track.live .head,
  .track:focus-visible .head { opacity: 1; transform: translate(-50%, -50%) scale(1); }

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
  .track.live .head { transform: translate(-50%, -50%) scale(1.25); }

  .scrub-line {
    position: absolute;
    left: var(--hx);
    top: 0;
    bottom: 0;
    width: 1px;
    background: rgba(255, 255, 255, 0.35);
    pointer-events: none;
  }

  /* Deliberately not .pl-pane: the tip tracks the pointer, and a moving
     backdrop-filter re-blurs its backdrop every frame. */
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
    /* `left` is smoothed only so the jump to centre when a scrub starts reads
       as a move; at 130ms the hover follow still tracks the pointer. */
    transition:
      opacity var(--m-fast, 130ms) var(--ease-out),
      left var(--m-fast, 130ms) var(--ease-out),
      padding var(--m, 260ms) var(--ease-out),
      translate var(--m-fast, 130ms) var(--ease-out);
  }
  @starting-style {
    .tip { opacity: 0; translate: 0 6px; }
  }
  .tip.withtile { padding: 5px; }

  /* Scrub cinema: the preview leaves the bar, centres itself over the picture
     and becomes the lit thing in the room. */
  .tip.cine {
    left: 50%;
    bottom: 34px;
    padding: 10px;
    border-radius: var(--r-lg);
    border-color: color-mix(in oklab, var(--art-1) 50%, var(--glass-bd));
    box-shadow:
      var(--sh-4),
      0 0 90px -20px color-mix(in oklab, var(--art-1) 80%, transparent);
  }

  .tile {
    border-radius: var(--r-sm);
    background-color: #000;
    background-repeat: no-repeat;
    margin-bottom: 5px;
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.1);
    zoom: 1.25;
    transition: zoom var(--m, 260ms) var(--ease-out);
  }
  .tip.cine .tile {
    zoom: 2.6;
    border-radius: 3px;
    margin-bottom: 4px;
  }
  @container dock (max-width: 620px) {
    .tip.cine .tile { zoom: 1.9; }
  }

  .tip-time {
    display: flex;
    align-items: baseline;
    justify-content: center;
    gap: 7px;
    color: var(--tx-5);
  }
  .tip.cine .tip-time {
    font-size: 17px;
    font-weight: 600;
    letter-spacing: -0.02em;
    padding-top: 2px;
  }
  .tip-mark {
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--tx-2);
  }
  .gap {
    text-align: center;
    font-size: 11px;
    color: color-mix(in oklab, var(--red) 60%, var(--tx-4));
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

  .vol-range {
    width: 0;
    opacity: 0;
    margin-left: 0;
    accent-color: var(--red);
    min-height: 22px;
    cursor: pointer;
    transition:
      width var(--m, 260ms) var(--ease-out),
      opacity var(--m, 260ms) var(--ease-out),
      margin var(--m, 260ms) var(--ease-out);
  }
  /* focus-within, not a JS flag: the slider stays open while either the knob
     or the mute button owns focus, which is what a keyboard user needs. */
  .vol:hover .vol-range,
  .vol:focus-within .vol-range {
    width: 74px;
    opacity: 1;
    margin-left: 4px;
  }
  /* An opacity-0 slider still takes taps, and there is no hover to open it. */
  @media (hover: none) {
    .vol-range { width: 68px; opacity: 1; margin-left: 4px; min-height: 44px; }
  }

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
    min-height: 30px;
    font-size: 12px;
    font-weight: 500;
    perspective: 600px;
    transition: background var(--m-fast, 130ms) var(--ease-out);
  }
  @media (hover: none) {
    .speed-chip { min-height: 44px; min-width: 52px; }
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

  /* Narrow dock: the title is the first thing to go — it is repeated in the
     top bar — then the slider, which the mute button and ↑/↓ still cover. */
  @container dock (max-width: 620px) {
    .title { display: none; }
  }
  @container dock (max-width: 470px) {
    .vol-range { display: none; }
    .row { gap: 4px; }
  }
  @container dock (max-width: 380px) {
    .time .sep, .time .tot { display: none; }
  }
</style>
