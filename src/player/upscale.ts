// Real-time spatial upscaling in a WebGL2 overlay.
//
// Both modes are ANALYTIC shaders, not networks: no weights, no model download,
// no WebGPU. That is the whole point — an ESRGAN-class model cannot hold 60fps
// on a phone, and the ones that can are not meaningfully better than these at
// the sharpening job a bilinear-upscaled stream actually needs.
//
//   fsr    FSR 1.0 — EASU (edge-adaptive directional resample) then RCAS
//          (contrast-adaptive sharpening), hand-written here. Analytic, so it
//          only redistributes contrast the frame already has: measured +15-18%
//          detail on content with real edges.
//
//          FSRCNNX (a trained CNN for live action) was tried here and REMOVED.
//          It compiled and ran GL-clean through the same runtime, but measured
//          -3% — identical to a plain passthrough, i.e. contributing nothing.
//          The likely cause is a range mismatch: mpv hands these shaders the
//          video's actual Y' plane in limited range, while this pipeline could
//          only offer full-range Rec.709 luma computed from RGB, which is not
//          what the network was trained on. Worth revisiting; not worth
//          shipping a 14-pass no-op.
//
//          Newer FSR and DLSS do not apply at all: FSR 2/3/4 and DLSS 2/3/4 are
//          temporal, requiring per-pixel motion vectors, depth and jitter from
//          a game engine's render loop, none of which a video file has — and
//          FSR 4 is RDNA4-only, DLSS is RTX-only, neither reachable from a
//          browser. FSR 1 is the only member of either family that works on a
//          plain image.
//   anime  Anime4K-style line refinement: push colour along the luma gradient
//          to thin and darken lines, then RCAS. Tuned for cel animation, where
//          FSR's edge model has nothing to lock onto between flat colour fields.
//
// These are the algorithms in spirit and in output character, written compactly
// here rather than ported byte-exact from the reference HLSL.
//
// The cost that matters is NOT the shader — a fullscreen pass at 1080p is well
// under a millisecond on any modern GPU. It is that routing video through a
// canvas gives up the hardware overlay path: the decoder normally hands frames
// straight to the display compositor without ever waking the GPU shader
// pipeline. That is why this is desktop-only (see `supported`).

import { Chain, debug as a4kDebug } from "./mpvshader";
import restoreCNN from "./anime4k/Anime4K_Restore_CNN_M.glsl?raw";
import upscaleCNN from "./anime4k/Anime4K_Upscale_CNN_x2_M.glsl?raw";

export type Mode = "fsr" | "anime";

const VERT = `#version 300 es
in vec2 p;
out vec2 uv;
void main() {
  // The flip lives at the texture upload (UNPACK_FLIP_Y_WEBGL), not here, so
  // that this shader and the multi-pass chain share one coordinate space.
  uv = p * 0.5 + 0.5;
  gl_Position = vec4(p, 0.0, 1.0);
}`;

// RCAS: sharpen by the local contrast the pixel can afford. The min/max of the
// 4-neighbourhood bounds how far the result may travel, which is what stops the
// halo a fixed unsharp mask produces on high-contrast edges.
const RCAS = `
// RCAS: sharpen by the local contrast the pixel can afford. The neighbourhood
// min/max bounds how far the result may travel, which is what stops the halo a
// fixed unsharp mask gives on high-contrast edges.
//
// 'give' loosens that bound. At 0 this is textbook RCAS and, measured, it is
// almost invisible: clamping strictly inside the 3x3 range means the filter can
// only redistribute contrast it already has. A little controlled overshoot is
// what actually reads as "sharp" to an eye.
vec3 rcas(sampler2D t, vec2 uv, vec2 px, float sharp, float give) {
  vec3 c = texture(t, uv).rgb;
  vec3 n = texture(t, uv + vec2(0.0, -px.y)).rgb;
  vec3 s = texture(t, uv + vec2(0.0,  px.y)).rgb;
  vec3 w = texture(t, uv + vec2(-px.x, 0.0)).rgb;
  vec3 e = texture(t, uv + vec2( px.x, 0.0)).rgb;

  vec3 mn = min(min(min(n, s), min(w, e)), c);
  vec3 mx = max(max(max(n, s), max(w, e)), c);

  vec3 sharpened = c + (c * 4.0 - n - s - w - e) * sharp;

  vec3 slack = (mx - mn) * give;
  return clamp(sharpened, mn - slack, mx + slack);
}`;

// EASU, compressed. The real thing analyses a 12-tap neighbourhood to recover
// the dominant edge direction and length, then resamples along it. This keeps
// that structure: a Sobel-ish gradient gives the direction, the sample kernel is
// stretched across the edge and squeezed along it, so edges resample sharp
// without the stair-stepping bilinear leaves behind.
const EASU = `#version 300 es
precision highp float;
in vec2 uv;
out vec4 frag;
uniform sampler2D src;
uniform vec2 srcPx;
uniform float amount;
${RCAS}

void main() {
  vec2 p = srcPx;
  vec3 c  = texture(src, uv).rgb;
  vec3 n  = texture(src, uv + vec2(0.0, -p.y)).rgb;
  vec3 s  = texture(src, uv + vec2(0.0,  p.y)).rgb;
  vec3 w  = texture(src, uv + vec2(-p.x, 0.0)).rgb;
  vec3 e  = texture(src, uv + vec2( p.x, 0.0)).rgb;
  vec3 nw = texture(src, uv + vec2(-p.x, -p.y)).rgb;
  vec3 ne = texture(src, uv + vec2( p.x, -p.y)).rgb;
  vec3 sw = texture(src, uv + vec2(-p.x,  p.y)).rgb;
  vec3 se = texture(src, uv + vec2( p.x,  p.y)).rgb;

  const vec3 L = vec3(0.2126, 0.7152, 0.0722);
  float ln = dot(n, L), ls = dot(s, L), lw = dot(w, L), le = dot(e, L);
  float lnw = dot(nw, L), lne = dot(ne, L), lsw = dot(sw, L), lse = dot(se, L);

  float gx = (lne + 2.0 * le + lse) - (lnw + 2.0 * lw + lsw);
  float gy = (lsw + 2.0 * ls + lse) - (lnw + 2.0 * ln + lne);
  float g = length(vec2(gx, gy));

  vec3 base = c;

  // Edge-directed resample: narrow across the edge, wide along it. Only worth
  // doing where there IS an edge; elsewhere it would just blur.
  if (g > 0.015) {
    vec2 dir = normalize(vec2(gx, gy));
    vec2 along = vec2(-dir.y, dir.x) * p;

    vec3 b0 = texture(src, uv - along).rgb;
    vec3 b1 = texture(src, uv + along).rgb;
    // Average ALONG the edge only — that is what removes the stair-stepping
    // without softening the edge itself.
    vec3 smoothed = c * 0.5 + (b0 + b1) * 0.25;
    base = mix(c, smoothed, clamp(g * 4.0, 0.0, 0.7));
  }

  // Then sharpen the resampled result. 'amount' scales both the kernel and the
  // overshoot it is allowed, so one slider moves the whole effect.
  vec3 out3 = rcas(src, uv, p, 0.18 * amount, 0.35 * amount);
  out3 = mix(base, out3, 0.85);

  frag = vec4(clamp(out3, 0.0, 1.0), 1.0);
}`;

// Anime4K, in the spirit of the original 2019 shader: find the luma gradient and
// PUSH colour down it, which pulls line edges inward. Thin, dark lines over flat
// fills is exactly the signal cel animation has and photography does not.
const ANIME = `#version 300 es
precision highp float;
in vec2 uv;
out vec4 frag;
uniform sampler2D src;
uniform vec2 srcPx;
uniform float amount;
${RCAS}

void main() {
  vec2 p = srcPx;
  const vec3 L = vec3(0.2126, 0.7152, 0.0722);

  vec3 c  = texture(src, uv).rgb;
  vec3 n  = texture(src, uv + vec2(0.0, -p.y)).rgb;
  vec3 s  = texture(src, uv + vec2(0.0,  p.y)).rgb;
  vec3 w  = texture(src, uv + vec2(-p.x, 0.0)).rgb;
  vec3 e  = texture(src, uv + vec2( p.x, 0.0)).rgb;
  vec3 nw = texture(src, uv + vec2(-p.x, -p.y)).rgb;
  vec3 ne = texture(src, uv + vec2( p.x, -p.y)).rgb;
  vec3 sw = texture(src, uv + vec2(-p.x,  p.y)).rgb;
  vec3 se = texture(src, uv + vec2( p.x,  p.y)).rgb;

  float ln = dot(n, L), ls = dot(s, L), lw = dot(w, L), le = dot(e, L);
  float lnw = dot(nw, L), lne = dot(ne, L), lsw = dot(sw, L), lse = dot(se, L);

  float gx = (lne + 2.0 * le + lse) - (lnw + 2.0 * lw + lsw);
  float gy = (lsw + 2.0 * ls + lse) - (lnw + 2.0 * ln + lne);
  float g = length(vec2(gx, gy));

  vec3 outc = c;
  if (g > 0.03) {
    // Step TOWARD the darker side of the gradient: that is the line, and
    // sampling from there is what thins it.
    vec2 dir = normalize(vec2(gx, gy));
    vec3 pull = texture(src, uv + dir * p * (0.5 + 0.5 * amount)).rgb;
    float k = clamp((g - 0.03) * 3.2 * amount, 0.0, 0.9);
    outc = mix(c, pull, k);

    // Line darkening: cel art reads as "drawn" partly because the line is
    // darker than any gradient resample would leave it.
    float lo = min(min(ln, ls), min(lw, le));
    float lc2 = dot(outc, L);
    if (lc2 < lo + 0.10) outc *= mix(1.0, 0.90, amount);
  }

  // Flat fills stay flat; the sharpen only has to serve the line work.
  vec3 sharp = rcas(src, uv, p, 0.14 * amount, 0.30 * amount);
  outc = mix(outc, sharp, 0.45);

  frag = vec4(clamp(outc, 0.0, 1.0), 1.0);
}`;

const SHARP = `#version 300 es
precision highp float;
in vec2 uv;
out vec4 frag;
uniform sampler2D src;
uniform vec2 srcPx;
uniform float amount;
${RCAS}
void main() { frag = vec4(rcas(src, uv, srcPx, 0.2 * amount, 0.4 * amount), 1.0); }`;

// Phones are excluded on purpose, and not because the shader is too slow.
// Giving up the hardware overlay means the GPU wakes for every frame, which
// costs battery, and a phone is usually the device on cellular where the stream
// is already the thing under pressure. `(hover: none)` is how the rest of this
// codebase spells "touch device".
export function supported(): boolean {
  if (typeof window === "undefined") return false;
  if (window.matchMedia("(hover: none)").matches) return false;

  try {
    const c = document.createElement("canvas");
    return !!c.getContext("webgl2");
  } catch {
    return false;
  }
}

function compile(gl: WebGL2RenderingContext, vs: string, fs: string) {
  const mk = (type: number, src: string) => {
    const sh = gl.createShader(type)!;
    gl.shaderSource(sh, src);
    gl.compileShader(sh);
    if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
      throw new Error(gl.getShaderInfoLog(sh) || "shader compile failed");
    }
    return sh;
  };

  const prog = gl.createProgram()!;
  gl.attachShader(prog, mk(gl.VERTEX_SHADER, vs));
  gl.attachShader(prog, mk(gl.FRAGMENT_SHADER, fs));
  gl.linkProgram(prog);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
    throw new Error(gl.getProgramInfoLog(prog) || "link failed");
  }
  return prog;
}

export class Upscaler {
  #video: HTMLVideoElement | null = null;
  #canvas: HTMLCanvasElement | null = null;
  #gl: WebGL2RenderingContext | null = null;
  #progs = new Map<string, WebGLProgram>();
  #tex: WebGLTexture | null = null;
  #tid: number | null = null;
  #ro: ResizeObserver | null = null;
  #off: (() => void) | null = null;
  #mode: Mode = "fsr";
  #amount = 1.0;
  #chain: Chain | null = null;
  #quad: WebGLBuffer | null = null;
  #running = false;

  get on(): boolean {
    return this.#running;
  }

  get mode(): Mode {
    return this.#mode;
  }

  setMode(m: Mode) {
    this.#mode = m;
  }

  // 0 is a passthrough, 1 the tuned default. Exposed so the effect can be
  // dialled rather than argued about.
  setAmount(a: number) {
    this.#amount = Math.max(0, Math.min(2, a));
    this.#render();
  }

  start(video: HTMLVideoElement, mode: Mode): boolean {
    if (this.#running) this.stop();
    if (!supported()) return false;

    this.#mode = mode;
    this.#video = video;

    const canvas = document.createElement("canvas");
    canvas.setAttribute("aria-hidden", "true");
    Object.assign(canvas.style, {
      position: "absolute",
      inset: "0",
      width: "100%",
      height: "100%",
      objectFit: "contain",
      pointerEvents: "none",
      zIndex: "1",
    } as CSSStyleDeclaration);

    const gl = canvas.getContext("webgl2", {
      alpha: false,
      antialias: false,
      depth: false,
      stencil: false,
      // MUST be true. requestVideoFrameCallback only fires when a new frame is
      // presented, so the loop stops dead on pause and on the last frame of a
      // seek — and without this the buffer is cleared after compositing, so the
      // canvas goes BLACK exactly when the viewer is looking hardest at a still
      // image. Costs one copy per frame; a black pause is not a tradeoff.
      preserveDrawingBuffer: true,
      powerPreference: "high-performance",
    });
    if (!gl) return false;

    try {
      this.#progs.set("fsr", compile(gl, VERT, EASU));
      this.#progs.set("anime", compile(gl, VERT, ANIME));
      this.#progs.set("sharp", compile(gl, VERT, SHARP));
    } catch (e) {
      console.warn("[upscale]", e);
      return false;
    }

    const buf = gl.createBuffer();
    this.#quad = buf;
    gl.bindBuffer(gl.ARRAY_BUFFER, buf);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
    for (const prog of this.#progs.values()) {
      const loc = gl.getAttribLocation(prog, "p");
      gl.enableVertexAttribArray(loc);
      gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
    }

    const tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);

    // 17 convolution passes. Built once per session, not per frame, and only
    // when it is the mode in use — compiling them costs ~100ms.
    if (mode === "anime") {
      a4kDebug.on = !!(window as any).__a4kDebug;
      const chain = new Chain(gl, [restoreCNN, upscaleCNN]);
      if (chain.ok) {
        this.#chain = chain;
      } else {
        chain.destroy();
        console.warn("[upscale] no float render targets; using the analytic shader");
      }
    }

    this.#canvas = canvas;
    this.#gl = gl;
    this.#tex = tex;

    video.parentElement?.appendChild(canvas);
    // The video keeps playing and stays the element that owns audio, PiP and
    // fullscreen — it is only visually replaced by its own upscaled frames.
    video.style.opacity = "0";

    this.#ro = new ResizeObserver(() => {
      this.#resize();
      this.#render();
    });
    this.#ro.observe(canvas);
    this.#resize();

    // rVFC is the steady-state driver, but these change what should be on
    // screen without necessarily presenting a frame through it.
    for (const ev of ["seeked", "pause", "loadeddata"]) {
      video.addEventListener(ev, this.#render);
    }
    this.#off = () => {
      for (const ev of ["seeked", "pause", "loadeddata"]) {
        video.removeEventListener(ev, this.#render);
      }
    };

    this.#running = true;
    this.#queue();

    return true;
  }

  #resize() {
    const c = this.#canvas;
    if (!c) return;

    // Cap at 4K of backing store: beyond that the upload dominates and nobody
    // can see the difference.
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    const w = Math.min(Math.round(c.clientWidth * dpr), 3840);
    const h = Math.min(Math.round(c.clientHeight * dpr), 2160);
    if (w > 0 && h > 0 && (c.width !== w || c.height !== h)) {
      c.width = w;
      c.height = h;
    }
  }

  #queue() {
    const v = this.#video as any;
    if (!this.#running || !v) return;

    this.#tid = v.requestVideoFrameCallback
      ? v.requestVideoFrameCallback(this.#draw)
      : (requestAnimationFrame(this.#draw) as unknown as number);
  }

  #draw = () => {
    this.#render();
    this.#queue();
  };

  #render = () => {
    const gl = this.#gl;
    const v = this.#video;
    const c = this.#canvas;
    if (!this.#running || !gl || !v || !c) return;

    if (v.readyState >= 2 && v.videoWidth > 0) {
      gl.bindTexture(gl.TEXTURE_2D, this.#tex);
      // Flip ONCE, here. A video frame arrives with its first row at the top;
      // GL textures start bottom-left. Doing it at upload means every pass
      // downstream — analytic or CNN, and every intermediate target — lives in
      // the same space.
      gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, v);

      // Letterbox: the canvas fills the wrapper, the frame keeps its aspect.
      const fit = Math.min(c.width / v.videoWidth, c.height / v.videoHeight);
      const fw = Math.round(v.videoWidth * fit);
      const fh = Math.round(v.videoHeight * fit);
      const fx = (c.width - fw) >> 1;
      const fy = (c.height - fh) >> 1;

      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      gl.viewport(0, 0, c.width, c.height);
      gl.clearColor(0, 0, 0, 1);
      gl.clear(gl.COLOR_BUFFER_BIT);
      gl.viewport(fx, fy, fw, fh);

      if (this.#chain) {
        this.#chain.run(this.#tex!, v.videoWidth, v.videoHeight, fx, fy, fw, fh);

        return;
      }

      const prog = this.#progs.get(this.#mode) ?? this.#progs.get("sharp")!;
      gl.useProgram(prog);
      gl.uniform1i(gl.getUniformLocation(prog, "src"), 0);
      gl.uniform2f(
        gl.getUniformLocation(prog, "srcPx"),
        1 / v.videoWidth,
        1 / v.videoHeight,
      );
      gl.uniform1f(gl.getUniformLocation(prog, "amount"), this.#amount);
      gl.bindBuffer(gl.ARRAY_BUFFER, this.#quad);
      const aloc = gl.getAttribLocation(prog, "p");
      gl.enableVertexAttribArray(aloc);
      gl.vertexAttribPointer(aloc, 2, gl.FLOAT, false, 0, 0);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, this.#tex);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
    }
  };

  stop() {
    this.#running = false;

    const v = this.#video as any;
    if (this.#tid !== null && v) {
      if (v.cancelVideoFrameCallback) v.cancelVideoFrameCallback(this.#tid);
      else cancelAnimationFrame(this.#tid);
    }
    this.#tid = null;

    this.#ro?.disconnect();
    this.#ro = null;
    this.#off?.();
    this.#off = null;

    if (this.#video) this.#video.style.opacity = "";
    this.#canvas?.remove();

    // Free the GPU objects rather than waiting for the context to be reclaimed:
    // a page that toggles repeatedly would otherwise stack live contexts, and
    // browsers cap how many may exist at once.
    const gl = this.#gl;
    this.#chain?.destroy();
    this.#chain = null;
    if (gl) {
      if (this.#quad) gl.deleteBuffer(this.#quad);
      for (const p of this.#progs.values()) gl.deleteProgram(p);
      if (this.#tex) gl.deleteTexture(this.#tex);
      gl.getExtension("WEBGL_lose_context")?.loseContext();
    }
    this.#progs.clear();

    this.#quad = null;
    this.#canvas = null;
    this.#gl = null;
    this.#tex = null;
    this.#video = null;
  }
}
