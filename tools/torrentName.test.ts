import { parseName, parseDirSeason } from "../src/library/torrentName.ts";

type Case = {
  name: string;
  season?: number;
  episode?: number;
  title?: string;
  year?: number;
};

// Possessive "'s" before a number is not a season marker — three unrelated
// films once shared a fake "Ocean'" show key with seasons 11/12/13.
const cases: Case[] = [
  { name: "Ocean's 11", season: undefined, title: "Ocean's 11" },
  { name: "Ocean's 12", season: undefined },
  { name: "Ocean's 13", season: undefined },
  { name: "Ocean's Eleven 2001", season: undefined, year: 2001 },
  { name: "Ocean’s 8 2018", season: undefined, year: 2018 },

  { name: "Blade Runner 2049 2017", title: "Blade Runner 2049", year: 2017 },
  { name: "WALL-E", title: "WALL-E", season: undefined },
  { name: "Sense8 S01", season: 1 },
  { name: "9-1-1 S05E03", season: 5, episode: 3, title: "9-1-1" },
  { name: "City Hunter (1987-88)", season: undefined },
  { name: "S01E10v2", season: 1, episode: 10 },
  { name: "4th Season", season: 4 },
];

let failed = 0;
const show = (v: unknown) => (v === undefined ? "undefined" : JSON.stringify(v));

for (const c of cases) {
  const p = parseName(c.name);
  for (const k of ["season", "episode", "title", "year"] as const) {
    if (!(k in c)) continue;
    if (p[k] !== c[k]) {
      failed++;
      console.log(`FAIL ${c.name} .${k}: got ${show(p[k])} want ${show(c[k])}`);
    }
  }
}

// groupRows folds dirs sharing a title AND a season marker, so the possessive
// bug has to be dead at the parseDirSeason level too.
for (const d of ["Ocean's 11", "Ocean's 12", "Ocean's 13"]) {
  const s = parseDirSeason(d);
  if (s.season !== undefined || s.range) {
    failed++;
    console.log(`FAIL parseDirSeason(${d}): ${JSON.stringify(s)}`);
  }
}

const grp = ["Breaking Bad S01", "Breaking Bad S02"].map(parseDirSeason);
if (grp[0].season !== 1 || grp[1].season !== 2 || grp[0].title !== grp[1].title) {
  failed++;
  console.log(`FAIL season grouping: ${JSON.stringify(grp)}`);
}

console.log(failed ? `${failed} failure(s)` : `ok — ${cases.length + 4} checks`);
process.exit(failed ? 1 : 0);
