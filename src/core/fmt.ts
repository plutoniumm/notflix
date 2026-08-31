// The one place a number becomes a string. Every formatter clamps: NaN,
// Infinity and negatives are display noise (metadata that hasn't loaded, a
// clock-skewed timestamp) and must never leak "NaN:NaN" into the UI.

const pad = (n: number) => String(n).padStart(2, "0");

const clamp = (n: number) => (Number.isFinite(n) && n > 0 ? Math.floor(n) : 0);

export function dur(sec: number): string {
  const t = clamp(sec);
  const h = Math.floor(t / 3600);
  const m = Math.floor((t % 3600) / 60);

  return h > 0 ? `${h}:${pad(m)}:${pad(t % 60)}` : `${m}:${pad(t % 60)}`;
}

export function durWords(sec: number): string {
  const t = clamp(sec);
  const h = Math.floor(t / 3600);
  const m = Math.floor((t % 3600) / 60);
  const s = t % 60;

  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  if (m > 0) return s > 0 ? `${m}m ${s}s` : `${m}m`;

  return `${s}s`;
}

// 1000-base: disks and aria2 both report decimal units, and the library has
// always displayed them that way.
const TIERS: [number, string, number][] = [
  [1e12, "TB", 1],
  [1e9, "GB", 1],
  [1e6, "MB", 0],
  [1e3, "KB", 0],
];

export function bytes(b: number): string {
  if (!Number.isFinite(b)) return "—";

  const n = Math.max(0, b);
  for (const [size, unit, digits] of TIERS)
    if (n >= size) return `${(n / size).toFixed(digits)} ${unit}`;

  return `${Math.round(n)} B`;
}

export const rate = (bps: number) => `${bytes(bps)}/s`;

const AGO: [number, string][] = [
  [31536000, "y"],
  [2592000, "mo"],
  [86400, "d"],
  [3600, "h"],
  [60, "m"],
];

export function ago(atMs: number): string {
  const s = clamp((Date.now() - atMs) / 1000);
  for (const [size, unit] of AGO)
    if (s >= size) return `${Math.floor(s / size)}${unit} ago`;

  return "just now";
}

export const pct = (n: number, digits = 0) =>
  `${(Number.isFinite(n) ? n : 0).toFixed(digits)}%`;
