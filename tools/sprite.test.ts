// Card hover-scrub maths, fed every real manifest under ./images: a zone must
// land in its own fifth of the runtime (to within the one tile that straddles
// the boundary), zones must not overlap, and the cell must stay on the sheet.
import { test } from "node:test";
import assert from "node:assert/strict";
import { register } from "node:module";
import { readFileSync, readdirSync } from "node:fs";

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

const dir = new URL("../images/", import.meta.url);
const sheets = readdirSync(dir)
  .filter((f) => f.endsWith(".sprite.json"))
  .slice(0, 300)
  .map((f) => JSON.parse(readFileSync(new URL(f, dir), "utf8")));

let urls: string[] = [];
let missing = false;
(globalThis as any).fetch = async (url: string) => {
  urls.push(url);

  return {
    ok: !missing,
    status: missing ? 404 : 200,
    headers: { get: () => "application/json" },
    text: async () => (missing ? "" : JSON.stringify(sheets[0])),
  };
};

const { cell, manifest, zoneSec } = await import("../src/core/sprite.ts");

const ZONES = 5;

test("every zone lands in its own fifth, in order", () => {
  assert.ok(sheets.length > 10, "no sprite manifests on disk to check against");

  for (const m of sheets) {
    const runtime = m.count * m.interval;
    let prev = -1;

    for (let z = 0; z < ZONES; z++) {
      const lo = zoneSec(m, z, ZONES, 0);
      const hi = zoneSec(m, z, ZONES, 0.9999);

      assert.ok(lo > prev, `zone ${z} starts inside zone ${z - 1}`);
      assert.ok(hi >= lo, `zone ${z} runs backwards`);
      prev = hi;

      for (const sec of [lo, zoneSec(m, z, ZONES, 0.5), hi]) {
        const f = sec / runtime;

        // One tile of slack at the low end: the tile holding the boundary
        // second belongs to the zone that starts there.
        assert.ok(f >= z / ZONES - 1 / m.count, `zone ${z} fell short: ${f}`);
        assert.ok(f < (z + 1) / ZONES, `zone ${z} overran: ${f}`);

        const c = cell(m, sec);
        assert.equal(c.i, Math.floor(sec / m.interval));
        assert.ok(c.i >= 0 && c.i < m.count, `tile ${c.i} off the sheet`);
        assert.ok(c.col < m.cols && c.row < m.rows, "cell off the grid");
        assert.ok(c.tileH > 0, "tileH must never be 0 — it divides the crop");
      }
    }
  }
});

test("zone 0 opens the file and the last zone reaches the end", () => {
  for (const m of sheets) {
    assert.equal(zoneSec(m, 0, ZONES, 0), 0);
    assert.equal(
      zoneSec(m, ZONES - 1, ZONES, 0.9999),
      (m.count - 1) * m.interval,
    );
  }
});

test("a one-tile sheet still yields a frame", () => {
  const m = { interval: 10, cols: 10, rows: 1, count: 1, tileW: 160, tileH: 0 };

  for (let z = 0; z < ZONES; z++) {
    assert.equal(zoneSec(m, z, ZONES, 0.9999), 0);
    assert.deepEqual(cell(m, 0), { i: 0, col: 0, row: 0, tileH: 90 });
  }
});

test("the manifest is fetched once per key, misses included", async () => {
  urls = [];
  const a = await manifest("0000001");
  const b = await manifest("0000001");

  assert.deepEqual(a, sheets[0]);
  assert.equal(b, a);
  assert.deepEqual(urls, ["/api/sprite/0000001"]);

  missing = true;
  urls = [];
  assert.equal(await manifest("0000002"), null);
  assert.equal(await manifest("0000002"), null);
  assert.equal(urls.length, 1, "a 404 must cache too — a hovered row refetches");
});
