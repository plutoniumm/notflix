<script lang="ts">
  import { onMount } from "svelte";
  import { cleanName } from "./lib/video";

  let data: Record<string, string[]> = $state({});
  let disks: DiskInfo[] = $state([]);
  let loading = $state(true);
  let editing = $state<string | null>(null);
  let val = $state("");
  let open: Record<string, boolean> = $state({});

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
      fetch("/api/manage/list").then((r) => r.json()),
      fetch("/api/manage/diskinfo").then((r) => r.json()),
    ]);

    for (const dir of Object.keys(data as Record<string, string[]>)) {
      if (!(dir in open)) open[dir] = true;
    }

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

    const res = await fetch("/api/rename", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, name }),
    })
      .then((r) => r.json())
      .catch(() => ({ ok: false }));

    if (res.ok) {
      await load();
    } else {
      alert("Rename failed: " + (res.error ?? "unknown"));
    }
  }

  async function delFile(dir: string, filename: string) {
    const rel = dir === "." ? filename : `${dir}/${filename}`;
    if (!confirm(`Delete "${filename}"?`)) return;

    await fetch(`/video/${rel}`, { method: "DELETE" });
    await load();
  }

  async function delDir(dir: string) {
    if (
      !confirm(
        `Delete entire folder "${dir}" and all its contents? This cannot be undone.`,
      )
    )
      return;

    const res = await fetch(`/api/dir?path=${encodeURIComponent(dir)}`, {
      method: "DELETE",
    })
      .then((r) => r.json())
      .catch(() => ({ ok: false }));

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
  <header class="f fw al-ct g10">
    <a href="/" class="back fs-base c-muted"> ← Home </a>
    <h1 class="m0 fw6">Manage Library</h1>
    <span class="fs-base c-dim">
      {rows.length} folders · {total} files
    </span>

    {#if disks.length > 0}
      <div class="f g20 sh-0" style="margin-left:auto">
        {#each disks as d}
          {@const pct = Math.round((1 - d.free / d.total) * 100)}

          <div class="f al-ct g5">
            <span class="fs-xs c-dim up" style="white-space:nowrap">
              {d.root}
            </span>
            <div class="disk-bar">
              <div class="disk-fill" style="width:{pct}%"></div>
            </div>

            <span class="fs-xs c-dim" style="white-space:nowrap">
              {fmtBytes(d.free)} free
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </header>

  <main>
    {#if loading}
      <div class="c-dim p20">Loading…</div>
    {:else}
      {#each rows as [dir, files]}
        <div class="folder flow-h">
          <div
            class="folder-hd f al-ct g10 ptr"
            role="button"
            tabindex="0"
            onclick={() => ((1)[dir] = !open[dir])}
            onkeydown={(e) => e.key === "Enter" && (open[dir] = !open[dir])}
          >
            <span class="fs-xs c-dim sh-0" style="width:12px">
              {open[dir] ? "▾" : "▸"}
            </span>
            <span class="sh-0" style="font-size:0.95rem">📁</span>

            {#if editing === dir}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
              <form
                class="rename-form f"
                onsubmit={confirmEdit}
                onclick={(e) => e.stopPropagation()}
              >
                <input
                  class="rename-input"
                  bind:value={val}
                  onkeydown={(e) => e.key === "Escape" && cancelEdit()}
                  onblur={confirmEdit}
                  use:focus
                />
              </form>
            {:else}
              <span class="fw5 fs-md">
                {dir === "." ? "Root" : cleanName(dir) || dir}
              </span>
              <span class="folder-raw fs-xs c-dim trunc">
                {dir === "." ? "" : dir}
              </span>
              <span class="fs-sm c-dim sh-0" style="margin-left:auto">
                {files.length}
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
                  ✏
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
              {/if}
            {/if}
          </div>

          {#if open[dir]}
            <ul class="m0 p0" style="list-style:none">
              {#each files as f (f)}
                {@const fpath = dir === "." ? f : `${dir}/${f}`}
                <li class="file f al-ct g10">
                  <span
                    class="fs-xs c-dim sh-0 tc"
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
                      <span class="d-b fs-base c-light trunc">
                        {cleanName(f)}
                      </span>
                      <span class="d-b fs-xs trunc" style="color:#3a3a3a">
                        {f}
                      </span>
                    </div>
                    <div class="file-actions f g2">
                      <button
                        class="btn-icon"
                        title="Rename"
                        onclick={() => startEdit(fpath, f)}
                      >
                        ✏
                      </button>
                      <button
                        class="btn-icon danger"
                        title="Delete"
                        onclick={() => delFile(dir, f)}
                      >
                        ✕
                      </button>
                    </div>
                  {/if}
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/each}
    {/if}
  </main>
</div>

<style>
  header {
    padding: 20px 40px;
    background: #0a0a0a;
    border-bottom: 1px solid #222;
    position: sticky;
    top: 0;
    z-index: 10;
  }

  h1 {
    font-size: 1.2rem;
  }

  .disk-bar {
    width: 80px;
    height: 3px;
    background: #2a2a2a;
    border-radius: 2px;
    overflow: hidden;
  }
  .disk-fill {
    height: 100%;
    background: #e50914;
    border-radius: 2px;
    transition: width 0.3s;
  }

  .back:hover {
    color: #fff;
  }

  main {
    padding: 24px 40px 60px;
    max-width: 960px;
  }

  .folder {
    border: 1px solid #2a2a2a;
    border-radius: 6px;
    margin-bottom: 8px;
  }

  .folder-hd {
    padding: 10px 14px;
    background: #1a1a1a;
    user-select: none;
    transition: background 0.1s;
  }
  .folder-hd:hover {
    background: #212121;
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
    background: #0d0d0d;
    border: 1px solid #e50914;
    color: #fff;
    padding: 3px 10px;
    border-radius: 3px;
    font-size: 13px;
  }

  .file {
    padding: 7px 14px 7px 42px;
    border-top: 1px solid #1e1e1e;
    transition: background 0.1s;
  }
  .file:hover {
    background: #191919;
  }
  .file:hover .file-actions {
    opacity: 1;
  }
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
