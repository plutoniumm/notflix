import { api } from "../core/api";
import { kv } from "../core/kv";
import { clean } from "../core/video";
import { JOBS_POLL_MS } from "../core/events.svelte";
import { Down } from "./dl";

type ContinueItem = { dir: string; name: string; key: string; t: number; at: number };

// Tolerate either the current ordered array OR a legacy {dir: files} object
// map (a stale cached bundle ↔ new server, or vice-versa, during rollout) —
// and guarantee every group's `files` is an array so iteration never throws.
function normalizeVideoData(raw: any): VideoData {
  if (Array.isArray(raw)) {
    return raw.filter(
      (g) => g && typeof g.dir === "string" && Array.isArray(g.files),
    );
  }
  if (raw && typeof raw === "object") {
    return Object.entries(raw)
      .filter(([, files]) => Array.isArray(files))
      .map(([dir, files]) => ({ dir, files: files as VideoEntry[] }));
  }
  return [];
}

// HomeData owns Home's reactive view-model: the video tree, "Continue
// Watching" row, downloaded-set, in-progress downloads, offline indicator.
// Mount: call `start()` for the unsubscribe; on navigation away call that.
export class HomeData {
  data = $state<VideoData>([]);
  continues = $state<ContinueItem[]>([]);
  downloadedSet = $state(new Set<string>());
  inProg = $state<DownloadRecord[]>([]);
  conversions = $state<Job[]>([]);
  loading = $state(true);
  offline = $state(false);
  // Disks the server currently has mounted. Surfaced in the empty-state
  // (so the user can distinguish "server has no roots" from "roots exist
  // but no videos in them") and used as the download-target picker when
  // a torrent search runs from the home bar.
  disks = $state<DiskInfo[]>([]);

  // The server already orders groups LRU (recently-watched dirs first); we
  // just drop empties and expose the [dir, files] tuple Home.svelte iterates.
  rows = $derived(
    this.data
      .filter((g) => g.files?.length)
      .map((g) => [g.dir, g.files] as [string, VideoEntry[]]),
  );

  // Begin loading. Returns a function to dispose subscriptions.
  start(): () => void {
    const recordsP = Down.all().catch((err) => {
      console.warn("[Down.all]", err);
      return [] as DownloadRecord[];
    });

    (async () => {
      try {
        // api.videoList() returns null on network/HTTP failure (request()
        // catches and returns null). Distinguish that from a successful but
        // empty response — an empty library is NOT offline; it just means
        // the server has roots but no videos in them (or no roots at all).
        // Conflating the two used to show "Offline" every time the server
        // came up with no volumes attached.
        const [raw, disks] = await Promise.all([
          api.videoList({ silent: true }),
          api.manage.diskInfo({ silent: true }),
        ]);
        if (Array.isArray(disks)) {
          this.disks = disks.filter((d: any) => d && d.root && d.path);
        }
        if (raw == null) {
          this.offline = true;
          this.data = this.buildOfflineData(await recordsP);
        } else {
          const d = normalizeVideoData(raw);
          this.data = d;
          if (d.length) this.loadState(d);
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

    // Surface active ffmpeg conversions (the user-useful progress) on the
    // default screen — same poll cadence as Manage.
    const pollJobs = async () => {
      const j = await api.conversions({ silent: true });
      this.conversions = Array.isArray(j) ? j : [];
    };
    pollJobs();
    const jobsTimer = setInterval(pollJobs, JOBS_POLL_MS);

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

    return () => {
      clearInterval(jobsTimer);
      unsubSW();
    };
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
    return this.data.flatMap((g) =>
      (g.files || [])
        .filter((f) => {
          const name = f.name.toLowerCase();
          return clean(name).includes(trimmed) || name.includes(trimmed);
        })
        .map((f) => ({ dir: g.dir, ...f })),
    );
  }

  private async loadState(d: VideoData) {
    const allParams: { dir: string; name: string; key: string; param: string }[] = [];
    for (const g of d) {
      for (const f of g.files ?? []) {
        const param = g.dir === "." ? f.name : `${g.dir}/${f.name}`;
        allParams.push({ dir: g.dir, name: f.name, key: f.key, param });
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
        at: store[`watched:${v.param}`]!.at,
      }));
  }

  private buildOfflineData(records: DownloadRecord[]): VideoData {
    const byDir = new Map<string, VideoEntry[]>();
    for (const r of records) {
      if (r.status !== "done") continue;
      const slash = r.videoParam.lastIndexOf("/");
      const dir = slash >= 0 ? r.videoParam.slice(0, slash) : ".";
      const name = slash >= 0 ? r.videoParam.slice(slash + 1) : r.videoParam;
      let files = byDir.get(dir);
      if (!files) byDir.set(dir, (files = []));
      files.push({ name, key: r.key });
    }
    return [...byDir].map(([dir, files]) => ({ dir, files }));
  }
}
