<script lang="ts">
  import { onMount } from "svelte";
  import { cleanName } from "./lib/video";


  let data: Record<string, string[]> = $state({});
  let disks: DiskInfo[] = $state([]);
  let loading = $state(true);
  let editing = $state<string | null>(null);
  let val = $state("");
  let expanded: Record<string, boolean> = $state({});

  function focus(el: HTMLInputElement) {
    el.focus();
    el.select();
  }

  onMount(async () => {
    await load();
  });

  function fmtBytes(b: number): string {
    if (b >= 1e12) return (b / 1e12).toFixed(1) + " TB";
    if (b >= 1e9) return (b / 1e9).toFixed(1) + " GB";
    return (b / 1e6).toFixed(0) + " MB";
  }

  async function load() {
    loading = true;
    [data, disks] = await Promise.all([
      fetch("/api/manage/list")
        .then((r) => r.json())
        .catch(() => ({})),
      fetch("/api/manage/diskinfo")
        .then((r) => r.json())
        .catch(() => []),
    ]);
    for (const dir of Object.keys(data as Record<string, string[]>)) {
      if (!(dir in expanded)) expanded[dir] = true;
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

<div class="page">
  <header>
    <a href="/" class="back">← Home</a>
    <h1>Manage Library</h1>
    <span class="count">{rows.length} folders · {total} files</span>
    {#if disks.length > 0}
      <div class="disks">
        {#each disks as d}
          {@const pct = Math.round((1 - d.free / d.total) * 100)}
          <div class="disk">
            <span class="disk-name">{d.root}</span>
            <div class="disk-bar">
              <div class="disk-fill" style="width:{pct}%"></div>
            </div>
            <span class="disk-free">{fmtBytes(d.free)} free</span>
          </div>
        {/each}
      </div>
    {/if}
  </header>

  <main>
    {#if loading}
      <div class="loading">Loading…</div>
    {:else}
      {#each rows as [dir, files]}
        <div class="folder">
          <div
            class="folder-header"
            role="button"
            tabindex="0"
            onclick={() => (expanded[dir] = !expanded[dir])}
            onkeydown={(e) =>
              e.key === "Enter" && (expanded[dir] = !expanded[dir])}
          >
            <span class="chevron">{expanded[dir] ? "▾" : "▸"}</span>
            <span class="folder-icon">📁</span>

            {#if editing === dir}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
              <form
                class="rename-form"
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
              <span class="folder-name"
                >{dir === "." ? "Root" : cleanName(dir) || dir}</span
              >
              <span class="folder-raw">{dir === "." ? "" : dir}</span>
              <span class="file-count">{files.length}</span>
              {#if dir !== "."}
                <button
                  class="icon-btn"
                  title="Rename folder"
                  onclick={(e) => {
                    e.stopPropagation();
                    startEdit(dir, dir);
                  }}>✏</button
                >
                <button
                  class="icon-btn danger"
                  title="Delete folder"
                  onclick={(e) => {
                    e.stopPropagation();
                    delDir(dir);
                  }}>🗑</button
                >
              {/if}
            {/if}
          </div>

          {#if expanded[dir]}
            <ul class="file-list">
              {#each files as f (f)}
                {@const fpath = dir === "." ? f : `${dir}/${f}`}
                <li class="file">
                  <span
                    class="file-type-icon"
                    title={f.split(".").pop()?.toUpperCase()}>{icon(f)}</span
                  >

                  {#if editing === fpath}
                    <form class="rename-form" onsubmit={confirmEdit}>
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
                      <span class="clean-name">{cleanName(f)}</span>
                      <span class="raw-name">{f}</span>
                    </div>
                    <div class="file-actions">
                      <button
                        class="icon-btn"
                        title="Rename"
                        onclick={() => startEdit(fpath, f)}>✏</button
                      >
                      <button
                        class="icon-btn danger"
                        title="Delete"
                        onclick={() => delFile(dir, f)}>✕</button
                      >
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
  .page {
    min-height: 100vh;
  }

  header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 16px;
    padding: 20px 40px;
    background: #0a0a0a;
    border-bottom: 1px solid #222;
    position: sticky;
    top: 0;
    z-index: 10;
  }

  .disks {
    display: flex;
    gap: 24px;
    margin-left: auto;
    flex-shrink: 0;
  }
  .disk {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .disk-name {
    font-size: 12px;
    color: #666;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    white-space: nowrap;
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
  .disk-free {
    font-size: 12px;
    color: #555;
    white-space: nowrap;
  }

  .back {
    color: #aaa;
    font-size: 13px;
    transition: color 0.15s;
  }
  .back:hover {
    color: #fff;
  }

  h1 {
    margin: 0;
    font-size: 1.2rem;
    font-weight: 600;
  }
  .count {
    color: #555;
    font-size: 13px;
  }

  main {
    padding: 24px 40px 60px;
    max-width: 960px;
  }
  .loading {
    color: #555;
    padding: 40px 0;
  }

  .folder {
    border: 1px solid #2a2a2a;
    border-radius: 6px;
    margin-bottom: 8px;
    overflow: hidden;
  }

  .folder-header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: #1a1a1a;
    cursor: pointer;
    user-select: none;
    transition: background 0.1s;
  }
  .folder-header:hover {
    background: #212121;
  }
  .folder-header:hover .icon-btn {
    opacity: 1;
  }

  .chevron {
    color: #555;
    font-size: 11px;
    width: 12px;
    flex-shrink: 0;
  }
  .folder-icon {
    font-size: 0.95rem;
    flex-shrink: 0;
  }
  .folder-name {
    font-weight: 500;
    font-size: 14px;
  }
  .folder-raw {
    color: #444;
    font-size: 11px;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .file-count {
    color: #555;
    font-size: 12px;
    margin-left: auto;
    flex-shrink: 0;
  }

  .icon-btn {
    background: none;
    border: none;
    color: #555;
    padding: 3px 7px;
    border-radius: 3px;
    font-size: 12px;
    transition:
      color 0.15s,
      background 0.15s;
    flex-shrink: 0;
    opacity: 0;
  }
  .icon-btn:hover {
    background: #333;
    color: #ccc;
  }
  .icon-btn.danger:hover {
    color: #e50914;
    background: rgba(229, 9, 20, 0.1);
  }

  .rename-form {
    flex: 1;
    display: flex;
    min-width: 0;
  }
  .rename-input {
    flex: 1;
    background: #0d0d0d;
    border: 1px solid #e50914;
    color: #fff;
    padding: 3px 10px;
    border-radius: 3px;
    font-size: 13px;
    outline: none;
    min-width: 0;
  }

  .file-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .file {
    display: flex;
    align-items: center;
    gap: 10px;
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
  .file:hover .icon-btn {
    opacity: 1;
  }

  .file-type-icon {
    font-size: 10px;
    color: #555;
    width: 16px;
    flex-shrink: 0;
    text-align: center;
  }

  .file-names {
    flex: 1;
    min-width: 0;
  }
  .clean-name {
    display: block;
    font-size: 13px;
    color: #ddd;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .raw-name {
    display: block;
    font-size: 11px;
    color: #3a3a3a;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .file-actions {
    display: flex;
    gap: 2px;
    opacity: 0;
    transition: opacity 0.15s;
  }
</style>
