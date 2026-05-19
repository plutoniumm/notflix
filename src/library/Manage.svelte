<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { api } from "../core/api";
  import { kv } from "../core/kv";
  import { toast } from "../core/toast.svelte";
  import { JOBS_POLL_MS, DL_POLL_MS, PWA_POLL_MS } from "../core/events.svelte";
  import { usePoll } from "../core/poll";
  import { Down, isSupported } from "./dl";
  import { clean } from "../core/video";

  import Header from "./Header.svelte";
  import Magnet from "./Magnet.svelte";
  import Downloads from "./Downloads.svelte";
  import FolderRow from "./FolderRow.svelte";
  import ProgressBar from "../components/ProgressBar.svelte";

  declare const __BUILD_TIME__: string;
  const frontendBuild = __BUILD_TIME__;

  type FileEntry = { name: string; root: string; corrupt?: boolean };
  let data: Record<string, FileEntry[]> = $state({});
  let disks: DiskInfo[] = $state([]);
  let hiddenSet = $state(new Set<string>());
  let backendBuild = $state("");
  let loading = $state(true);
  let editing = $state<string | null>(null);
  let val = $state("");
  let jobs: Job[] = $state([]);
  let dlJobs: Downjob[] = $state([]);
  let processingLib = $state(false);
  let pwaDownloads = $state<
    {
      videoParam: string;
      title: string;
      bgFetchId: string | null;
      progress: number;
    }[]
  >([]);
  let dirSizes = $state<Record<string, { bytes: number; root: string }>>({});

  let pollers: (() => void)[] = [];
  let pwaUnsub: (() => void) | null = null;

  onDestroy(() => {
    pollers.forEach((stop) => stop());
    pwaUnsub?.();
  });

  async function pollPwaDownloads() {
    let records: DownloadRecord[];
    try {
      records = await Down.all();
    } catch (err) {
      console.warn("[pollPwa all()]", err);
      return;
    }
    const active = records.filter((r) => r.status === "downloading");
    const updated = await Promise.all(
      active.map(async (r) => {
        let progress = 0;
        if (r.bgFetchId) {
          try {
            progress = await Down.progress(r.bgFetchId);
          } catch (err) {
            console.warn("[pollPwa progress]", r.videoParam, err);
          }
        }
        return {
          videoParam: r.videoParam,
          title: r.title,
          bgFetchId: r.bgFetchId,
          progress,
        };
      }),
    );
    pwaDownloads = updated;
  }

  onMount(() => {
    loadAll();

    api
      .build({ silent: true })
      .then((r) => {
        if (r?.backend) backendBuild = r.backend;
      })
      .catch((err) => console.warn("[build]", err));
    api.manage
      .dirSizes({ silent: true })
      .then((sizes: any[]) => {
        if (!Array.isArray(sizes)) return;
        const map: Record<string, { bytes: number; root: string }> = {};
        for (const s of sizes) map[s.dir] = { bytes: s.bytes, root: s.root };
        dirSizes = map;
      })
      .catch((err) => console.warn("[dirSizes]", err));

    pollers.push(
      usePoll(async () => {
        jobs = (await api.conversions({ silent: true })) ?? [];
        if (processingLib && jobs.length === 0) processingLib = false;
      }, JOBS_POLL_MS),
    );

    pollers.push(
      usePoll(async () => {
        dlJobs = (await api.aria2.list({ silent: true })) ?? [];
      }, DL_POLL_MS),
    );

    if (isSupported()) {
      pollers.push(usePoll(pollPwaDownloads, PWA_POLL_MS, { immediate: true }));
      pwaUnsub = Down.on(() => pollPwaDownloads());
    }
  });

  async function loadAll() {
    loading = true;
    try {
      const [d, disk, hidden] = await Promise.all([
        api.manage.list(),
        api.manage.diskInfo(),
        api.manage.hidden(),
      ]);
      data = d ?? {};
      disks = Array.isArray(disk) ? disk : [];
      hiddenSet = new Set(Array.isArray(hidden) ? hidden : []);
    } catch (err: any) {
      toast.err(`Failed to load library: ${err?.message ?? err}`);
    } finally {
      loading = false;
    }
  }

  async function toggleHidden(dir: string) {
    const isHidden = hiddenSet.has(dir);
    await kv.set(`hidden:${dir}`, isHidden ? null : true);

    const next = new Set(hiddenSet);
    if (isHidden) next.delete(dir);
    else next.add(dir);
    hiddenSet = next;
  }

  async function reloadData() {
    const d = await api.manage.list();
    if (d) data = d;
  }

  function fmtBytes(b: number): string {
    if (b >= 1e12) return (b / 1e12).toFixed(1) + " TB";
    if (b >= 1e9) return (b / 1e9).toFixed(1) + " GB";
    return (b / 1e6).toFixed(0) + " MB";
  }

  function startEdit(path: string, name: string) {
    editing = path;
    val = name;
  }
  function cancelEdit() {
    editing = null;
    val = "";
  }
  async function confirmEdit(e?: Event) {
    e?.preventDefault();

    const path = editing;
    const name = val.trim();
    editing = null;
    val = "";

    if (!path || !name) return;

    const res = await api.rename(path, name);
    if (res?.ok) {
      await reloadData();
      toast.ok(`Renamed to ${name}`);
    } else {
      toast.err("Rename failed: " + (res?.error ?? "unknown"));
    }
  }

  async function delFile(dir: string, f: string) {
    const rel = dir === "." ? f : `${dir}/${f}`;
    const res = await api.deleteVideo(rel);
    if (res === null) {
      toast.err(`Delete failed: ${rel}`);
      return;
    }

    data[dir] = data[dir].filter((x) => x.name !== f);
    if (!data[dir]?.length) delete data[dir];
  }

  async function delDir(dir: string) {
    const res = await api.deleteDir(dir);
    if (!res?.ok) {
      toast.err("Delete failed: " + (res?.error ?? "unknown"));
      return;
    }

    delete data[dir];
  }

  async function addDownload(magnet: string, dir: string) {
    const res = await api.aria2.add(magnet, dir);
    if (res === null) return;
    toast.ok("Torrent added");
  }

  async function addTorrentFile(file: File, dir: string) {
    try {
      const form = new FormData();
      form.append("torrent", file);
      form.append("dir", dir);
      const r = await fetch("/api/aria2/add-torrent", {
        method: "POST",
        body: form,
      });
      if (!r.ok) {
        const text = await r.text().catch(() => "");
        toast.err(`Torrent upload failed: HTTP ${r.status} ${text.slice(0, 80)}`);
        return;
      }
      toast.ok(`Torrent uploaded: ${file.name}`);
    } catch (err: any) {
      toast.err(`Torrent upload failed: ${err?.message ?? err}`);
    }
  }

  async function removeDownload(gid: string) {
    const res = await api.aria2.remove(gid);
    if (res === null) return;
    dlJobs = dlJobs.filter((j) => j.gid !== gid);
  }

  async function pauseDownload(gid: string) {
    const res = await api.aria2.pause(gid);
    if (res === null) return;
    dlJobs = dlJobs.map((j) =>
      j.gid === gid ? { ...j, status: "paused", speed: 0 } : j,
    );
  }

  async function resumeDownload(gid: string) {
    const res = await api.aria2.resume(gid);
    if (res === null) return;
    dlJobs = dlJobs.map((j) =>
      j.gid === gid ? { ...j, status: "active" } : j,
    );
  }

  async function processLibrary() {
    processingLib = true;
    const res = await api.process();
    if (res === null) {
      processingLib = false;
      return;
    }
    toast.ok("Library processing started");
  }

  let rows = $derived(
    Object.entries(data)
      .filter(([, files]) => files?.length > 0)
      .sort(([a], [b]) => {
        if (a === ".") return 1;
        if (b === ".") return -1;

        return a.localeCompare(b);
      }),
  );

  let total = $derived(rows.reduce((n, [, files]) => n + files.length, 0));
</script>

<div style="min-height:100vh">
  <Header
    {rows}
    {total}
    {disks}
    {fmtBytes}
    processing={processingLib}
    onProcess={processLibrary}
  />
  <Magnet
    {disks}
    {fmtBytes}
    onAdd={addDownload}
    onAddTorrent={addTorrentFile}
  />

  <main>
    <Downloads
      jobs={dlJobs}
      {fmtBytes}
      onRemove={removeDownload}
      onPause={pauseDownload}
      onResume={resumeDownload}
    />

    {#if pwaDownloads.length > 0}
      <section class="pwa-dl surface flow-h">
        <h3 class="m0 p10 fs-sm fw6">
          Offline Downloads ({pwaDownloads.length})
        </h3>
        {#each pwaDownloads as dl (dl.videoParam)}
          <div class="pwa-item f al-ct g10">
            <div class="pwa-info">
              <span class="d-b fs-sm tx-4 trunc"
                >{clean(dl.title || dl.videoParam)}</span
              >
              <div class="pwa-bar">
                <ProgressBar value={dl.progress} variant="success" label />
              </div>
            </div>
          </div>
        {/each}
      </section>
    {/if}

    {#if loading}
      <div class="tx-1 p20">Loading…</div>
    {:else}
      {#each rows as [dir, files], idx}
        {@const ds = dirSizes[dir]}
        {@const disk = ds ? disks.find((d) => d.root === ds.root) : null}
        <FolderRow
          {dir}
          {files}
          {jobs}
          {idx}
          {editing}
          bind:val
          {startEdit}
          {cancelEdit}
          {confirmEdit}
          onDelFile={(f) => delFile(dir, f)}
          onDelDir={() => delDir(dir)}
          sizeBytes={ds?.bytes ?? 0}
          diskTotal={disk?.total ?? 0}
          hidden={hiddenSet.has(dir)}
          onToggleHidden={() => toggleHidden(dir)}
          {fmtBytes}
        />
      {/each}
    {/if}

    <div class="build-info fs-xs tx-1">
      frontend {frontendBuild
        .replace("T", " ")
        .replace(/:\d\dZ?$/, "")
        .replace("Z", "")} UTC
      {#if backendBuild}
        &middot; backend {backendBuild
          .replace("T", " ")
          .replace(/:\d\dZ?$/, "")
          .replace("Z", "")} UTC
      {/if}
    </div>
  </main>
</div>

<style>
  main {
    padding: 24px 40px 60px;
    max-width: 960px;
    --i: 0;
    position: relative;
    z-index: 1;
  }

  .pwa-dl {
    margin-bottom: 24px;
  }
  .pwa-dl h3 {
    background: rgba(255, 255, 255, 0.02);
    color: var(--tx-4);
    border-bottom: 1px solid var(--glass-bd);
  }
  .pwa-item {
    padding: 12px 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.04);
  }
  .pwa-info { flex: 1; min-width: 0; }
  .pwa-bar {
    margin-top: 6px;
  }

  .build-info {
    margin-top: 56px;
    padding: 16px 0;
    border-top: 1px solid var(--glass-bd);
    text-align: center;
    color: var(--tx-1);
    font-family: var(--font-display);
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }
</style>
