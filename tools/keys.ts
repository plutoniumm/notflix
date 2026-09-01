// The README's shortcut section, rendered from src/player/ux.ts's KEYS — the
// one table the help overlay also renders. tools/parity.test.ts asserts the
// file still matches; run `node tools/keys.ts` and paste when the table moves.
import { register } from "node:module";

// Source imports are extensionless (Vite resolution); retry with .ts so the
// real modules load unmodified.
register(
  "data:text/javascript," +
    encodeURIComponent(`
      export async function resolve(spec, ctx, next) {
        try {
          return await next(spec, ctx);
        } catch (e) {
          if (spec.startsWith(".")) return await next(spec + ".ts", ctx);
          throw e;
        }
      }
    `),
  import.meta.url,
);

// Runes are compile-time in Svelte but plain calls syntactically; an identity
// shim lets the real modules import under bare Node.
(globalThis as any).$state = Object.assign((v: any) => v, {
  snapshot: (v: any) => v,
});
(globalThis as any).$derived = (v: any) => v;

// Dynamic, not static: a static import resolves before the loader above is
// registered, and then the extensionless specifiers inside ux.ts throw.
const { KEYS } = await import("../src/player/ux.ts");

export const START = "<!-- keys -->";
export const END = "<!-- /keys -->";

export function keysMarkdown(): string {
  return KEYS.map(
    (g) =>
      `### ${g.g}\n\n| Key | Action |\n| --- | --- |\n` +
      g.keys.map((r) => `| \`${r.k}\` | ${r.a} |`).join("\n"),
  ).join("\n\n");
}

if (process.argv[1]?.endsWith("keys.ts")) console.log(keysMarkdown());
