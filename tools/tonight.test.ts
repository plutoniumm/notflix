// The picker's whole job is to be short, deterministic and honest about why
// each row is there. Those three are what the tests pin.
import { register } from "node:module";
import test from "node:test";
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

const { tonight, finished, MAX } = await import("../src/library/tonight.ts");

const ep = (dir: string, n: number) => ({
  name: `${dir} S01E${String(n).padStart(2, "0")}.mkv`,
  key: `${dir}-${n}`,
});

const data = [
  { dir: "Orville", files: [ep("Orville", 1), ep("Orville", 2), ep("Orville", 3)] },
  { dir: "Movies", files: [{ name: "Brazil 1985.mp4", key: "bz" }] },
];

const watched = (dir: string, name: string, t: number, dur: number, at: number) => ({
  dir, name, key: `${dir}-x`, t, dur, at,
});

test("the list never grows past four", () => {
  const big = Array.from({ length: 30 }, (_, i) => ({
    dir: "Orville",
    files: [{ name: `f${i}.mkv`, key: `k${i}` }],
  }));
  assert.ok(tonight(big, []).length <= MAX);
});

test("a finished episode puts the next one first", () => {
  const got = tonight(data, [
    watched("Orville", "Orville S01E01.mkv", 1400, 1440, 5),
  ]);

  assert.equal(got[0].name, "Orville S01E02.mkv");
  assert.equal(got[0].action, "Next episode");
  assert.match(got[0].reason, /^after /);
});

test("a part-watched episode is resumed, not restarted", () => {
  const got = tonight(data, [
    watched("Orville", "Orville S01E02.mkv", 600, 1440, 9),
  ]);

  assert.equal(got[0].name, "Orville S01E02.mkv");
  assert.equal(got[0].action, "Resume");
  assert.equal(Math.round(got[0].pct), 42);
  assert.match(got[0].left, /left$/);
});

test("credits count as finished so the queue can empty", () => {
  assert.equal(finished(watched("d", "n", 1400, 1440, 0)), true, "within 90s of the end");
  assert.equal(finished(watched("d", "n", 1358, 1440, 0)), true, "past 93%");
  assert.equal(finished(watched("d", "n", 600, 1440, 0)), false);
  assert.equal(finished({ dir: "d", name: "n", key: "k", t: 600, at: 0 }), false, "no duration");
});

test("the title is a show name, not a release string", () => {
  const got = tonight(data, [
    watched("Orville", "Orville S01E02.mkv", 600, 1440, 9),
  ]);

  assert.equal(got[0].title, "Orville", "folder title wins over the filename");
  assert.equal(got[0].tag, "S01E02", "the episode number is parsed out, not left inline");
  assert.ok(!got[0].title.includes(".mkv"));
});

test("every pick states a reason and an end", () => {
  for (const p of tonight(data, [watched("Orville", "Orville S01E01.mkv", 1400, 1440, 5)])) {
    assert.ok(p.reason, `no reason on ${p.name}`);
    assert.ok(p.title, `no title on ${p.name}`);
    assert.ok(p.key, `no key on ${p.name}`);
  }
});

test("the last episode of a folder says so", () => {
  const got = tonight(data, [
    watched("Orville", "Orville S01E02.mkv", 1430, 1440, 5),
  ]);

  assert.equal(got[0].name, "Orville S01E03.mkv");
  assert.equal(got[0].end, "last episode");
});

test("a mid-folder episode counts what is left after it", () => {
  const got = tonight(data, [
    watched("Orville", "Orville S01E01.mkv", 1400, 1440, 5),
  ]);

  assert.equal(got[0].end, "1 more after this");
});

test("a loose film gets no episode-count cue", () => {
  const got = tonight([{ dir: ".", files: [{ name: "Brazil 1985.mp4", key: "bz" }] }], []);

  assert.equal(got[0].end, "");
  assert.equal(got[0].sub, "");
});

test("nothing watched still offers something unopened", () => {
  const got = tonight(data, []);

  assert.ok(got.length > 0);
  assert.equal(got[0].action, "Start");
  assert.equal(got[0].reason, "not started");
});

test("an empty library yields an empty list, not a crash", () => {
  assert.deepEqual(tonight([], []), []);
  assert.deepEqual(tonight([{ dir: "x", files: [] }], []), []);
});

test("the same state always produces the same list", () => {
  const c = [watched("Orville", "Orville S01E01.mkv", 1400, 1440, 5)];
  const a = tonight(data, c).map((p) => p.name);
  const b = tonight(data, c).map((p) => p.name);

  assert.deepEqual(a, b, "a shuffle would be a variable reward");
});

test("an already-seen next episode is not offered again", () => {
  const got = tonight(data, [
    watched("Orville", "Orville S01E01.mkv", 1400, 1440, 9),
    watched("Orville", "Orville S01E02.mkv", 300, 1440, 5),
  ]);

  // E02 is part-watched, so it is a Resume — never a "next episode" suggestion.
  assert.equal(got[0].name, "Orville S01E02.mkv");
  assert.equal(got[0].action, "Resume");
  assert.equal(got.filter((p) => p.name === "Orville S01E02.mkv").length, 1);
});
