import { GET, POST } from './api';

const E = encodeURIComponent;

export const kv = {
  get: <T = any>(keys: string | string[]): Promise<T | null> => {
    const arr = Array.isArray(keys) ? keys : [keys];
    const qs = arr.map(k => `key=${E(k)}`).join('&');

    return GET(`/kv/get?${qs}`);
  },

  set: (key: string, value: any) => POST('/kv/set', { key, value }),

  setBulk: (items: { key: string; value: any }[]) => POST('/kv/set', items),
};
