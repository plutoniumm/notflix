import { parseName } from "./torrentName";
import { vidURL } from "../core/video";

// An absolute calendar date, for the title attribute and the detail panel —
// fmt.ts owns number→string, and a locale date is neither a number nor
// drifting (one call site, one Intl call). "ago" still does every relative
// date in the table itself.
export const day = (ms?: number) =>
  ms
    ? new Date(ms).toLocaleString(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      })
    : "—";

// The row carries a rel path; the player wants (dir, name) like every other
// link in the app.
export function playURL(rel: string): string {
  const i = rel.lastIndexOf("/");

  return vidURL(i < 0 ? "." : rel.slice(0, i), rel.slice(i + 1));
}

// One row per FILE, as /api/history serves it. Every field is optional
// because every field comes from a different store: absent means "no store
// knows", which the table prints as a dash.
export type HRow = {
  key: string;
  name: string;
  rel?: string;
  size?: number;
  disk: boolean;
  tor?: string;
  ih?: string;
  dlAt?: number;
  procAt?: number;
  firstAt?: number;
  lastAt?: number;
  sess?: number;
  t?: number;
  dur?: number;
  dev?: string;
  marks?: string[];
  steps?: Record<string, string>;
  hls?: boolean;
};

export type Node = {
  id: string;
  label: string;
  sub: string; // the episode range + gaps, or "" for a leaf
  rows: HRow[]; // every row beneath, for the rolled-up columns
  kids: Node[];
  leaf?: HRow;
};

const pad = (n: number) => String(n).padStart(2, "0");

// Runs of consecutive episodes. The caller collapses each run to one line and
// shows what falls between them: a missing episode is information, not noise
// to smooth over.
export function runs(eps: number[]): [number, number][] {
  const sorted = [...new Set(eps)].sort((a, b) => a - b);
  const out: [number, number][] = [];

  for (const e of sorted) {
    const last = out[out.length - 1];
    if (last && e === last[1] + 1) last[1] = e;
    else out.push([e, e]);
  }

  return out;
}

export function gaps(eps: number[]): number[] {
  const sorted = [...new Set(eps)].sort((a, b) => a - b);
  const out: number[] = [];

  for (let i = 1; i < sorted.length; i++)
    for (let e = sorted[i - 1] + 1; e < sorted[i] && out.length < 400; e++)
      out.push(e);

  return out;
}

const GAPS_SHOWN = 6;

// "S01E01–E09, E11–E12 (E10 missing)". A long gap list is truncated with a
// count so one hole in a 1000-episode anime can't push the row off screen.
export function rangeLabel(season: number | undefined, eps: number[]): string {
  if (!eps.length) return "";

  const s = season === undefined ? "" : `S${pad(season)}`;
  const parts = runs(eps).map(([a, b]) =>
    a === b ? `E${pad(a)}` : `E${pad(a)}–E${pad(b)}`,
  );
  const head = s + parts.join(", ");

  const miss = gaps(eps);
  if (!miss.length) return head;

  const shown = miss
    .slice(0, GAPS_SHOWN)
    .map((e) => `E${pad(e)}`)
    .join(", ");
  const rest = miss.length > GAPS_SHOWN ? ` +${miss.length - GAPS_SHOWN}` : "";

  return `${head} (${shown}${rest} missing)`;
}

export type Agg = {
  n: number;
  disk: number;
  size: number;
  dlAt: number;
  lastAt: number;
  firstAt: number;
  sess: number;
  watched: number;
  pct: number;
};

export function agg(rows: HRow[]): Agg {
  const a: Agg = {
    n: rows.length,
    disk: 0,
    size: 0,
    dlAt: 0,
    lastAt: 0,
    firstAt: 0,
    sess: 0,
    watched: 0,
    pct: 0,
  };

  for (const r of rows) {
    if (r.disk) a.disk++;
    a.size += r.size ?? 0;
    a.sess += r.sess ?? 0;
    if (r.dur && Math.min(100, ((r.t ?? 0) / r.dur) * 100) > 50) a.watched++;
    a.dlAt = Math.max(a.dlAt, r.dlAt ?? 0);
    a.lastAt = Math.max(a.lastAt, r.lastAt ?? 0);
    if (r.firstAt)
      a.firstAt = a.firstAt ? Math.min(a.firstAt, r.firstAt) : r.firstAt;
    if (r.dur)
      a.pct = Math.max(a.pct, Math.min(100, ((r.t ?? 0) / r.dur) * 100));
  }

  return a;
}

export const progress = (r: HRow) =>
  r.dur ? Math.min(100, ((r.t ?? 0) / r.dur) * 100) : 0;

// Watched is binary here, not a percentage: a column of 90%/92%/94% bars is
// unreadable at a glance, and the only question the table answers is "have I
// seen this". Past halfway counts as seen; the exact figure stays in the
// row's title attribute rather than in the scan path.
export const seen = (r: HRow) => progress(r) > 50;

export type SortKey = "title" | "dl" | "watch" | "size";

const sortVal = (a: Agg, k: SortKey) =>
  k === "dl" ? a.dlAt : k === "watch" ? a.lastAt : a.size;

export function sortNodes(nodes: Node[], k: SortKey, desc: boolean): Node[] {
  const sign = desc ? -1 : 1;
  const out = [...nodes].sort((x, y) => {
    if (k === "title")
      return (
        sign * x.label.localeCompare(y.label, undefined, { numeric: true })
      );
    const d = sortVal(agg(x.rows), k) - sortVal(agg(y.rows), k);

    return d === 0
      ? x.label.localeCompare(y.label, undefined, { numeric: true })
      : sign * d;
  });

  for (const n of out) n.kids = sortNodes(n.kids, k, desc);

  return out;
}

// Title → season → episodes. A row with neither season nor episode is a film
// and stays a leaf at the top level; a title whose rows are all films becomes
// one node per file rather than a fake season.
export function build(rows: HRow[]): Node[] {
  // Keyed on the squashed title, displayed with the nicest spelling seen: the
  // torrent name is often all lowercase and hyphenated where the on-disk name
  // is title-cased, and "star trek strange new worlds" must not sit beside
  // "Star Trek Strange New Worlds" as a second show.
  const byTitle = new Map<string, { label: string; rows: HRow[] }>();

  for (const r of rows) {
    const t = parseName(r.name).title || r.name;
    const k = t.toLowerCase().replace(/[^a-z0-9]+/g, "");
    const g = byTitle.get(k);
    if (!g) {
      byTitle.set(k, { label: t, rows: [r] });
      continue;
    }
    g.rows.push(r);
    if (/[A-Z]/.test(t) && !/[A-Z]/.test(g.label)) g.label = t;
  }

  const out: Node[] = [];

  for (const { label: title, rows: list } of byTitle.values()) {
    const episodic = list.filter((r) => {
      const p = parseName(r.name);

      return p.season !== undefined || p.episode !== undefined;
    });

    if (!episodic.length) {
      // Films: one leaf each, named by the file so two rips stay distinct.
      for (const r of list)
        out.push({
          id: `f:${r.key}`,
          label: list.length > 1 ? r.name : title,
          sub: "",
          rows: [r],
          kids: [],
          leaf: r,
        });

      continue;
    }

    const seasons = new Map<string, HRow[]>();
    for (const r of list) {
      const p = parseName(r.name);
      const k = p.season === undefined ? "" : String(p.season);
      const list2 = seasons.get(k);
      if (list2) list2.push(r);
      else seasons.set(k, [r]);
    }

    const kids: Node[] = [];
    for (const [sk, srows] of seasons) {
      const season = sk === "" ? undefined : Number(sk);
      const eps = srows
        .map((r) => parseName(r.name).episode)
        .filter((e): e is number => e !== undefined);

      kids.push({
        id: `t:${title}|${sk}`,
        label: season === undefined ? "Extras" : `Season ${season}`,
        sub: rangeLabel(season, eps),
        rows: srows,
        kids: srows.map((r) => ({
          id: `f:${r.key}`,
          label: epLabel(r),
          sub: "",
          rows: [r],
          kids: [],
          leaf: r,
        })),
      });
    }

    kids.sort((a, b) =>
      a.label.localeCompare(b.label, undefined, { numeric: true }),
    );
    out.push({ id: `t:${title}`, label: title, sub: "", rows: list, kids });
  }

  return out;
}

function epLabel(r: HRow): string {
  const p = parseName(r.name);
  if (p.season !== undefined && p.episode !== undefined)
    return `S${pad(p.season)}E${pad(p.episode)}`;
  if (p.episode !== undefined) return `E${pad(p.episode)}`;

  return p.pack ? "Pack" : r.name;
}
