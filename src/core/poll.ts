// usePoll runs `fn` on a setInterval that auto-pauses while the tab is hidden
// and clears itself when the returned dispose function is called. Designed
// to be called inside onMount and the dispose returned (or pushed onto a
// cleanup list).
export function usePoll(
  fn: () => void | Promise<void>,
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
