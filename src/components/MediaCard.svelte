<script lang="ts">
  import type { Snippet } from "svelte";
  import { cell, manifest, sheet, zoneSec, type SpriteManifest } from "../core/sprite";
  import { dur } from "../core/fmt";
  import { clean, thumbFail, vidURL } from "../core/video";
  import { art, morph } from "./art";

  let {
    dir,
    name,
    key,
    idx = 0,
    downloaded = false,
    overlay,
    footer,
  }: {
    dir: string;
    name: string;
    key: string;
    idx?: number;
    downloaded?: boolean;
    overlay?: Snippet;
    footer?: Snippet;
  } = $props();

  let frame = $state<HTMLElement | null>(null);

  // The art splits into ZONES columns left→right; hovering one shows a frame
  // drawn from that fraction of the runtime.
  const ZONES = 5;

  // Keyed by `key` because an unkeyed row recycles this component onto another
  // video, and a stale sheet would scrub the wrong title.
  let got = $state<{ key: string; m: SpriteManifest | null } | null>(null);
  let zone = $state(-1);
  let at = $state(0);

  // Re-rolled only on a zone change — per-pointermove would strobe.
  function roll(z: number) {
    zone = z;
    const m = got?.m;
    if (m && z >= 0) at = zoneSec(m, z, ZONES);
  }

  // Touch keeps the static poster: a tap is the tile's one job (navigate), and
  // the sheet is a few hundred KB nobody asked for.
  function scrub(e: PointerEvent) {
    if (e.pointerType === "touch" || !frame) return;

    // Lazily, once per hovered tile: claim the slot before awaiting so the
    // pointermove stream behind this one doesn't queue a callback per event.
    if (got?.key !== key) {
      got = { key, m: null };
      manifest(key).then((m) => {
        got = { key, m };
        roll(zone);
      });
    }

    const box = frame.getBoundingClientRect();
    const z = Math.min(
      ZONES - 1,
      Math.max(0, Math.floor(((e.clientX - box.left) / box.width) * ZONES)),
    );
    if (z !== zone) roll(z);
  }

  // Scrubbing is only legible if the five divisions are VISIBLE — a silent
  // image swap reads as "the thumbnail changed", not as "I am moving through
  // the episode". The track below draws them and marks the one under the
  // pointer, which is the whole point of splitting the art into fifths.
  const live = $derived(got?.key === key && got.m !== null && zone >= 0);

  const crop = $derived.by(() => {
    const m = got?.key === key ? got.m : null;
    if (!m || zone < 0) return "";

    const c = cell(m, at);
    // Percentage positioning stretches one cell over the whole box, matching
    // the static thumbnail (which is object-fit: fill), so nothing shifts.
    const x = m.cols > 1 ? (c.col / (m.cols - 1)) * 100 : 0;
    const y = m.rows > 1 ? (c.row / (m.rows - 1)) * 100 : 0;

    return (
      `background-image:url('${sheet(key)}');` +
      `background-size:${m.cols * 100}% ${m.rows * 100}%;` +
      `background-position:${x}% ${y}%`
    );
  });
</script>

<a
  href={vidURL(dir, name)}
  class="tile ambient ptr p-rel"
  style="--i: {idx}"
  use:art={key}
  onclick={() => morph(frame, key)}
  onpointerenter={scrub}
  onpointermove={scrub}
  onpointerleave={() => roll(-1)}
>
  <div class="art p-rel flow-h" bind:this={frame}>
    <img
      class="h-100 w-100 d-b"
      src="/images/{key}.jpg"
      alt=""
      loading="lazy"
      onerror={thumbFail}
    />
    {#if crop}
      <div class="shot p-abs dead" style={crop}></div>
    {/if}
    <div class="scrim p-abs dead"></div>
    {#if live}
      <div class="zones p-abs dead">
        {#each { length: ZONES } as _, z}
          <i class:on={z === zone}></i>
        {/each}
      </div>
      <div class="stamp p-abs dead">{dur(at)}</div>
    {/if}
    <div class="play p-abs cc dead">
      <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
        <path d="M8 5.5v13l11-6.5z" fill="currentColor" stroke="none" />
      </svg>
    </div>
    {#if downloaded}
      <div class="dot p-abs dead" title="Available offline"></div>
    {/if}
    {@render overlay?.()}
  </div>

  <div class="label">
    <div class="name trunc">{clean(name)}</div>
    {@render footer?.()}
  </div>
</a>

<style>
  .tile {
    container-type: inline-size;
    container-name: tile;
    display: block;
    flex-shrink: 0;
    scroll-snap-align: start;
    border-radius: var(--r-lg);
    color: var(--tx-4);
    animation: tile-in var(--motion, 0.4s) var(--ease-out) both;
    animation-delay: calc(min(var(--i, 0), 14) * 26ms);
  }

  @keyframes tile-in {
    from {
      opacity: 0;
      translate: 0 14px;
      scale: 0.97;
    }
  }

  .art {
    aspect-ratio: 16 / 9;
    border-radius: var(--r-lg);
    background: var(--bg-3);
    box-shadow:
      0 0 0 1px color-mix(in oklab, var(--art-1) 16%, transparent),
      0 6px 18px -12px rgb(0 0 0 / 0.8);
    transition:
      box-shadow var(--motion, 0.3s) var(--ease-out),
      scale var(--motion, 0.3s) var(--ease-snap);
  }

  .art img {
    filter: saturate(0.92) contrast(1.02);
    transition:
      scale var(--motion-slow, 0.6s) var(--ease-out),
      filter var(--motion, 0.3s) var(--ease-out);
  }

  /* The .svg stand-in is the logo — a mark, not a frame, so never cropped. */
  .art img[src$=".svg"] {
    object-fit: contain;
    padding: 18%;
    opacity: 0.3;
    box-sizing: border-box;
  }

  /* No background-color: a sheet that 404s leaves the thumbnail showing. */
  .shot {
    inset: 0;
    background-repeat: no-repeat;
    animation: shot-in var(--motion-fast, 0.18s) var(--ease-out) both;
  }

  /* The five divisions, drawn. Bottom edge so it never covers a face, and
     inside .art so it scrubs with the tile rather than the page. */
  .zones {
    left: 6px;
    right: 6px;
    bottom: 6px;
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: 1fr;
    gap: 3px;
    height: 3px;
  }
  .zones i {
    border-radius: 2px;
    background: color-mix(in oklab, #fff 28%, transparent);
    transition:
      background var(--motion-fast, 0.18s) var(--ease-out),
      box-shadow var(--motion-fast, 0.18s) var(--ease-out),
      scale var(--motion-fast, 0.18s) var(--ease-out);
  }
  .zones i.on {
    background: var(--red);
    box-shadow: 0 0 10px var(--red-glow);
    scale: 1 2.2;
  }

  .stamp {
    right: 7px;
    bottom: 14px;
    padding: 2px 6px;
    border-radius: var(--r-sm, 4px);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    letter-spacing: 0.02em;
    color: #fff;
    background: color-mix(in oklab, #000 62%, transparent);
    backdrop-filter: blur(6px);
    animation: shot-in var(--motion-fast, 0.18s) var(--ease-out) both;
  }

  
  @keyframes shot-in {
    from {
      opacity: 0;
    }
  }

  .scrim {
    inset: 0;
    background: linear-gradient(
      to top,
      color-mix(in oklab, var(--art-2) 55%, #000) 0%,
      transparent 46%
    );
    opacity: 0.55;
    transition: opacity var(--motion, 0.3s) var(--ease-out);
  }

  .play {
    inset: auto 10px 10px auto;
    width: 34px;
    height: 34px;
    border-radius: 50%;
    color: var(--tx-5);
    background: color-mix(in oklab, var(--art-1) 34%, rgb(0 0 0 / 0.55));
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
    opacity: 0;
    scale: 0.8;
    transition:
      opacity var(--motion-fast, 0.18s) var(--ease-out),
      scale var(--motion, 0.28s) var(--ease-spring);
  }

  .dot {
    bottom: 10px;
    left: 10px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--grn);
    box-shadow:
      0 0 0 2px rgb(0 0 0 / 0.55),
      0 0 10px color-mix(in oklab, var(--grn) 70%, transparent);
  }

  .label {
    padding: 9px 2px 2px;
    min-width: 0;
  }

  .name {
    color: var(--tx-4);
    font-size: clamp(12px, 6cqi, 15px);
    font-weight: 500;
    line-height: 1.3;
    transition: color var(--motion-fast, 0.18s) var(--ease-out);
  }

  @media (hover: hover) {
    .tile:hover .art,
    .tile:focus-visible .art {
      scale: 1.018;
      box-shadow:
        0 0 0 1px color-mix(in oklab, var(--art-1) 52%, transparent),
        0 22px 44px -22px color-mix(in oklab, var(--art-1) 85%, transparent),
        0 8px 20px -10px rgb(0 0 0 / 0.7);
    }
    .tile:hover .art img {
      scale: 1.05;
      filter: saturate(1.06) contrast(1.02);
    }
    .tile:hover .scrim {
      opacity: 0.85;
    }
    .tile:hover .play,
    .tile:focus-visible .play {
      opacity: 1;
      scale: 1;
    }
    .tile:hover .name {
      color: var(--tx-5);
    }
  }

  .tile:active .art {
    scale: 0.985;
    transition-duration: var(--motion-fast, 0.12s);
  }

  @container tile (max-width: 168px) {
    .label {
      padding-top: 6px;
    }
    .play {
      display: none;
    }
  }

  @container tile (min-width: 360px) {
    .art {
      aspect-ratio: 2.2 / 1;
    }
    .scrim {
      background: linear-gradient(
        to top,
        color-mix(in oklab, var(--art-2) 62%, #000) 0%,
        color-mix(in oklab, var(--art-2) 24%, transparent) 38%,
        transparent 70%
      );
      opacity: 0.95;
    }
    .label {
      position: absolute;
      inset: auto 0 0 0;
      padding: 0 82px 16px 20px;
      pointer-events: none;
    }
    .name {
      color: var(--tx-5);
      font-size: clamp(17px, 3.6cqi, 26px);
      font-weight: 600;
      letter-spacing: -0.02em;
      text-shadow: 0 2px 14px rgb(0 0 0 / 0.6);
    }
    .play {
      inset: auto 20px 20px auto;
      width: 46px;
      height: 46px;
      opacity: 1;
      scale: 1;
    }
    .dot {
      top: 14px;
      bottom: auto;
      left: 20px;
    }
  }

  @media (hover: hover) {
    :global(.spotlight > .tile) {
      transition:
        opacity var(--motion, 0.3s) var(--ease-out),
        filter var(--motion, 0.3s) var(--ease-out);
    }
    :global(.spotlight:has(> .tile:hover) > .tile:not(:hover)) {
      opacity: 0.55;
      filter: saturate(0.5) brightness(0.78);
    }
  }

  
</style>
