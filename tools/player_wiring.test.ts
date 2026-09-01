// Asserts on the text of COMPILED component output (or module source for the
// plain .ts files) — it guards the wiring, not the behaviour.
import { readFileSync } from "node:fs";
import { register } from "node:module";
import { compile } from "svelte/compiler";

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

// Runes are compile-time in Svelte but plain calls syntactically; an identity
// shim lets the real modules import under bare Node.
(globalThis as any).$state = Object.assign((v: any) => v, {
  snapshot: (v: any) => v,
});
(globalThis as any).$derived = (v: any) => v;

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

const dock = built("dock.svelte");
for (const ev of ["pointerdown", "pointermove", "pointerup", "pointercancel", "pointerleave"])
  check(`dock listens for ${ev}`, dock.js.includes(ev));
for (const ev of ["mousedown", "mousemove", "mouseup", "mouseleave"])
  check(`dock no longer listens for ${ev}`, !dock.js.includes(ev));
check("dock captures the pointer", dock.js.includes("setPointerCapture"));
check("dock releases the pointer", dock.js.includes("releasePointerCapture"));
check("dock track opts out of browser gestures", dock.css.includes("touch-action: none"));

// One seek on press, one on release: a seek per move restarts the in-flight
// HLS segment fetch dozens of times a second.
const moveBody = dock.source.match(/function onMove\([^)]*\) \{([\s\S]*?)\n  \}/)?.[1] ?? "";
check("dock onMove found", moveBody.length > 0);
check("dock onMove does not seek", !moveBody.includes("onSeek("));
check("dock seeks exactly twice", (dock.source.match(/\bonSeek\(/g) ?? []).length === 2);

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

check("bar pads the notch", built("bar.svelte").css.includes("env(safe-area-inset-top)"));
check("dock pads the home indicator", dock.css.includes("env(safe-area-inset-bottom)"));
check("dock pads the landscape notch", dock.css.includes("env(safe-area-inset-left)"));
const skip = built("skip.svelte");
check("skip clears the home indicator", skip.css.includes("env(safe-area-inset-bottom)"));
check("skip clears the landscape notch", skip.css.includes("env(safe-area-inset-right)"));

const rail = built("related.svelte");
check("rail toggles on click", /onclick[\s\S]{0,80}open\s*=\s*!open/.test(rail.source));
check("rail hides without hover", /@media \(hover: none\)[\s\S]{0,80}display: none/.test(rail.css));
check("rail clears the landscape notch", rail.css.includes("left: env(safe-area-inset-left)"));

const ux = src("ux.ts");
check(
  "hotkeys take a fullscreen callback",
  /export function Hotkeys\(\s*player: any,\s*onFullscreen: \(\) => void,/.test(ux),
);
check("no container.requestFullscreen", !ux.includes("container.requestFullscreen"));
check("PiP is gesture-driven", ux.includes("export function togglePip"));
check("PiP has the WebKit path", ux.includes("webkitSetPresentationMode"));
// A tap outside the centre third used to do nothing, which reads as a dead
// player; the play/pause timer must stay unconditional.
check(
  "single tap toggles anywhere",
  !/if \(x > w \/ 3 && x < \(2 \* w\) \/ 3\) \{\s*if \(tapTimer\)/.test(ux),
);

const avsync = src("avsync.ts");
check("avsync is gated off Safari", avsync.includes("IS_ANY_SAFARI"));
check("avsync is gated off iOS", avsync.includes("IS_IOS"));
check("avsync exposes availability", avsync.includes("get on()"));
check("avsync resumes on playing", avsync.includes('addEventListener("playing"'));

check("master URL carries the av1 hint", /master\?file=\$\{[^}]+\}&av1=\$\{av1\}/.test(player.source));
// The server honours only the literal "1"/"0" — anything else (a stringified
// boolean included) falls through to its User-Agent heuristic.
check("hint is a literal 1 or 0", player.source.includes("const av1 = av1Ok() ? 1 : 0"));
// Master only: it hands out URLs for the codec it resolved, so the choice
// propagates to variant/segment/init on its own.
check("hint is on the master request only", (player.source.match(/av1=/g) ?? []).length === 1);

const AV1 = 'video/mp4; codecs="av01.0.05M.08"';
const { av1Ok } = await import("../src/player/ux.ts");

// canPlayType answers "", "maybe" or "probably" — only "" means no, and the
// undeterminable cases must answer no: a wrong yes is a black screen.
function probe(mse: unknown, canPlay: unknown): boolean {
  (globalThis as any).window =
    mse === "absent" ? {} : { MediaSource: { isTypeSupported: () => mse } };
  (globalThis as any).document = {
    createElement: () => {
      if (canPlay === "throws") throw new Error("no video element");

      return { canPlayType: (t: string) => (t === AV1 ? canPlay : "") };
    },
  };

  return av1Ok();
}

check("MSE says yes", probe(true, "") === true);
// The trap `||` would fall through here and consult canPlayType instead.
check("MSE says no beats canPlayType", probe(false, "probably") === false);
check("no MSE, canPlayType probably", probe("absent", "probably") === true);
check("no MSE, canPlayType maybe", probe("absent", "maybe") === true);
check("no MSE, canPlayType empty", probe("absent", "") === false);
check("nothing determinable", probe("absent", "throws") === false);

console.log(failed ? `${failed} failure(s)` : "ok — player wiring");
process.exit(failed ? 1 : 0);
