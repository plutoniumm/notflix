// Dock-side wrapper over core/sprite: `tile(sec)` returns inline CSS cropping
// one cell at its native size. `manifest` is $state (hence .svelte.ts) so the
// dock's tile re-runs on load.

import { cell, manifest, sheet, type SpriteManifest } from "../core/sprite";

export type { SpriteManifest };

export class Sprite {
  manifest = $state<SpriteManifest | null>(null);
  url = $state("");

  async load(key: string): Promise<void> {
    if (!key) return;

    const m = await manifest(key);
    if (!m) return; // no sprite yet, dock falls back silently

    this.manifest = m;
    this.url = sheet(key);
  }

  tile(sec: number): string {
    const m = this.manifest;
    if (!m || !this.url) return "";

    const { col, row, tileH } = cell(m, sec);

    return (
      `background-image:url('${this.url}');` +
      `background-position:-${col * m.tileW}px -${row * tileH}px;` +
      `background-size:${m.cols * m.tileW}px ${m.rows * tileH}px;` +
      `width:${m.tileW}px;height:${tileH}px;`
    );
  }
}
