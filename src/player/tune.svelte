<script lang="ts" module>
  export type Fx = {
    bright: number;
    contrast: number;
    sat: number;
    gamma: number;
  };

  const FX_KEY = "vidfx";
  const num = (x: any, d: number) => (Number.isFinite(+x) ? +x : d);

  function loadFx(): Fx {
    try {
      const v = JSON.parse(localStorage.getItem(FX_KEY) ?? "{}");
      return {
        bright: num(v.bright, 1),
        contrast: num(v.contrast, 1),
        sat: num(v.sat, 1),
        gamma: num(v.gamma, 1),
      };
    } catch {
      return { bright: 1, contrast: 1, sat: 1, gamma: 1 };
    }
  }
</script>

<script lang="ts">
  let {
    open,
    videoEl,
    syncMs,
    onSync,
    speed,
    onSpeed,
    onClose,
  }: {
    open: boolean;
    videoEl: () => HTMLVideoElement | null;
    syncMs: number;
    onSync: (ms: number) => void;
    speed: number;
    onSpeed: (rate: number) => void;
    onClose: () => void;
  } = $props();

  let fx = $state(loadFx());

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
    localStorage.setItem(FX_KEY, JSON.stringify($state.snapshot(fx)));
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
    <span class="fs-xs tx-1">{label}</span>
    <input
      type="range"
      {min}
      {max}
      {step}
      {value}
      oninput={(e) => set(+e.currentTarget.value)}
    />
    <span class="val fs-xs tx-4">{fmt}</span>
  </label>
{/snippet}

<aside class="tune" class:show={open}>
  <div class="hd f al-ct">
    <span class="fs-sm fw6 tx-5">Tune</span>
    <button class="x ptr tx-1" onclick={onClose}>✕</button>
  </div>

  <div class="sec fs-xs tx-1">video</div>
  {@render slider("Brightness", 0.25, 1.75, 0.01, fx.bright, (v) => (fx.bright = v), fx.bright.toFixed(2))}
  {@render slider("Contrast", 0.5, 1.5, 0.01, fx.contrast, (v) => (fx.contrast = v), fx.contrast.toFixed(2))}
  {@render slider("Saturation", 0, 2, 0.01, fx.sat, (v) => (fx.sat = v), fx.sat.toFixed(2))}
  {@render slider("Gamma", 0.5, 2, 0.01, fx.gamma, (v) => (fx.gamma = v), fx.gamma.toFixed(2))}

  <div class="sec fs-xs tx-1">audio</div>
  {@render slider("Delay", 0, 2000, 25, syncMs, onSync, `+${syncMs}ms`)}
  <div class="hint fs-xs tx-1">
    browsers can only play audio later, not earlier
  </div>

  <div class="sec fs-xs tx-1">playback</div>
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
    z-index: 14;
    width: 272px;
    padding: 14px 16px;
    background: rgba(7, 6, 10, 0.92);
    border-left: 1px solid var(--glass-bd);
    backdrop-filter: blur(14px) saturate(140%);
    -webkit-backdrop-filter: blur(14px) saturate(140%);
    transform: translateX(100%);
    transition: transform 0.22s var(--ease-out);
    overflow-y: auto;
  }
  .tune.show {
    transform: translateX(0);
  }

  .hd {
    justify-content: space-between;
    margin-bottom: 6px;
  }
  .x {
    background: none;
    border: none;
    font-size: 14px;
  }
  .x:hover {
    color: var(--tx-5);
  }

  .sec {
    margin: 14px 0 4px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .row {
    display: grid;
    grid-template-columns: 72px 1fr 52px;
    align-items: center;
    gap: 8px;
    padding: 5px 0;
  }
  .row input {
    width: 100%;
    accent-color: var(--red);
  }
  .val {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .hint {
    margin-top: 2px;
    opacity: 0.7;
  }

  .reset {
    margin-top: 18px;
    width: 100%;
    padding: 8px;
    background: transparent;
    color: var(--tx-1);
    border: 1px solid var(--glass-bd);
  }
  .reset:hover {
    color: var(--tx-5);
  }
</style>
