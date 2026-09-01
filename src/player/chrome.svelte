<!-- The room. Three painted layers plus the player's shared surface rules,
     kept together because they are one idea: light from the picture, the
     falloff around it, and the scrim a panel casts over it. Mounted once, from
     Player.svelte; every layer is z-indexed, so its position in the markup does
     not matter. -->

<div class="pl-glow"></div>
<div class="pl-vig"></div>
<div class="pl-scrim"></div>

<style>
  :global {
  /* The player's shared surface. Global on purpose: one panel recipe, one motion
     scale and one set of receding-chrome rules that every player component opts
     into by class, instead of five scoped copies drifting apart.

     Everything reads tokens from the design system with a local fallback, so the
     player still looks right if a token has not landed yet. */

  .pl {
    --m-fast: var(--motion-fast, 130ms);
    --m: var(--motion, 260ms);
    --m-slow: var(--motion-slow, 520ms);

    /* Seeded neutral; the ambilight overwrites these inline, per frame. */
    --art-1: #221d2c;
    --art-2: #16131c;
  }

  /* --- ambilight -------------------------------------------------------- */

  /* Light from the picture, thrown onto the room. It lives behind the video, so
     the only place it is visible is the surround: letterbox bars, the pillar
     boxes on a 4:3 source, the strip under the dock. That is the whole idea. */
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
    /* Deliberately unblurred. A full-viewport blur re-rasterises on every
       colour change, which is the one thing here that could compete with
       decode — and three radial gradients are already soft enough that it
       bought nothing it could pay for.

       Custom properties only interpolate once they are registered with
       @property; when they are, the wash slides between shots instead of
       stepping. Harmless and ignored when they are not. */
    transition:
      --art-1 var(--m-slow) linear,
      --art-2 var(--m-slow) linear,
      opacity var(--m-slow) var(--ease-out);
    will-change: opacity;
  }

  /* A vignette keeps the wash off the picture edge, so the spill reads as light
     in the room rather than a coloured frame stuck to the video. */
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

  /* video.js paints its own black box, which would sit on top of the spill and
     leave the letterbox dead. */
  .pl .video-js,
  .pl .video-js .vjs-tech,
  .pl .vjs-poster {
    background-color: transparent;
  }

  /* --- panels ----------------------------------------------------------- */

  /* One glass recipe: dropdowns, the tune sheet and the up-next rail are the
     same material at different sizes. */
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

  /* Anything that counts. */
  .pl-num {
    font-family: var(--font-display);
    font-variant-numeric: tabular-nums;
    font-feature-settings: "tnum" 1, "ss01" 1;
    letter-spacing: -0.01em;
  }

  /* --- chrome that arrives and leaves ----------------------------------- */

  /* Discrete-property transitions: the element really leaves the layout when it
     is gone, but both directions still animate. @starting-style supplies the
     pre-insertion frame, allow-discrete keeps `display` alive for the exit. */
  .pl-enter {
    transition:
      opacity var(--m) var(--ease-out),
      transform var(--m) var(--ease-out);
  }

  /* Split rather than folded into the shorthand: a parser that does not know
     `allow-discrete` throws away the whole shorthand, taking the opacity fade
     with it. Each component supplies its own `.out` and @starting-style, since
     the direction of the motion is the component's business. */
  @supports (transition-behavior: allow-discrete) {
    .pl-enter {
      transition-property: opacity, transform, display, overlay;
      transition-behavior: allow-discrete;
    }
  }

  /* --- page reacts to its own state, without a JS class ----------------- */

  /* A panel being open is a fact about the DOM, so let the DOM say it. No open
     flag has to be mirrored onto the page element for this to work. */
  .pl:has(.tune.show) .pl-scrim,
  .pl:has(.dropdown:not(.out)) .pl-scrim {
    opacity: 1;
  }
  .pl:has(.tune.show) .pl-glow,
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

  /* :paused is real CSS state on a media element; where it ships, the room
     brightens the moment playback stops without anyone toggling a class. The JS
     `paused` flag stays the functional path — this only decorates. */
  @supports selector(video:paused) {
    .pl:has(video:paused) .pl-glow {
      opacity: 0.55;
    }
    .pl:has(video:paused) .pl-vig {
      opacity: 0.65;
    }
  }

  /* Scrubbing is a whole-page mode: the picture is being navigated, so the rest
     of the chrome steps back and lets the timeline own the screen. */
  .pl:has(.track.live) .rail {
    opacity: 0.3;
  }

  /* --- motion off ------------------------------------------------------- */

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
