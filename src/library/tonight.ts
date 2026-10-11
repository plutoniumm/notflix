// What to watch now, as a short list with stated reasons.
//
// The catalogue grid is an attention-economy artefact: it exists to keep you
// choosing. This does the opposite — at most four candidates, each with the
// reason it is there and a visible end point, so the surface can be finished
// with instead of browsed. Nothing here is random; the same library state
// always produces the same list, because a shuffle is a variable reward.

import { ago, durWords } from "../core/fmt";
import { clean } from "../core/video";
import { join } from "../core/path";
import { parseName, parseDirSeason, epTag } from "./torrentName";
import type { ContinueItem } from "./homeData.svelte";

export type Pick = {
  dir: string;
  name: string;
  key: string;
  // Display title, not a filename. At 90px a release string is unreadable, and
  // the parser that already serves the torrent rows gets the show name and the
  // SxxEyy out of it properly.
  title: string;
  tag: string;
  sub: string;
  reason: string;
  action: string;
  left: string;
  pct: number;
  // The stopping cue streaming services leave out on purpose.
  end: string;
};

export const MAX = 4;

// Within 90s of the end, or past 93%, counts as watched: credits are not
// "unfinished", and offering them back is how a queue never empties.
export function finished(c: ContinueItem): boolean {
  if (!c.dur || c.dur <= 0) return false;

  return c.dur - c.t <= 90 || c.t / c.dur > 0.93;
}

const pctOf = (c: ContinueItem) =>
  c.dur && c.dur > 0 ? Math.min(100, (c.t / c.dur) * 100) : 0;

const leftOf = (c: ContinueItem) =>
  c.dur && c.dur > c.t ? `${durWords(c.dur - c.t)} left` : "";

function tail(files: string[], name: string, dir: string): string {
  const i = files.indexOf(name);
  if (i < 0 || dir === ".") return "";

  const rest = files.length - 1 - i;
  if (rest <= 0) return "last episode";

  return `${rest} more after this`;
}

export function tonight(data: VideoData, continues: ContinueItem[]): Pick[] {
  const byDir = new Map<string, string[]>();
  const keyOf = new Map<string, string>();
  for (const g of data) {
    const names = (g.files ?? []).map((f) => f.name).sort();
    byDir.set(g.dir, names);
    for (const f of g.files ?? []) keyOf.set(join(g.dir, f.name), f.key);
  }

  const seen = new Set(continues.map((c) => join(c.dir, c.name)));
  const recent = [...continues].sort((a, b) => b.at - a.at);
  const out: Pick[] = [];
  const taken = new Set<string>();

  const push = (p: Pick) => {
    const param = join(p.dir, p.name);
    if (taken.has(param) || out.length >= MAX) return;
    taken.add(param);
    out.push(p);
  };

  const make = (
    dir: string,
    name: string,
    reason: string,
    action: string,
    c?: ContinueItem,
  ): Pick | null => {
    const key = c?.key ?? keyOf.get(join(dir, name));
    if (!key) return null;

    const f = parseName(name);
    // Only a *season* folder names the thing being watched. A bucket like
    // "2010s" or "Movies" parses to a title too, and taking it put "2010s" at
    // 90px above a film called Cocktail.
    const d = dir === "." ? null : parseDirSeason(dir);
    const show = d && (d.season !== undefined || d.range) ? d.title : "";

    return {
      dir,
      name,
      key,
      // A folder titled with the show wins: the per-episode title carries the
      // episode's own name, which is the smaller of the two things on screen.
      title: clean(show || f.title || name),
      tag: epTag(f),
      sub: show && f.title && f.title !== show ? f.title : "",
      reason,
      action,
      left: c ? leftOf(c) : "",
      pct: c ? pctOf(c) : 0,
      end: tail(byDir.get(dir) ?? [], name, dir),
    };
  };

  // 1. The episode after the one you just finished — the single most likely
  //    intent, and the only one worth putting first.
  for (const c of recent) {
    if (!finished(c)) continue;

    const names = byDir.get(c.dir) ?? [];
    const i = names.indexOf(c.name);
    const next = i >= 0 ? names[i + 1] : undefined;
    if (!next || seen.has(join(c.dir, next))) continue;

    const p = make(c.dir, next, `after ${epTag(parseName(c.name)) || clean(c.name)}`, "Next episode");
    if (p) push(p);
    break;
  }

  // 2. Things genuinely left part-way, newest first.
  for (const c of recent) {
    if (finished(c)) continue;

    const p = make(
      c.dir,
      c.name,
      `stopped ${ago(c.at)}`,
      "Resume",
      c,
    );
    if (p) push(p);
  }

  // 3. Only if the list is still short: something never opened. Oldest dirs
  //    first is deliberate — the newest arrival is the one already on your mind.
  if (out.length < MAX) {
    for (const g of [...data].reverse()) {
      for (const name of byDir.get(g.dir) ?? []) {
        if (seen.has(join(g.dir, name))) continue;

        const p = make(g.dir, name, "not started", "Start");
        if (p) push(p);
        break;
      }
      if (out.length >= MAX) break;
    }
  }

  return out;
}
