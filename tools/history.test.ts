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

const { runs, gaps, rangeLabel, build, agg, seen } = await import("../src/library/history.ts");

test("consecutive episodes collapse, gaps split", () => {
  const cases: [number[], [number, number][]][] = [
    [[], []],
    [[7], [[7, 7]]],
    [
      [1, 2, 3],
      [[1, 3]],
    ],
    // The whole point: a hole must break the run, not be smoothed over.
    [
      [1, 2, 3, 5, 6],
      [
        [1, 3],
        [5, 6],
      ],
    ],
    // Unsorted and duplicated input (two rips of one episode) is normal.
    [
      [3, 1, 2, 2],
      [[1, 3]],
    ],
    [
      [1, 3, 5],
      [
        [1, 1],
        [3, 3],
        [5, 5],
      ],
    ],
  ];

  for (const [eps, want] of cases)
    assert.deepEqual(runs(eps), want, JSON.stringify(eps));

  assert.deepEqual(gaps([1, 2, 3]), []);
  assert.deepEqual(gaps([1, 3, 6]), [2, 4, 5]);
  assert.deepEqual(gaps([5]), []);
  // One hole in a 1000-episode anime must not build a 1000-long array.
  assert.ok(gaps([1, 5000]).length <= 400);
});

test("range label names the hole", () => {
  assert.equal(rangeLabel(1, [1, 2, 3]), "S01E01–E03");
  assert.equal(
    rangeLabel(1, [1, 2, 3, 4, 5, 6, 7, 8, 9, 11, 12]),
    "S01E01–E09, E11–E12 (E10 missing)",
  );
  assert.equal(rangeLabel(2, [5]), "S02E05");
  assert.equal(rangeLabel(undefined, [1, 2]), "E01–E02");
  assert.equal(rangeLabel(1, []), "");
  // Truncated gap list, so one row can't run off the screen.
  const many = rangeLabel(1, [1, 20]);
  assert.match(many, /\+12 missing\)$/);
});

const row = (name: string, extra: Record<string, unknown> = {}) => ({
  key: name,
  name,
  disk: true,
  ...extra,
});

test("tree groups show → season → episode, films stay flat", () => {
  const nodes = build([
    row("Himouto Umaru chan S01E01 YURASUKA.mp4"),
    row("Himouto Umaru chan S01E02 YURASUKA.mp4"),
    row("Himouto Umaru chan S02E11 YURASUKA.mp4"),
    row("Freaky.Friday.2003.1080p.BluRay.x264.mp4"),
  ]);

  const show = nodes.find((n) => n.label.startsWith("Himouto"));
  assert.ok(show, "show node");
  assert.equal(show!.kids.length, 2, "two seasons");
  assert.equal(show!.kids[0].sub, "S01E01–E02");
  assert.equal(show!.kids[0].kids.length, 2);
  assert.equal(show!.kids[0].kids[0].label, "S01E01");

  const film = nodes.find((n) => n.label.startsWith("Freaky"));
  assert.ok(film, "film node");
  assert.equal(film!.kids.length, 0, "a film gets no season level");
  assert.ok(film!.leaf, "a film node is its own row");
});

test("aggregation rolls the two independent dates up separately", () => {
  const a = agg([
    row("A S01E01.mp4", { dlAt: 100, size: 10 }),
    row("A S01E02.mp4", { dlAt: 50, lastAt: 900, firstAt: 800, sess: 3, t: 50, dur: 100 }),
    row("A S01E03.mp4", { disk: false, lastAt: 400, firstAt: 300, sess: 1 }),
  ]);

  assert.equal(a.n, 3);
  assert.equal(a.disk, 2);
  assert.equal(a.size, 10);
  assert.equal(a.dlAt, 100, "newest download");
  assert.equal(a.lastAt, 900, "newest watch — never derived from the download");
  assert.equal(a.firstAt, 300, "oldest watch");
  assert.equal(a.sess, 4);
  // watched is BINARY and strict: E02 sits at exactly 50%, which is not "more
  // than 50", and E03 has no duration to judge against. Neither counts.
  assert.equal(a.watched, 0, "exactly 50% is not watched");
  assert.equal(a.pct, 50);
});

test("watched is a binary call at the halfway mark, not a percentage", () => {
  const at = (t: number, dur = 100) =>
    row("X.mp4", { t, dur, sess: 1, lastAt: 1 });

  assert.equal(seen(at(51)), true, "just past half counts");
  assert.equal(seen(at(50)), false, "exactly half does not");
  assert.equal(seen(at(49)), false);
  assert.equal(seen(at(100)), true);
  assert.equal(seen(at(0)), false);
  // No duration means nothing to judge, so it can never read as watched.
  assert.equal(seen(row("Y.mp4", { sess: 9, lastAt: 1 })), false);

  const a = agg([at(90), at(60), at(10), row("Z.mp4", { sess: 2, lastAt: 1 })]);
  assert.equal(a.watched, 2, "two of four are past halfway");
  assert.equal(a.n, 4);
});
