// The mpv `//!HOOK` dialect is parsed, not interpreted by mpv, so the parser is
// the part that can silently be wrong. Everything else is GPU state.
import test from "node:test";
import assert from "node:assert/strict";

const { parse, evalSize, Chain } = await import("../src/player/mpvshader.ts");

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
// //!WIDTH on most. The parser claims to read the format, not one file of
// it — and a DESC-triggered
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

// NNEDI3 sizes itself as `2 HOOKED.w *`. evalSize knows textures by name, and
// HOOKED resolved to 0 — the pass rendered into a 1-pixel strip.
test("HOOKED in WIDTH/HEIGHT resolves to the hooked texture", () => {
  const [p] = parse(`
//!HOOK LUMA
//!BIND HOOKED
//!WIDTH 2 HOOKED.w *
//!HEIGHT HOOKED.h
//!OFFSET 0.000000 -0.500000
vec4 hook() { return HOOKED_tex(HOOKED_pos); }
`);
  assert.deepEqual(p.width, ["2", "LUMA.w", "*"]);
  const dims = new Map([["LUMA", [960, 540] as [number, number]]]);
  assert.equal(evalSize(p.width, dims), 1920);
  assert.equal(evalSize(p.height, dims), 540);
});

test("OFFSET parses, and a pass without one is unshifted", () => {
  const [p] = parse("//!HOOK LUMA\n//!OFFSET -0.500000 0.000000\nvec4 hook() { return vec4(0.0); }\n");
  assert.deepEqual(p.offset, [-0.5, 0]);
  assert.deepEqual(parse(FSRCNNX_SHAPE)[0].offset, [0, 0]);
});

// A recording stand-in for WebGL2: every program is remembered by the fragment
// source it was linked from, so a test can read the order things were drawn in.
function fakeGL() {
  const src = new Map<object, string>();
  const frag = new Map<object, string>();
  const drawn: string[] = [];
  let cur: object | null = null;
  const gl: any = new Proxy({}, {
    get(_, k: string) {
      switch (k) {
        case "FRAGMENT_SHADER": return 2;
        case "createShader": return (t: number) => ({ t });
        case "shaderSource": return (sh: any, s: string) => src.set(sh, s);
        case "createProgram": return () => ({});
        case "attachShader": return (p: any, sh: any) => { if (sh.t === 2) frag.set(p, src.get(sh)!); };
        case "useProgram": return (p: any) => { cur = p; };
        case "drawArrays": return () => drawn.push(frag.get(cur!) ?? "");
        case "getShaderParameter": case "getProgramParameter": return () => true;
        case "getExtension": case "createTexture": case "createFramebuffer": case "createBuffer": case "getUniformLocation": return () => ({});
        case "getAttribLocation": case "getError": return () => 0;
        default: return () => {};
      }
    },
  });
  return { gl, drawn };
}

const LUMA_NET = `
//!HOOK LUMA
//!BIND HOOKED
//!WIDTH HOOKED.w 2 *
//!HEIGHT HOOKED.h 2 *
vec4 hook() { return vec4(0.5); }
`;

// Without the split the LUMA bind fell back to MAIN and the network read the
// RED channel as brightness, then drew its luma straight to the screen as red.
test("a LUMA chain splits first and merges colour back last", () => {
  const { gl, drawn } = fakeGL();
  const chain = new Chain(gl, [LUMA_NET], "full");
  assert.ok(chain.ok);
  chain.run({} as any, 960, 540, 0, 0, 1920, 1080);
  assert.equal(drawn.length, 3);
  assert.match(drawn[0], /dot\(texture\(MAIN_raw/, "first draw is not the luma split");
  assert.match(drawn[1], /vec4\(0\.5\)/, "the network pass did not run in between");
  assert.match(drawn[2], /LUMA_raw[\s\S]*c \+ \(y - dot/, "last draw is not the colour merge");
});

test("an RGB chain draws its own last pass, with no split or merge", () => {
  const { gl, drawn } = fakeGL();
  const chain = new Chain(gl, [SRC]);
  chain.run({} as any, 960, 540, 0, 0, 1920, 1080);
  assert.equal(drawn.length, 2);
  assert.ok(!drawn.some((d) => d.includes("MAIN_raw, vpos).rgb, vec3(0.2126")), "a split ran for an RGB chain");
});
