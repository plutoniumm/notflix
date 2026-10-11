import { parseName } from "./torrentName";

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

// One rail label per file, in the order given (the server lists a dir by
// filename, which for episodic names is already episode order — sorting on the
// parsed episode would collapse S01E01 and S02E01 onto each other).
export function ordinals(names: string[]): string[] {
  const parsed = names.map(parseName);
  const multi =
    new Set(parsed.map((p) => p.season).filter((s) => s !== undefined)).size > 1;

  return parsed.map((p, i) => {
    if (p.episode === undefined) return pad(i + 1);

    return multi && p.season !== undefined
      ? `${p.season}·${pad(p.episode)}`
      : pad(p.episode);
  });
}
