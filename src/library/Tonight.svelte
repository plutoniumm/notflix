<script lang="ts">
  import { vidURL, thumbFail } from "../core/video";
  import { pct } from "../core/fmt";
  import { tonight, type Pick } from "./tonight";
  import type { ContinueItem } from "./homeData.svelte";

  let {
    data,
    continues,
    onLibrary,
  }: {
    data: VideoData;
    continues: ContinueItem[];
    onLibrary: () => void;
  } = $props();

  let picks = $derived(tonight(data, continues));
  let at = $state(0);
  let cur = $derived(picks[Math.min(at, picks.length - 1)]);
  // The strip is always picks[1..] and never reorders. Promoting the hovered
  // card into the stage and demoting the old one swapped two DOM nodes under a
  // stationary pointer, which fired mouseenter again and flipped it straight
  // back — the stage oscillated instead of following.
  let alts = $derived(picks.slice(1));

  // Pointer position as a -1..1 pair. The layers move at different rates
  // against it, so the depth is a consequence of your own movement rather
  // than an animation playing on a timer.
  let mx = $state(0);
  let my = $state(0);

  function track(e: PointerEvent) {
    if (e.pointerType === "touch") return;

    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    mx = ((e.clientX - r.left) / r.width) * 2 - 1;
    my = ((e.clientY - r.top) / r.height) * 2 - 1;
  }

  function keys(e: KeyboardEvent) {
    const el = document.activeElement;
    if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return;

    if (e.key >= "1" && e.key <= "9") {
      const i = Number(e.key) - 1;
      if (i < picks.length) {
        at = i;
        e.preventDefault();
      }
    } else if (e.key === "Enter" && cur) {
      location.href = vidURL(cur.dir, cur.name);
    } else if (e.key.toLowerCase() === "l") {
      onLibrary();
    }
  }
</script>

<svelte:window onkeydown={keys} />

<!-- Real lensing. backdrop-filter: blur() frosts what is behind the glass;
     it does not bend it. feDisplacementMap pushes each backdrop pixel by the
     R/G of a normal map that is neutral (128,128) across the middle and ramps
     at the rim, so content actually refracts around the edge the way it does
     through a physical edge. Guarded: a browser without url() in
     backdrop-filter keeps the plain frost. -->
<svg class="defs" width="0" height="0" aria-hidden="true">
  <filter id="lens" x="-12%" y="-12%" width="124%" height="124%"
    color-interpolation-filters="sRGB">
    <feImage
      href="data:image/svg+xml;utf8,%3Csvg xmlns='http://www.w3.org/2000/svg' width='320' height='120'%3E%3Cdefs%3E%3ClinearGradient id='x' x1='0' x2='1'%3E%3Cstop offset='0' stop-color='%23000'/%3E%3Cstop offset='0.16' stop-color='%23808080'/%3E%3Cstop offset='0.84' stop-color='%23808080'/%3E%3Cstop offset='1' stop-color='%23fff'/%3E%3C/linearGradient%3E%3ClinearGradient id='y' x1='0' y1='0' x2='0' y2='1'%3E%3Cstop offset='0' stop-color='%23000'/%3E%3Cstop offset='0.3' stop-color='%23808080'/%3E%3Cstop offset='0.7' stop-color='%23808080'/%3E%3Cstop offset='1' stop-color='%23fff'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='320' height='120' fill='url(%23x)'/%3E%3Crect width='320' height='120' fill='url(%23y)' style='mix-blend-mode:screen;opacity:0.5'/%3E%3C/svg%3E"
      result="map"
      preserveAspectRatio="none"
    />
    <feDisplacementMap
      in="SourceGraphic"
      in2="map"
      scale="26"
      xChannelSelector="R"
      yChannelSelector="G"
    />
  </filter>
</svg>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<section
  class="stage"
  style="--mx:{mx}; --my:{my}"
  onpointermove={track}
  onpointerleave={() => ((mx = 0), (my = 0))}
>
  {#if cur}
    <!-- The artwork IS the room: full bleed, out of focus, behind everything.
         A thumbnail in a box makes the library a catalogue; this makes it a
         place you are already inside. -->
    {#key cur.key}
      <div class="env" style="background-image:url('/images/{cur.key}.jpg')"></div>
    {/key}
    <div class="veil"></div>

    <div class="hold">
      <article class="lead">
        <!-- Every candidate says why it is here. An unexplained row is the
             mechanism that keeps you scrolling for a better one. -->
        <p class="why">{cur.reason}</p>
        <h1 class="ttl">{cur.title}</h1>
        <p class="sub">
          {#if cur.tag}<b class="tag">{cur.tag}</b>{/if}
          {#if cur.sub}<span>{cur.sub}</span>{/if}
        </p>

        <p class="facts">
          {#if cur.left}<span>{cur.left}</span>{/if}
          <!-- The stopping cue. Knowing where the end is, is the difference
               between watching something and being kept. -->
          {#if cur.end}<span class="end">{cur.end}</span>{/if}
        </p>

        <a class="go" href={vidURL(cur.dir, cur.name)}>
          <svg viewBox="0 0 24 24" width="17" height="17" aria-hidden="true">
            <path d="M8 5.5v13l11-6.5z" fill="currentColor" stroke="none" />
          </svg>
          {cur.action}
        </a>
      </article>

      <a class="plate" href={vidURL(cur.dir, cur.name)}>
        {#key cur.key}
          <img src="/images/{cur.key}.jpg" alt="" onerror={thumbFail} />
        {/key}
        {#if cur.pct > 0}
          <span class="seam" style="--p:{cur.pct}%" title={pct(cur.pct)}></span>
        {/if}
      </a>
    </div>

    {#if alts.length}
      <nav class="alts">
        <span class="orr">or</span>
        {#each alts as p, i (p.key)}
          <button
            class="alt"
            class:on={at === i + 1}
            onmouseenter={() => (at = i + 1)}
            onmouseleave={() => (at = 0)}
            onfocus={() => (at = i + 1)}
            onclick={() => (location.href = vidURL(p.dir, p.name))}
          >
            <img src="/images/{p.key}.jpg" alt="" onerror={thumbFail} />
            <span class="txt">
              <span class="an f al-ct g5">
                <span class="trunc">{p.title}</span>
                {#if p.tag}<b class="atag">{p.tag}</b>{/if}
              </span>
              <span class="ar trunc">{p.reason}</span>
            </span>
          </button>
        {/each}
      </nav>
    {/if}
  {:else}
    <!-- The honest end of the list. A catalogue never admits this exists. -->
    <div class="hold empty">
      <article class="lead">
        <p class="why">nothing waiting</p>
        <h1 class="ttl">You're caught up.</h1>
        <p class="facts"><span>Nothing is part-watched and nothing is queued.</span></p>
      </article>
    </div>
  {/if}

  <button class="lib" onclick={onLibrary}>
    Library <kbd>L</kbd>
  </button>
</section>

<style>
  .defs {
    position: absolute;
    pointer-events: none;
  }

  .stage {
    position: relative;
    isolation: isolate;
    box-sizing: border-box;
    min-height: calc(100dvh - var(--hdr, 64px));
    max-height: calc(100dvh - var(--hdr, 64px));
    display: grid;
    grid-template-rows: 1fr auto;
    overflow: hidden;
    padding: clamp(20px, 4vh, 54px) clamp(20px, 5vw, 76px);
    gap: 24px;
  }

  .env {
    position: absolute;
    inset: -14%;
    z-index: -2;
    background-size: cover;
    background-position: center;
    filter: blur(54px) saturate(185%) brightness(0.92);
    opacity: 0.95;
    translate: calc(var(--mx) * -22px) calc(var(--my) * -16px);
    scale: 1.14;
    transition:
      translate 700ms var(--ease-out),
      opacity var(--motion-slow) linear;
  }
  @starting-style {
    .env {
      opacity: 0;
      scale: 1.3;
    }
  }

  .veil {
    position: absolute;
    inset: 0;
    z-index: -1;
    /* Dark enough on the left for the type to hold, clear on the right so the
       artwork is actually the room rather than a brown wash. */
    background:
      linear-gradient(
        100deg,
        var(--bg-1) 0%,
        color-mix(in oklab, var(--bg-1) 86%, transparent) 26%,
        color-mix(in oklab, var(--bg-1) 40%, transparent) 52%,
        transparent 76%
      ),
      linear-gradient(0deg, var(--bg-1) 0%, transparent 34%),
      radial-gradient(120% 90% at 50% 50%, transparent 38%, rgb(0 0 0 / 0.5) 100%);
  }

  .hold {
    display: grid;
    grid-template-columns: minmax(0, 0.84fr) minmax(0, 1.16fr);
    align-items: stretch;
    gap: clamp(24px, 4vw, 60px);
    min-height: 0;
  }
  .empty {
    grid-template-columns: minmax(0, 1fr);
  }

  .lead {
    display: grid;
    align-content: center;
    justify-items: start;
    gap: 2px;
    max-width: 19ch;
  }

  /* The reason, not a genre tag. Small, quiet, first — it frames the title
     instead of selling it. */
  .why {
    margin: 0 0 10px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.22em;
    text-transform: uppercase;
    color: var(--red-2);
  }

  .ttl {
    margin: 0;
    font-family: var(--font-display);
    font-size: clamp(38px, 6.4vw, 92px);
    line-height: 0.92;
    letter-spacing: -0.035em;
    font-weight: 700;
    text-wrap: balance;
    color: var(--tx-5);
    translate: calc(var(--mx) * -5px) 0;
    transition: translate 800ms var(--ease-out);
  }
  @starting-style {
    .ttl {
      opacity: 0;
      translate: 0 26px;
    }
  }

  .sub {
    display: flex;
    align-items: baseline;
    gap: 10px;
    margin: 12px 0 0;
    font-size: clamp(13px, 1.1vw, 16px);
    letter-spacing: 0.04em;
    color: var(--tx-2);
  }
  .tag {
    font-variant-numeric: tabular-nums;
    letter-spacing: 0.1em;
    font-size: 12px;
    font-weight: 700;
    padding: 3px 9px;
    border-radius: 6px;
    color: var(--tx-4);
    background: color-mix(in oklab, var(--tx-5) 11%, transparent);
  }
  .atag {
    flex-shrink: 0;
    font-weight: 500;
    font-size: 11px;
    letter-spacing: 0.08em;
    color: var(--tx-1);
    margin-left: 6px;
  }

  .facts {
    display: flex;
    flex-wrap: wrap;
    gap: 7px 14px;
    margin: 16px 0 26px;
    font-size: 13px;
    color: var(--tx-3);
  }
  .facts .end {
    color: var(--tx-2);
    padding-left: 14px;
    border-left: 1px solid var(--glass-bd);
  }

  /* Glass: a real backdrop sample, a light edge on the lit side and a dark one
     opposite, so the control sits ON the artwork rather than over it. */
  .go {
    display: inline-flex;
    align-items: center;
    gap: 9px;
    padding: 13px 26px 13px 21px;
    border-radius: 999px;
    font-size: 15px;
    font-weight: 600;
    letter-spacing: 0.01em;
    color: var(--tx-5);
    text-decoration: none;
    background: color-mix(in oklab, var(--red) 82%, transparent);
    backdrop-filter: blur(18px) saturate(170%);
    box-shadow:
      inset 0 1px 0 color-mix(in oklab, var(--tx-5) 38%, transparent),
      inset 0 -1px 0 rgb(0 0 0 / 0.3),
      0 14px 34px -14px color-mix(in oklab, var(--red) 75%, transparent);
    transition:
      scale 420ms var(--ease-spring),
      box-shadow 300ms var(--ease-out);
  }
  .go:hover {
    scale: 1.045;
    box-shadow:
      inset 0 1px 0 color-mix(in oklab, var(--tx-5) 52%, transparent),
      inset 0 -1px 0 rgb(0 0 0 / 0.3),
      0 20px 46px -14px color-mix(in oklab, var(--red) 92%, transparent);
  }
  .go:active {
    scale: 0.985;
  }

  /* Bleeds past the stage padding on the right: key art, not a thumbnail in a
     box. The box is what made every previous pass read as a catalogue. */
  .plate {
    position: relative;
    display: block;
    border-radius: 22px 0 0 22px;
    overflow: hidden;
    min-height: 0;
    margin-right: calc(clamp(20px, 5vw, 76px) * -1);
    translate: calc(var(--mx) * 10px) calc(var(--my) * 8px);
    box-shadow:
      inset 0 0 0 1px color-mix(in oklab, var(--tx-5) 14%, transparent),
      -30px 40px 110px -40px rgb(0 0 0 / 0.95);
    transition:
      translate 620ms var(--ease-out),
      scale 520ms var(--ease-spring);
  }
  .plate:hover {
    scale: 1.008;
  }
  .plate img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  @starting-style {
    .plate {
      opacity: 0;
      scale: 0.94;
    }
  }

  .seam {
    position: absolute;
    inset: auto 0 0 0;
    height: 3px;
    background: linear-gradient(
      90deg,
      var(--red) 0 var(--p),
      color-mix(in oklab, var(--tx-5) 16%, transparent) var(--p)
    );
  }

  .alts {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    padding-right: 140px;
  }
  .orr {
    font-size: 12px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--tx-1);
    padding-right: 6px;
  }

  .alt {
    display: flex;
    align-items: center;
    gap: 11px;
    min-width: 0;
    max-width: 290px;
    padding: 7px 16px 7px 7px;
    border: 0;
    border-radius: 14px;
    cursor: pointer;
    text-align: left;
    color: inherit;
    background: color-mix(in oklab, var(--tx-5) 6%, transparent);
    backdrop-filter: blur(22px) saturate(150%);
    /* Dropped wholesale where url() is not a valid backdrop-filter value, which
       leaves the frost above as the fallback. */
    backdrop-filter: url(#lens) blur(13px) saturate(155%);
    box-shadow: inset 0 0 0 1px color-mix(in oklab, var(--tx-5) 10%, transparent);
    transition:
      background 260ms var(--ease-out),
      scale 420ms var(--ease-spring),
      box-shadow 260ms var(--ease-out);
  }
  .alt.on,
  .alt:hover,
  .alt:focus-visible {
    background: color-mix(in oklab, var(--tx-5) 13%, transparent);
    box-shadow: inset 0 0 0 1px color-mix(in oklab, var(--tx-5) 24%, transparent);
    scale: 1.025;
  }
  .alt img {
    width: 74px;
    height: 42px;
    object-fit: cover;
    border-radius: 8px;
    flex-shrink: 0;
  }
  .txt {
    display: grid;
    min-width: 0;
  }
  .an {
    font-size: 13px;
    font-weight: 600;
    color: var(--tx-4);
  }
  .ar {
    font-size: 11px;
    color: var(--tx-1);
  }

  .lib {
    position: absolute;
    right: clamp(20px, 5vw, 76px);
    bottom: clamp(20px, 4vh, 54px);
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 9px 15px;
    border: 0;
    border-radius: 999px;
    cursor: pointer;
    font-size: 12.5px;
    letter-spacing: 0.04em;
    color: var(--tx-3);
    background: color-mix(in oklab, var(--tx-5) 5%, transparent);
    backdrop-filter: blur(20px);
    backdrop-filter: url(#lens) blur(12px);
    box-shadow: inset 0 0 0 1px color-mix(in oklab, var(--tx-5) 9%, transparent);
    transition:
      color 240ms var(--ease-out),
      background 240ms var(--ease-out);
  }
  .lib:hover {
    color: var(--tx-5);
    background: color-mix(in oklab, var(--tx-5) 11%, transparent);
  }
  .lib kbd {
    font: inherit;
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 4px;
    color: var(--tx-2);
    background: color-mix(in oklab, var(--tx-5) 10%, transparent);
  }

  @media (max-width: 900px) {
    .hold {
      grid-template-columns: minmax(0, 1fr);
      align-content: end;
      gap: 20px;
    }
    .plate {
      order: -1;
      translate: none;
      margin-right: 0;
      border-radius: 18px;
      aspect-ratio: 16 / 9;
    }
    .lead {
      max-width: none;
    }
    .alts {
      padding-bottom: 46px;
    }
    .alt {
      max-width: none;
      flex: 1 1 220px;
    }
  }
</style>
