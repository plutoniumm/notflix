import { api } from "../core/api";
import { kv } from "../core/kv";
import { clean } from "../core/video";
import { matches } from "../core/fuzzy";
import { join } from "../core/path";
import { JOBS_POLL_MS } from "../core/events.svelte";
import { usePoll } from "../core/poll";
import { Down } from "./dl";
import { parseDirSeason } from "./torrentName";
import { episodic } from "./series";

export type ContinueItem = {
  dir: string;
  name: string;
  key: string;
  t: number;
  at: number;
  dur?: number;
};

export type SeasonEntry = {
  dir: string;
  label: string;
  n: number;
  files: VideoEntry[];
};

export type HomeRow =
  | { kind: "dir"; dir: string; files: VideoEntry[]; series: boolean }
  | { kind: "show"; key: string; title: string; seasons: SeasonEntry[] };

const showKey = (title: string) =>
  title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .trim();

// A show forms only from ≥2 dirs whose parsed titles match exactly and that
// each carry a season number or episode range; anything looser stays a plain
// row. The show row sits at its best member's position, keeping server order.
export function groupRows(data: VideoData): HomeRow[] {
  const parsed = data
    .filter((g) => g.files?.length)
    .map((g) => ({ g, p: parseDirSeason(g.dir) }));

  const marked = ({ g, p }: (typeof parsed)[number]) =>
    g.dir !== "." && (p.season !== undefined || !!p.range);

  const counts = new Map<string, number>();
  for (const m of parsed) {
    if (!marked(m)) continue;
    const k = showKey(m.p.title);
    if (k) counts.set(k, (counts.get(k) ?? 0) + 1);
  }

  const emitted = new Set<string>();
  const out: HomeRow[] = [];
  for (const m of parsed) {
    const k = marked(m) ? showKey(m.p.title) : "";
    if (!k || (counts.get(k) ?? 0) < 2) {
      out.push({
        kind: "dir",
        dir: m.g.dir,
        files: m.g.files,
        // A show row is a show by construction; a plain dir has to be read off
        // its filenames, since a folder of episodes and a folder of unrelated
        // films arrive in exactly the same shape.
        series: episodic(m.g.files.map((f) => f.name)),
      });
      continue;
    }
    if (emitted.has(k)) continue;
    emitted.add(k);

    const members = parsed.filter((x) => marked(x) && showKey(x.p.title) === k);
    const seasons = members
      .map(({ g, p }, i): SeasonEntry => ({
        dir: g.dir,
        label:
          p.season !== undefined
            ? `Season ${p.season}`
            : p.range
              ? `Ep ${p.range[0]}–${p.range[1]}`
              : `Part ${i + 1}`,
        n:
          p.season !== undefined
            ? p.season
            : p.range
              ? 1000 + p.range[0]
              : 10000 + i,
        files: g.files,
      }))
      .sort((a, b) => a.n - b.n);

    const dupes = new Map<string, number>();
    for (const s of seasons) {
      const c = (dupes.get(s.label) ?? 0) + 1;
      dupes.set(s.label, c);
      if (c > 1) s.label = `${s.label} · ${c}`;
    }

    out.push({ kind: "show", key: k, title: members[0].p.title, seasons });
  }
  return out;
}

// Tolerates the legacy {dir: files} object map (stale cached bundle ↔ new
// server) and guarantees every group's `files` is an array.
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

export class HomeData {
  data = $state<VideoData>([]);
  continues = $state<ContinueItem[]>([]);
  downloadedSet = $state(new Set<string>());
  inProg = $state<DownloadRecord[]>([]);
  conversions = $state<Job[]>([]);
  loading = $state(true);
  offline = $state(false);
  disks = $state<DiskInfo[]>([]);

  // The server already orders groups LRU (recently-watched dirs first).
  rows = $derived(groupRows(this.data));

  start(): () => void {
    const recordsP = Down.all().catch((err) => {
      console.warn("[Down.all]", err);
      return [] as DownloadRecord[];
    });

    (async () => {
      try {
        // api.videoList() returns null on failure; an empty list is NOT
        // offline — a server with no volumes attached would read as offline.
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

    const stopJobs = usePoll(
      async () => {
        const j = await api.conversions({ silent: true });
        this.conversions = Array.isArray(j) ? j : [];
      },
      JOBS_POLL_MS,
      { immediate: true },
    );

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
      stopJobs();
      unsubSW();
    };
  }

  removeContinue(dir: string, name: string) {
    const param = join(dir, name);
    kv.set(`watched:${param}`, null);
    this.continues = this.continues.filter(
      (c) => !(c.dir === dir && c.name === name),
    );
  }

  async cancelDownload(videoParam: string): Promise<string | null> {
    try {
      await Down.del(videoParam);
      this.inProg = this.inProg.filter((r) => r.videoParam !== videoParam);
      return null;
    } catch (err: any) {
      return err?.message ?? String(err);
    }
  }

  filter(q: string): { dir: string; name: string; key: string }[] | null {
    if (!q.trim()) return null;

    // Same token-AND matcher as the torrent results: library filenames are
    // dot-separated too, so a two-word query was never a substring of them.
    return this.data.flatMap((g) =>
      (g.files || [])
        .filter((f) => matches(q, clean(f.name), f.name, g.dir))
        .map((f) => ({ dir: g.dir, ...f })),
    );
  }

  private async loadState(d: VideoData) {
    const allParams: { dir: string; name: string; key: string; param: string }[] = [];
    for (const g of d) {
      for (const f of g.files ?? []) {
        const param = join(g.dir, f.name);
        allParams.push({ dir: g.dir, name: f.name, key: f.key, param });
      }
    }
    if (!allParams.length) return;

    const keys = allParams.map((v) => "watched:" + v.param);
    const store: Record<string, { t: number; at: number; dur?: number } | null> =
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
        dur: store[`watched:${v.param}`]!.dur,
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
