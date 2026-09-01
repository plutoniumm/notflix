// Ambilight: a 16x9 canvas sample reduced to two colours for --art-1/--art-2.
// Must never compete with decode: rVFC idles while the video isn't presenting,
// and the loop hard-stops on pause / hidden / reduced-motion.

export type RGB = [number, number, number];

const W = 16;
const H = 9;
const BUCKETS = 12;
const SAMPLE_MS = 260;
// Per-sample step; higher strobes on a cut instead of washing over.
const SMOOTH = 0.3;
const NEUTRAL: RGB = [34, 29, 44];

export function hsl(r: number, g: number, b: number): [number, number, number] {
  const R = r / 255;
  const G = g / 255;
  const B = b / 255;
  const mx = Math.max(R, G, B);
  const mn = Math.min(R, G, B);
  const l = (mx + mn) / 2;
  const d = mx - mn;
  if (d === 0) return [0, 0, l];

  const s = l > 0.5 ? d / (2 - mx - mn) : d / (mx + mn);
  let h: number;
  if (mx === R) h = ((G - B) / d + (G < B ? 6 : 0)) / 6;
  else if (mx === G) h = ((B - R) / d + 2) / 6;
  else h = ((R - G) / d + 4) / 6;

  return [h, s, l];
}

export function toRGB(h: number, s: number, l: number): RGB {
  if (s <= 0) {
    const v = Math.round(l * 255);

    return [v, v, v];
  }

  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const ch = (t: number) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;

    return p;
  };

  return [
    Math.round(ch(h + 1 / 3) * 255),
    Math.round(ch(h) * 255),
    Math.round(ch(h - 1 / 3) * 255),
  ];
}

// Saturation pushed, lightness only clamped — a dark scene must stay dark.
function punch([r, g, b]: RGB): RGB {
  const [h, s, l] = hsl(r, g, b);

  return toRGB(
    h,
    Math.min(0.92, s * 1.5 + 0.05),
    Math.min(0.6, Math.max(0.07, l * 0.95 + 0.05)),
  );
}

export function css([r, g, b]: RGB): string {
  return `rgb(${r} ${g} ${b})`;
}

// Chroma-weighted, or letterbox bars and grey walls win every vote.
export function analyze(px: Uint8ClampedArray | number[]): [RGB, RGB] {
  const wt = new Float64Array(BUCKETS);
  const acc = new Float64Array(BUCKETS * 3);
  let n = 0;
  let ar = 0;
  let ag = 0;
  let ab = 0;

  for (let i = 0; i + 3 < px.length; i += 4) {
    const r = px[i];
    const g = px[i + 1];
    const b = px[i + 2];

    ar += r;
    ag += g;
    ab += b;
    n++;

    const [h, s, l] = hsl(r, g, b);
    if (s < 0.12 || l < 0.05 || l > 0.97) continue;

    const k = Math.min(BUCKETS - 1, Math.floor(h * BUCKETS));
    const w = s * s * (0.25 + l);
    wt[k] += w;
    acc[k * 3] += r * w;
    acc[k * 3 + 1] += g * w;
    acc[k * 3 + 2] += b * w;
  }

  const mean: RGB = n
    ? [Math.round(ar / n), Math.round(ag / n), Math.round(ab / n)]
    : NEUTRAL;
  const at = (k: number): RGB => [
    Math.round(acc[k * 3] / wt[k]),
    Math.round(acc[k * 3 + 1] / wt[k]),
    Math.round(acc[k * 3 + 2] / wt[k]),
  ];

  let first = -1;
  for (let k = 0; k < BUCKETS; k++)
    if (wt[k] > 0 && (first < 0 || wt[k] > wt[first])) first = k;

  if (first < 0) {
    const m = punch(mean);

    return [m, m];
  }

  // Second colour needs 60 degrees of separation or the pair collapses into one wash.
  let second = -1;
  for (let k = 0; k < BUCKETS; k++) {
    if (wt[k] <= 0 || k === first) continue;
    const gap = Math.abs(k - first);
    if (Math.min(gap, BUCKETS - gap) < 2) continue;
    if (second < 0 || wt[k] > wt[second]) second = k;
  }

  const a = punch(at(first));
  if (second >= 0) return [a, punch(at(second))];

  // Monochrome frame: rotate and darken the same hue so the two lamps differ.
  const [h, s, l] = hsl(a[0], a[1], a[2]);

  return [a, toRGB((h + 0.06) % 1, s * 0.8, Math.max(0.05, l * 0.62))];
}

type Sink = (a: string, b: string) => void;
type Opts = { every?: number; canvas?: () => any; reduced?: () => boolean };

const prefersReduced = () => {
  try {
    return !!globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  } catch {
    return false;
  }
};

const makeCanvas = () => {
  if (typeof OffscreenCanvas !== "undefined") return new OffscreenCanvas(W, H);
  const c = document.createElement("canvas");
  c.width = W;
  c.height = H;

  return c;
};

export class Ambient {
  samples = 0;
  running = false;

  #sink: Sink;
  #every: number;
  #makeCanvas: () => any;
  #reduced: () => boolean;
  #video: HTMLVideoElement | null = null;
  #ctx: any = null;
  #cur: [RGB, RGB] = [NEUTRAL, NEUTRAL];
  #last = 0;
  #tid: any = null;
  #dead = false;

  constructor(sink: Sink, opts: Opts = {}) {
    this.#sink = sink;
    this.#every = opts.every ?? SAMPLE_MS;
    this.#makeCanvas = opts.canvas ?? makeCanvas;
    this.#reduced = opts.reduced ?? prefersReduced;
  }

  attach(video: HTMLVideoElement) {
    if (this.#dead || this.#video || this.#reduced()) return this;

    this.#video = video;
    for (const ev of ["play", "playing", "seeked", "loadeddata"])
      video.addEventListener(ev, this.#kick);
    for (const ev of ["pause", "ended", "emptied", "waiting", "stalled"])
      video.addEventListener(ev, this.#halt);
    globalThis.document?.addEventListener?.("visibilitychange", this.#vis);
    if (!video.paused) this.#kick();

    return this;
  }

  detach() {
    const video = this.#video;
    this.#halt();
    this.#dead = true;
    if (!video) return;

    for (const ev of ["play", "playing", "seeked", "loadeddata"])
      video.removeEventListener(ev, this.#kick);
    for (const ev of ["pause", "ended", "emptied", "waiting", "stalled"])
      video.removeEventListener(ev, this.#halt);
    globalThis.document?.removeEventListener?.("visibilitychange", this.#vis);
    this.#video = null;
    this.#ctx = null;
  }

  #vis = () => {
    if (globalThis.document?.hidden) this.#halt();
    else this.#kick();
  };

  #kick = () => {
    const v = this.#video;
    if (this.running || this.#dead || !v || v.paused) return;
    if (globalThis.document?.hidden) return;

    this.running = true;
    this.#queue();
  };

  #halt = () => {
    this.running = false;
    if (this.#tid === null) return;

    const v = this.#video as any;
    if (v?.cancelVideoFrameCallback) v.cancelVideoFrameCallback(this.#tid);
    else clearTimeout(this.#tid);
    this.#tid = null;
  };

  #queue() {
    const v = this.#video as any;
    this.#tid = v?.requestVideoFrameCallback
      ? v.requestVideoFrameCallback(this.#loop)
      : setTimeout(this.#loop, this.#every);
  }

  #loop = () => {
    this.#tid = null;
    if (!this.running) return;

    const now = Date.now();
    if (now - this.#last >= this.#every) {
      this.#last = now;
      this.#sample();
    }
    if (this.running) this.#queue();
  };

  #sample() {
    const v = this.#video as any;
    if (!v || !v.videoWidth) return;

    try {
      if (!this.#ctx) {
        this.#ctx = this.#makeCanvas().getContext("2d", {
          willReadFrequently: true,
          alpha: false,
        });
      }
      const ctx = this.#ctx;
      if (!ctx) return;

      ctx.drawImage(v, 0, 0, W, H);
      const [a, b] = analyze(ctx.getImageData(0, 0, W, H).data);
      this.#cur = [step(this.#cur[0], a), step(this.#cur[1], b)];
      this.samples++;
      this.#sink(css(this.#cur[0]), css(this.#cur[1]));
    } catch {
      // A tainted canvas throws on every read and never recovers — stop for good.
      this.#dead = true;
      this.#halt();
    }
  }
}

const step = (cur: RGB, tgt: RGB): RGB => [
  Math.round(cur[0] + (tgt[0] - cur[0]) * SMOOTH),
  Math.round(cur[1] + (tgt[1] - cur[1]) * SMOOTH),
  Math.round(cur[2] + (tgt[2] - cur[2]) * SMOOTH),
];
