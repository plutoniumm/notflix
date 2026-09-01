// localStorage throws beyond quota too — Chrome's "block all cookies" makes
// every access raise. Reads fall back, writes are best-effort.
export function local<T>(key: string, fallback: T) {
  return {
    get(): T {
      try {
        const raw = localStorage.getItem(key);

        return raw === null ? fallback : (JSON.parse(raw) as T);
      } catch {
        return fallback;
      }
    },

    set(value: T) {
      try {
        localStorage.setItem(key, JSON.stringify(value));
      } catch (err) {
        console.warn("[store]", key, err);
      }
    },
  };
}
