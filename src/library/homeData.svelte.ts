import { api } from "../core/api";
import { kv } from "../core/kv";
import { clean } from "../core/video";
import { Down } from "./dl";

type ContinueItem = { dir: string; name: string; key: string; t: number };

// HomeData owns Home's reactive view-model: the video tree, "Continue
// Watching" row, downloaded-set, in-progress downloads, offline indicator.
// Mount: call `start()` for the unsubscribe; on navigation away call that.
export class HomeData {
  data = $state<VideoData>({});
  continues = $state<ContinueItem[]>([]);
  downloadedSet = $state(new Set<string>());
  inProg = $state<DownloadRecord[]>([]);
  loading = $state(true);
  offline = $state(false);

  rows = $derived(
    Object.entries(this.data).filter(([, files]) => files?.length),
  );

  // Begin loading. Returns a function to dispose subscriptions.
  start(): () => void {
    const recordsP = Down.all().catch((err) => {
      console.warn("[Down.all]", err);
      return [] as DownloadRecord[];
    });

    (async () => {
      try {
        const d = await api.videoList();
        if (d && Object.keys(d).length) {
          this.data = d;
          this.loadState(d);
        } else {
          this.offline = true;
          this.data = this.buildOfflineData(await recordsP);
        }
      } catch (err) {
        console.error("[Home load]", err);
        this.offline = true;
        this.data = this.buildOfflineData(await recordsP);
      } finally {
        this.loading = false;
      }
    })();

    recordsP.then((records) => {
      this.downloadedSet = new Set(
        records.filter((r) => r.status === "done").map((r) => r.videoParam),
      );
      this.inProg = records.filter((r) => r.status === "downloading");
    });

    Down.recover().catch((err) => console.warn("[Down.recover]", err));

    const unsubSW = Down.on((videoParam, record) => {
      const next = new Set(this.downloadedSet);
      if (record?.status === "done") {
        next.add(videoParam);
        this.inProg = this.inProg.filter((r) => r.videoParam !== videoParam);
      } else if (record?.status === "downloading") {
        this.inProg = [
          ...this.inProg.filter((r) => r.videoParam !== videoParam),
          record,
        ];
      } else {
        next.delete(videoParam);
        this.inProg = this.inProg.filter((r) => r.videoParam !== videoParam);
      }
      this.downloadedSet = next;
    });

    return unsubSW;
  }

  // Remove an item from Continue Watching (server + local).
  removeContinue(dir: string, name: string) {
    const param = dir === "." ? name : `${dir}/${name}`;
    kv.set(`watched:${param}`, null);
    this.continues = this.continues.filter(
      (c) => !(c.dir === dir && c.name === name),
    );
  }

  // Cancel an in-progress PWA download.
  async cancelDownload(videoParam: string): Promise<string | null> {
    try {
      await Down.del(videoParam);
      this.inProg = this.inProg.filter((r) => r.videoParam !== videoParam);
      return null;
    } catch (err: any) {
      return err?.message ?? String(err);
    }
  }

  // Filter the tree by query — returns null when the query is empty.
  filter(q: string): { dir: string; name: string; key: string }[] | null {
    const trimmed = q.trim().toLowerCase();
    if (!trimmed) return null;
    return Object.entries(this.data).flatMap(([dir, files]) =>
      (files || [])
        .filter((f) => {
          const name = f.name.toLowerCase();
          return clean(name).includes(trimmed) || name.includes(trimmed);
        })
        .map((f) => ({ dir, ...f })),
    );
  }

  private async loadState(d: VideoData) {
    const allParams: { dir: string; name: string; key: string; param: string }[] = [];
    for (const [dir, files] of Object.entries(d)) {
      for (const f of files ?? []) {
        const param = dir === "." ? f.name : `${dir}/${f.name}`;
        allParams.push({ dir, name: f.name, key: f.key, param });
      }
    }
    if (!allParams.length) return;

    const keys = allParams.map((v) => "watched:" + v.param);
    const store: Record<string, { t: number; at: number } | null> =
      (await kv.get(keys)) ?? {};

    this.continues = allParams
      .filter((v) => {
        const val = store[`watched:${v.param}`];
        return val && val.t > 60;
      })
      .sort((a, b) => {
        const at = store[`watched:${a.param}`]?.at ?? 0;
        const bt = store[`watched:${b.param}`]?.at ?? 0;
        return bt - at;
      })
      .slice(0, 20)
      .map((v) => ({
        dir: v.dir,
        name: v.name,
        key: v.key,
        t: store[`watched:${v.param}`]!.t,
      }));
  }

  private buildOfflineData(records: DownloadRecord[]): VideoData {
    const out: VideoData = {};
    for (const r of records) {
      if (r.status !== "done") continue;
      const slash = r.videoParam.lastIndexOf("/");
      const dir = slash >= 0 ? r.videoParam.slice(0, slash) : ".";
      const name = slash >= 0 ? r.videoParam.slice(slash + 1) : r.videoParam;
      if (!out[dir]) out[dir] = [];
      out[dir].push({ name, key: r.key });
    }
    return out;
  }
}
