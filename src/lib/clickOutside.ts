/** Close a dropdown when clicking outside. Call in onMount, returns cleanup. */
export function clickOutside(onClose: () => void): () => void {
  const handler = () => onClose();
  const t = setTimeout(() => window.addEventListener('click', handler), 0);
  return () => {
    clearTimeout(t);
    window.removeEventListener('click', handler);
  };
}
