// Runs mpv-style user shaders (the `//!HOOK` dialect Anime4K ships in) as a
// WebGL2 multi-pass chain.
//
// Why adapt rather than rewrite: the Anime4K CNN weights ARE the shader — tens
// of thousands of mat4 literals inline in the GLSL. Reformatting them by hand
// would be transcription with no upside, and it would make re-pulling upstream
// impossible. So the files are vendored byte-for-byte and the handful of
// mpv-isms are supplied here.
//
// What mpv gives a pass, and what this provides:
//
//   NAME_tex(pos)     sample the bound texture at a normalised position
//   NAME_texOff(off)  sample it offset by `off` TEXELS from this fragment
//   NAME_pos          this fragment's position in that texture's space
//   NAME_size         its dimensions in texels
//   NAME_pt           1/size, i.e. one texel step
//
// Intermediate passes hold convolution activations, which are signed and
// routinely leave [0,1]. They must land in float textures — in a UNORM target
// everything negative clamps to zero and the network silently outputs mush.

export type Pass = {
  binds: string[];
  save: string;
  width: string[];
  height: string[];
  body: string;
};

// WIDTH/HEIGHT are RPN over `NAME.w` / `NAME.h`, numbers and operators:
// `conv2d_last_tf.w 2 *` means "twice that texture's width".
export function evalSize(rpn: string[], dims: Map<string, [number, number]>): number {
  const st: number[] = [];

  for (const tok of rpn) {
    if (tok === "*" || tok === "/" || tok === "+" || tok === "-") {
      const b = st.pop() ?? 0;
      const a = st.pop() ?? 0;
      st.push(tok === "*" ? a * b : tok === "/" ? a / b : tok === "+" ? a + b : a - b);

      continue;
    }

    const m = /^(\w+)\.([wh])$/.exec(tok);
    if (m) {
      const d = dims.get(m[1]);
      st.push(d ? (m[2] === "w" ? d[0] : d[1]) : 0);

      continue;
    }

    const n = parseFloat(tok);
    st.push(Number.isFinite(n) ? n : 0);
  }

  return Math.max(1, Math.round(st.pop() ?? 1));
}

export function parse(src: string): Pass[] {
  const out: Pass[] = [];
  let dirs: [string, string[]][] = [];
  let body: string[] = [];
  let hook = "MAIN";

  // A pass ENDS where the next directive block begins. Keying on //!DESC is not
  // enough: Anime4K writes DESC first, FSRCNNX writes HOOK first and sometimes
  // omits DESC entirely, so a DESC-triggered split silently attached FSRCNNX's
  // HOOK line to the previous pass.
  const flush = () => {
    if (!dirs.length) {
      body = [];

      return;
    }

    const get = (k: string) => dirs.find(([d]) => d === k)?.[1];
    hook = get("HOOK")?.[0] ?? hook;

    const pass: Pass = {
      binds: dirs.filter(([d]) => d === "BIND").map(([, v]) => (v[0] === "HOOKED" ? hook : v[0])),
      // No SAVE means "write back to what you hooked" — that is how both
      // FSRCNNX and Anime4K end their chains.
      save: get("SAVE")?.[0] ?? hook,
      // No WIDTH/HEIGHT means "same size as what you hooked".
      width: get("WIDTH") ?? [`${hook}.w`],
      height: get("HEIGHT") ?? [`${hook}.h`],
      body: body.join("\n"),
    };

    if (pass.body.trim()) out.push(pass);
    dirs = [];
    body = [];
  };

  for (const raw of src.split("\n")) {
    let line = raw.trimEnd();

    if (line.startsWith("//!")) {
      // A directive after body text opens the next pass.
      if (body.length) flush();
      const [key, ...rest] = line.slice(3).trim().split(/\s+/);
      // COMPONENTS and WHEN are mpv scheduling concerns; the caller has already
      // decided to upscale, so the chain runs unconditionally.
      dirs.push([key, rest]);

      continue;
    }
    if (!dirs.length) continue;

    // HOOKED is mpv's alias for whatever this pass hooked. Normalise it so the
    // generated accessors line up with the bound texture's name.
    const h = dirs.find(([d]) => d === "HOOK")?.[1]?.[0] ?? hook;
    if (h !== "HOOKED") line = line.replace(/\bHOOKED_/g, `${h}_`);
    body.push(line);
  }
  flush();

  return out;
}

const VERT = `#version 300 es
in vec2 p;
out vec2 vpos;
void main() {
  // No y-flip here, deliberately. The video texture is uploaded with
  // UNPACK_FLIP_Y_WEBGL so it already matches GL's bottom-left origin, and
  // every intermediate render target is produced in that same space. Flipping
  // per pass instead flips the INTERMEDIATES too: the luma texture came back
  // upside down relative to the frame it was split from, and the merge then
  // added the delta from the opposite row. The image still looked coherent,
  // which is what made it survive a visual check.
  vpos = p * 0.5 + 0.5;
  gl_Position = vec4(p, 0.0, 1.0);
}`;

// The accessor surface, generated per bound texture.
function preamble(binds: string[]): string {
  const parts = [
    `#version 300 es`,
    `precision highp float;`,
    `precision highp sampler2D;`,
    `in vec2 vpos;`,
    `out vec4 fragColor;`,
  ];

  for (const b of binds) {
    parts.push(
      `uniform sampler2D ${b}_raw;`,
      `uniform vec2 ${b}_size;`,
      `#define ${b}_pt (1.0 / ${b}_size)`,
      `#define ${b}_pos vpos`,
      // MACROS, not functions. mpv defines texOff as `vec2(off)` inside a
      // macro, so shaders legitimately call it with a scalar — FSRCNNX writes
      // `FEATURE1_texOff(0)` meaning vec2(0.0). A `vec4 f(vec2)` overload
      // rejects that, and GLSL ES has no implicit scalar-to-vector conversion.
      // The inner vec2() is an identity constructor for a vec2 argument, so
      // both call styles work.
      `#define ${b}_tex(q) (texture(${b}_raw, vec2(q)))`,
      `#define ${b}_texOff(o) (texture(${b}_raw, vpos + vec2(o) * ${b}_pt))`,
    );
  }

  return parts.join("\n");
}

type Target = { tex: WebGLTexture; fb: WebGLFramebuffer; w: number; h: number };

// Anime4K's widest pass binds 8 textures; 15 is the floor WebGL2 guarantees.
const SCRATCH = 15;

export const debug = { log: [] as string[], on: false };
if (typeof window !== "undefined") (window as any).__a4kLog = debug.log;

export class Chain {
  #gl: WebGL2RenderingContext;
  #progs: WebGLProgram[] = [];
  #passes: Pass[] = [];
  #targets = new Map<string, Target>();
  #quad: WebGLBuffer | null = null;
  ok = false;

  constructor(gl: WebGL2RenderingContext, sources: string[]) {
    this.#gl = gl;

    // Signed activations need a float render target. Without this extension the
    // chain cannot run correctly at all, so the caller falls back rather than
    // rendering something subtly wrong.
    const half = gl.getExtension("EXT_color_buffer_half_float");
    const full = gl.getExtension("EXT_color_buffer_float");
    if (debug.on) debug.log.push(`ext half=${!!half} float=${!!full}`);
    if (!half && !full) {
      return;
    }
    gl.getExtension("OES_texture_float_linear");

    for (const src of sources) this.#passes.push(...parse(src));
    if (!this.#passes.length) return;

    try {
      for (let i = 0; i < this.#passes.length; i++) {
        const p = this.#passes[i];
        try {
          this.#progs.push(this.#compile(preamble(p.binds) + "\n" + p.body + "\nvoid main() { fragColor = hook(); }"));
        } catch (e) {
          throw new Error(`pass ${i} save=${p.save} binds=[${p.binds}]: ${e}`);
        }
      }
    } catch (e) {
      if (debug.on) debug.log.push(`compile: ${e}`);
      console.warn("[anime4k]", e);

      return;
    }

    this.#quad = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, this.#quad);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);

    this.ok = true;
  }

  #compile(fs: string): WebGLProgram {
    const gl = this.#gl;
    const mk = (type: number, src: string) => {
      const sh = gl.createShader(type)!;
      gl.shaderSource(sh, src);
      gl.compileShader(sh);
      if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
        throw new Error((gl.getShaderInfoLog(sh) || "").slice(0, 300));
      }

      return sh;
    };

    const prog = gl.createProgram()!;
    gl.attachShader(prog, mk(gl.VERTEX_SHADER, VERT));
    gl.attachShader(prog, mk(gl.FRAGMENT_SHADER, fs));
    gl.linkProgram(prog);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
      throw new Error((gl.getProgramInfoLog(prog) || "").slice(0, 300));
    }

    return prog;
  }

  #target(name: string, w: number, h: number): Target {
    const gl = this.#gl;
    const got = this.#targets.get(name);
    if (got && got.w === w && got.h === h) return got;

    if (got) {
      gl.deleteTexture(got.tex);
      gl.deleteFramebuffer(got.fb);
    }

    const tex = gl.createTexture()!;
    // A scratch unit, never one the passes sample from. Creating a texture
    // binds it to the ACTIVE unit, so doing this while an input unit was
    // selected left the render target bound as a sampler — a feedback loop,
    // which WebGL answers with INVALID_OPERATION on every draw.
    gl.activeTexture(gl.TEXTURE0 + SCRATCH);
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA16F, w, h, 0, gl.RGBA, gl.HALF_FLOAT, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

    const fb = gl.createFramebuffer()!;
    gl.bindFramebuffer(gl.FRAMEBUFFER, fb);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
    if (debug.on) {
      const st = gl.checkFramebufferStatus(gl.FRAMEBUFFER);
      debug.log.push(`fbo ${name} ${w}x${h} status=${st === gl.FRAMEBUFFER_COMPLETE ? "COMPLETE" : st}`);
    }

    const t = { tex, fb, w, h };
    this.#targets.set(name, t);

    return t;
  }

  // `main` is the source frame. The last pass draws to the default framebuffer
  // at the viewport the caller has already set.
  run(
    main: WebGLTexture,
    mainW: number,
    mainH: number,
    outX: number,
    outY: number,
    outW: number,
    outH: number,
  ) {
    const gl = this.#gl;
    if (!this.ok) return;

    const dims = new Map<string, [number, number]>([["MAIN", [mainW, mainH]]]);
    const texOf = new Map<string, WebGLTexture>([["MAIN", main]]);

    for (let i = 0; i < this.#passes.length; i++) {
      const pass = this.#passes[i];
      const last = i === this.#passes.length - 1;
      const w = last ? outW : evalSize(pass.width, dims);
      const h = last ? outH : evalSize(pass.height, dims);

      let wrote: { name: string; tex: WebGLTexture; w: number; h: number } | null = null;

      const prog = this.#progs[i];
      if (debug.on) { while (gl.getError()) {} }
      gl.useProgram(prog);
      if (debug.on && gl.getError()) debug.log.push(`p${i} useProgram`);

      gl.bindBuffer(gl.ARRAY_BUFFER, this.#quad);
      const loc = gl.getAttribLocation(prog, "p");
      gl.enableVertexAttribArray(loc);
      gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
      if (debug.on && gl.getError()) debug.log.push(`p${i} attrib loc=${loc}`);

      if (last) {
        // The caller's viewport is the letterboxed rect on the canvas; an
        // intermediate pass has just overwritten it with its own size.
        gl.bindFramebuffer(gl.FRAMEBUFFER, null);
        gl.viewport(outX, outY, outW, outH);
      } else {
        // Both CNN chains end with a pass that BINDS MAIN and SAVES MAIN.
        // Sampling a texture while rendering into it is undefined — WebGL
        // rejects the draw outright — so an aliasing pass gets its own buffer
        // and the NAME is then repointed at it. Keying the physical target on
        // the pass index guarantees it differs from what the name resolves to.
        const physical = pass.binds.includes(pass.save) ? `${pass.save}~${i}` : pass.save;
        const t = this.#target(physical, w, h);
        gl.bindFramebuffer(gl.FRAMEBUFFER, t.fb);
        gl.viewport(0, 0, w, h);
        // Deliberately NOT remapped yet: a pass that saves over a name it also
        // binds must still READ the old texture. The remap happens after the
        // draw, below.
        wrote = { name: pass.save, tex: t.tex, w, h };
      }

      pass.binds.forEach((b, unit) => {
        gl.activeTexture(gl.TEXTURE0 + unit);
        gl.bindTexture(gl.TEXTURE_2D, texOf.get(b) ?? main);

        // A bind the pass never reads is optimised out, and getUniformLocation
        // then returns null — which is NOT a silent no-op, it raises
        // INVALID_OPERATION and poisons the rest of the frame. Anime4K's later
        // passes bind textures they only use in one branch, so this is the
        // common case, not an edge case.
        const sampler = gl.getUniformLocation(prog, `${b}_raw`);
        if (sampler) gl.uniform1i(sampler, unit);

        const size = gl.getUniformLocation(prog, `${b}_size`);
        if (size) {
          const d = dims.get(b) ?? [mainW, mainH];
          gl.uniform2f(size, d[0], d[1]);
        }
        if (debug.on && gl.getError()) debug.log.push(`p${i} bind ${b} u${unit}`);
      });

      if (debug.on && gl.getError()) debug.log.push(`p${i} fbo bind`);
      if (debug.on) {
        gl.validateProgram(prog);
        if (!gl.getProgramParameter(prog, gl.VALIDATE_STATUS)) {
          debug.log.push(`p${i} validate: ${(gl.getProgramInfoLog(prog) || "").slice(0, 160)}`);
        }
        while (gl.getError()) {}
      }
      gl.drawArrays(gl.TRIANGLES, 0, 3);
      if (debug.on) {
        const e = gl.getError();
        if (e) debug.log.push(`pass ${i} (${pass.save}) glError=${e}`);
      }

      if (wrote) {
        dims.set(wrote.name, [wrote.w, wrote.h]);
        texOf.set(wrote.name, wrote.tex);
      }
    }

    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  }

  destroy() {
    const gl = this.#gl;
    for (const p of this.#progs) gl.deleteProgram(p);
    for (const t of this.#targets.values()) {
      gl.deleteTexture(t.tex);
      gl.deleteFramebuffer(t.fb);
    }
    if (this.#quad) gl.deleteBuffer(this.#quad);
    this.#progs = [];
    this.#targets.clear();
    this.ok = false;
  }
}
