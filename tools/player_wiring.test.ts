// Wiring assertions for the player's mobile/PWA fixes. Everything here is
// checked against COMPILED component output (or module source for the plain
// .ts files) — none of it can exercise a real touchscreen, so it guards the
// wiring, not the behaviour. Run: node tools/player_wiring.test.ts
import { readFileSync } from "node:fs";
import { compile } from "svelte/compiler";

let failed = 0;
function check(name: string, ok: boolean) {
  if (ok) return;
  failed++;
  console.log(`FAIL ${name}`);
}

const src = (f: string) => readFileSync(`src/player/${f}`, "utf8");

function built(f: string) {
  const source = src(f);
  const out = compile(source, { filename: f, generate: "client" });

  return { source, js: out.js.code, css: out.css?.code ?? "" };
}

// --- scrub bar: pointer events cover mouse and touch in one path ---
const dock = built("dock.svelte");
for (const ev of ["pointerdown", "pointermove", "pointerup", "pointercancel", "pointerleave"])
  check(`dock listens for ${ev}`, dock.js.includes(ev));
for (const ev of ["mousedown", "mousemove", "mouseup", "mouseleave"])
  check(`dock no longer listens for ${ev}`, !dock.js.includes(ev));
check("dock captures the pointer", dock.js.includes("setPointerCapture"));
check("dock releases the pointer", dock.js.includes("releasePointerCapture"));
check("dock track opts out of browser gestures", dock.css.includes("touch-action: none"));

// One seek on press, one on release — never one per move, which restarts the
// in-flight HLS segment fetch dozens of times a second.
const moveBody = dock.source.match(/function onMove\([^)]*\) \{([\s\S]*?)\n  \}/)?.[1] ?? "";
check("dock onMove found", moveBody.length > 0);
check("dock onMove does not seek", !moveBody.includes("onSeek("));
check("dock seeks exactly twice", (dock.source.match(/\bonSeek\(/g) ?? []).length === 2);

// --- Player: inline playback, safe area, leave-saves, no PiP on visibility ---
const player = built("Player.svelte");
check("video tag is playsinline", player.js.includes("playsinline"));
check("videojs opts set playsinline", player.source.includes("playsinline: true"));
check("page suppresses double-tap zoom", player.css.includes("touch-action: manipulation"));
check("saves on pagehide", player.source.includes('addEventListener("pagehide"'));
check("saves on visibilitychange", player.source.includes('addEventListener("visibilitychange", onHide)'));
check("leave path uses the beacon route", player.source.includes("watch.flushOnLeave"));
check("no PiP driven from visibility", !/(visibilityState|document\.hidden)[\s\S]{0,400}PictureInPicture/.test(player.source));
check("fullscreen is feature-detected", player.source.includes("el?.requestFullscreen ?? el?.webkitRequestFullscreen"));
check("fullscreen falls back to the video element", player.source.includes("webkitEnterFullscreen"));
check("no bare pageEl.requestFullscreen", !/pageEl[!?]?\.requestFullscreen\(\)/.test(player.source));
check("falls back to direct mp4 on decode error", /err\.code === 3 \|\| err\.code === 4/.test(player.source));
check("videoList is silent", player.source.includes("videoList({ silent: true })"));
check("audio info is silent", player.source.includes("info(param, { silent: true })"));

// --- safe-area insets (inert until viewport-fit=cover lands in index.html) ---
check("bar pads the notch", built("bar.svelte").css.includes("env(safe-area-inset-top)"));
check("dock pads the home indicator", dock.css.includes("env(safe-area-inset-bottom)"));
check("dock pads the landscape notch", dock.css.includes("env(safe-area-inset-left)"));
const skip = built("skip.svelte");
check("skip clears the home indicator", skip.css.includes("env(safe-area-inset-bottom)"));
check("skip clears the landscape notch", skip.css.includes("env(safe-area-inset-right)"));

// --- Up Next rail: tappable, and gone where it would only eat taps ---
const rail = built("related.svelte");
check("rail toggles on click", /onclick[\s\S]{0,80}open\s*=\s*!open/.test(rail.source));
check("rail hides without hover", /@media \(hover: none\)[\s\S]{0,80}display: none/.test(rail.css));
check("rail clears the landscape notch", rail.css.includes("left: env(safe-area-inset-left)"));

// --- plain modules ---
const ux = src("ux.ts");
check(
  "hotkeys take a fullscreen callback",
  /export function Hotkeys\(\s*player: any,\s*onFullscreen: \(\) => void,/.test(ux),
);
check("no container.requestFullscreen", !ux.includes("container.requestFullscreen"));
check("PiP is gesture-driven", ux.includes("export function togglePip"));
check("PiP has the WebKit path", ux.includes("webkitSetPresentationMode"));
// A tap outside the centre third used to do nothing, which reads as a dead
// player; the play/pause timer is now unconditional.
check(
  "single tap toggles anywhere",
  !/if \(x > w \/ 3 && x < \(2 \* w\) \/ 3\) \{\s*if \(tapTimer\)/.test(ux),
);

const avsync = src("avsync.ts");
check("avsync is gated off Safari", avsync.includes("IS_ANY_SAFARI"));
check("avsync is gated off iOS", avsync.includes("IS_IOS"));
check("avsync exposes availability", avsync.includes("get on()"));
check("avsync resumes on playing", avsync.includes('addEventListener("playing"'));

console.log(failed ? `${failed} failure(s)` : "ok — player wiring");
process.exit(failed ? 1 : 0);
