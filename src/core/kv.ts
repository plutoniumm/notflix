import { GET, POST } from "./api";

const E = encodeURIComponent;

export const kv = {
  get: <T = any>(keys: string | string[]): Promise<T | null> => {
    const arr = Array.isArray(keys) ? keys : [keys];
    const qs = arr.map((k) => `key=${E(k)}`).join("&");

    return GET(`/kv/get?${qs}`);
  },

  set: (key: string, value: any) => POST("/kv/set", { key, value }),

  setBulk: (items: { key: string; value: any }[]) => POST("/kv/set", items),

  // Fire-and-forget beacon for use in beforeunload handlers — sync over the
  // wire with no response, no completion event.
  beacon: (key: string, value: any) => {
    navigator.sendBeacon(
      "/kv/set",
      new Blob([JSON.stringify({ key, value })], { type: "application/json" }),
    );
  },
};
