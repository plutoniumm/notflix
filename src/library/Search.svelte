<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../core/api";
  import { toast } from "../core/toast.svelte";
  import { parseName, epTag, type Parsed } from "./torrentName";
  import Badge from "../components/Badge.svelte";
  import EmptyState from "../components/EmptyState.svelte";
  import BackLink from "../components/BackLink.svelte";

  type Row = { t: Torrent; p: Parsed };

  let q = $state("");
  let results = $state<Torrent[]>([]);
  let loading = $state(false);
  let searched = $state(false);
  let disks = $state<DiskInfo[]>([]);
  let dir = $state("");
  let added = $state(new Set<string>());

  let resFilter = $state("all");
  let textFilter = $state("");
  let sortKey = $state<"seeders" | "size" | "leechers" | "files" | "added" | "title" | "res">(
    "seeders",
  );
  let sortDir = $state<1 | -1>(-1);

  onMount(async () => {
    const d = await api.manage.diskInfo();
    disks = Array.isArray(d) ? d : [];
    if (disks.length > 0) dir = disks[0].path;
  });

  function fmtBytes(b: number): string {
    if (b >= 1e12) return (b / 1e12).toFixed(1) + " TB";
    if (b >= 1e9) return (b / 1e9).toFixed(1) + " GB";
    return (b / 1e6).toFixed(0) + " MB";
  }

  function ago(ts: number): string {
    const s = Date.now() / 1000 - ts;
    if (s < 3600) return Math.max(1, Math.floor(s / 60)) + "m";
    if (s < 86400) return Math.floor(s / 3600) + "h";
    if (s < 2592000) return Math.floor(s / 86400) + "d";
    if (s < 31536000) return Math.floor(s / 2592000) + "mo";
    return Math.floor(s / 31536000) + "y";
  }

  async function run() {
    const term = q.trim();
    if (!term) return;
    loading = true;
    searched = true;
    const r = await api.search(term);
    results = Array.isArray(r) ? r : [];
    loading = false;
  }

  async function add(t: Torrent) {
    if (!dir) {
      toast.err("No download disk available");
      return;
    }
    if (added.has(t.infoHash)) return;
    const res = await api.aria2.add(t.magnet, dir);
    if (res === null) return;
    added = new Set(added).add(t.infoHash);
    toast.ok(`Added: ${parseName(t.name).title}`);
  }

  function sortBy(key: typeof sortKey) {
    if (sortKey === key) sortDir = sortDir === 1 ? -1 : 1;
    else {
      sortKey = key;
      sortDir = key === "title" ? 1 : -1;
    }
  }
  const arrow = (key: typeof sortKey) => (sortKey !== key ? "" : sortDir === 1 ? " ▲" : " ▼");

  let rows = $derived<Row[]>(results.map((t) => ({ t, p: parseName(t.name) })));

  let view = $derived(
    rows
      .filter(({ p }) => {
        if (resFilter === "sd") return p.resRank > 0 && p.resRank <= 480;
        if (resFilter !== "all") return p.res === resFilter;
        return true;
      })
      .filter(({ t, p }) => {
        const f = textFilter.trim().toLowerCase();
        return !f || p.title.toLowerCase().includes(f) || t.name.toLowerCase().includes(f);
      })
      .sort((a, b) => {
        let d: number;
        if (sortKey === "title") d = a.p.title.localeCompare(b.p.title);
        else if (sortKey === "res") d = a.p.resRank - b.p.resRank;
        else d = (a.t[sortKey] as number) - (b.t[sortKey] as number);
        return d * sortDir;
      }),
  );
</script>

<div style="min-height:100vh">
  <header class="glass-header f al-ct g20">
    <BackLink />
    <h1 class="m0 fw6">Explore</h1>

    <div class="search-wrap">
      <input
        class="field w-100"
        type="search"
        placeholder="Search The Pirate Bay…"
        bind:value={q}
        onkeydown={(e) => e.key === "Enter" && run()}
        autocomplete="off"
        spellcheck="false"
      />
    </div>

    <button class="btn-action ptr fs-xs" onclick={run} disabled={loading}>
      {loading ? "Searching…" : "Search"}
    </button>

    {#if disks.length > 0}
      <select class="field sh-0" bind:value={dir} title="Download target disk">
        {#each disks as d}
          <option value={d.path}>{d.root} ({fmtBytes(d.free)} free)</option>
        {/each}
      </select>
    {/if}
  </header>

  {#if rows.length > 0}
    <div class="toolbar f al-ct g10">
      <select class="field" bind:value={resFilter}>
        <option value="all">All resolutions</option>
        <option value="2160p">2160p / 4K</option>
        <option value="1080p">1080p</option>
        <option value="720p">720p</option>
        <option value="sd">≤ 480p</option>
      </select>
      <input
        class="field"
        style="flex:1; min-width:0"
        placeholder="Filter these results…"
        bind:value={textFilter}
      />
      <span class="fs-xs tx-1 sh-0">{view.length} / {rows.length}</span>
    </div>
  {/if}

  <main>
    {#if loading}
      <EmptyState>Searching The Pirate Bay…</EmptyState>
    {:else if searched && rows.length === 0}
      <EmptyState>No results for “{q.trim()}”.</EmptyState>
    {:else if !searched}
      <EmptyState>
        Type a title and hit Search. Names are auto-parsed; click any column to
        sort. One click adds the magnet straight to downloads.
      </EmptyState>
    {:else}
      <table class="res">
        <thead>
          <tr>
            <th class="ptr" onclick={() => sortBy("title")}>Title{arrow("title")}</th>
            <th class="ptr num" onclick={() => sortBy("res")}>Quality{arrow("res")}</th>
            <th class="ptr num" onclick={() => sortBy("size")}>Size{arrow("size")}</th>
            <th class="ptr num" onclick={() => sortBy("seeders")}>Seed{arrow("seeders")}</th>
            <th class="ptr num" onclick={() => sortBy("leechers")}>Leech{arrow("leechers")}</th>
            <th class="ptr num" onclick={() => sortBy("files")}>Files{arrow("files")}</th>
            <th class="ptr num" onclick={() => sortBy("added")}>Age{arrow("added")}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each view as { t, p } (t.infoHash)}
            <tr>
              <td title={t.name}>
                <span class="nm">{p.title || t.name}</span>
                <span class="meta fs-xs tx-1">
                  {#if epTag(p)}<Badge variant="tag">{epTag(p)}</Badge>{/if}
                  {#each p.langs as l}<Badge>{l}</Badge>{/each}
                  {#if p.codec}<Badge>{p.codec}</Badge>{/if}
                  {#if p.hdr}<Badge variant="hdr">{p.hdr}</Badge>{/if}
                  {#if p.group}<Badge variant="dim">·{p.group}</Badge>{/if}
                </span>
              </td>
              <td class="num">
                {#if p.res}<Badge variant="quality">{p.res}</Badge>{:else}<span
                    class="tx-1">—</span
                  >{/if}
                {#if p.source}<span class="src fs-xs tx-1">{p.source}</span>{/if}
              </td>
              <td class="num">{fmtBytes(t.size)}</td>
              <td class="num seed">{t.seeders}</td>
              <td class="num tx-1">{t.leechers}</td>
              <td class="num tx-1">{t.files || "—"}</td>
              <td class="num tx-1">{ago(t.added)}</td>
              <td class="num">
                <button
                  class="add ptr fs-xs"
                  class:done={added.has(t.infoHash)}
                  disabled={added.has(t.infoHash)}
                  onclick={() => add(t)}
                >
                  {added.has(t.infoHash) ? "Added ✓" : "+ Add"}
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </main>
</div>

<style>
  header {
    padding: 20px 40px;
  }
  header h1 {
    font-size: 22px;
  }
  .search-wrap {
    flex: 1;
    min-width: 0;
    max-width: 520px;
  }
  select {
    cursor: pointer;
    white-space: nowrap;
  }
  .toolbar {
    padding: 12px 40px;
    border-bottom: 1px solid var(--glass-bd);
  }
  table.res {
    width: 100%;
    border-collapse: collapse;
    font-size: 14px;
  }
  .res th,
  .res td {
    padding: 9px 14px;
    text-align: left;
    border-bottom: 1px solid var(--glass-bd);
    vertical-align: middle;
  }
  .res th {
    position: sticky;
    top: 0;
    background: var(--bg, #0d0b12);
    color: var(--tx-1);
    font-weight: 500;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    user-select: none;
  }
  .res th.ptr:hover {
    color: var(--tx-4);
  }
  .res td.num,
  .res th.num {
    text-align: right;
    white-space: nowrap;
  }
  .res tbody tr:hover {
    background: rgba(255, 255, 255, 0.03);
  }
  .nm {
    display: block;
    font-weight: 500;
    word-break: break-word;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    align-items: center;
    margin-top: 3px;
  }
  .src {
    display: block;
    margin-top: 2px;
  }
  .seed {
    color: #4ade80;
    font-weight: 600;
  }
  .add {
    padding: 5px 12px;
    border-radius: var(--r-md);
    background: var(--red);
    color: #fff;
    font-family: inherit;
    border: none;
    white-space: nowrap;
  }
  .add.done {
    background: var(--glass);
    color: var(--tx-3);
    cursor: default;
  }
</style>
