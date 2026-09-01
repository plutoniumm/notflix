// Trickplay sprite client: one JPG tiled `cols × rows`, manifest from
// `/api/sprite/:key`, `tile(sec)` returns inline CSS cropping one cell.
// `manifest` is $state (hence .svelte.ts) so the dock's tile re-runs on load.

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
