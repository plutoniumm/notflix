// Run: node --test tools/fmt.test.ts   (node >= 23 strips the types itself)
//
// Guards the drift that made this module necessary: three duration
// formatters, two byte formatters and two relative-time formatters that
// disagreed with each other on the same screen.
import { test } from "node:test";
import assert from "node:assert/strict";

import { ago, bytes, dur, durWords, pct, rate } from "../src/core/fmt.ts";

// The three implementations fmt.ts replaced, kept verbatim so the bugs they
// shipped stay pinned to a failing-then-fixed expectation.
const legacy = {
  fmtTime(s: number) {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = Math.floor(s % 60);
    if (h > 0)
      return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
    return `${m}:${String(sec).padStart(2, "0")}`;
  },
  continuesFmt(t: number) {
    const h = Math.floor(t / 3600);
    return h > 0
      ? `${h}h ${Math.floor((t % 3600) / 60)}m`
      : `${Math.floor(t / 60)}m`;
  },
  fmtElapsed(ms: number) {
    if (ms < 1000) return "<1s";
    const s = Math.floor(ms / 1000);
    if (s < 60) return `${s}s`;
    const m = Math.floor(s / 60);
    const rem = s % 60;
    return rem > 0 ? `${m}m ${rem}s` : `${m}m`;
  },
  fmtBytes(b: number) {
    if (b >= 1e12) return (b / 1e12).toFixed(1) + " TB";
    if (b >= 1e9) return (b / 1e9).toFixed(1) + " GB";
    return (b / 1e6).toFixed(0) + " MB";
  },
};

test("the drift that was", () => {
  assert.equal(legacy.fmtElapsed(7325_000), "122m 5s");
  assert.equal(legacy.continuesFmt(45), "0m");
  assert.equal(legacy.fmtTime(NaN), "NaN:NaN");
  assert.equal(legacy.fmtTime(-5), "-1:-5");
  assert.equal(legacy.fmtBytes(400_000), "0 MB");
});

test("dur", () => {
  assert.equal(dur(3661), "1:01:01");
  assert.equal(dur(3600), "1:00:00");
  assert.equal(dur(45), "0:45");
  assert.equal(dur(59.9), "0:59");
  assert.equal(dur(0), "0:00");
  assert.equal(dur(NaN), "0:00");
  assert.equal(dur(-5), "0:00");
  assert.equal(dur(Infinity), "0:00");
});

test("durWords", () => {
  assert.equal(durWords(3661), "1h 1m");
  assert.equal(durWords(7325), "2h 2m");
  assert.equal(durWords(3600), "1h");
  assert.equal(durWords(90), "1m 30s");
  assert.equal(durWords(45), "45s");
  assert.equal(durWords(0), "0s");
  assert.equal(durWords(NaN), "0s");
  assert.equal(durWords(-1), "0s");
});

test("a two-hour library run reports hours", () => {
  assert.equal(durWords(7325_000 / 1000), "2h 2m");
  assert.equal(durWords(3661_000 / 1000), "1h 1m");
});

test("a 45s clip reads the same everywhere", () => {
  assert.equal(dur(45), "0:45");
  assert.equal(durWords(45), "45s");
});

test("bytes", () => {
  assert.equal(bytes(400_000), "400 KB");
  assert.equal(bytes(999), "999 B");
  assert.equal(bytes(1000), "1 KB");
  assert.equal(bytes(5e6), "5 MB");
  assert.equal(bytes(1.5e9), "1.5 GB");
  assert.equal(bytes(2.4e12), "2.4 TB");
  assert.equal(bytes(0), "0 B");
  assert.equal(bytes(-1), "0 B");
  assert.equal(bytes(NaN), "—");
});

test("rate", () => {
  assert.equal(rate(400_000), "400 KB/s");
  assert.equal(rate(0), "0 B/s");
  assert.equal(rate(12e6), "12 MB/s");
});

test("ago", () => {
  const now = Date.now();
  assert.equal(ago(now), "just now");
  assert.equal(ago(now - 59_000), "just now");
  assert.equal(ago(now - 5 * 60_000), "5m ago");
  assert.equal(ago(now - 2 * 3600_000), "2h ago");
  assert.equal(ago(now - 3 * 86400_000), "3d ago");
  assert.equal(ago(now - 60 * 86400_000), "2mo ago");
  assert.equal(ago(now - 400 * 86400_000), "1y ago");
  assert.equal(ago(now + 10_000), "just now");
  assert.equal(ago(NaN), "just now");
});

test("pct", () => {
  assert.equal(pct(12.345), "12%");
  assert.equal(pct(12.345, 1), "12.3%");
  assert.equal(pct(100), "100%");
  assert.equal(pct(NaN), "0%");
});
