// A gap is an episode missing from BETWEEN ones on disk. The user deletes what
// they have watched, so a missing head is history, and anything with a watch
// record was had and let go. Those two rules are what keep the marker honest.
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

const { gaps, holes, order, ordinals } = await import("../src/library/series.ts");
const { parseName } = await import("../src/library/torrentName.ts");
const { tonight } = await import("../src/library/tonight.ts");

const se = (s: number, e: number) =>
  `The.Sopranos.S${String(s).padStart(2, "0")}E${String(e).padStart(2, "0")}.1080p.BluRay.x265-RARBG.mp4`;
const dir = "The.Sopranos.S01.1080p.BluRay.x265-RARBG";

const labels = (g: { label: string }[]) => g.map((x) => x.label);

test("a missing head is never a gap", () => {
  assert.deepEqual(gaps(dir, [5, 6, 7].map((e) => se(1, e))), []);
});

test("a missing tail is never a gap without a declared count", () => {
  assert.deepEqual(gaps(dir, [1, 2].map((e) => se(1, e))), []);
});

test("an interior hole is flagged after the episode it follows", () => {
  const names = [3, 4, 5, 9].map((e) => se(1, e));
  const got = gaps(dir, names);
  assert.deepEqual(labels(got), ["06–08"]);
  assert.equal(got[0].tag, "S01E06–E08");
  assert.equal(got[0].after, 2);
  assert.equal(got[0].term, "The Sopranos S01");

  const one = gaps(dir, [1, 3].map((e) => se(1, e)));
  assert.equal(one[0].term, "The Sopranos S01E02");
});

test("a watched hole is not a gap, wherever the record lives now", () => {
  const names = [3, 4, 5, 9].map((e) => se(1, e));
  const watched = [`Some Old Folder/${se(1, 7)}`, `elsewhere/The Sopranos S01E06 Renamed.mp4`];
  assert.deepEqual(labels(gaps(dir, names, watched)), ["08"]);
});

test("a multi-episode file covers every episode it names", () => {
  assert.equal(parseName("Show.S01E01-E02.mkv").upto, 2);
  assert.equal(parseName("Show.S01E03E04.mkv").upto, 4);
  assert.equal(parseName("Show S01E05 - E06 1080p.mkv").upto, 6);
  assert.equal(parseName("Show.S01E05-720p.mkv").upto, undefined);

  const names = ["Show.S01E01-E02.mkv", "Show.S01E03.mkv", "Show.S01E05.mkv"];
  assert.deepEqual(labels(gaps("Show S01", names)), ["04"]);
});

test("absolute numbering reads off the stem, and a pack range declares the tail", () => {
  const names = ["Bleach 145.mp4", "Bleach 146.mp4", "Bleach 150.mp4"];
  const bare = gaps("[Judas] Bleach 140-167 [BD 1080p]", names);
  assert.deepEqual(labels(bare), ["147–149", "151–167"]);
  assert.equal(bare[0].term, "Bleach 147");

  assert.deepEqual(labels(gaps("Bleach", names)), ["147–149"]);
  assert.deepEqual(labels(gaps("Bleach", names, ["Bleach 147.mp4", "x/Bleach 148.mp4"])), ["149"]);

  // "Show 10".."Show 14" share the stem "Show 1"; read there, 10 would be 0.
  const teens = ["Ao Haru Ride 10.mp4", "Ao Haru Ride 11.mp4", "Ao Haru Ride 14.mp4"];
  assert.deepEqual(labels(gaps("Ao Haru Ride", teens)), ["12–13"]);
  assert.deepEqual(ordinals(teens), ["10", "11", "14"]);
});

test("a season missing between two present ones", () => {
  const pack = "[Judas] JoJo (Seasons 1-4)";
  const names = ["JoJo S01E08.mp4", "JoJo S01E10.mp4", "JoJo S03E01.mp4"];
  const got = gaps(pack, names);
  // In a multi-season folder the rail reads "1·09", so the marker does too.
  assert.deepEqual(labels(got), ["1·09", "S02"]);
  assert.equal(got[1].after, 1);
  assert.deepEqual(labels(gaps(pack, names, ["JoJo S02E30.mp4"])), ["1·09"]);

  const seasons = [1, 3, 4].map((n) => ({
    dir: `The.Sopranos.S0${n}.1080p`,
    n,
    files: [{ name: se(n, 1) }],
  }));
  assert.deepEqual(
    holes("The Sopranos", seasons).map((h) => [h.label, h.term]),
    [["S2", "The Sopranos S02"]],
  );
  assert.deepEqual(holes("The Sopranos", seasons, [`The.Sopranos.S02.1080p/${se(2, 4)}`]), []);
  assert.deepEqual(holes("The Sopranos", seasons.slice(1)), []);

  const packs = [
    { dir: "[Judas] Bleach 056-111 [BD]", n: 1056, files: [{ name: "Bleach 084.mp4" }, { name: "Bleach 085.mp4" }] },
    { dir: "[Judas] Bleach 140-167 [BD]", n: 1140, files: [{ name: "Bleach 159.mp4" }, { name: "Bleach 160.mp4" }] },
  ];
  assert.deepEqual(holes("Bleach", packs).map((h) => h.label), ["112–139"]);
  assert.deepEqual(holes("Bleach", packs, ["Bleach 120.mp4"]).map((h) => h.label), ["112–119", "121–139"]);
});

test("episode order beats filename order", () => {
  const names = [
    "Smoking Behind the Supermarket With You S01E01 Smoke 1.mp4",
    "Smoking Behind the Supermarket With You S01E03 Smoke 3.mp4",
    "Smoking Behind the Supermarket with You S01E02v2.mp4",
  ];
  assert.deepEqual(order(names), [names[0], names[2], names[1]]);
  assert.deepEqual(gaps("Smoking Behind the Supermarket With You", names), []);

  const mixed = ["Ram Ke Naam 1991.mp4", "WWII S01E14.mp4"];
  assert.deepEqual(order(mixed), mixed);
});

test("tonight names the missing episode instead of skipping past it", () => {
  const data = [{ dir, files: [1, 2, 4].map((e) => ({ name: se(1, e), key: `k${e}` })) }];
  const done = [{ dir, name: se(1, 2), key: "k2", t: 1400, dur: 1440, at: 5 }];

  const blind = tonight(data, done)[0];
  assert.equal(blind.reason, "after S01E02");

  const [p] = tonight(data, done, []);
  assert.equal(p.reason, "S01E03 missing");
  assert.equal(p.find, "The Sopranos S01E03");

  assert.equal(tonight(data, done, [`gone/${se(1, 3)}`])[0].reason, "after S01E02");
});
