// Ambilight tests. Run: node --test tools/ambient.test.ts
//
// The colour reduction is pure, so it can be driven with synthetic ImageData.
// The sampler can't be pointed at a real decoder, but every rule that keeps it
// off the CPU — stop on pause, stop on hidden, never start under reduced
// motion, die silently on a tainted canvas — is scheduler behaviour, and that
// runs unmodified here against a stub video.
import { test } from "node:test";
import assert from "node:assert/strict";
import { register } from "node:module";

register(
  "data:text/javascript," +
    encodeURIComponent(`
      export async function resolve(spec, ctx, next) {
        try {
          return await next(spec, ctx);
        } catch (e) {
          if (spec.startsWith(".")) return await next(spec + ".ts", ctx);
          throw e;
        }
      }
    `),
  import.meta.url,
);

const docOn: Record<string, Function[]> = {};
const doc = {
  hidden: false,
  addEventListener(t: string, fn: Function) {
    (docOn[t] ??= []).push(fn);
  },
  removeEventListener(t: string, fn: Function) {
    docOn[t] = (docOn[t] ?? []).filter((f) => f !== fn);
  },
};
const visibility = () => {
  for (const fn of docOn.visibilitychange ?? []) fn();
};
(globalThis as any).document = doc;

const { analyze, hsl, css, Ambient } = await import("../src/player/ambient.ts");

const fill = (px: number, rgb: [number, number, number]) => {
  const out = new Uint8ClampedArray(px * 4);
  for (let i = 0; i < px; i++) {
    out[i * 4] = rgb[0];
    out[i * 4 + 1] = rgb[1];
    out[i * 4 + 2] = rgb[2];
    out[i * 4 + 3] = 255;
  }

  return out;
};

const splice = (a: Uint8ClampedArray, b: Uint8ClampedArray) => {
  const out = new Uint8ClampedArray(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);

  return out;
};

const hueOf = (s: string) => {
  const [r, g, b] = s.match(/\d+/g)!.map(Number);

  return hsl(r, g, b)[0] * 360;
};
const near = (a: number, b: number, tol = 40) => {
  const d = Math.abs(a - b) % 360;

  return Math.min(d, 360 - d) <= tol;
};

test("a single-hue frame yields that hue", () => {
  const [a] = analyze(fill(144, [220, 30, 40]));
  assert.ok(near(hueOf(css(a)), 0), `expected red, got ${css(a)}`);

  const [g] = analyze(fill(144, [30, 200, 60]));
  assert.ok(near(hueOf(css(g)), 120), `expected green, got ${css(g)}`);
});

test("a two-hue frame yields two separated hues", () => {
  const [a, b] = analyze(splice(fill(90, [220, 30, 40]), fill(54, [40, 60, 230])));
  const [ha, hb] = [hueOf(css(a)), hueOf(css(b))];

  // Larger field wins first slot; the second must be a genuinely different
  // colour, not a near-duplicate of the first.
  assert.ok(near(ha, 0), `first should be red, got ${css(a)}`);
  assert.ok(near(hb, 225), `second should be blue, got ${css(b)}`);
  assert.ok(!near(ha, hb, 60), "the pair collapsed into one hue");
});

test("chroma beats area", () => {
  // Mostly flat grey with a small vivid accent — the accent is the light in
  // the room, the grey is the wall.
  const [a] = analyze(splice(fill(130, [96, 96, 100]), fill(14, [255, 120, 0])));
  assert.ok(near(hueOf(css(a)), 28), `expected orange, got ${css(a)}`);
});

test("black and grey frames degrade to neutrals, never NaN", () => {
  for (const src of [fill(144, [0, 0, 0]), fill(144, [128, 128, 128])]) {
    const [a, b] = analyze(src);
    for (const c of [...a, ...b]) {
      assert.ok(Number.isFinite(c), "channel is not finite");
      assert.ok(c >= 0 && c <= 255, `channel out of range: ${c}`);
    }
  }
});

test("output is always a parseable CSS colour", () => {
  const [a, b] = analyze(fill(144, [12, 200, 210]));
  for (const c of [css(a), css(b)]) assert.match(c, /^rgb\(\d+ \d+ \d+\)$/);
});

// --- scheduler ---

class FakeVideo {
  paused = true;
  videoWidth = 1920;
  #on: Record<string, Function[]> = {};

  addEventListener(t: string, fn: Function) {
    (this.#on[t] ??= []).push(fn);
  }
  removeEventListener(t: string, fn: Function) {
    this.#on[t] = (this.#on[t] ?? []).filter((f) => f !== fn);
  }
  emit(t: string) {
    for (const fn of this.#on[t] ?? []) fn();
  }
}

const stubCanvas = (throws = false) => () => ({
  getContext: () => ({
    drawImage() {},
    getImageData() {
      if (throws) throw new Error("tainted");

      return { data: fill(144, [200, 40, 30]) };
    },
  }),
});

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));
const rig = (opts: any = {}) => {
  const seen: string[] = [];
  const amb = new Ambient((a) => seen.push(a), {
    every: 4,
    canvas: stubCanvas(),
    reduced: () => false,
    ...opts,
  });

  return { amb, seen };
};

test("sampling runs while playing and stops on pause", async () => {
  const { amb, seen } = rig();
  const v = new FakeVideo();
  amb.attach(v as any);

  v.paused = false;
  v.emit("play");
  await wait(40);
  assert.ok(seen.length > 0, "never sampled while playing");
  assert.ok(amb.running);

  v.paused = true;
  v.emit("pause");
  assert.equal(amb.running, false);
  const at = seen.length;
  await wait(40);
  assert.equal(seen.length, at, "kept sampling after pause");

  amb.detach();
});

test("sampling stops while the document is hidden", async () => {
  const { amb, seen } = rig();
  const v = new FakeVideo();
  amb.attach(v as any);
  v.paused = false;
  v.emit("play");
  await wait(40);
  assert.ok(seen.length > 0);

  doc.hidden = true;
  visibility();
  assert.equal(amb.running, false, "kept running with the tab hidden");
  const at = seen.length;
  await wait(40);
  assert.equal(seen.length, at, "kept sampling with the tab hidden");

  // A play event arriving while hidden must not restart it either.
  v.emit("play");
  await wait(40);
  assert.equal(seen.length, at, "restarted while hidden");
  assert.equal(amb.running, false);

  doc.hidden = false;
  visibility();
  await wait(40);
  assert.ok(seen.length > at, "did not resume when the tab came back");

  amb.detach();
});

test("reduced motion never starts the sampler", async () => {
  const { amb, seen } = rig({ reduced: () => true });
  const v = new FakeVideo();
  v.paused = false;
  amb.attach(v as any);
  v.emit("play");
  await wait(40);
  assert.equal(seen.length, 0);
  assert.equal(amb.running, false);
});

test("a tainted canvas stops the sampler silently", async () => {
  const { amb, seen } = rig({ canvas: stubCanvas(true) });
  const v = new FakeVideo();
  amb.attach(v as any);
  v.paused = false;
  v.emit("play");
  await wait(40);
  assert.equal(seen.length, 0, "emitted from a failed read");
  assert.equal(amb.running, false, "kept retrying a read that cannot succeed");
});

test("detach unhooks the video", async () => {
  const { amb, seen } = rig();
  const v = new FakeVideo();
  amb.attach(v as any);
  amb.detach();

  v.paused = false;
  v.emit("play");
  await wait(40);
  assert.equal(seen.length, 0);
});

test("smoothing walks toward the target instead of snapping", async () => {
  const { amb, seen } = rig();
  const v = new FakeVideo();
  amb.attach(v as any);
  v.paused = false;
  v.emit("play");
  await wait(60);
  v.emit("pause");
  v.paused = true;

  assert.ok(seen.length >= 3, `too few samples: ${seen.length}`);
  const first = hsl(...(seen[0].match(/\d+/g)!.map(Number) as [number, number, number]))[2];
  const last = hsl(...(seen.at(-1)!.match(/\d+/g)!.map(Number) as [number, number, number]))[2];
  assert.ok(last > first, "the wash never moved toward the frame");
});
