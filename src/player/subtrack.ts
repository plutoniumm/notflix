// Rune-free so `node --test` can import it: ux.ts pulls in a .svelte.ts module
// and `$state` is a compiler construct, not a runtime one.
export type SubTrack = { mode: string; label?: string };

// What should be showing after one press of `c`. Exactly one track at a time,
// never "all of them inverted": with English on and Spanish off, inverting each
// track put Spanish on screen and took English off.
export function subToggle<T extends SubTrack>(
  subs: T[],
  lastSub: string,
): { show: T | null; remember: string } {
  const on = subs.find((t) => t.mode === "showing");
  if (on) return { show: null, remember: on.label || "" };

  const back = subs.find((t) => (t.label || "") === lastSub) ?? subs[0] ?? null;

  return { show: back, remember: lastSub };
}
