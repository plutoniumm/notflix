// Runtime tests for watch-progress persistence. Run: node tools/watch.test.ts
import { register } from "node:module";

// Source imports are extensionless (Vite bundler resolution); teach Node to
// retry with .ts so the real modules load unmodified.
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

// Runes are compile-time in Svelte but plain calls syntactically, so an
// identity shim lets the real modules import under bare Node.
(globalThis as any).$state = Object.assign((v: any) => v, {
  snapshot: (v: any) => v,
});
(globalThis as any).$derived = (v: any) => v;

let beacons: any[] = [];
let posts: any[] = [];
let gets: string[] = [];
let getFails = false;

Object.defineProperty(globalThis, "navigator", {
  configurable: true,
  value: {
    sendBeacon: (_url: string, blob: any) => {
      beacons.push(JSON.parse(blob.__text));
      return true;
    },
  },
});

// Blob body isn't readable synchronously; keep the text alongside it.
const RealBlob = globalThis.Blob;
(globalThis as any).Blob = class extends RealBlob {
  __text: string;
  constructor(parts: any[], opts?: any) {
    super(parts, opts);
    this.__text = parts.join("");
  }
};

(globalThis as any).fetch = async (url: string, init: any = {}) => {
  if (init.method === "POST") posts.push({ url, body: JSON.parse(init.body) });
  else {
    gets.push(url);
    if (getFails) throw new Error("network error");
  }

  return {
    ok: !getFails,
    status: getFails ? 500 : 200,
    headers: { get: () => "application/json" },
    text: async () => JSON.stringify({ value: { t: 1200, dur: 7200 } }),
  };
};

const { default: WatchProgress } = await import("../src/player/watch.ts");
const { toast } = await import("../src/core/toast.svelte.ts");

const w = new WatchProgress();
let failed = 0;
const check = (name: string, ok: boolean, got?: unknown) => {
  if (ok) return;
  failed++;
  console.log(`FAIL ${name}${got === undefined ? "" : ` — got ${JSON.stringify(got)}`}`);
};
const reset = () => {
  beacons = [];
  posts = [];
  gets = [];
};

// END_CUTOFF_S is 300. A flat cutoff cleared every position of any video
// shorter than that, so a short clip could never save progress at all.
reset();
w.save("clip.mp4", 100, 240); // 4-minute clip, 140s from the end
check("short clip saves", posts.length === 1 && posts[0].body.value?.t === 100, posts);

reset();
w.save("clip.mp4", 225, 240); // inside the relative 24s cutoff -> finished
check("short clip clears near end", posts.length === 1 && posts[0].body.value === null, posts);

reset();
w.save("film.mp4", 3600, 7200);
check("film saves mid-way", posts[0]?.body.value?.t === 3600, posts);

reset();
w.save("film.mp4", 6950, 7200); // 250s left, inside the flat 300s cutoff
check("film clears in last 5min", posts[0]?.body.value === null, posts);

reset();
w.save("film.mp4", 30, 7200); // below RESUME_THRESHOLD_S
check("no save below threshold", posts.length === 0, posts);

reset();
w.save("film.mp4", 99999, 7200); // insane position, rejected
check("insane position rejected", posts.length === 0, posts);

// The leave path must use sendBeacon: a suspending page drops a plain fetch.
reset();
w.flushOnLeave("film.mp4", 3600, 7200);
check("flushOnLeave beacons", beacons.length === 1 && posts.length === 0, { beacons, posts });
check("beacon carries position", beacons[0]?.value?.t === 3600, beacons);

reset();
w.clear("film.mp4");
check("clear beacons null", beacons.length === 1 && beacons[0].value === null, beacons);

// Resume must be silent: a red toast over the video on every flaky request
// is worse than starting from zero, which is what happens anyway.
reset();
toast.items = [];
getFails = true;
const t = await w.resume("film.mp4");
getFails = false;
check("resume returns 0 on failure", t === 0, t);
check("resume made a GET", gets.length === 1 && gets[0].includes("watched%3Afilm.mp4"), gets);
check("resume raises no toast", toast.items.length === 0, toast.items);

reset();
const t2 = await w.resume("film.mp4"); // t=1200, minus RESUME_THRESHOLD_S=60
check("resume seeks back 60s", t2 === 1140, t2);

console.log(failed ? `${failed} failure(s)` : "ok — watch progress");
process.exit(failed ? 1 : 0);
