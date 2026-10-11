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

const { episodic, ordinals } = await import("../src/library/series.ts");

// Every case below is a real folder from the live library (plus the two
// near-miss shapes the heuristic exists to separate). Home styles a series
// folder as an ordered, rail-numbered strip and a movie folder as an unordered
// block, so a wrong answer here is a wrong answer on screen.
const SERIES: [string, string[]][] = [
  [
    "S/E marker",
    [
      "Himouto Umaru chan S01E01 10 Bit Dual Audio FLAC2 0 YURASUKA.mp4",
      "Himouto Umaru chan S01E02 10 Bit Dual Audio FLAC2 0 YURASUKA.mp4",
      "Himouto Umaru chan S01E03 10 Bit Dual Audio FLAC2 0 YURASUKA.mp4",
    ],
  ],
  [
    "dash episode, no season",
    [
      "City Hunter - 05 (BDRip 1440x1080p x265 HEVC FLAC)[sxales].mkv",
      "City Hunter - 06 (BDRip 1440x1080p x265 HEVC FLAC)[sxales].mkv",
      "City Hunter - 07 (BDRip 1440x1080p x265 HEVC FLAC)[sxales].mkv",
    ],
  ],
  [
    // No S/E and no " - " either: only the shared stem + numeric tail says show.
    "bare trailing number",
    ["Ao Haru Ride 01.mp4", "Ao Haru Ride 02.mp4", "Ao Haru Ride 10.mp4"],
  ],
  [
    "bare number mid-name",
    [
      "Wangan Midnight 01 Devil Z.mp4",
      "Wangan Midnight 02 Akio.mp4",
      "Wangan Midnight 03 Reina.mp4",
    ],
  ],
  [
    // One stray film in a folder of doc episodes must not flip the verdict.
    "episodes plus one stray",
    [
      "World War II With Tom Hanks S01E16 Resistance NOW AAC2 0 H 264.mp4",
      "World War II With Tom Hanks S01E17 Endgame NOW AAC2 0 H 264.mp4",
      "World War II With Tom Hanks S01E18 Reckoning NOW AAC2 0 H 264.mp4",
      "Ram_Ke_Naam_In_the_name_of_God_-_a_documentary_by_Anand_Patwardhan_1991.mp4",
    ],
  ],
];

const PILES: [string, string[]][] = [
  [
    "library root",
    [
      "Brazil 1985 Directors Cut BONE.mp4",
      "Companion.2025.1080p.WEBRip.x264.AAC5.1-[YTS.MX].mp4",
      "Seven.Samurai.1954.CRITERION.1080p.BluRay.x264.anoXmous_.mp4",
      "Udta.Punjab.2016.1080p.BluRay.x264.Hindi.AAC-ETRG.mp4",
    ],
  ],
  [
    "decade folder",
    [
      "Cocktail.2012.1080p.BluRay.x264-[YTS.AM].mp4",
      "Killers.Of.The.Flower.Moon.2023.1080p.WEBRip.x265.10bit.AAC5.1.mp4",
      "8Mile BrRip 600MB YIFY.mp4",
    ],
  ],
  [
    // The stem clause's reason to demand a DIGIT: a franchise shares a long
    // prefix but differs by words, and these are films, not episodes.
    "one franchise, many films",
    [
      "Harry Potter and the Philosophers Stone 2001.mp4",
      "Harry Potter and the Chamber of Secrets 2002.mp4",
      "Harry Potter and the Prisoner of Azkaban 2004.mp4",
    ],
  ],
  ["a lone file is neither", ["Ran 1985 Remastered DTS SARTRE.mp4"]],
];

test("series folders are recognised", () => {
  for (const [label, names] of SERIES)
    assert.equal(episodic(names), true, label);
});

test("movie folders are not", () => {
  for (const [label, names] of PILES)
    assert.equal(episodic(names), false, label);
});

test("rail labels are ordinals in list order", () => {
  assert.deepEqual(ordinals(SERIES[1][1]), ["05", "06", "07"]);

  // No parseable episode: fall back to position, never a blank rail.
  assert.deepEqual(ordinals(["a.mp4", "b.mp4", "c.mp4"]), ["01", "02", "03"]);

  // A folder spanning seasons needs the season, or S01E01 and S02E01 both
  // read "01" and the rail stops being a sequence.
  assert.deepEqual(ordinals(["JoJo S01E26.mp4", "JoJo S02E01.mp4"]), [
    "1·26",
    "2·01",
  ]);
  assert.deepEqual(ordinals(["JoJo S01E25.mp4", "JoJo S01E26.mp4"]), [
    "25",
    "26",
  ]);
});
