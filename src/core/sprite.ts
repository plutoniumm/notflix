// Trickplay sheet maths, shared by the player dock and the card hover-scrub:
// one JPG tiled `cols × rows`, manifest from `/api/sprite/:key`. Each consumer
// formats its own CSS (the dock crops a fixed-size tile, a card stretches one
// cell over its box) but the index→cell step lives here only.

import { GET } from "./api";

export interface SpriteManifest {
  interval: number;
  cols: number;
  rows: number;
  count: number;
  tileW: number;
  tileH: number;
}

const cache = new Map<string, Promise<SpriteManifest | null>>();

// Cached per key including the null: a 404 is the routine "sprites phase hasn't
// run" answer, and a hovered row would otherwise refetch it on every pass.
export function manifest(key: string): Promise<SpriteManifest | null> {
  let hit = cache.get(key);
  if (!hit) {
    hit = GET(`/api/sprite/${key}`, { silent: true, tag: "sprite" }).then(
      (m) => (m && typeof m === "object" ? (m as SpriteManifest) : null),
    );
    cache.set(key, hit);
  }

  return hit;
}

export const sheet = (key: string) => `/images/${key}.sprite.jpg`;

// Tile index for a second, clamped into the sheet, plus its grid cell.
export function cell(m: SpriteManifest, sec: number) {
  const i = Math.min(Math.max(0, Math.floor(sec / m.interval)), m.count - 1);

  return {
    i,
    col: i % m.cols,
    row: Math.floor(i / m.cols),
    // Defensive: probe sometimes returns tileH=0; fall back to 16:9 of tileW.
    tileH: m.tileH > 0 ? m.tileH : Math.round((m.tileW * 9) / 16),
  };
}

// A second drawn at random from zone `z` of `zones` across the sheet's own
// runtime (`count * interval` — ffmpeg wrote one tile per interval, so no
// probe is needed). The card's hover-scrub splits the art the same way, so
// zone 0 is the opening 1/zones of the video.
export function zoneSec(
  m: SpriteManifest,
  z: number,
  zones: number,
  r = Math.random(),
): number {
  const a = Math.floor((z * m.count) / zones);
  const b = Math.floor(((z + 1) * m.count) / zones);

  return (a + Math.floor(r * Math.max(1, b - a))) * m.interval;
}
