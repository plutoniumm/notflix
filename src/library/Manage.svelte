<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { POST, GET, DEL } from "../core/api";
  import { JOBS_POLL_MS, DL_POLL_MS, PWA_POLL_MS } from "../core/events.svelte";
  import { Down, isSupported } from "./dl";
  import { clean } from "../core/video";

  import Header from "./Header.svelte";
  import Magnet from "./Magnet.svelte";
  import Downloads from "./Downloads.svelte";
  import FolderRow from "./FolderRow.svelte";

  declare const __BUILD_TIME__: string;
  const frontendBuild = __BUILD_TIME__;

  type FileEntry = { name: string; root: string };
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
  let pwaDownloads = $state<{ videoParam: string; title: string; bgFetchId: string | null; progress: number }[]>([]);
  let dirSizes = $state<Record<string, { bytes: number; root: string }>>({});

  let jobsTimer: ReturnType<typeof setInterval>;
  let dlTimer: ReturnType<typeof setInterval>;
  let pwaTimer: ReturnType<typeof setInterval>;
  let pwaUnsub: (() => void) | null = null;

  onDestroy(() => {
    clearInterval(jobsTimer);
    clearInterval(dlTimer);
    clearInterval(pwaTimer);
    pwaUnsub?.();
  });

  async function pollPwaDownloads() {
    const records = await Down.all();
    const active = records.filter((r) => r.status === "downloading");
    const updated = await Promise.all(
      active.map(async (r) => ({
        videoParam: r.videoParam,
        title: r.title,
        bgFetchId: r.bgFetchId,
        progress: r.bgFetchId ? await Down.progress(r.bgFetchId) : 0,
      })),
    );
    pwaDownloads = updated;
  }

  onMount(() => {
    loadAll();
    GET("/api/build").then((r) => { if (r?.backend) backendBuild = r.backend; });
    GET("/api/manage/dirsizes").then((sizes: any[]) => {
      if (!Array.isArray(sizes)) return;
      const map: Record<string, { bytes: number; root: string }> = {};
      for (const s of sizes) map[s.dir] = { bytes: s.bytes, root: s.root };
      dirSizes = map;
    });
    jobsTimer = setInterval(async () => {
      if (document.hidden) return;
      jobs = (await GET("/api/conversions")) ?? [];
      if (processingLib && jobs.length === 0) processingLib = false;
    }, JOBS_POLL_MS);
    dlTimer = setInterval(async () => {
      if (document.hidden) return;
      dlJobs = (await GET("/api/aria2/list")) ?? [];
    }, DL_POLL_MS);

    if (isSupported()) {
      pollPwaDownloads();
      pwaTimer = setInterval(() => {
        if (document.hidden) return;
        pollPwaDownloads();
      }, PWA_POLL_MS);
      pwaUnsub = Down.on(() => pollPwaDownloads());
    }
  });

  async function loadAll() {
    loading = true;
    const [d, disk, hidden] = await Promise.all([
      GET("/api/manage/list"),
      GET("/api/manage/diskinfo"),
      GET("/api/manage/hidden"),
    ]);
    data = d;
    disks = disk;
    hiddenSet = new Set(Array.isArray(hidden) ? hidden : []);
    loading = false;
  }

  async function toggleHidden(dir: string) {
    const isHidden = hiddenSet.has(dir);
    await POST("/kv/set", { key: `hidden:${dir}`, value: isHidden ? null : true });
    const next = new Set(hiddenSet);
    if (isHidden) next.delete(dir);
    else next.add(dir);
    hiddenSet = next;
  }

  async function reloadData() {
    data = await GET("/api/manage/list");
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
    const res = await POST("/api/rename", { path, name });
    if (res?.ok) {
      await reloadData();
    } else {
      alert("Rename failed: " + (res?.error ?? "unknown"));
    }
  }

  async function delFile(dir: string, f: string) {
    const rel = dir === "." ? f : `${dir}/${f}`;
    await DEL(`/video/${rel}`);
    data[dir] = data[dir].filter((x) => x.name !== f);
    if (!data[dir]?.length) delete data[dir];
  }

  async function delDir(dir: string) {
    const res = await DEL(`/api/dir?path=${encodeURIComponent(dir)}`);
    if (!res?.ok) {
      alert("Delete failed: " + (res?.error ?? "unknown"));
      return;
    }
    delete data[dir];
  }

  async function addDownload(magnet: string, dir: string) {
    await POST("/api/aria2/add", { magnet, dir });
  }
  async function addTorrentFile(file: File, dir: string) {
    const form = new FormData();
    form.append("torrent", file);
    form.append("dir", dir);
    await fetch("/api/aria2/add-torrent", { method: "POST", body: form });
  }
  async function removeDownload(gid: string) {
    await DEL(`/api/aria2/remove?gid=${gid}`);
    dlJobs = dlJobs.filter((j) => j.gid !== gid);
  }
  async function pauseDownload(gid: string) {
    await POST(`/api/aria2/pause?gid=${gid}`, {});
    dlJobs = dlJobs.map((j) => (j.gid === gid ? { ...j, status: "paused", speed: 0 } : j));
  }
  async function resumeDownload(gid: string) {
    await POST(`/api/aria2/resume?gid=${gid}`, {});
    dlJobs = dlJobs.map((j) => (j.gid === gid ? { ...j, status: "active" } : j));
  }

  async function processLibrary() {
    processingLib = true;
    await POST("/api/process", {});
    // stays "processing" while conversions are active
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
  <Header {rows} {total} {disks} {fmtBytes} processing={processingLib} onProcess={processLibrary} />
  <Magnet {disks} {fmtBytes} onAdd={addDownload} onAddTorrent={addTorrentFile} />

  <main>
    <Downloads
      jobs={dlJobs}
      {fmtBytes}
      onRemove={removeDownload}
      onPause={pauseDownload}
      onResume={resumeDownload}
    />

    {#if pwaDownloads.length > 0}
      <section class="pwa-dl rx5 flow-h">
        <h3 class="m0 p10 fs-sm fw6 bg-3">
          Offline Downloads ({pwaDownloads.length})
        </h3>
        {#each pwaDownloads as dl (dl.videoParam)}
          <div class="pwa-item f al-ct g10">
            <div class="pwa-info">
              <span class="d-b fs-sm tx-4 trunc">{clean(dl.title || dl.videoParam)}</span>
              <div class="f al-ct g10">
                <div class="pwa-bar rx2">
                  <div class="pwa-fill h-100 rx2" style="width:{dl.progress}%"></div>
                </div>
                <span class="fs-xs tx-1">{dl.progress}%</span>
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
      frontend {frontendBuild.replace("T", " ").replace(/:\d\dZ?$/, "").replace("Z", "")} UTC
      {#if backendBuild}
        &middot; backend {backendBuild.replace("T", " ").replace(/:\d\dZ?$/, "").replace("Z", "")} UTC
      {/if}
    </div>
  </main>
</div>

<style>
  main {
    padding: 24px 40px 60px;
    max-width: 960px;
    --i: 0;
  }

  .pwa-dl {
    margin-bottom: 20px;
    border: 1px solid var(--bg-3);
  }
  .pwa-item {
    padding: 10px 14px;
    border-top: 1px solid var(--bg-3);
  }
  .pwa-info {
    flex: 1;
    min-width: 0;
  }
  .pwa-bar {
    flex: 1;
    height: 4px;
    background: var(--bg-4);
    margin-top: 4px;
  }
  .pwa-fill {
    background: var(--grn);
    transition: width 0.5s;
  }

  .build-info {
    margin-top: 40px;
    padding: 12px 0;
    border-top: 1px solid var(--bg-3);
    text-align: center;
  }
</style>
