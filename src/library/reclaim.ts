import { END_CUTOFF_S } from "../core/events.svelte";
import { join } from "../core/path";
import type { HRow } from "./history";

export const IDLE_DAYS = 30;

// The player clears a title's record at min(END_CUTOFF_S, 10%) from the end and
// the clear is never logged, so the furthest LOGGED position of a finished
// title sits just short of that line (measured: 4.7–4.9 min left on 47-min
// episodes whose cutoff is 4.7). The slack covers the save cadence.
const SLACK_S = 30;
const DAY = 86_400_000;

// Position is the live resume record when one exists, so a title at 90% that
// was reopened from the start is not "finished" just because it once was.
export function finished(r: HRow): boolean {
  if (!r.dur) return false;

  const t = r.resume || r.t || 0;

  return (
    r.dur - t <= Math.min(END_CUTOFF_S, r.dur * 0.1) + SLACK_S ||
    t >= r.dur * 0.95
  );
}

export const idle = (r: HRow, now: number) =>
  !!r.lastAt && now - r.lastAt >= IDLE_DAYS * DAY;

export type Pick = {
  id: string;
  dir: string;
  name?: string;
  n: number;
  size: number;
  lastAt: number;
  redl: boolean;
};

// Folder membership comes from the caller's listing of what is ON DISK, never
// from history rows: history joins on a normalised basename, and a folder must
// only be offered whole when every file actually in it is accounted for.
export function reclaim(
  rows: HRow[],
  dirs: Record<string, { name: string }[]>,
  now = Date.now(),
): Pick[] {
  const byRel = new Map<string, HRow>();
  for (const r of rows) if (r.disk && r.rel) byRel.set(r.rel, r);

  const out: Pick[] = [];

  for (const [dir, files] of Object.entries(dirs)) {
    const hits = (files ?? []).flatMap((f) => {
      const r = byRel.get(join(dir, f.name));

      return r && finished(r) && idle(r, now) ? [{ f, r }] : [];
    });

    if (dir !== "." && hits.length > 0 && hits.length === files.length) {
      out.push({
        id: `d:${dir}`,
        dir,
        n: hits.length,
        size: hits.reduce((n, h) => n + (h.r.size ?? 0), 0),
        lastAt: Math.max(...hits.map((h) => h.r.lastAt ?? 0)),
        redl: hits.every((h) => !!h.r.tor),
      });

      continue;
    }

    for (const { f, r } of hits)
      out.push({
        id: `f:${join(dir, f.name)}`,
        dir,
        name: f.name,
        n: 1,
        size: r.size ?? 0,
        lastAt: r.lastAt ?? 0,
        redl: !!r.tor,
      });
  }

  return out.sort((a, b) => b.size - a.size);
}
