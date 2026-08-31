import { IDB } from "../core/idb";
import { head } from "../core/api";
import { toast } from "./../core/toast.svelte";

const OFFLINE_CACHE = "notflix-offline-v1";

export function isSupported(): boolean {
  return "serviceWorker" in navigator && "BackgroundFetchManager" in self;
}

let handle: IDB | null = null;

// Opened on first use, never at module scope: `indexedDB` throws where site
// data is blocked, and dl.ts is imported by both Home and Player — an eager
// open takes the whole bundle down with a blank page. Absent DB = no
// downloads, which is exactly true.
function db(): IDB | null {
  if (!handle && globalThis.indexedDB)
    handle = new IDB("notflix-offline", 1, (idb) => {
      if (!idb.objectStoreNames.contains("downloads"))
        idb.createObjectStore("downloads", { keyPath: "videoParam" });

      if (!idb.objectStoreNames.contains("bgfetch-map"))
        idb.createObjectStore("bgfetch-map", { keyPath: "bgFetchId" });
    });

  return handle;
}

type UpdateCb = (videoParam: string, record: DownloadRecord | null) => void;

class Downloads {
  readonly #listeners = new Set<UpdateCb>();

  constructor() {
    if (!("serviceWorker" in navigator)) return;
    navigator.serviceWorker.addEventListener("message", (e: MessageEvent) => {
      const { type, videoParam, record } = e.data ?? {};

      if (
        type === "download-complete" ||
        type === "download-error" ||
        type === "download-abort"
      ) {
        this.#emit(videoParam, record ?? null);
      }
    });
  }

  #emit(videoParam: string, record: DownloadRecord | null) {
    for (const cb of this.#listeners) cb(videoParam, record);
  }

  on(cb: UpdateCb): () => void {
    this.#listeners.add(cb);

    return () => this.#listeners.delete(cb);
  }

  async get(videoParam: string): Promise<DownloadRecord | null> {
    const d = db();
    if (!d) return null;

    try {
      return (await d.get<DownloadRecord>("downloads", videoParam)) ?? null;
    } catch (err) {
      console.warn("[Down.get]", videoParam, err);

      return null;
    }
  }

  async set(record: DownloadRecord): Promise<void> {
    const d = db();
    if (!d) return;

    await d.set("downloads", record);
  }

  async del(videoParam: string): Promise<void> {
    const d = db();
    if (!d) return;

    const record = await this.get(videoParam);

    if (record?.bgFetchId && record.status === "downloading") {
      const reg = await navigator.serviceWorker.ready;
      const bgReg = await (reg as any).backgroundFetch.get(record.bgFetchId);

      if (bgReg) await bgReg.abort();
    }

    const cache = await caches.open(OFFLINE_CACHE);
    await cache.delete(`/video/${videoParam}`);
    await cache.delete(`/subs/${videoParam.replace(/\.mp4$/i, ".vtt")}`);

    await d.del("downloads", videoParam);
    if (record?.bgFetchId) await d.del("bgfetch-map", record.bgFetchId);

    this.#emit(videoParam, null);
  }

  async all(): Promise<DownloadRecord[]> {
    const d = db();
    if (!d) return [];

    try {
      return await d.all<DownloadRecord>("downloads");
    } catch (err) {
      console.warn("[Down.all]", err);

      return [];
    }
  }

  async recover(): Promise<void> {
    if (!isSupported()) return;
    let records: DownloadRecord[];
    try {
      records = await this.all();
    } catch (err) {
      console.warn("[recover] all()", err);
      return;
    }
    const reg = await navigator.serviceWorker.ready;
    const bgFetch = (reg as any).backgroundFetch;

    for (const r of records) {
      if (r.status !== "downloading" || !r.bgFetchId) continue;
      try {
        const bgReg = await bgFetch.get(r.bgFetchId);
        if (!bgReg) {
          r.status = "error";
          await this.set(r);
          this.#emit(r.videoParam, r);
          toast.err(`Download "${r.title}" was lost — please retry`);
        }
      } catch (err) {
        console.warn("[recover] per-record", err);
      }
    }
  }

  async start(videoParam: string, title: string, key: string): Promise<void> {
    try {
      const reg = await navigator.serviceWorker.ready;
      const bgFetch = (reg as any).backgroundFetch;

      const src = await head(`/video/${videoParam}`);
      if (!src.ok) throw new Error(`HEAD /video/${videoParam} failed`);
      if (!src.size) {
        console.warn("[Down.start] no Content-Length — downloading with unknown size");
      }

      if (navigator.storage?.persist) {
        try {
          await navigator.storage.persist();
        } catch (err) {
          console.warn("[storage.persist]", err);
        }
      }

      const d = db();
      if (!d) throw new Error("offline storage unavailable");

      const bgFetchId = `dl-${videoParam}`;
      const bgReg = await bgFetch.fetch(bgFetchId, `/video/${videoParam}`, {
        title,
        downloadTotal: src.size || undefined,
        icons: [
          {
            src: "/assets/icon.svg",
            type: "image/svg+xml",
          },
        ],
      });

      const record: DownloadRecord = {
        videoParam,
        title,
        key,
        size: src.size,
        status: "downloading",
        downloadedAt: null,
        bgFetchId: bgReg.id,
      };

      await d.tx(["downloads", "bgfetch-map"], "readwrite", (tx) => {
        tx.objectStore("downloads").put(record);
        tx.objectStore("bgfetch-map").put({
          bgFetchId: bgReg.id,
          videoParam,
        });
      });
    } catch (err: any) {
      const msg = err?.message || String(err);
      console.error("[Down.start]", err);
      toast.err(`Download failed: ${msg}`);
      throw err;
    }
  }

  async progress(bgFetchId: string): Promise<number> {
    const reg = await navigator.serviceWorker.ready;
    const bgReg = await (reg as any).backgroundFetch.get(bgFetchId);

    if (!bgReg || bgReg.downloadTotal === 0) return 0;

    return Math.round((bgReg.downloaded / bgReg.downloadTotal) * 100);
  }

  async storage(): Promise<{ used: number; quota: number }> {
    if (!navigator.storage?.estimate) return { used: 0, quota: 0 };
    const est = await navigator.storage.estimate();

    return {
      used: est.usage ?? 0,
      quota: est.quota ?? 0,
    };
  }
}

export const Down = new Downloads();
