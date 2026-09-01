<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { api } from "../core/api";
  import { kv } from "../core/kv";
  import { toast } from "../core/toast.svelte";
  import { ARM_MS, JOBS_POLL_MS, DL_POLL_MS, PWA_POLL_MS } from "../core/events.svelte";
  import { usePoll } from "../core/poll";
  import { Down, isSupported } from "./dl";
  import { clean } from "../core/video";
  import { join } from "../core/path";

  import Header from "./Header.svelte";
  import Magnet from "./Magnet.svelte";
  import Downloads from "./Downloads.svelte";
  import FolderRow from "./FolderRow.svelte";
  import Organize from "./Organize.svelte";
  import ProgressBar from "../components/ProgressBar.svelte";
  import DeleteBtn from "../components/DeleteBtn.svelte";
  import ProcessStatusPanel from "./ProcessStatus.svelte";

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
  let procStatus: ProcessStatus | null = $state(null);
  let pwaDownloads = $state<
    {
      videoParam: string;
      title: string;
      bgFetchId: string | null;
      progress: number;
    }[]
  >([]);
  let dirSizes = $state<Record<string, { bytes: number; root: string }>>({});

  let armed = $state<string | null>(null);
  let armTimer: ReturnType<typeof setTimeout> | undefined;

  let pollers: (() => void)[] = [];
  let pwaUnsub: (() => void) | null = null;
  let wasProcessing = false;

  onDestroy(() => {
    pollers.forEach((stop) => stop());
    pwaUnsub?.();
    clearTimeout(armTimer);
  });

  // Two-step: first tap arms, a second within ARM_MS commits. They sit inches
  // apart on a phone and the server deletes off the disk.
  function arm(key: string) {
    armed = key;
    clearTimeout(armTimer);
    armTimer = setTimeout(() => (armed = null), ARM_MS);
  }

  function disarm() {
    armed = null;
    clearTimeout(armTimer);
  }

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
        procStatus = (await api.processStatus({ silent: true })) ?? null;
        const isProc = !!procStatus?.running;
        // ScanCorrupt rebuilds the corrupt set per run — refetch when one ends.
        if (wasProcessing && !isProc) await reloadData();
        wasProcessing = isProc;
        processingLib = isProc;
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
    const rel = join(dir, f);
    if (armed !== rel) {
      arm(rel);
      return;
    }
    disarm();

    const res = await api.deleteVideo(rel, { tag: `Delete ${f}` });
    if (res === null) return;
    if (res.ok === false) {
      toast.err(`Delete failed: ${res.error ?? rel}`);
      return;
    }

    data[dir] = data[dir].filter((x) => x.name !== f);
    if (!data[dir]?.length) delete data[dir];
  }

  async function delDir(dir: string) {
    if (armed !== dir) {
      arm(dir);
      return;
    }
    disarm();

    const res = await api.deleteDir(dir, { tag: `Delete ${dir}` });
    if (res === null) return;
    if (res.ok === false) {
      toast.err(`Delete failed: ${res.error ?? dir}`);
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
    const res = await api.aria2.addTorrent(file, dir, { tag: "Torrent upload" });
    if (res === null) return;
    toast.ok(`Torrent uploaded: ${file.name}`);
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

  let corruptFiles = $derived(
    Object.entries(data).flatMap(([dir, files]) =>
      (files ?? [])
        .filter((f) => f.corrupt)
        .map((f) => ({ dir, name: f.name, root: f.root })),
    ),
  );
</script>

<div style="min-height:100vh; min-height:100dvh">
  <Header
    {rows}
    {total}
    {disks}
    processing={processingLib}
    onProcess={processLibrary}
  />
  <Magnet {disks} onAdd={addDownload} onAddTorrent={addTorrentFile} />

  <main>
    <ProcessStatusPanel status={procStatus} {jobs} />

    {#if corruptFiles.length > 0}
      <section class="panel bad surface flow-h">
        <h3 class="phead m0 f al-ct g10">
          <span class="plabel">Corrupt / unplayable</span>
          <span class="pcount fs-xs">{corruptFiles.length}</span>
          <span class="pnote fs-xs tx-1">re-scanned on every library process</span>
        </h3>
        {#each corruptFiles as cf (cf.dir + "/" + cf.name)}
          <div class="item f al-ct g10">
            <div class="info">
              <span class="d-b fs-sm tx-4 trunc">{clean(cf.name)}</span>
              <span class="d-b fs-xs tx-1 trunc">
                {cf.dir === "." ? "Root" : cf.dir} · {cf.root}
              </span>
            </div>
            <DeleteBtn
              armed={armed === join(cf.dir, cf.name)}
              title="Delete file"
              onclick={() => delFile(cf.dir, cf.name)}
            />
          </div>
        {/each}
      </section>
    {/if}

    <Organize onApplied={() => loadAll()} />

    <Downloads
      jobs={dlJobs}
      onRemove={removeDownload}
      onPause={pauseDownload}
      onResume={resumeDownload}
    />

    {#if pwaDownloads.length > 0}
      <section class="panel surface flow-h">
        <h3 class="phead m0 f al-ct g10">
          <span class="plabel">Offline downloads</span>
          <span class="pcount fs-xs">{pwaDownloads.length}</span>
        </h3>
        {#each pwaDownloads as dl (dl.videoParam)}
          <div class="item f al-ct g10">
            <div class="info">
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
      <div class="skel f-col g10" aria-hidden="true">
        {#each Array(7) as _, i}
          <div class="skel-row" style="--i:{i}"></div>
        {/each}
      </div>
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
          {armed}
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
    max-width: 980px;
    --i: 0;
    position: relative;
    z-index: 1;
  }

  .panel {
    margin-bottom: 20px;
    transition:
      opacity var(--motion, 0.32s) var(--ease-out),
      translate var(--motion, 0.32s) var(--ease-out);
  }
  @starting-style {
    .panel {
      opacity: 0;
      translate: 0 10px;
    }
  }
  .phead {
    padding: 10px 16px;
    background: rgb(255 255 255 / 0.02);
    border-bottom: 1px solid var(--glass-bd);
  }
  .plabel {
    color: var(--tx-3);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }
  .pcount {
    color: var(--tx-1);
    font-variant-numeric: tabular-nums;
  }
  .pnote {
    margin-left: auto;
    text-align: right;
  }

  .panel.bad {
    border-color: color-mix(in oklab, var(--red) 45%, transparent);
    box-shadow: inset 2px 0 0 var(--red);
  }
  .panel.bad .plabel {
    color: var(--red);
  }

  .item {
    padding: 10px 16px;
    border-top: 1px solid rgb(255 255 255 / 0.04);
  }
  .item:first-of-type {
    border-top: none;
  }
  .info {
    flex: 1;
    min-width: 0;
  }
  .pwa-bar {
    margin-top: 6px;
  }

  .skel-row {
    height: 46px;
    border-radius: var(--r-lg);
    background: linear-gradient(
      90deg,
      var(--bg-3) 0%,
      var(--bg-4) 45%,
      var(--bg-3) 90%
    );
    background-size: 420px 100%;
    animation: shimmer 1.7s linear infinite;
    animation-delay: calc(var(--i) * -160ms);
    opacity: calc(1 - var(--i) * 0.1);
  }

  .build-info {
    margin-top: 56px;
    padding: 16px 0;
    border-top: 1px solid var(--glass-bd);
    text-align: center;
    color: var(--tx-1);
    font-family: var(--font-display);
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  @media (max-width: 640px) {
    main {
      padding: 16px 12px 48px;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .panel {
      transition-duration: 1ms;
    }
    .skel-row {
      animation: none;
    }
  }
</style>
