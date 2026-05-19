// Heuristic torrent-name parser — no LLM. Scene/P2P names are dot/underscore
// separated with a fairly fixed token vocabulary, so regex extraction is
// reliable. parseName splits the muddled name into a clean title plus the
// structured fields the Explore UI sorts and filters on.

export type Parsed = {
  title: string;
  year?: number;
  season?: number;
  episode?: number;
  kind?: string; // "OVA" | "ONA" | "Batch" | "Specials"
  pack?: boolean; // full season / complete series / batch / episode range
  res?: string; // "1080p"
  resRank: number; // sortable: 2160>1080>720>480>0
  source?: string; // "BluRay"
  codec?: string; // "x265"
  audio?: string; // "DDP5.1"
  hdr?: string; // "HDR10" | "DV"
  bit?: string; // "10bit"
  langs: string[];
  group?: string;
};

const RES = /\b(4320p|2160p|1440p|1080p|720p|576p|480p|360p)\b|\b(4k|uhd)\b/i;
// (?!\d) instead of \b so a "v2"/title char right after the number doesn't
// veto the match (e.g. "S01E10v2"); \d{1,3} still caps the episode digits.
const SE = /\bS(\d{1,2})[ ._]?E(\d{1,3})(?!\d)/i;
const SXE = /\b(\d{1,2})x(\d{2,3})(?!\d)/i;
const SEASON = /\bS(?:eason)?[ ._-]?(\d{1,2})\b/i;
// "4th Season", "2nd-season".
const SEASON_ORD = /\b(\d{1,2})(?:st|nd|rd|th)[ ._-]?season\b/i;
const EP = /\b(?:Episode|Ep|E)[ ._-]?(\d{1,3})(?!\d)/i;
const YEAR = /\b(19\d{2}|20\d{2})\b/g;
const SOURCE =
  /\b(blu[- ]?ray|bdrip|brrip|bd25|bd50|web[- ]?dl|web[- ]?rip|webrip|web|hdrip|hd[- ]?tv|hdtv|dvdrip|dvdscr|dvd|hdcam|camrip|cam|telesync|\bts\b|pdtv|sdtv|remux)\b/i;
const CODEC = /\b(x265|x264|h[._ ]?265|h[._ ]?264|hevc|avc|av1|xvid|divx|vp9)\b/i;
const AUDIO =
  /\b(ddp?[._ ]?5[._ ]?1|dd\+?5[._ ]?1|dd\+|eac3|e[- ]ac[- ]3|ac3|dts[- ]?hd[- ]?ma|dts[- ]?hd|dts|truehd|atmos|aac[._ ]?2[._ ]?0|aac[._ ]?5[._ ]?1|aac|flac|opus|mp3)\b/i;
const HDR = /\b(hdr10\+?|hdr|dolby[._ ]?vision|dovi|\bdv\b|sdr)\b/i;
const BIT = /\b(10[._ ]?bit|8[._ ]?bit)\b/i;
const PACK = /\b(complete|collection|batch|season[._ ]?\d{1,2}|s\d{1,2}[._ ]?(?:complete|full)|all[._ ]?seasons)\b/i;
// Episode number stuck to an anime kind token: "OVA 6", "OVA-09", "ONA 3".
const KIND_EP = /\b(?:ova|ona|oad)\b[ ._-]*(\d{1,3})\b/i;
const GROUP = /-\s*([A-Za-z0-9]{2,})\s*$/;
const SITE = /^\s*(?:\[[^\]]*\]|\([^)]*\)|www\.[^\s]+|[A-Za-z0-9-]+\.(?:com|net|org|me|cc|info|tv)|metadata)\s*[-_. ]*/i;
const EXT = /\.(mp4|mkv|avi|mov|webm|flv|wmv|m4v|mpe?g|m2ts|ts|3gp|ogv|vob|rmvb|divx)$/i;
const MAX_LEN = 600; // cap input so no pathological string can stall a regex
// Leading fansub group: [SubsPlease] / [Erai-raws] / [sage] — kept as the
// release group when no trailing -GROUP is present.
const LEADGRP = /^\s*\[([A-Za-z][A-Za-z0-9._-]{1,18})\]/;
// Anime-specific. OVA/ONA/OAD/Specials/Batch are never standalone tokens in
// real film titles, so they're safe to treat as structured markers.
const KIND = /\b(ova|ona|oad|specials|batch)\b/i;
// Episode-range batch: "1-26", "(01-10)", "S01-S09". The look-arounds reject
// a longer hyphen-number chain so a title like "9-1-1" is not read as a range.
const RANGE = /(?<![\d-])S?(\d{1,3})\s*[-–]\s*S?(\d{1,3})\b(?![-\d])/;
// Bare anime episode: "Show - 10", "Show - 1095" (absolute numbering). The
// number must not look like a year and must not be a resolution.
const DASH_EP = /\s[-–]\s*(\d{1,4})(?=\s|$|[\[(._-])/;

const LANGS: [RegExp, string][] = [
  [/\bdual[._ ]?audio\b|\bdual\b/i, "Dual"],
  [/\bmulti(?:[._ ]?audio)?\b/i, "Multi"],
  [/\benglish\b|\beng\b/i, "English"],
  [/\bhindi\b|\bhin\b/i, "Hindi"],
  [/\btamil\b|\btam\b/i, "Tamil"],
  [/\btelugu\b|\btel\b/i, "Telugu"],
  [/\bmalayalam\b|\bmal\b/i, "Malayalam"],
  [/\bkannada\b/i, "Kannada"],
  [/\bbengali\b/i, "Bengali"],
  [/\bpunjabi\b/i, "Punjabi"],
  [/\bmarathi\b/i, "Marathi"],
  [/\bspanish\b|\bcastellano\b/i, "Spanish"],
  [/\bfrench\b|\bvostfr\b|\btruefrench\b/i, "French"],
  [/\bgerman\b/i, "German"],
  [/\bitalian\b/i, "Italian"],
  [/\bjapanese\b|\bjpn\b/i, "Japanese"],
  [/\bkorean\b/i, "Korean"],
  [/\bchinese\b|\bmandarin\b/i, "Chinese"],
  [/\brussian\b/i, "Russian"],
  [/\b[em]subs?\b|\bsubbed\b|\bsubs\b/i, "Sub"],
];

function resRank(res?: string): number {
  switch (res) {
    case "2160p":
      return 2160;
    case "1440p":
      return 1440;
    case "1080p":
      return 1080;
    case "720p":
      return 720;
    case "576p":
      return 576;
    case "480p":
      return 480;
    case "360p":
      return 360;
    default:
      return 0;
  }
}

function firstIdx(s: string, ...res: RegExp[]): number {
  let min = -1;
  for (const re of res) {
    const m = re.exec(s);
    if (m && (min === -1 || m.index < min)) min = m.index;
  }
  return min;
}

const BRACKETS = /\[[^\]]*\]|\([^)]*\)|\{[^}]*\}/g;
const EDGES = /^[\s\-_.·|+[\]{}()]+|[\s\-_.·|+[\]{}()]+$/g;
// A bracket opened but never closed (the title was cut mid-token) — drop it.
const DANGLE = /[[({][^\])}]*$/;

function norm(t: string): string {
  return t
    .replace(/[._+]+/g, " ")
    .replace(/\s{2,}/g, " ")
    .replace(EDGES, "")
    .trim();
}

// Keep token shape readable: DDP5.1, DTS-HD, x265 — only normalise the
// scene separators, never collapse the meaningful dots/dashes.
function tidy(t?: string): string | undefined {
  if (!t) return undefined;
  return t.replace(/_/g, ".").replace(/\s{2,}/g, " ").trim();
}

function isYearish(n: number): boolean {
  return n >= 1900 && n <= new Date().getFullYear() + 1;
}

function bare(title: string): Parsed {
  return { title, resRank: 0, langs: [] };
}

// Public entry point. Nothing a torrent tracker (or a corrupt filename) can
// send must ever throw — a parse failure degrades to "raw name as title",
// never an exception that blanks the results list.
export function parseName(input: unknown): Parsed {
  let raw = typeof input === "string" ? input : input == null ? "" : String(input);
  raw = raw.replace(/[\x00-\x1f\x7f]+/g, " ").trim();
  if (raw.length > MAX_LEN) raw = raw.slice(0, MAX_LEN);
  if (!raw) return bare("");
  try {
    const p = parseImpl(raw);
    if (!p.title || !p.title.trim()) p.title = raw.replace(EXT, "").trim() || raw;
    return p;
  } catch {
    return bare(raw.replace(EXT, "").trim() || raw);
  }
}

function parseImpl(input: string): Parsed {
  const raw = input.replace(EXT, "");
  const leadGrp = raw.match(LEADGRP)?.[1];
  // Underscore is a \w char, so "_OVA_" defeats every \b…\b marker — anime
  // fansub names are underscore-separated, so fold _ and + to spaces up front.
  const name = raw.replace(SITE, "").replace(/[+_]/g, " ").trim();

  const res = (name.match(RES)?.[0] ?? "")
    .toLowerCase()
    .replace(/^(4k|uhd)$/, "2160p");

  const seM = name.match(SE) ?? name.match(SXE);
  const sOrd = name.match(SEASON_ORD);
  let season = seM
    ? +seM[1]
    : sOrd
      ? +sOrd[1]
      : name.match(SEASON)?.[1]
        ? +name.match(SEASON)![1]
        : undefined;
  let episode = seM ? +seM[2] : name.match(EP)?.[1] ? +name.match(EP)![1] : undefined;

  const kind = name.match(KIND)?.[1]?.toUpperCase();

  // A real range ("01-26", "S01-S09") is a batch and wins over the bare
  // " - NN" reading (its first number would otherwise look like an episode).
  // But a known season + " - NN" ("S1 - 02") is a single episode, not a pack.
  const rg = name.match(RANGE);
  const isRange = !!rg && !isYearish(+rg[1]) && !isYearish(+rg[2]);
  let rangePack = false;
  if (isRange && season === undefined && episode === undefined) {
    rangePack = true;
  } else if (episode === undefined) {
    const de = name.match(DASH_EP);
    const ke = name.match(KIND_EP);
    if (de && !isYearish(+de[1]) && !RES.test(de[0])) episode = +de[1];
    else if (ke) episode = +ke[1];
  }
  const pack =
    PACK.test(name) ||
    rangePack ||
    kind === "BATCH" ||
    (season !== undefined && episode === undefined);

  const source = name.match(SOURCE)?.[0];
  const codec = name.match(CODEC)?.[0];
  const audio = name.match(AUDIO)?.[0];
  const hdr = name.match(HDR)?.[0];
  const bit = name.match(BIT)?.[0];
  const group = name.match(GROUP)?.[1] ?? leadGrp;

  const langs: string[] = [];
  for (const [re, label] of LANGS) {
    if (re.test(name) && !langs.includes(label)) langs.push(label);
  }

  // Title = everything before the earliest structured marker. Anime alt
  // titles come after a slash ("Attack on Titan / Shingeki…") — keep the
  // first. Strip closed brackets, then any bracket left dangling by the cut.
  const cut = firstIdx(name, SE, SXE, SEASON_ORD, SEASON, EP, KIND, RANGE, DASH_EP, RES, SOURCE, CODEC);
  let head = (cut === -1 ? name : name.slice(0, cut))
    .split("/")[0]
    .replace(BRACKETS, " ")
    .replace(DANGLE, " ");

  // For movies the title ends right before the release year — take the LAST
  // year in the head (so "Blade Runner 2049 2017" keeps "2049" in the title),
  // and drop it plus any trailing junk ("… 2024 MULTi").
  let year: number | undefined;
  const ym = [...head.matchAll(YEAR)].filter((m) => isYearish(+m[0]));
  if (ym.length) {
    const last = ym[ym.length - 1];
    year = +last[0];
    if (norm(head.slice(0, last.index)).length > 0) head = head.slice(0, last.index);
  }
  head = norm(head);

  // Marker-prefixed files ("S01E04 Don't Stop the Dance.mp4") leave an empty
  // head — fall back to the episode title that sits after the markers.
  if (!head) {
    const tail = name
      .replace(SE, " ")
      .replace(SXE, " ")
      .replace(SEASON_ORD, " ")
      .replace(SEASON, " ")
      .replace(EP, " ")
      .replace(KIND_EP, " ")
      .replace(KIND, " ")
      .replace(DASH_EP, " ")
      .replace(RANGE, " ");
    const c2 = firstIdx(tail, RES, SOURCE, CODEC);
    head = norm((c2 === -1 ? tail : tail.slice(0, c2)).split("/")[0].replace(BRACKETS, " "));
  }

  // Series rarely carry the year before the SxxExx marker — pull it from
  // anywhere in the name for display, without touching the title.
  if (year === undefined) {
    const any = [...name.matchAll(YEAR)].filter((m) => isYearish(+m[0]));
    if (any.length) year = +any[any.length - 1][0];
  }

  return {
    title: head || norm(name),
    year,
    season,
    episode,
    kind: kind === "BATCH" ? undefined : kind,
    pack,
    res: res || undefined,
    resRank: resRank(res || undefined),
    source: tidy(source)?.toUpperCase().replace(/\s+/g, "-"),
    codec: tidy(codec)?.replace(/\s+/g, ""),
    audio: tidy(audio)?.toUpperCase(),
    hdr: tidy(hdr)?.toUpperCase(),
    bit: tidy(bit)?.replace(/\s+/g, "").toLowerCase(),
    langs,
    group,
  };
}

export function epTag(p: Parsed): string {
  if (!p) return "";
  const k = p.kind ? p.kind + " " : "";
  if (p.season !== undefined && p.episode !== undefined)
    return `${k}S${String(p.season).padStart(2, "0")}E${String(p.episode).padStart(2, "0")}`;
  if (p.season !== undefined)
    return `${k}S${String(p.season).padStart(2, "0")}${p.pack ? " · Pack" : ""}`;
  if (p.episode !== undefined) return `${k}E${String(p.episode).padStart(2, "0")}`;
  if (p.kind) return `${p.kind}${p.pack ? " · Pack" : ""}`;
  if (p.pack) return "Pack";
  if (p.year) return String(p.year);
  return "";
}
