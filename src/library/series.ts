import { parseName, parseDirSeason } from "./torrentName";

// Longest literal prefix shared by every name. Episodic rips differ from each
// other only past the show title; a pile of unrelated films shares nothing.
function stem(names: string[]): string {
  let p = names[0] ?? "";
  for (const n of names) {
    let i = 0;
    while (i < p.length && i < n.length && p[i] === n[i]) i++;
    p = p.slice(0, i);
  }

  return p;
}

const majority = (hits: number, n: number) => hits * 2 > n;

// A folder reads as a series when most files carry an episode number, OR when
// the names share a stem and the remainder STARTS with a digit. The second
// clause exists for bare-numbered anime rips ("Wangan Midnight 01 Devil Z"),
// which carry no S/E marker at all; requiring a numeric remainder is what
// keeps a one-franchise movie folder ("Harry Potter and the …") out, since
// those differ by words.
export function episodic(names: string[]): boolean {
  if (names.length < 2) return false;

  const eps = names.filter((n) => parseName(n).episode !== undefined).length;
  if (majority(eps, names.length)) return true;

  const s = stem(names);
  if (s.length < 4) return false;

  return majority(
    names.filter((n) => /^\d/.test(n.slice(s.length))).length,
    names.length,
  );
}

const pad = (n: number) => String(n).padStart(2, "0");

export const showKey = (title: string) =>
  title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .trim();

type Ep = { key: string; title: string; season?: number; ep: number; upto: number };

// The trailing digits come off: "Show 10".."Show 14" share the stem "Show 1",
// and slicing there would read episode 10 as 0.
const base = (names: string[]) =>
  names.length < 2 ? "" : stem(names).replace(/\d+$/, "");

const file = (rel: string) => rel.slice(rel.lastIndexOf("/") + 1);

// What an episode IS, not where it lives: watch records are dir-qualified and
// files move, so a watched S01E06 has to match on identity, never on path.
function ident(rel: string, s: string): Ep | null {
  const name = file(rel);
  const p = parseName(name);
  if (p.episode !== undefined) {
    return {
      key: showKey(p.title),
      title: p.title,
      season: p.season,
      ep: p.episode,
      upto: p.upto ?? p.episode,
    };
  }

  if (s.length < 4 || !name.startsWith(s)) return null;

  const m = name.slice(s.length).match(/^(\d{1,4})(?!\d)/);
  if (!m) return null;

  const title = s.replace(/[\s._-]+$/, "");

  return { key: showKey(title), title, ep: +m[1], upto: +m[1] };
}

const rank = (e: Ep) => (e.season ?? 0) * 1e4 + e.upto;

// One rail label per file, in the order given. Bare-numbered rips read their
// number off the name too, so a folder whose first four were deleted starts at
// 05, not 01.
export function ordinals(names: string[]): string[] {
  const s = base(names);
  const ids = names.map((n) => ident(n, s));
  const multi =
    new Set(ids.map((e) => e?.season).filter((n) => n !== undefined)).size > 1;

  return ids.map((e, i) => {
    if (!e) return pad(i + 1);

    return multi && e.season !== undefined ? `${e.season}·${pad(e.ep)}` : pad(e.ep);
  });
}

// Episode order, when every name has one. Filename order is not episode order:
// "…with You S01E02v2" sorts after "…With You S01E12" on a lowercase w.
export function order(names: string[]): string[] {
  const s = base(names);
  const ids = names.map((n) => ident(n, s));
  if (ids.some((e) => !e)) return names;

  return names
    .map((n, i) => ({ n, r: rank(ids[i]!) }))
    .sort((a, b) => a.r - b.r)
    .map((x) => x.n);
}

const slot = (key: string, season: number | undefined, n: number) =>
  `${key}|${season ?? ""}|${n}`;

// Episode -1 stands for "something from this season was watched".
function seenSet(watched: string[], s: string): Set<string> {
  const out = new Set<string>();
  for (const rel of watched) {
    const e = ident(rel, s);
    if (!e) continue;

    for (let n = e.ep; n <= e.upto; n++) out.add(slot(e.key, e.season, n));
    out.add(slot(e.key, e.season, -1));
  }

  return out;
}

function runs(lo: number, hi: number, skip: (n: number) => boolean): [number, number][] {
  const out: [number, number][] = [];
  for (let n = lo; n <= hi; n++) {
    if (skip(n)) continue;

    const last = out[out.length - 1];
    if (last && last[1] === n - 1) last[1] = n;
    else out.push([n, n]);
  }

  return out;
}

const span = (a: number, b: number, f: (n: number) => string) =>
  a === b ? f(a) : `${f(a)}–${f(b)}`;

export type Gap = {
  // Index into `names` of the episode the gap follows on the rail.
  after: number;
  // In the rail's own style ("05–06", "1·05"); `tag` stands alone ("S01E05–E06").
  label: string;
  tag: string;
  term: string;
};

// Only INTERIOR holes. The user deletes what they have watched, so missing
// leading episodes are history, not gaps; and with no episode count known, a
// missing tail can't be told from one not yet aired. A range in the folder name
// ("Bleach 140-167") IS that count for absolute numbering, so there the tail
// counts. Anything with a watch record was had and let go, never a gap.
export function gaps(dir: string, names: string[], watched: string[] = []): Gap[] {
  const s = base(names);
  const ids = names.map((n) => ident(n, s));
  const seen = seenSet(watched, s);
  const range = parseDirSeason(dir).range;
  const out: Gap[] = [];

  const after = (idx: number[], lt: (e: Ep) => boolean) =>
    idx
      .filter((i) => lt(ids[i]!))
      .reduce((a, i) => (a < 0 || rank(ids[i]!) >= rank(ids[a]!) ? i : a), -1);

  const groups = new Map<string, number[]>();
  const shows = new Map<string, number[]>();
  for (const [i, e] of ids.entries()) {
    if (!e) continue;

    const g = `${e.key}|${e.season ?? ""}`;
    groups.set(g, [...(groups.get(g) ?? []), i]);
    if (e.season !== undefined) shows.set(e.key, [...(shows.get(e.key) ?? []), i]);
  }

  const multi = new Set(ids.map((e) => e?.season).filter((n) => n !== undefined)).size > 1;

  for (const idx of groups.values()) {
    const e0 = ids[idx[0]]!;
    const have = new Set(
      idx.flatMap((i) => {
        const e = ids[i]!;

        return Array.from({ length: e.upto - e.ep + 1 }, (_, k) => e.ep + k);
      }),
    );
    const lo = Math.min(...have);
    let hi = Math.max(...have);
    if (range && e0.season === undefined && range[0] <= lo && range[1] >= hi) hi = range[1];

    const se = e0.season !== undefined ? `S${pad(e0.season)}` : "";
    const ord = (n: number) => (multi && se ? `${e0.season}·${pad(n)}` : pad(n));
    const holes = runs(lo, hi, (n) => have.has(n) || seen.has(slot(e0.key, e0.season, n)));
    for (const [a, b] of holes) {
      out.push({
        after: after(idx, (e) => e.upto < a),
        label: span(a, b, ord),
        tag: se + span(a, b, (n) => (se ? "E" : "") + pad(n)),
        term: `${e0.title} ${a === b ? se + (se ? "E" : "") + pad(a) : se || pad(a)}`.trim(),
      });
    }
  }

  // A season missing from between two present ones in a multi-season folder
  // (JoJo's "Seasons 1-4" pack).
  for (const idx of shows.values()) {
    const e0 = ids[idx[0]]!;
    const have = new Set(idx.map((i) => ids[i]!.season!));
    const holes = runs(
      Math.min(...have),
      Math.max(...have),
      (n) => have.has(n) || seen.has(slot(e0.key, n, -1)),
    );
    for (const [a, b] of holes) {
      const label = span(a, b, (n) => `S${pad(n)}`);
      out.push({
        after: after(idx, (e) => e.season! < a),
        label,
        tag: label,
        term: `${e0.title} S${pad(a)}`,
      });
    }
  }

  return out;
}

export type Hole = { n: number; label: string; term: string };

// The same rule one level up, between a show's folders: a season (S01 and S03
// on disk) or an episode range between two packs ("56-111", "140-167").
export function holes(
  title: string,
  seasons: { dir: string; n: number; files: { name: string }[] }[],
  watched: string[] = [],
): Hole[] {
  const key = showKey(title);
  const out: Hole[] = [];

  const nums = seasons.filter((s) => s.n < 1000).map((s) => s.n);
  if (nums.length > 1) {
    const seen = new Set(
      watched.flatMap((rel) => {
        const p = parseName(file(rel));
        const dir = rel.slice(0, Math.max(0, rel.lastIndexOf("/")));
        const season = p.season ?? parseDirSeason(dir).season;

        return showKey(p.title) === key && season !== undefined ? [season] : [];
      }),
    );
    const skip = (n: number) => nums.includes(n) || seen.has(n);
    for (const [a, b] of runs(Math.min(...nums), Math.max(...nums), skip))
      out.push({ n: a, label: span(a, b, (n) => `S${n}`), term: `${title} S${pad(a)}` });
  }

  const packs = seasons
    .map((s) => ({ s, r: parseDirSeason(s.dir).range }))
    .filter((x) => x.r && x.s.n >= 1000)
    .sort((a, b) => a.r![0] - b.r![0]);
  for (let i = 1; i < packs.length; i++) {
    const lo = packs[i - 1].r![1] + 1;
    const hi = packs[i].r![0] - 1;
    if (lo > hi) continue;

    const s = base(packs[i].s.files.map((f) => f.name));
    const seen = seenSet(watched, s);
    const k = showKey(s.replace(/[\s._-]+$/, ""));
    for (const [a, b] of runs(lo, hi, (n) => seen.has(slot(k, undefined, n))))
      out.push({ n: 1000 + a, label: span(a, b, String), term: `${title} ${a}` });
  }

  return out;
}
