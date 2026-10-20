import { register } from "node:module";
import { test } from "node:test";
import assert from "node:assert/strict";

// Source imports are extensionless (Vite resolution); retry with .ts so the
// real modules load unmodified.
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

const { finished, reclaim } = await import("../src/library/reclaim.ts");

const DAY = 86_400_000;
const NOW = 1_800_000_000_000;
const old = NOW - 45 * DAY;

const row = (rel: string, o: Record<string, unknown> = {}) => ({
  key: rel,
  name: rel.split("/").pop()!,
  rel,
  disk: true,
  size: 1e9,
  dur: 2820,
  t: 2538,
  lastAt: old,
  ...o,
});

test("finished = the player's own cutoff, measured on the real log", () => {
  // 47-min episode: cutoff min(300, 282). The last logged ping sits 282–294s out.
  assert.equal(finished(row("a", { t: 2820 - 282 })), true);
  assert.equal(finished(row("a", { t: 2820 - 294 })), true);
  // Stopped 8 minutes out: maybe skipped the outro, maybe not. Not suggested.
  assert.equal(finished(row("a", { t: 2820 - 492 })), false);
  // Reached the end once, now being rewatched from the top.
  assert.equal(finished(row("a", { t: 2538, resume: 292 })), false);
  // A long film parked on its credits (95.3%, 6 min left).
  assert.equal(finished(row("a", { dur: 7620, t: 7261 })), true);
  // No duration: unknowable, so never suggested.
  assert.equal(finished(row("a", { dur: undefined })), false);
  assert.equal(finished(row("a", { dur: 0 })), false);
});

test("whole folder when every file on disk is finished and idle", () => {
  const rows = [row("Show/e1.mp4"), row("Show/e2.mp4", { tor: "Show S01" })];
  const dirs = { Show: [{ name: "e1.mp4" }, { name: "e2.mp4" }] };

  const out = reclaim(rows, dirs, NOW);
  assert.equal(out.length, 1);
  assert.deepEqual(
    { id: out[0].id, n: out[0].n, size: out[0].size, redl: out[0].redl },
    { id: "d:Show", n: 2, size: 2e9, redl: false },
  );
});

test("one unfinished, idle-too-recently or unmatched file splits the folder", () => {
  const dirs = { Show: [{ name: "e1.mp4" }, { name: "e2.mp4" }] };

  const cases = [
    [row("Show/e1.mp4"), row("Show/e2.mp4", { t: 600 })],
    [row("Show/e1.mp4"), row("Show/e2.mp4", { lastAt: NOW - 3 * DAY })],
    // e2 is on disk but history never joined it: unknown means not finished.
    [row("Show/e1.mp4")],
  ];

  for (const rows of cases) {
    const out = reclaim(rows, dirs, NOW);
    assert.deepEqual(
      out.map((p) => p.id),
      ["f:Show/e1.mp4"],
    );
  }
});

test("root files are never offered as a folder; biggest first", () => {
  const rows = [
    row("a.mp4", { size: 1e9 }),
    row("b.mp4", { size: 3e9, tor: "B" }),
  ];
  const out = reclaim(rows, { ".": [{ name: "a.mp4" }, { name: "b.mp4" }] }, NOW);

  assert.deepEqual(
    out.map((p) => [p.id, p.redl]),
    [
      ["f:b.mp4", true],
      ["f:a.mp4", false],
    ],
  );
});

test("off-disk history rows are ignored", () => {
  const out = reclaim([row("Gone/e1.mp4", { disk: false })], { Gone: [] }, NOW);
  assert.equal(out.length, 0);
});
