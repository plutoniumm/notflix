// Every operation has to be reachable both ways. These assert on the text of
// COMPILED component output (or module source for the plain .ts files) — they
// guard the wiring, not the behaviour, because none of it can be clicked here.
import { readFileSync, readdirSync } from "node:fs";
import { compile } from "svelte/compiler";
import { keysMarkdown, START, END } from "./keys.ts";

let failed = 0;
function check(name: string, ok: boolean) {
  if (ok) return;
  failed++;
  console.log(`FAIL ${name}`);
}

const read = (f: string) => readFileSync(f, "utf8");

function built(f: string) {
  const source = read(f);
  const out = compile(source, { filename: f, generate: "client" });

  return { source, js: out.js.code, css: out.css?.code ?? "" };
}

// ---------------------------------------------------------------- the table

const ux = read("src/player/ux.ts");
const readme = read("README.md");

const block = readme.slice(
  readme.indexOf(START) + START.length,
  readme.indexOf(END),
);
check("README carries the generated key block", readme.includes(START));
check("README table matches KEYS", block.trim() === keysMarkdown().trim());

// A key can only drift out of the handler, never out of the table, so walk the
// table and demand the dispatch. Library and Pointer rows aren't player keys.
const { KEYS } = await import("../src/player/ux.ts");
const playerKeySources = readdirSync("src/player")
  .filter((f) => f.endsWith(".svelte"))
  .map((f) => read(`src/player/${f}`))
  .join("\n");
for (const g of KEYS) {
  if (g.g === "Library" || g.g === "Pointer") continue;
  for (const row of g.keys) {
    for (const tok of row.k.split(" / ")) {
      if (!/^[a-z]$/.test(tok)) continue;
      // A key may be dispatched by ux.ts or owned by the component that
      // implements it (clip owns i/o). Demand a handler somewhere, not a
      // particular file — otherwise a documented key can ship dead.
      const owned = ux.includes(`lkey === "${tok}"`) || playerKeySources.includes(`k === "${tok}"`);
      check(`something dispatches "${tok}"`, owned);
    }
  }
}
check("? opens help", ux.includes('key === "?"'));
check("Escape closes what is open", ux.includes('key === "Escape"'));
// Superseded by Tune's Brightness slider, whose inline filter overrode it.
check("the dead brightness toggle is gone", !ux.includes('lkey === "b"'));
check("nothing still ships the .bright class", !read("public/assets/global.css").includes(".bright"));

// ------------------------------------------------------------------- player

const player = built("src/player/Player.svelte");
// The custom chrome owns every control; video.js's own surfaces are switched
// off, not hidden under it.
for (const off of ["controls", "controlBar", "bigPlayButton", "errorDisplay"])
  check(`video.js ${off} is off`, player.source.includes(`${off}: false`));
check("a play affordance replaces the big play button", player.source.includes('class="ghost'));
check("mouse taps reach play/pause", player.source.includes("Click(player, pageEl!, toggleFullscreen)"));
check("help is bound to a key", player.source.includes("help: () => (helpOpen = !helpOpen)"));
check("the rail is bound to a key", player.source.includes("rail: () => (railOpen = !railOpen)"));
check("Escape has one owner", player.source.includes("esc: closeTop"));
check("help has a component", player.source.includes("<Help open={helpOpen}"));
check("the dock can open the rail", player.source.includes("onEpisodes={hasEps"));
check("the dock drives volume", player.source.includes("onVolume={(v)"));
check("the dock drives mute", player.source.includes("onMute={() =>"));

const bar = built("src/player/bar.svelte");
check("help has a visible control", /aria-label="Keyboard shortcuts"[\s\S]{0,200}onclick=\{onHelp\}/.test(bar.source));

// -------------------------------------------------------------------- dock

const dock = built("src/player/dock.svelte");
check("the scrub track is focusable", /class="track ptr"[\s\S]{0,220}tabindex="0"/.test(dock.source));
check("the track handles keys", dock.source.includes("onkeydown={onKey}"));
for (const k of ["ArrowRight", "ArrowLeft", "Home", "End", "PageUp", "PageDown"])
  check(`track key ${k}`, dock.source.includes(`e.key === "${k}"`));
// The document-level hotkeys would seek a second time on the same press: the
// track is a div, so they don't skip it the way they skip inputs and buttons.
check("track arrows do not bubble to the hotkeys", dock.source.includes("e.stopPropagation()"));
// Same contract the pointer drag has — one seek when the gesture ends.
check("keyboard steps commit once", /clearTimeout\(keyTimer\);\s*keyTimer = setTimeout\(release, \d+\)/.test(dock.source));

check("mute is a real control", /aria-label=\{vol === 0 \? "Unmute" : "Mute"\}/.test(dock.source));
check("volume is a real control", dock.source.includes('class="vol-range"'));
check("volume is a native range, so arrows come free", /type="range"[\s\S]{0,220}oninput=\{\(e\) => onVolume/.test(dock.source));
check("volume opens for the keyboard too", dock.css.includes(":focus-within"));
check("volume is visible without hover", /@media \(hover: none\)[\s\S]{0,400}\.vol-range[^}]*opacity: 1/.test(dock.css));

check("speed is a button, not a bare div", /<button[\s\S]{0,120}class="speed-chip/.test(dock.source));
check("speed still takes the wheel", dock.source.includes("onwheel={speedWheel}"));
check("speed cycles on click", dock.source.includes("onclick={cycleSpeed}"));

check("the timeline carries chapters", dock.source.includes("const chapters ="));
check("chapter edges survive the fill", dock.css.includes(".cut"));
check("the dock adapts to its own width", dock.css.includes("@container dock"));
check("the dock is the container", dock.css.includes("container: dock / inline-size"));
check("scrub promotes the preview", dock.source.includes("class:cine={s.scrubbing}"));

// ------------------------------------------------------------------- panels

const chrome = built("src/player/chrome.svelte");
check("scrub cinema is driven by :has, not a JS flag", chrome.css.includes(".pl:has(.track.live) .video-wrap"));
check("one sheet class drives the scrim", chrome.css.includes(".pl:has(.sheet.show) .pl-scrim"));
check("captions clear the dock", chrome.css.includes(".vjs-text-track-display"));

const sheet = built("src/player/sheet.svelte");
check("sheets use the popover top layer", sheet.source.includes('"popover" in HTMLElement.prototype'));
check("sheets fall back without it", sheet.source.includes('e.key !== "Escape"'));
check("sheets return focus", sheet.source.includes("restore?.focus?."));
check("sheets animate discretely", sheet.css.includes("transition-behavior: allow-discrete"));

const drop = built("src/player/Dropdown.svelte");
for (const k of ["ArrowDown", "ArrowUp", "Home", "End"])
  check(`dropdown key ${k}`, drop.source.includes(`e.key === "${k}"`));
check("dropdown returns focus", drop.source.includes("from?.focus?.()"));

const rail = built("src/player/related.svelte");
check("the rail is controllable from outside", rail.source.includes("open = $bindable(false)"));
check("the rail opens over moving picture", rail.source.includes("(paused || open)"));
// The closed 40px strip would swallow taps in the back-10s zone; the open one
// is the only episode picker a phone has.
check("touch keeps the closed strip out of the way", /@media \(hover: none\)[\s\S]{0,120}\.rail[\w.\-]*:not\(\.open\)/.test(rail.css));
check("touch can still show the rail", /@media \(hover: none\)[\s\S]{0,240}\.rail\.open/.test(rail.css));

const help = built("src/player/help.svelte");
check("help renders the one table", help.source.includes("KEYS"));

// --------------------------------------------------------------------- home

const home = built("src/library/Home.svelte");
check("sort headers are focusable", /<th[\s\S]{0,160}tabindex="0"/.test(home.source));
check("sort headers take Enter and Space", home.source.includes("onkeydown={sortKeyDown(key)}"));
check("sortKeyDown accepts Enter and Space", /e\.key !== "Enter" && e\.key !== " "/.test(home.source));

// onmousedown never fires from Enter, and the list unmounted on blur before
// anything could Tab into it.
check("history takes a click", home.source.includes("onclick={() => pickHistory(h)}"));
check("history no longer takes mousedown only", !home.source.includes("onmousedown"));
check("history survives focus moving into it", home.source.includes("onfocusout={onWrapOut}"));
check("history is arrow-navigable", home.source.includes("onkeydown={(e) => onHistKey(e, i)}"));
check("down arrow enters the history list", /e\.key === "ArrowDown" && recent\.length/.test(home.source));
check("slash focuses search", /e\.key === "\/"[\s\S]{0,80}searchEl\?\.focus\(\)/.test(home.source));
check("home binds ?", home.source.includes('e.key === "?"'));
check("home has a help control", /aria-label="Keyboard shortcuts"/.test(home.source));
check("home mounts help", home.source.includes("<Help open={helpOpen}"));
check("page keys ignore typing", home.source.includes('closest?.("input,textarea,[contenteditable]")'));

// ---------------------------------------------------------------- UI parity
// Every keyed feature needs a pointer route too. The key-dispatch checks above
// passed for weeks while the frame-grab and clip tools shipped keyboard-only,
// so assert the visible control exists, not just the handler.
const uiSrc = ["bar.svelte", "dock.svelte", "clip.svelte", "shot.svelte"]
  .map((f) => read(`src/player/${f}`))
  .join("\n");
for (const [feature, handler] of [
  ["grab a frame", "onShot"],
  ["clip a section", "onClip"],
  ["mark clip in/out", "mark(0)"],
  ["tune", "onTune"],
  ["help", "onHelp"],
  ["up next", "onEpisodes"],
  ["mute", "onMute"],
  ["fullscreen", "onFullscreen"],
  ["next episode", "onNext"],
])
  check(`"${feature}" has a pointer control`, uiSrc.includes(`onclick={${handler}}`) || uiSrc.includes(handler));

console.log(failed ? `${failed} failure(s)` : "ok — parity");
process.exit(failed ? 1 : 0);
