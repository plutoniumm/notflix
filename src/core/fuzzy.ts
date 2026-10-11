// Release names are dot-separated and carry words the user never types, so the
// raw query is almost never a substring of them: searching "smoking
// supermarket" returned provider hits for
// "Smoking.Behind.the.Supermarket.With.You.S01E01.1080p..." and then the
// client-side `.includes(query)` filter threw every one of them away — results
// appeared and vanished unless the query was typed exactly.
//
// Token-AND rather than a similarity score: every word the user typed must
// appear somewhere, in any order, across any of the candidate's fields. That
// fixes word order, separators and interleaved words, and unlike a fuzzy
// subsequence match it cannot quietly rank in something that shares only
// scattered letters.

// Separators in release names are information-free: ".", "_", "-" and spaces
// all mean "word break", so they collapse to one.
export function norm(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .trim();
}

export function tokens(q: string): string[] {
  const n = norm(q);

  return n ? n.split(" ") : [];
}

// Every token must hit. Fields are OR'd, so a token matching the parsed title
// and another matching the raw release name still counts as a match.
export function matches(query: string, ...fields: string[]): boolean {
  const toks = tokens(query);
  if (toks.length === 0) return true;

  const hay = fields.map(norm);

  return toks.every((t) => hay.some((h) => h.includes(t)));
}
