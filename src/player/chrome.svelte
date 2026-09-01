<div class="pl-glow"></div>
<div class="pl-vig"></div>
<div class="pl-scrim"></div>

<style>
  :global {
  /* Global on purpose: one recipe every player component opts into by class. */
  .pl {
    --m-fast: var(--motion-fast, 130ms);
    --m: var(--motion, 260ms);
    --m-slow: var(--motion-slow, 520ms);

    /* Seeded neutral; the ambilight overwrites these inline, per frame. */
    --art-1: #221d2c;
    --art-2: #16131c;
  }

  .pl-glow {
    position: absolute;
    inset: -14%;
    z-index: 0;
    pointer-events: none;
    opacity: 0.85;
    background:
      radial-gradient(58% 52% at 16% 26%, var(--art-1) 0%, transparent 70%),
      radial-gradient(54% 50% at 84% 76%, var(--art-2) 0%, transparent 68%),
      radial-gradient(90% 70% at 50% 50%, var(--art-1) 0%, transparent 78%);
    /* Unblurred on purpose: a full-viewport blur re-rasterises on every colour
       change, and three radial gradients are soft enough. Custom properties only
       interpolate once registered with @property; ignored, harmlessly, when not. */
    transition:
      --art-1 var(--m-slow) linear,
      --art-2 var(--m-slow) linear,
      opacity var(--m-slow) var(--ease-out);
    will-change: opacity;
  }

  .pl-vig {
    position: absolute;
    inset: 0;
    z-index: 0;
    pointer-events: none;
    background: radial-gradient(
      118% 92% at 50% 50%,
      transparent 46%,
      rgba(3, 2, 6, 0.62) 100%
    );
    transition: opacity var(--m-slow) var(--ease-out);
  }

  /* video.js paints its own black box on top of the spill, killing the letterbox. */
  .pl .video-js,
  .pl .video-js .vjs-tech,
  .pl .vjs-poster {
    background-color: transparent;
  }

  /* What is left of video.js once its control components are switched off: the
     caption box and the spinner. Both are dressed to match, because they are
     the only pieces of it still on screen.

     video.js drops the caption box to `bottom: 1em` the moment controls are
     disabled — straight behind the dock — so it rides above the dock while the
     chrome is up and drops back once the chrome hides. `.video-js` is in the
     selector to outweigh that rule rather than to depend on file order. */
  .pl .video-js.vjs-controls-disabled .vjs-text-track-display {
    bottom: calc(98px + var(--safe-b, 0px));
    transition: bottom var(--m) var(--ease-out);
  }

  .pl.hide-ui .video-js.vjs-controls-disabled .vjs-text-track-display {
    bottom: 2.6em;
  }

  /* video.js writes the cue background inline, so only !important reaches it. */
  .pl .vjs-text-track-cue > div {
    background: transparent !important;
    font-family: var(--font-display) !important;
    font-weight: 500 !important;
    letter-spacing: -0.014em;
    text-shadow:
      0 0 4px #000,
      0 1px 3px #000,
      1px 0 3px #000,
      -1px 0 3px #000;
  }

  .pl .vjs-loading-spinner {
    border-color: color-mix(in oklab, var(--art-1) 55%, transparent);
    width: 3.2em;
    height: 3.2em;
    border-width: 0.28em;
    opacity: 0.9;
  }

  /* The pseudos are the spinning arcs and re-declare their own top colour. */
  .pl .vjs-seeking .vjs-loading-spinner::before,
  .pl .vjs-seeking .vjs-loading-spinner::after,
  .pl .vjs-waiting .vjs-loading-spinner::before,
  .pl .vjs-waiting .vjs-loading-spinner::after {
    border-top-color: var(--red);
  }

  .pl-pane {
    background: rgba(13, 11, 18, 0.82);
    background: color-mix(in oklab, var(--bg-2) 78%, transparent);
    backdrop-filter: blur(22px) saturate(160%);
    -webkit-backdrop-filter: blur(22px) saturate(160%);
    border: 1px solid var(--glass-bd);
    box-shadow:
      var(--sh-3),
      inset 0 1px 0 rgba(255, 255, 255, 0.06);
    color: var(--tx-4);
  }

  .pl-lbl {
    font-family: var(--font-display);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--tx-2);
  }

  .pl-num {
    font-family: var(--font-display);
    font-variant-numeric: tabular-nums;
    font-feature-settings: "tnum" 1, "ss01" 1;
    letter-spacing: -0.01em;
  }

  /* @starting-style supplies the pre-insertion frame; allow-discrete keeps
     `display` alive for the exit. */
  .pl-enter {
    transition:
      opacity var(--m) var(--ease-out),
      transform var(--m) var(--ease-out);
  }

  /* Split rather than folded into the shorthand: a parser that does not know
     `allow-discrete` throws away the whole shorthand, opacity fade included. */
  @supports (transition-behavior: allow-discrete) {
    .pl-enter {
      transition-property: opacity, transform, display, overlay;
      transition-behavior: allow-discrete;
    }
  }

  .pl:has(.sheet.show) .pl-scrim,
  .pl:has(.dropdown:not(.out)) .pl-scrim {
    opacity: 1;
  }
  .pl:has(.sheet.show) .pl-glow,
  .pl:has(.dropdown:not(.out)) .pl-glow {
    opacity: 0.45;
  }

  .pl-scrim {
    position: absolute;
    inset: 0;
    z-index: 2;
    pointer-events: none;
    opacity: 0;
    background: linear-gradient(
      to left,
      rgba(3, 2, 6, 0.62) 0%,
      rgba(3, 2, 6, 0.28) 45%,
      transparent 78%
    );
    transition: opacity var(--m) var(--ease-out);
  }

  /* Decoration only, where :paused ships; the JS `paused` flag stays the
     functional path. */
  @supports selector(video:paused) {
    .pl:has(video:paused) .pl-glow {
      opacity: 0.55;
    }
    .pl:has(video:paused) .pl-vig {
      opacity: 0.65;
    }
  }

  /* Scrub cinema. The room goes dark around the one lit thing: the picture
     dims, the surround brightens (the ambilight samples the decoded frame, not
     the styled one, so it keeps the true colour), and every other piece of
     chrome steps back until the pointer comes up. State comes off the DOM —
     .track carries .live already — so nothing here needs a second JS flag.
     The :not()s are load-bearing: each of these already has its own hidden
     state at one class and this selector outweighs it, so without them a
     scrub would fade the hidden chrome back IN at 16%. */
  .pl:has(.track.live) .rail:not(.out),
  .pl:has(.track.live) > .bar:not(.hidden),
  .pl:has(.track.live) .synopsis:not(.out),
  .pl:has(.track.live) .skip:not(.out),
  .pl:has(.track.live) .ghost:not(.out) {
    opacity: 0.16;
  }

  .pl:has(.track.live) .video-wrap {
    filter: brightness(0.34) saturate(0.7);
    scale: 0.985;
  }

  .pl:has(.track.live) .pl-glow {
    opacity: 1;
  }
  .pl:has(.track.live) .pl-vig {
    opacity: 1;
  }

  @media (prefers-reduced-motion: reduce) {
    .pl *,
    .pl *::before,
    .pl *::after {
      animation-duration: 1ms !important;
      animation-iteration-count: 1 !important;
      transition-duration: 1ms !important;
    }
  }

  }
</style>
