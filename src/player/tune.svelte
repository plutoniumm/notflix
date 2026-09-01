<script lang="ts" module>
  import { local } from "../core/store";

  export type Fx = {
    bright: number;
    contrast: number;
    sat: number;
    gamma: number;
  };

  const fxStore = local<Partial<Fx>>("vidfx", {});
  const num = (x: any, d: number) => (Number.isFinite(+x) ? +x : d);

  function loadFx(): Fx {
    const v = fxStore.get();

    return {
      bright: num(v.bright, 1),
      contrast: num(v.contrast, 1),
      sat: num(v.sat, 1),
      gamma: num(v.gamma, 1),
    };
  }
</script>

<script lang="ts">
  let {
    open,
    videoEl,
    canSync = true,
    syncMs,
    onSync,
    speed,
    onSpeed,
    onClose,
  }: {
    open: boolean;
    videoEl: () => HTMLVideoElement | null;
    canSync?: boolean;
    syncMs: number;
    onSync: (ms: number) => void;
    speed: number;
    onSpeed: (rate: number) => void;
    onClose: () => void;
  } = $props();

  let fx = $state(loadFx());
  let sheet = $state<HTMLElement>();

  // The top layer brings Escape and light-dismiss and can't be trapped by an
  // ancestor's stacking context; without it, `.show` drives a plain fixed panel.
  const popOk =
    typeof HTMLElement !== "undefined" && "popover" in HTMLElement.prototype;

  $effect(() => {
    const el = sheet as any;
    if (!popOk || !el?.isConnected) return;

    try {
      if (open) el.showPopover?.();
      else el.hidePopover?.();
    } catch {
      // showPopover throws if the state already matches; nothing to repair.
    }
  });

  const isDefault = $derived(
    fx.bright === 1 && fx.contrast === 1 && fx.sat === 1 && fx.gamma === 1,
  );

  $effect(() => {
    let f = "";
    if (!isDefault) {
      f = `brightness(${fx.bright}) contrast(${fx.contrast}) saturate(${fx.sat})`;
      if (fx.gamma !== 1) f += " url(#fx-gamma)";
    }
    const el = videoEl();
    if (el) el.style.filter = f;
    fxStore.set($state.snapshot(fx));
  });

  function reset() {
    fx = { bright: 1, contrast: 1, sat: 1, gamma: 1 };
    onSync(0);
    onSpeed(1);
  }
</script>

{#snippet slider(
  label: string,
  min: number,
  max: number,
  step: number,
  value: number,
  set: (v: number) => void,
  fmt: string,
)}
  <label class="row">
    <span class="lbl fs-xs">{label}</span>
    <input
      type="range"
      {min}
      {max}
      {step}
      {value}
      oninput={(e) => set(+e.currentTarget.value)}
    />
    <span class="val pl-num fs-xs">{fmt}</span>
  </label>
{/snippet}

<aside
  bind:this={sheet}
  class="tune pl-pane"
  class:show={open}
  popover={popOk ? "auto" : undefined}
  ontoggle={(e: any) => e.newState === "closed" && open && onClose()}
>
  <div class="hd f al-ct">
    <span class="ttl">Tune</span>
    <button class="x ptr tx-1" aria-label="Close" onclick={onClose}>✕</button>
  </div>

  <div class="sec pl-lbl">video</div>
  {@render slider("Brightness", 0.25, 1.75, 0.01, fx.bright, (v) => (fx.bright = v), fx.bright.toFixed(2))}
  {@render slider("Contrast", 0.5, 1.5, 0.01, fx.contrast, (v) => (fx.contrast = v), fx.contrast.toFixed(2))}
  {@render slider("Saturation", 0, 2, 0.01, fx.sat, (v) => (fx.sat = v), fx.sat.toFixed(2))}
  {@render slider("Gamma", 0.5, 2, 0.01, fx.gamma, (v) => (fx.gamma = v), fx.gamma.toFixed(2))}

  {#if canSync}
    <div class="sec pl-lbl">audio</div>
    {@render slider("Delay", 0, 2000, 25, syncMs, onSync, `+${syncMs}ms`)}
    <div class="hint fs-xs tx-1">
      browsers can only play audio later, not earlier
    </div>
  {/if}

  <div class="sec pl-lbl">playback</div>
  {@render slider("Speed", 0.25, 3, 0.05, speed, onSpeed, `${speed.toFixed(2)}×`)}

  <button class="reset rx5 ptr fs-xs" onclick={reset}>Reset all</button>
</aside>

<svg width="0" height="0" style="position:absolute" aria-hidden="true">
  <filter id="fx-gamma">
    <feComponentTransfer>
      <feFuncR type="gamma" amplitude="1" exponent={1 / fx.gamma} offset="0" />
      <feFuncG type="gamma" amplitude="1" exponent={1 / fx.gamma} offset="0" />
      <feFuncB type="gamma" amplitude="1" exponent={1 / fx.gamma} offset="0" />
    </feComponentTransfer>
  </filter>
</svg>

<style>
  .tune {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    /* The UA popover sets `inset: 0`; without left:auto the element is
       over-constrained and pins to the left edge. */
    left: auto;
    z-index: 14;
    width: min(300px, 86vw);
    margin: 0;
    padding: calc(16px + var(--safe-t, 0px)) 18px
      calc(18px + var(--safe-b, 0px));
    border: 0;
    border-left: 1px solid var(--glass-bd);
    border-radius: 0;
    transform: translateX(101%);
    transition: transform var(--m, 260ms) var(--ease-out);
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .tune.show {
    transform: translateX(0);
  }

  /* The UA hides a closed popover outright, so the slide has to survive a
     discrete display change. The fallback path never sees these rules. */
  .tune:popover-open {
    transform: translateX(0);
  }
  .tune[popover] {
    transition-property: transform, display, overlay;
    transition-duration: var(--m, 260ms);
    transition-timing-function: var(--ease-out);
    transition-behavior: allow-discrete;
  }
  @starting-style {
    .tune:popover-open {
      transform: translateX(101%);
    }
  }
  .tune[popover]::backdrop {
    background: rgba(3, 2, 6, 0.35);
    backdrop-filter: blur(2px);
    transition:
      opacity var(--m, 260ms) var(--ease-out),
      display var(--m, 260ms) allow-discrete;
  }

  .hd {
    justify-content: space-between;
    margin-bottom: 10px;
  }
  .ttl {
    font-family: var(--font-display);
    font-size: 16px;
    font-weight: 600;
    letter-spacing: -0.02em;
    color: var(--tx-5);
  }
  .x {
    background: none;
    border: none;
    font-size: 14px;
    min-width: 32px;
    min-height: 32px;
    border-radius: var(--r-sm);
    transition: color var(--m-fast, 130ms) var(--ease-out);
  }
  .x:hover {
    color: var(--tx-5);
  }

  .sec {
    margin: 18px 0 2px;
  }

  .row {
    display: grid;
    grid-template-columns: 74px 1fr 54px;
    align-items: center;
    gap: 10px;
    padding: 5px 0;
  }
  .lbl {
    color: var(--tx-3);
  }
  .row input {
    width: 100%;
    accent-color: var(--red);
    min-height: 24px;
  }
  @media (hover: none) {
    .row input { min-height: 44px; }
  }
  .val {
    text-align: right;
    color: var(--tx-4);
  }

  .hint {
    margin-top: 4px;
    line-height: 1.45;
    opacity: 0.75;
  }

  .reset {
    margin-top: 22px;
    width: 100%;
    min-height: 40px;
    padding: 9px;
    background: transparent;
    color: var(--tx-2);
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-md);
    transition:
      color var(--m-fast, 130ms) var(--ease-out),
      background var(--m-fast, 130ms) var(--ease-out);
  }
  .reset:hover {
    color: var(--tx-5);
    background: var(--glass);
  }
</style>
