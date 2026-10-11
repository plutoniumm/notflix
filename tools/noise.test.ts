// The ambient field exists to not look like an animation. Both properties that
// buy that are cheap to pin: the walk has to be 1/f (low-frequency energy, no
// jitter) and the breath has to be asymmetric (or it reads as a metronome).
import test from "node:test";
import assert from "node:assert/strict";

const { Pink, breath, BREATH_S, lerp } = await import("../src/core/noise.ts");

// Deterministic stand-in for Math.random so a failure is reproducible.
function rng(seed: number) {
  let s = seed >>> 0;

  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
}

const series = (n: number, gen: () => number) =>
  Array.from({ length: n }, gen);

const varianceOfDiffs = (xs: number[]) => {
  const d = xs.slice(1).map((v, i) => v - xs[i]);
  const mean = d.reduce((a, b) => a + b, 0) / d.length;

  return d.reduce((a, b) => a + (b - mean) ** 2, 0) / d.length;
};

test("pink noise stays inside the range the CSS calc assumes", () => {
  const p = new Pink(rng(7));
  for (const v of series(4000, () => p.next())) {
    assert.ok(v >= -1 && v <= 1, `out of range: ${v}`);
  }
});

test("pink noise carries less high-frequency energy than white", () => {
  const r = rng(11);
  const p = new Pink(rng(11));
  const pink = series(4000, () => p.next());
  const white = series(4000, () => r() * 2 - 1);

  // 1/f means neighbouring samples are correlated; white noise has none, so its
  // first difference is far more energetic. This is the whole "no jitter" claim.
  assert.ok(
    varianceOfDiffs(pink) < varianceOfDiffs(white) / 8,
    `pink ${varianceOfDiffs(pink)} vs white ${varianceOfDiffs(white)}`,
  );
});

test("pink noise actually wanders instead of sitting at zero", () => {
  const p = new Pink(rng(3));
  const xs = series(2000, () => p.next());

  assert.ok(Math.max(...xs) - Math.min(...xs) > 0.4, "field would look static");
});

test("breath is bounded and returns to rest each cycle", () => {
  for (let i = 0; i <= 400; i++) {
    const v = breath((i / 400) * BREATH_S * 3);
    assert.ok(v >= 0 && v <= 1, `breath out of range at ${i}: ${v}`);
  }

  assert.ok(breath(0) < 0.001, "cycle starts at rest");
  assert.ok(breath(BREATH_S) < 0.001, "and comes back to it");
  assert.ok(breath(BREATH_S * 0.4) > 0.999, "peaks at the end of the inhale");
});

test("breath is asymmetric: a symmetric pulse reads as a metronome", () => {
  // Rise occupies 40% of the cycle, fall 60% — so the curve is NOT its own
  // mirror. Compare equal offsets either side of the peak.
  const peak = BREATH_S * 0.4;
  const d = BREATH_S * 0.2;

  assert.ok(
    Math.abs(breath(peak - d) - breath(peak + d)) > 0.1,
    "rise and fall have the same shape",
  );
});

test("breath handles negative and wrapped time", () => {
  assert.ok(breath(-BREATH_S * 0.5) >= 0, "no NaN before zero");
  assert.equal(breath(BREATH_S * 7).toFixed(6), breath(0).toFixed(6));
});

test("lerp reaches both ends", () => {
  assert.equal(lerp(2, 10, 0), 2);
  assert.equal(lerp(2, 10, 1), 10);
  assert.equal(lerp(2, 10, 0.25), 4);
});
