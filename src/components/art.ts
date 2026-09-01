// Every card publishes two colours pulled out of its own artwork, and the
// surface around it is tinted from them. A deterministic seed derived from the
// key paints instantly (and covers a missing thumbnail); the real sample
// replaces it once the image has actually decoded.

type Pair = { a: string; b: string };

const cache = new Map<string, Pair>();
const queue: (() => void)[] = [];

const W = 12;
const H = 7;

let ctx: CanvasRenderingContext2D | null | undefined;
let draining = false;
let named: HTMLElement | null = null;

const idle: (cb: () => void) => void =
  typeof requestIdleCallback === "function"
    ? (cb) => requestIdleCallback(() => cb(), { timeout: 500 })
    : (cb) => setTimeout(cb, 24);

// One readback per idle slice. A library row can mount thirty images at once
// and getImageData is synchronous — batching them all would stall the scroll.
function drain() {
  if (draining || !queue.length) return;
  draining = true;

  idle(() => {
    draining = false;
    queue.shift()?.();
    drain();
  });
}

function push(job: () => void) {
  queue.push(job);
  drain();
}

function seed(key: string): Pair {
  let n = 0;
  for (let i = 0; i < key.length; i++) n = (n * 31 + key.charCodeAt(i)) >>> 0;

  const hue = n % 360;

  return {
    a: `oklch(0.62 0.12 ${hue})`,
    b: `oklch(0.4 0.08 ${(hue + 52) % 360})`,
  };
}

function surface() {
  if (ctx !== undefined) return ctx;

  try {
    const c = document.createElement("canvas");
    c.width = W;
    c.height = H;
    ctx = c.getContext("2d", { willReadFrequently: true });
  } catch {
    ctx = null;
  }

  return ctx;
}

// The accent is the most chromatic pixel that is neither crushed nor blown —
// those two ends carry no usable hue and a frame is mostly one or the other.
function sample(img: HTMLImageElement): Pair | null {
  const c = surface();
  if (!c || !img.naturalWidth || !img.naturalHeight) return null;

  let px: Uint8ClampedArray;
  try {
    c.clearRect(0, 0, W, H);
    c.drawImage(img, 0, 0, W, H);
    px = c.getImageData(0, 0, W, H).data;
  } catch {
    return null;
  }

  let sr = 0;
  let sg = 0;
  let sb = 0;
  let best = -1;
  let ar = 0;
  let ag = 0;
  let ab = 0;

  for (let i = 0; i < px.length; i += 4) {
    const r = px[i];
    const g = px[i + 1];
    const b = px[i + 2];
    sr += r;
    sg += g;
    sb += b;

    const hi = Math.max(r, g, b);
    const lo = Math.min(r, g, b);
    const score = hi > 26 && hi < 248 ? hi - lo : 0;

    if (score > best) {
      best = score;
      ar = r;
      ag = g;
      ab = b;
    }
  }

  const n = px.length / 4;
  const avg = `rgb(${Math.round(sr / n)} ${Math.round(sg / n)} ${Math.round(sb / n)})`;

  return {
    a: best > 8 ? `rgb(${ar} ${ag} ${ab})` : avg,
    b: avg,
  };
}

function paint(el: HTMLElement, p: Pair) {
  el.style.setProperty("--art-1", p.a);
  el.style.setProperty("--art-2", p.b);
}

// use:art — publishes --art-1/--art-2 on the node for `.ambient` to read.
export function art(node: HTMLElement, key: string) {
  let current = key;

  function apply(k: string) {
    const hit = cache.get(k);
    paint(node, hit ?? seed(k));
    if (hit) return;

    const img = node.querySelector("img");
    if (!img) return;

    const run = () => {
      if (/\.svg(\?|$)/i.test(img.currentSrc || img.src)) return;

      const got = sample(img);
      if (!got) return;

      cache.set(k, got);
      if (current === k) paint(node, got);
    };

    if (img.complete) push(run);
    else img.addEventListener("load", () => push(run), { once: true });
  }

  apply(key);

  return {
    update(next: string) {
      if (next === current) return;

      current = next;
      apply(next);
    },
  };
}

// A view-transition-name has to be unique in the document, and the same video
// is routinely on screen twice (Continue Watching plus its own row). Only the
// tile the user actually activated carries the name, so the morph has exactly
// one old snapshot to pair with the player's poster.
export function morph(el: HTMLElement | null, key: string) {
  if (named && named !== el) named.style.removeProperty("view-transition-name");

  named = el;
  el?.style.setProperty("view-transition-name", `art-${key}`);
}
