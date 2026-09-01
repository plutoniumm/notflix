// Asserts on the text of the COMPILED component — it guards the wiring, not
// the behaviour, because none of it can be clicked here. The server half of
// the contract (H.264/AAC/faststart, bounds, traversal) is covered by
// server/media/clip_test.go, which actually cuts a file and probes it.
import { readFileSync } from "node:fs";
import { compile } from "svelte/compiler";

let failed = 0;
function check(name: string, ok: boolean) {
  if (ok) return;
  failed++;
  console.log(`FAIL ${name}`);
}

const source = readFileSync("src/player/clip.svelte", "utf8");
const out = compile(source, { filename: "clip.svelte", generate: "client" });
const js = out.js.code;
const css = out.css?.code ?? "";

// ------------------------------------------------------------------ keyboard

for (const [k, what] of [
  ['"i"', "set in"],
  ['"o"', "set out"],
  ['"Backspace"', "clear"],
  ['"Enter"', "confirm"],
  ['"Escape"', "cancel"],
] as [string, string][])
  check(`${what} has a key (${k})`, source.includes(k));

// The player's own Escape ladder sits on the document too; without this both
// run and a panel behind the clip tool closes along with it.
check("Escape does not reach the player's ladder", source.includes("e.stopPropagation()"));
check("keys skip a focused control", source.includes("input,button,a,select,textarea,[contenteditable]"));

// ------------------------------------------------------------- scrub safety

// The dock's track owns pointer capture and one seek on release; this panel
// must not grow a second measurement basis for the same timeline.
for (const forbidden of ["setPointerCapture", "getBoundingClientRect", "pointerdown", "pointermove"])
  check(`clip does not re-implement scrubbing (${forbidden})`, !js.includes(forbidden));

// ux.ts's picture handlers skip a fixed chrome list this panel is not on, so
// a tap on it would otherwise toggle playback underneath.
check("clip eats its own pointer gestures", source.includes("const eat = (e: Event) => e.stopPropagation()"));
for (const ev of ["onpointerup={eat}", "ondblclick={eat}", "ontouchend={eat}"])
  check(`clip stops ${ev}`, source.includes(ev));

// ------------------------------------------------------------------ delivery

check("clip starts a job", source.includes("api.clip.start"));
check("clip polls that job", source.includes("api.clip.status"));
// Safari ignores the HTML `download` attribute, so the save has to come from a
// navigation the server answers with Content-Disposition.
check("the file is fetched by navigation", source.includes("window.location.href = url"));
check("a link stays as the fallback", /<a class="go done ptr" \{href\}/.test(source));
check("polling is hidden-tab aware", source.includes("usePoll"));
check("polling stops when the panel does", source.includes("onDestroy(endPoll)"));

// --------------------------------------------------------------------- shape

check("the cap is refused before the round trip", source.includes("const tooLong ="));
check("an empty selection cannot be saved", /const ready = \$derived\(span > /.test(source));

// -------------------------------------------------------------------- design

check("the panel animates in", css.includes("@starting-style"));
check("the panel clears the home indicator", css.includes("env(safe-area-inset-bottom)"));
check("the panel clears the landscape notch", css.includes("env(safe-area-inset-left)"));
check("the strip shares the dock's 16px basis", css.includes("16px + env(safe-area-inset-left)"));
check("motion comes from the tokens", css.includes("var(--m-fast, 130ms)"));
check("the selection uses the ambient pair", css.includes("var(--art-1)"));
check("touch targets reach 44px", /@media \(hover: none\)[\s\S]{0,220}min-height: 44px/.test(css));
// A 3px pin is unhittable with a thumb; the visible mark stays 3px and the
// tap target is a transparent inset around it.
check("the marks are tappable", /\.pin[\w.\-]*::before\s*\{[^}]*inset:/.test(css));

check("no ARIA was added", !/aria-|role=/.test(source));

console.log(failed ? `${failed} failure(s)` : "ok — clip tool");
process.exit(failed ? 1 : 0);
