import { register } from "node:module";
import test from "node:test";
import assert from "node:assert/strict";

register(
  "data:text/javascript," +
    encodeURIComponent(`
      export async function resolve(spec, ctx, next) {
        try { return await next(spec, ctx); }
        catch (e) { if (spec.startsWith(".")) return await next(spec + ".ts", ctx); throw e; }
      }
    `),
  import.meta.url,
);

const { subToggle } = await import("../src/player/subtrack.ts");

const t = (label: string, mode = "hidden") => ({ label, mode });

test("with one track showing, everything goes off", () => {
  const subs = [t("English", "showing"), t("Spanish")];
  const { show, remember } = subToggle(subs, "");

  assert.equal(show, null, "a press with subs on must turn them OFF");
  assert.equal(remember, "English", "and remember which to bring back");
});

test("the old bug: never turn the other tracks on", () => {
  const subs = [t("English", "showing"), t("Spanish"), t("French")];
  const { show } = subToggle(subs, "");

  // Inverting each track's mode put Spanish and French on screen at once.
  assert.equal(show, null);
  assert.equal(subs.filter((s) => s === show).length, 0);
});

test("pressing again restores the track that was on", () => {
  const subs = [t("English"), t("Spanish")];
  const { show } = subToggle(subs, "Spanish");

  assert.equal(show?.label, "Spanish", "not just the first track");
});

test("a forgotten or missing label falls back to the first track", () => {
  const subs = [t("English"), t("Spanish")];
  assert.equal(subToggle(subs, "").show?.label, "English");
  assert.equal(subToggle(subs, "Klingon").show?.label, "English", "stale label");
});

test("no subtitle tracks is not a crash", () => {
  assert.equal(subToggle([], "English").show, null);
});

test("a full off/on cycle lands exactly where it started", () => {
  const subs = [t("English", "showing"), t("Spanish")];

  const off = subToggle(subs, "");
  for (const s of subs) s.mode = s === off.show ? "showing" : "hidden";
  assert.deepEqual(subs.map((s) => s.mode), ["hidden", "hidden"]);

  const on = subToggle(subs, off.remember);
  for (const s of subs) s.mode = s === on.show ? "showing" : "hidden";
  assert.deepEqual(subs.map((s) => s.mode), ["showing", "hidden"]);
});
