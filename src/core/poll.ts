export function usePoll(
  fn: () => unknown,
  intervalMs: number,
  opts: { immediate?: boolean } = {},
): () => void {
  if (opts.immediate) {
    Promise.resolve(fn()).catch((err) => console.warn("[poll]", err));
  }

  const id = setInterval(() => {
    if (document.hidden) return;
    Promise.resolve(fn()).catch((err) => console.warn("[poll]", err));
  }, intervalMs);

  return () => clearInterval(id);
}
