// Pins the bug that made fuzzy.ts necessary: provider results arrived and were
// then filtered out locally unless the query was an exact substring.
import { test } from "node:test";
import assert from "node:assert/strict";

import { matches, norm, tokens } from "../src/core/fuzzy.ts";

const REAL = "Smoking.Behind.the.Supermarket.With.You.S01E01.Smoke.1.1080p.NF.WEB-DL.DUAL.AAC2.0.H.264.MSubs-ToonsHub";

test("the reported failure: words out of order across separators", () => {
  // What the old filter did, kept so the regression stays pinned.
  assert.equal(REAL.toLowerCase().includes("smoking supermarket"), false);
  assert.equal(matches("smoking supermarket", REAL), true);
});

test("separators are interchangeable", () => {
  for (const q of ["smoking.behind", "smoking behind", "smoking_behind", "SMOKING-BEHIND"]) {
    assert.equal(matches(q, REAL), true, q);
  }
});

test("episode codes and years still work", () => {
  assert.equal(matches("s01e01", REAL), true);
  assert.equal(matches("smoking s01e01", REAL), true);
  assert.equal(matches("1080p", REAL), true);
});

test("every token must hit — not a loose similarity score", () => {
  assert.equal(matches("smoking tokyo", REAL), false);
  assert.equal(matches("breaking bad", REAL), false);
  // a token that is only a scattered subsequence must NOT match
  assert.equal(matches("smkng", REAL), false);
});

test("fields are OR'd per token", () => {
  // parsed title and raw name each carry half the query
  assert.equal(matches("smoking toonshub", "Smoking Behind the Supermarket With You", REAL), true);
});

test("empty query matches everything, blank-ish query too", () => {
  assert.equal(matches("", REAL), true);
  assert.equal(matches("   ", REAL), true);
  assert.equal(matches("...", REAL), true);
});

test("norm and tokens", () => {
  assert.equal(norm("Smoking.Behind-the_Supermarket"), "smoking behind the supermarket");
  assert.deepEqual(tokens(" The..Fix "), ["the", "fix"]);
  assert.deepEqual(tokens("---"), []);
});
