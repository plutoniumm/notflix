// Touch/PWA rules can't be exercised without a device, so these assert on the
// text of the COMPILED bundle — run `pnpm run build` first.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (rel: string) => {
  try {
    return readFileSync(new URL(`../${rel}`, import.meta.url), "utf8");
  } catch {
    throw new Error(`${rel} missing — run \`pnpm run build\` first`);
  }
};

const css = read("public/assets/notflix.css");
const js = read("public/assets/notflix.js");

// Every `@media (hover: none)` body, brace-matched (the CSS ships minified).
const touch = (() => {
  const out: string[] = [];
  const marker = "hover:none){";
  for (let i = css.indexOf(marker); i !== -1; i = css.indexOf(marker, i + 1)) {
    let depth = 1;
    let j = i + marker.length;
    for (; j < css.length && depth > 0; j++) {
      if (css[j] === "{") depth++;
      else if (css[j] === "}") depth--;
    }
    out.push(css.slice(i + marker.length, j - 1));
  }

  return out.join("\n");
})();

const rule = (sel: string) => {
  const re = new RegExp(`\\${sel}[\\w.\\-]*\\{([^}]*)\\}`);
  const m = touch.match(re);
  assert.ok(m, `${sel} has no @media (hover: none) rule`);

  return m![1];
};

test("touch reveals every control that hover would", () => {
  // An opacity-0 ✕ still takes taps: an invisible delete under the thumb.
  const cw = rule(".cw-remove");
  assert.match(cw, /opacity:\.?\d/);
  assert.doesNotMatch(cw, /opacity:0[;}]/);

  // FileRow's controls sit in an opacity-0 wrapper; a transparent parent hides
  // the children whatever their own opacity says.
  assert.match(rule(".actions"), /opacity:1/);

  assert.match(rule(".scroll-arrow"), /opacity:\.?\d/);
  assert.match(rule(".btn-icon"), /opacity:1/);
});

test("touch targets reach 44px", () => {
  const btn = rule(".btn-icon");
  assert.match(btn, /min-width:44px/);
  assert.match(btn, /min-height:44px/);

  const cw = rule(".cw-remove");
  assert.match(cw, /width:44px/);
  assert.match(cw, /height:44px/);
});

test("fields are 16px on touch so iOS does not zoom in", () => {
  assert.match(rule(".field"), /font-size:16px/);
});

test("safe-area insets are defined and consumed", () => {
  assert.match(css, /--safe-t:\s*env\(safe-area-inset-top/);
  assert.match(css, /--safe-b:\s*env\(safe-area-inset-bottom/);

  // Two each: page headers up top, toasts + the Home panel at the bottom.
  assert.ok(
    css.split("var(--safe-t)").length - 1 >= 2,
    "expected the page headers to pad for the notch",
  );
  assert.ok(
    css.split("var(--safe-b)").length - 1 >= 2,
    "expected bottom-fixed chrome to pad for the home indicator",
  );
});

test("full-height surfaces use dvh with a vh fallback", () => {
  assert.match(css, /min-height:100vh;min-height:100dvh/);
  assert.match(js, /min-height:\s*100vh;\s*min-height:\s*100dvh/);
});

test("deletes are two-step", () => {
  assert.match(js, /Tap again to delete/);
});
