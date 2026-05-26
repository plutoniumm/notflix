// Trickplay sprite client. One sprite per video — a single JPG tiled
// `cols × rows` with `interval`-second steps, plus a manifest from
// `/api/sprite/:key`. The dock's scrub hover calls `tile(sec)` to get an
// inline CSS string that crops the right cell from the sprite.
//
// Reactive on purpose: `manifest` is $state, so the dock's $derived tile
// re-runs once the fetch lands and the preview pops in without a page nudge.

export interface SpriteManifest {
  interval: number;
  cols: number;
  rows: number;
  count: number;
  tileW: number;
  tileH: number;
}

export class Sprite {
  manifest = $state<SpriteManifest | null>(null);
  url = $state("");

  async load(key: string): Promise<void> {
    if (!key) return;
    try {
      const r = await fetch(`/api/sprite/${key}`);
      if (!r.ok) return; // 404 → no sprite yet, dock falls back silently
      this.manifest = (await r.json()) as SpriteManifest;
      this.url = `/images/${key}.sprite.jpg`;
    } catch {
      // network/parse errors are benign — graceful degrade
    }
  }

  tile(sec: number): string {
    const m = this.manifest;
    if (!m || !this.url) return "";
    const i = Math.min(Math.max(0, Math.floor(sec / m.interval)), m.count - 1);
    const col = i % m.cols;
    // Defensive: probe sometimes returns tileH=0; fall back to 16:9 of tileW.
    const tH = m.tileH > 0 ? m.tileH : Math.round((m.tileW * 9) / 16);
    const row = Math.floor(i / m.cols);
    return (
      `background-image:url('${this.url}');` +
      `background-position:-${col * m.tileW}px -${row * tH}px;` +
      `background-size:${m.cols * m.tileW}px ${m.rows * tH}px;` +
      `width:${m.tileW}px;height:${tH}px;`
    );
  }
}
