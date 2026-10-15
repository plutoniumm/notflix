// The mpv `//!HOOK` dialect is parsed, not interpreted by mpv, so the parser is
// the part that can silently be wrong. Everything else is GPU state.
import test from "node:test";
import assert from "node:assert/strict";

const { parse, evalSize } = await import("../src/player/mpvshader.ts");

const SRC = `
// license header, ignored
//!DESC first
//!HOOK MAIN
//!BIND MAIN
//!SAVE conv2d_tf
//!WIDTH MAIN.w
//!HEIGHT MAIN.h
//!COMPONENTS 4
//!WHEN OUTPUT.w MAIN.w / 1.200 >
vec4 hook() { return vec4(1.0); }

//!DESC last
//!HOOK MAIN
//!BIND MAIN
//!BIND conv2d_tf
//!SAVE MAIN
//!WIDTH conv2d_tf.w 2 *
//!HEIGHT conv2d_tf.h 2 *
vec4 hook() { return MAIN_tex(MAIN_pos); }
`;

test("parses one pass per //!DESC", () => {
  assert.equal(parse(SRC).length, 2);
});

test("collects every BIND, in order", () => {
  const [, last] = parse(SRC);
  assert.deepEqual(last.binds, ["MAIN", "conv2d_tf"]);
});

test("keeps the body and drops the directives", () => {
  const [first] = parse(SRC);
  assert.ok(first.body.includes("vec4 hook()"));
  assert.ok(!first.body.includes("//!"), "a directive leaked into the GLSL");
  assert.ok(!first.body.includes("WHEN"), "WHEN leaked into the GLSL");
});

test("a license header before the first //!DESC is not a pass", () => {
  assert.equal(parse("// just a comment\n" + SRC).length, 2);
});

test("evalSize walks the RPN mpv uses", () => {
  const dims = new Map([["MAIN", [1920, 804] as [number, number]]]);
  assert.equal(evalSize(["MAIN.w"], dims), 1920);
  assert.equal(evalSize(["MAIN.h"], dims), 804);
  assert.equal(evalSize(["MAIN.w", "2", "*"], dims), 3840);
  assert.equal(evalSize(["MAIN.w", "2", "/"], dims), 960);
});

test("evalSize never returns a zero or negative size", () => {
  const dims = new Map<string, [number, number]>();
  // An unknown texture resolves to 0, which would make a 0x0 framebuffer.
  assert.equal(evalSize(["nope.w"], dims), 1);
  assert.equal(evalSize([], dims), 1);
});

// The final pass of both vendored chains binds MAIN and saves MAIN. The runtime
// has to notice, or it samples a texture it is rendering into.
test("read-write alias on the same name is detectable from the parse", () => {
  const [, last] = parse(SRC);
  assert.ok(last.binds.includes(last.save), "alias not visible to the runtime");
});

// Shaders in this dialect disagree about ordering: Anime4K writes //!DESC
// first, FSRCNNX writes //!HOOK first and omits //!SAVE on its last pass and
// //!WIDTH on most. FSRCNNX is no longer vendored (see upscale.ts), but the
// parser claims to read the format, not one file of it — and a DESC-triggered
// split silently attached a HOOK line to the PREVIOUS pass, which is the kind
// of thing that only shows up as a wrong image.
const FSRCNNX_SHAPE = `
//!HOOK LUMA
//!WHEN OUTPUT.w LUMA.w / 1.300 >
//!DESC feature map 1
//!BIND LUMA
//!SAVE FEATURE1
//!COMPONENTS 4
vec4 hook() { return LUMA_texOff(vec2(0.0)); }

//!HOOK LUMA
//!WIDTH LUMA.w 2 *
//!HEIGHT LUMA.h 2 *
//!DESC aggregation
//!BIND SUBCONV1
vec4 hook() { return vec4(0.0); }
`;

test("HOOK before DESC still opens a new pass", () => {
  assert.equal(parse(FSRCNNX_SHAPE).length, 2);
});

test("a pass with no SAVE writes back to what it hooked", () => {
  const [, last] = parse(FSRCNNX_SHAPE);
  assert.equal(last.save, "LUMA");
});

test("a pass with no WIDTH defaults to the hooked texture, not MAIN", () => {
  const [first] = parse(FSRCNNX_SHAPE);
  assert.deepEqual(first.width, ["LUMA.w"]);
  assert.deepEqual(first.height, ["LUMA.h"]);
});

test("explicit WIDTH still wins", () => {
  const [, last] = parse(FSRCNNX_SHAPE);
  assert.deepEqual(last.width, ["LUMA.w", "2", "*"]);
});

test("HOOKED is rewritten to the hooked name in both binds and body", () => {
  const [p] = parse(`
//!HOOK LUMA
//!DESC x
//!BIND HOOKED
vec4 hook() { return HOOKED_tex(HOOKED_pos); }
`);
  assert.deepEqual(p.binds, ["LUMA"]);
  assert.ok(p.body.includes("LUMA_tex(LUMA_pos)"), p.body);
  assert.ok(!p.body.includes("HOOKED_"), "HOOKED_ leaked through");
});
