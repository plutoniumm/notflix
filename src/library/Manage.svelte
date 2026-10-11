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
  import { ago, pct } from "../core/fmt";

  import Header from "./Header.svelte";
  import Magnet from "./Magnet.svelte";
  import Downloads from "./Downloads.svelte";
  import FolderRow from "./FolderRow.svelte";
  import Organize from "./Organize.svelte";
  import Storage from "./Storage.svelte";
  import Sect from "./Sect.svelte";
  import DeleteBtn from "../components/DeleteBtn.svelte";
  import ProcessStatusPanel from "./ProcessStatus.svelte";
  import { room } from "../core/room.svelte";

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
  let dlDown = $state(false);

  // The room goes faintly red when something needs a person, so the state is
  // known from any page and this one is only opened once it already matters.
  $effect(() => {
    room.alarm = dlDown;
  });
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
        const r = await api.aria2.list({ silent: true });
        // aria2 can keep downloading with its RPC wedged; [] would read as
        // "nothing queued" and hide the one state that needs a restart.
        dlDown = r == null;
        dlJobs = r ?? [];
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

  const stamped = (s: string) => {
    const t = Date.parse(s);

    return Number.isFinite(t) ? ago(t) : "unknown";
  };

  // The footer's only job: catch a bundle the browser is holding after the
  // binary moved on. A date pair nobody can diff by eye can't do that.
  let stale = $derived.by(() => {
    const f = Date.parse(frontendBuild);
    const b = Date.parse(backendBuild);

    return Number.isFinite(f) && Number.isFinite(b) && b - f > 120_000;
  });
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
    <ProcessStatusPanel status={procStatus} {jobs} i={0} />

    {#if corruptFiles.length > 0}
      <Sect
        i={1}
        bad
        label="Corrupt / unplayable"
        meta="{corruptFiles.length} · re-scanned on every library process"
      >
        {#each corruptFiles as cf (cf.dir + "/" + cf.name)}
          <div class="led bad-row">
            <span class="fs-sm tx-4 trunc">{clean(cf.name)}</span>
            <span class="fs-xs tx-1 trunc tr">
              {cf.dir === "." ? "Root" : cf.dir} · {cf.root}
            </span>
            <DeleteBtn
              armed={armed === join(cf.dir, cf.name)}
              title="Delete file"
              onclick={() => delFile(cf.dir, cf.name)}
            />
          </div>
        {/each}
      </Sect>
    {/if}

    {#if dlDown}
      <Sect i={1} bad label="aria2 not answering">
        <p class="m0 fs-xs tx-1 pad">
          downloads may still be running; restart aria2 to get status back
        </p>
      </Sect>
    {/if}

    <div class="cols">
      <div class="col">
        <Downloads
          i={2}
          jobs={dlJobs}
          onRemove={removeDownload}
          onPause={pauseDownload}
          onResume={resumeDownload}
        />

        <Organize i={4} onApplied={() => loadAll()} />
      </div>

      <div class="col">
        <Storage i={5} {disks} {dirSizes} />

    {#if pwaDownloads.length > 0}
      <Sect i={3} label="Saving to this device" meta="{pwaDownloads.length} files">
        {#each pwaDownloads as dl (dl.videoParam)}
          <div class="led pwa" style="--fill:{dl.progress}%">
            <span class="fs-sm tx-4 trunc">{clean(dl.title || dl.videoParam)}</span>
            <span class="fs-xs tx-1 num tr">{pct(dl.progress)}</span>
          </div>
        {/each}
      </Sect>
    {/if}

      </div>
    </div>

    <Sect i={6} label="Library" meta="{rows.length} folders · {total} files">
      {#if loading}
        <div class="skel f-col g10">
          {#each Array(7) as _, k}
            <div class="skel-row" style="--i:{k}"></div>
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
    </Sect>

    <footer class="foot fs-xs f fw al-ct j-ct g10">
      <span class="tx-1">built</span>
      <span class="tx-3">frontend {stamped(frontendBuild)}</span>
      {#if backendBuild}
        <span class="tx-1">/</span>
        <span class="tx-3">backend {stamped(backendBuild)}</span>
      {/if}
      {#if stale}
        <span class="red">— reload, this bundle is older than the server</span>
      {/if}
    </footer>
  </main>
</div>

<style>
  main {
    padding: 24px 40px 60px;
    max-width: 1520px;
    --i: 0;
    position: relative;
    z-index: 1;
  }

  /* Processing and Library stay full width — a table and a long list both read
     worse in a half column. Everything else pairs up once there is room. */
  .cols {
    display: grid;
    gap: 0 44px;
    align-items: start;
  }
  @media (min-width: 1180px) {
    .cols {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
  }
  .col {
    min-width: 0;
  }

  .bad-row {
    grid-template-columns: minmax(0, 1fr) minmax(0, auto) auto;
  }
  .pwa {
    grid-template-columns: minmax(0, 1fr) 48px;
    --led-c: color-mix(in oklab, var(--grn) 22%, transparent);
  }
  .pad {
    padding: 8px 10px;
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

  .foot {
    margin-top: 48px;
    padding: 16px 0;
    border-top: 1px solid var(--glass-bd);
    font-family: var(--font-display);
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  @media (max-width: 640px) {
    main {
      padding: 16px 12px 48px;
    }
  }

  
</style>
