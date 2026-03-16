<script lang="ts">
  import { onMount } from "svelte";
  import { clean } from "./lib/video";
  import { POST, GET, DEL } from "./lib";

  let data: Record<string, string[]> = $state({});
  let disks: DiskInfo[] = $state([]);
  let loading = $state(true);
  let editing = $state<string | null>(null);
  let val = $state("");

  function focus(el: HTMLInputElement) {
    el.focus();
    el.select();
  }

  onMount(load);

  function fmtBytes(b: number): string {
    if (b >= 1e12) return (b / 1e12).toFixed(1) + " TB";
    if (b >= 1e9) return (b / 1e9).toFixed(1) + " GB";

    return (b / 1e6).toFixed(0) + " MB";
  }

  async function load() {
    loading = true;

    [data, disks] = await Promise.all([
      GET("/api/manage/list"),
      GET("/api/manage/diskinfo"),
    ]);

    loading = false;
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

    if (res.ok) {
      await load();
    } else {
      alert("Rename failed: " + (res.error ?? "unknown"));
    }
  }

  async function delFile(dir: string, filename: string) {
    const rel = dir === "." ? filename : `${dir}/${filename}`;
    if (!confirm(`Delete "${filename}"?`)) return;

    await DEL(`/video/${rel}`);
    await load();
  }

  async function delDir(dir: string) {
    if (
      !confirm(
        `Delete entire folder "${dir}" and all its contents? This cannot be undone.`,
      )
    )
      return;

    const res = await DEL(`/api/dir?path=${encodeURIComponent(dir)}`);

    if (!res.ok) {
      alert("Delete failed: " + (res.error ?? "unknown"));
      return;
    }
    await load();
  }

  function icon(name: string) {
    const ext = name.split(".").pop()?.toLowerCase();

    return ext === "mkv" || ext === "mov" ? "⟳" : "▶";
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
  <header class="f fw al-ct g10 p-stx">
    <a href="/" class="back fs tx-3"> ← Home </a>
    <h1 class="m0 fw6">Manage Library</h1>
    <span class="fs tx-1">
      {rows.length} folders · {total} files
    </span>

    {#if disks.length > 0}
      <div class="f g20 sh-0" style="margin-left:auto">
        {#each disks as d}
          {@const pct = Math.round((1 - d.free / d.total) * 100)}

          <div class="f al-ct g5">
            <span class="fs-xs tx-1 up" style="white-space:nowrap">
              {d.root}
            </span>
            <div class="disk-bar rx2 flow-h">
              <div class="disk-fill h-100 rx2" style="width:{pct}%"></div>
            </div>

            <span class="fs-xs tx-1" style="white-space:nowrap">
              {fmtBytes(d.free)} free
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </header>

  <main>
    {#if loading}
      <div class="tx-1 p20">Loading…</div>
    {:else}
      {#each rows as [dir, files], idx}
        <details class="folder rx5 flow-h" style="--i:{idx}">
          <summary class="folder-hd f al-ct g10 ptr">
            <span>📁</span>

            {#if editing === dir}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
              <form
                class="rename-form f"
                onsubmit={confirmEdit}
                onclick={(e) => e.stopPropagation()}
              >
                <input
                  class="rename-input fs-sm"
                  bind:value={val}
                  onkeydown={(e) => e.key === "Escape" && cancelEdit()}
                  onblur={confirmEdit}
                  use:focus
                />
              </form>
            {:else}
              <span class="fw5 fs-md">
                {dir === "." ? "Root" : clean(dir) || dir}
              </span>
              <span class="folder-raw fs-xs tx-1 trunc">
                {dir === "." ? "" : dir}
              </span>
              <span class="fs-sm tx-1 sh-0" style="margin-left:auto">
                ({files.length} files)
              </span>

              {#if dir !== "."}
                <button
                  class="btn-icon"
                  title="Rename folder"
                  onclick={(e) => {
                    e.stopPropagation();
                    startEdit(dir, dir);
                  }}
                >
                  ✏️
                </button>
                <button
                  class="btn-icon danger"
                  title="Delete folder"
                  onclick={(e) => {
                    e.stopPropagation();
                    delDir(dir);
                  }}
                >
                  🗑
                </button>
              {:else}
                <div style="width:32px">&nbsp;</div>
              {/if}
            {/if}
          </summary>

          <ul class="m0 p0">
            {#each files as f (f)}
              {@const fpath = dir === "." ? f : `${dir}/${f}`}
              <li class="file f al-ct g10">
                <span
                  class="fs-xs tx-1 sh-0 tc"
                  style="width:16px"
                  title={f.split(".").pop()?.toUpperCase()}
                >
                  {icon(f)}
                </span>

                {#if editing === fpath}
                  <form class="rename-form f" onsubmit={confirmEdit}>
                    <input
                      class="rename-input"
                      bind:value={val}
                      onkeydown={(e) => e.key === "Escape" && cancelEdit()}
                      onblur={confirmEdit}
                      use:focus
                    />
                  </form>
                {:else}
                  <div class="file-names">
                    <span class="d-b fs tx-4 trunc">
                      {clean(f)}
                    </span>
                    <span class="d-b fs-xs trunc" style="color:var(--bg-5)">
                      {f}
                    </span>
                  </div>
                  <div class="file-actions f g2">
                    <button
                      class="btn-icon"
                      title="Rename"
                      onclick={() => startEdit(fpath, f)}
                    >
                      ✏️
                    </button>
                    <button
                      class="btn-icon danger"
                      title="Delete"
                      onclick={() => delFile(dir, f)}
                    >
                      🗑
                    </button>
                  </div>
                {/if}
              </li>
            {/each}
          </ul>
        </details>
      {/each}
    {/if}
  </main>
</div>

<style>
  header {
    padding: 20px 40px;
    background: var(--bg-2);
    border-bottom: 1px solid var(--bg-3);
    top: 0;
    z-index: 10;
  }

  .disk-bar {
    width: 80px;
    height: 3px;
    background: var(--bg-3);
  }

  .disk-fill {
    background: var(--red);
    transition: width 0.3s;
  }

  .back:hover {
    color: var(--tx-5);
  }

  main {
    padding: 24px 40px 60px;
    max-width: 960px;
    --i: 0;
  }

  .folder {
    border: 1px solid var(--bg-3);
    margin-bottom: 8px;
    animation: slide-up 0.25s ease both;
    animation-delay: calc(var(--i) * 40ms);
  }

  .folder-hd {
    padding: 10px 14px;
    background: var(--bg-3);
    user-select: none;
    transition: background 0.1s;
  }

  .folder-hd:hover {
    background: var(--bg-3);
  }
  .folder-hd:hover .btn-icon {
    opacity: 1;
  }

  .folder-raw {
    flex: 1;
  }

  .rename-form {
    flex: 1;
    min-width: 0;
  }
  .rename-input {
    flex: 1;
    width: 100%;
    background: var(--bg-2);
    border: 1px solid var(--red);
    color: var(--tx-5);
    padding: 3px 10px;
    border-radius: 3px;
  }

  .file {
    padding: 7px 14px 7px 42px;
    border-top: 1px solid var(--bg-3);
    transition: background 0.1s;
  }

  .file:hover {
    background: var(--bg-3);
  }

  .file:hover .file-actions,
  .file:hover .btn-icon {
    opacity: 1;
  }

  .file-names {
    flex: 1;
    min-width: 0;
  }
  .file-actions {
    opacity: 0;
    transition: opacity 0.15s;
  }
</style>
