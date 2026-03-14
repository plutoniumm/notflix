import { IDB } from './idb';

const OFFLINE_CACHE = 'notflix-offline-v1';

export function isSupported (): boolean {
  return 'serviceWorker' in navigator && 'BackgroundFetchManager' in self;
}

const db = new IDB('notflix-offline', 1, (idb) => {
  if (!idb.objectStoreNames.contains('downloads'))
    idb.createObjectStore('downloads', { keyPath: 'videoParam' });

  if (!idb.objectStoreNames.contains('bgfetch-map'))
    idb.createObjectStore('bgfetch-map', { keyPath: 'bgFetchId' });
});

type UpdateCb = (videoParam: string, record: DownloadRecord | null) => void;

class Downloads {
  readonly #listeners = new Set<UpdateCb>();

  constructor() {
    if (!('serviceWorker' in navigator)) return;
    navigator.serviceWorker.addEventListener('message', (e: MessageEvent) => {
      const { type, videoParam, record } = e.data ?? {};

      if (
        type === 'download-complete' ||
        type === 'download-error' ||
        type === 'download-abort'
      ) {
        this.#emit(videoParam, record ?? null);
      }
    });
  }

  #emit (videoParam: string, record: DownloadRecord | null) {
    for (const cb of this.#listeners)
      cb(videoParam, record);
  }

  on (cb: UpdateCb): () => void {
    this.#listeners.add(cb);

    return () => this.#listeners.delete(cb);
  }

  async get (videoParam: string): Promise<DownloadRecord | null> {
    return (await db.get<DownloadRecord>('downloads', videoParam)) ?? null;
  }

  async set (record: DownloadRecord): Promise<void> {
    await db.set('downloads', record);
  }

  async del (videoParam: string): Promise<void> {
    const record = await this.get(videoParam);

    if (record?.bgFetchId && record.status === 'downloading') {
      const reg = await navigator.serviceWorker.ready;
      const bgReg = await (reg as any).backgroundFetch.get(record.bgFetchId);

      if (bgReg)
        await bgReg.abort();
    }

    const cache = await caches.open(OFFLINE_CACHE);
    await cache.delete(`/video/${videoParam}`);
    await cache.delete(`/subs/${videoParam.replace(/\.mp4$/i, '.vtt')}`);

    await db.del('downloads', videoParam);
    if (record?.bgFetchId)
      await db.del('bgfetch-map', record.bgFetchId);

    this.#emit(videoParam, null);
  }

  async all (): Promise<DownloadRecord[]> {
    return db.all<DownloadRecord>('downloads');
  }

  async start (videoParam: string, title: string, key: string): Promise<void> {
    const reg = await navigator.serviceWorker.ready;
    const bgFetch = (reg as any).backgroundFetch;

    const head = await fetch(`/video/${videoParam}`, { method: 'HEAD' });
    const size = parseInt(head.headers.get('Content-Length') ?? '0', 10);

    if (navigator.storage?.persist)
      await navigator.storage.persist();

    const bgFetchId = `dl-${videoParam}`;
    const bgReg = await bgFetch.fetch(bgFetchId, `/video/${videoParam}`, {
      title,
      downloadTotal: size || undefined,
      icons: [{
        src: '/assets/icon.svg',
        type: 'image/svg+xml'
      }],
    });

    const record: DownloadRecord = {
      videoParam,
      title,
      key,
      size,
      status: 'downloading',
      downloadedAt: null,
      bgFetchId: bgReg.id,
    };

    await db.tx(['downloads', 'bgfetch-map'], 'readwrite', (tx) => {
      tx.objectStore('downloads').put(record);
      tx.objectStore('bgfetch-map').put({
        bgFetchId: bgReg.id,
        videoParam
      });
    });
  }

  async progress (bgFetchId: string): Promise<number> {
    const reg = await navigator.serviceWorker.ready;
    const bgReg = await (reg as any).backgroundFetch.get(bgFetchId);

    if (!bgReg || bgReg.downloadTotal === 0)
      return 0;

    return Math.round((bgReg.downloaded / bgReg.downloadTotal) * 100);
  }

  async storage (): Promise<{ used: number; quota: number }> {
    if (!navigator.storage?.estimate)
      return { used: 0, quota: 0 };
    const est = await navigator.storage.estimate();

    return {
      used: est.usage ?? 0,
      quota: est.quota ?? 0
    };
  }
}

export const Down = new Downloads();
