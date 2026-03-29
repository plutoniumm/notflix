<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { POST, GET, DEL } from "./lib";

  import Header from "./components/Header.svelte";
  import Magnet from "./components/Magnet.svelte";
  import Downloads from "./components/Downloads.svelte";
  import FolderRow from "./components/FolderRow.svelte";

  let data: Record<string, string[]> = $state({});
  let disks: DiskInfo[] = $state([]);
  let loading = $state(true);
  let editing = $state<string | null>(null);
  let val = $state("");
  let jobs: Job[] = $state([]);
  let dlJobs: Downjob[] = $state([]);

  let jobsTimer: ReturnType<typeof setInterval>;
  let dlTimer: ReturnType<typeof setInterval>;

  onDestroy(() => {
    clearInterval(jobsTimer);
    clearInterval(dlTimer);
  });

  onMount(() => {
    loadAll();
    jobsTimer = setInterval(async () => {
      jobs = (await GET("/api/conversions")) ?? [];
    }, 2000);
    dlTimer = setInterval(async () => {
      dlJobs = (await GET("/api/aria2/list")) ?? [];
    }, 2000);
  });

  async function loadAll() {
    loading = true;
    [data, disks] = await Promise.all([
      GET("/api/manage/list"),
      GET("/api/manage/diskinfo"),
    ]);
    loading = false;
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
    if (!confirm(`Delete "${f}"?`)) return;
    await DEL(`/video/${rel}`);
    data[dir] = data[dir].filter((x) => x !== f);
    if (!data[dir]?.length) delete data[dir];
  }

  async function delDir(dir: string) {
    if (
      !confirm(
        `Delete entire folder "${dir}" and all its contents? This cannot be undone.`,
      )
    )
      return;
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
  async function removeDownload(gid: string) {
    await DEL(`/api/aria2/remove?gid=${gid}`);
    dlJobs = dlJobs.filter((j) => j.gid !== gid);
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
  <Header {rows} {total} {disks} {fmtBytes} />
  <Magnet {disks} {fmtBytes} onAdd={addDownload} />

  <main>
    <Downloads jobs={dlJobs} {fmtBytes} onRemove={removeDownload} />

    {#if loading}
      <div class="tx-1 p20">Loading…</div>
    {:else}
      {#each rows as [dir, files], idx}
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
        />
      {/each}
    {/if}
  </main>
</div>

<style>
  main {
    padding: 24px 40px 60px;
    max-width: 960px;
    --i: 0;
  }
</style>
