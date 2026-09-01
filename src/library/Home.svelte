<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../core/api";
  import { clean } from "../core/video";
  import { ago, bytes } from "../core/fmt";
  import { join } from "../core/path";
  import { local } from "../core/store";
  import { toast } from "../core/toast.svelte";
  import { HomeData } from "./homeData.svelte";
  import { parseName, epTag, type Parsed } from "./torrentName";
  import Continues from "./Continues.svelte";
  import ShowRow from "./ShowRow.svelte";
  import MediaCard from "../components/MediaCard.svelte";
  import ScrollRow from "../components/ScrollRow.svelte";
  import ProgressBar from "../components/ProgressBar.svelte";
  import EmptyState from "../components/EmptyState.svelte";
  import Icon from "../components/Icon.svelte";
  import Badge from "../components/Badge.svelte";

  type Row = { t: Torrent; p: Parsed };

  const home = new HomeData();
  let search = $state("");
  let swUpdate = $state<(() => void) | null>(null);

  const searches = local<string[]>("searches", []);
  const saved = searches.get();
  let history = $state<string[]>(
    Array.isArray(saved) ? saved.filter((s) => typeof s === "string").slice(0, 5) : [],
  );
  let searchFocus = $state(false);

  function remember(term: string) {
    history = [term, ...history.filter((s) => s !== term)].slice(0, 5);
    searches.set($state.snapshot(history));
  }

  const recent = $derived.by(() => {
    const q = search.trim().toLowerCase();
    return history.filter((h) => !q || (h.toLowerCase().includes(q) && h !== search.trim()));
  });

  let torrentMode = $state(false);
  let torrentLoading = $state(false);
  let torrentQuery = $state("");
  let torrentResults = $state<Torrent[]>([]);
  let dlDir = $state("");
  let added = $state(new Set<string>());
  let resFilter = $state("all");
  let sortKey = $state<
    "seeders" | "size" | "leechers" | "files" | "added" | "title" | "res"
  >("seeders");
  let sortDir = $state<1 | -1>(-1);

  onMount(() => {
    const unsubSW = home.start();

    const onSwUpdate = (e: Event) => {
      swUpdate = (e as CustomEvent).detail;
    };
    window.addEventListener("sw-update", onSwUpdate);

    return () => {
      unsubSW();
      window.removeEventListener("sw-update", onSwUpdate);
    };
  });

  $effect(() => {
    if (!dlDir && home.disks.length > 0) dlDir = home.disks[0].path;
  });

  let debouncedSearch = $state("");
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  $effect(() => {
    const q = search;
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      debouncedSearch = q;
    }, 150);
  });

  let localResults = $derived(torrentMode ? null : home.filter(debouncedSearch));

  async function runTorrent() {
    const term = search.trim();
    if (!term) return;
    remember(term);
    torrentLoading = true;
    torrentMode = true;
    torrentQuery = term;
    const r = await api.search(term);
    torrentResults = Array.isArray(r) ? r : [];
    torrentLoading = false;
  }

  async function addTorrent(t: Torrent) {
    if (!dlDir) {
      toast.err("No download disk available");
      return;
    }
    if (added.has(t.infoHash)) return;
    const res = await api.aria2.add(t.magnet, dlDir);
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
  const arrow = (key: typeof sortKey) =>
    sortKey !== key ? "" : sortDir === 1 ? " ▲" : " ▼";

  let torrentRows = $derived<Row[]>(
    torrentResults.map((t) => ({ t, p: parseName(t.name) })),
  );
  let torrentView = $derived(
    torrentRows
      .filter(({ p }) => {
        if (resFilter === "sd") return p.resRank > 0 && p.resRank <= 480;
        if (resFilter !== "all") return p.res === resFilter;
        return true;
      })
      .filter(({ t, p }) => {
        const f = search.trim().toLowerCase();
        return (
          !f ||
          p.title.toLowerCase().includes(f) ||
          t.name.toLowerCase().includes(f)
        );
      })
      .sort((a, b) => {
        let d: number;
        if (sortKey === "title") d = a.p.title.localeCompare(b.p.title);
        else if (sortKey === "res") d = a.p.resRank - b.p.resRank;
        else d = (a.t[sortKey] as number) - (b.t[sortKey] as number);
        return d * sortDir;
      }),
  );

  function clearSearch() {
    search = "";
    torrentMode = false;
    torrentResults = [];
    torrentQuery = "";
  }

  function onSearchKey(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      runTorrent();
    } else if (e.key === "Escape" && torrentMode) {
      e.preventDefault();
      clearSearch();
    }
  }

  function removeContinue(e: Event, dir: string, name: string) {
    e.preventDefault();
    e.stopPropagation();
    home.removeContinue(dir, name);
  }

  async function cancelDownload(videoParam: string) {
    const err = await home.cancelDownload(videoParam);
    if (err) toast.err(`Cancel failed: ${err}`);
  }

  function applyUpdate() {
    swUpdate?.();
  }
</script>

<div style="min-height: 100vh; min-height: 100dvh;">
  <header class="glass-header f al-ct g20 p-rel">
    <a href="/" class="brand sh-0">
      <img src="/assets/tight.svg" alt="Notflix" height="42" />
    </a>

    <div class="search-wrap p-rel">
      <span class="search-ic p-abs dead cc"><Icon name="search" size={15} /></span>
      <input
        class="field w-100"
        type="search"
        placeholder={torrentMode
          ? "Filter results — Enter to re-search"
          : "Search library — Enter to search torrents"}
        bind:value={search}
        onkeydown={onSearchKey}
        onfocus={() => (searchFocus = true)}
        onblur={() => (searchFocus = false)}
        autocomplete="off"
        spellcheck="false"
      />
      {#if searchFocus && recent.length}
        <div class="hist p-abs w-100 flow-h">
          {#each recent as h (h)}
            <button
              class="hist-item w-100 ptr f al-ct g10"
              onmousedown={(e) => {
                e.preventDefault();
                search = h;
                runTorrent();
              }}
            >
              <span class="hist-ic">↺</span>
              <span class="trunc">{h}</span>
            </button>
          {/each}
        </div>
      {/if}
    </div>

    {#if torrentMode && home.disks.length > 0}
      <select class="field sh-0" bind:value={dlDir} title="Download target disk">
        {#each home.disks as d}
          <option value={d.path}>{d.root} ({bytes(d.free)} free)</option>
        {/each}
      </select>
    {/if}

    <nav class="tools f al-ct g10">
      {#if swUpdate}
        <button class="update-btn btn-action ptr fs-xs" onclick={applyUpdate}>
          Update ready — reload
        </button>
      {/if}
      <a
        href="/manage"
        class="icon-btn glass glass-hover"
        aria-label="Settings"
        title="Settings"
      >
        <Icon name="settings" />
      </a>
    </nav>
  </header>

  <main>
    {#if torrentMode}
      <div class="search-header f al-ct g20">
        <span>
          Torrents for "<strong>{torrentQuery}</strong>"
          {#if !torrentLoading && torrentResults.length > 0}
            · <span class="tx-1">{torrentView.length} / {torrentRows.length}</span>
          {/if}
        </span>

        <select class="field" bind:value={resFilter}>
          <option value="all">All resolutions</option>
          <option value="2160p">2160p / 4K</option>
          <option value="1080p">1080p</option>
          <option value="720p">720p</option>
          <option value="sd">≤ 480p</option>
        </select>

        <button class="clear fs-xs" onclick={clearSearch}>✕ Back</button>
      </div>

      {#if torrentLoading}
        <div class="pad">
          <div class="scan f-col g10">
            {#each Array(6) as _, i}
              <div class="scan-line" style="--i:{i}"></div>
            {/each}
          </div>
          <EmptyState>Searching trackers…</EmptyState>
        </div>
      {:else if torrentResults.length === 0}
        <EmptyState icon="🛰" variant="hero">
          Nothing came back for "{torrentQuery}". Try a shorter title.
        </EmptyState>
      {:else}
        <div class="pad">
          <table class="res">
            <thead>
              <tr>
                <th class="ptr" onclick={() => sortBy("title")}
                  >Title{arrow("title")}</th
                >
                <th class="ptr num" onclick={() => sortBy("res")}
                  >Quality{arrow("res")}</th
                >
                <th class="ptr num" onclick={() => sortBy("size")}
                  >Size{arrow("size")}</th
                >
                <th class="ptr num" onclick={() => sortBy("seeders")}
                  >Seed{arrow("seeders")}</th
                >
                <th class="ptr num" onclick={() => sortBy("leechers")}
                  >Leech{arrow("leechers")}</th
                >
                <th class="ptr num" onclick={() => sortBy("files")}
                  >Files{arrow("files")}</th
                >
                <th class="ptr num" onclick={() => sortBy("added")}
                  >Age{arrow("added")}</th
                >
                <th></th>
              </tr>
            </thead>
            <tbody>
              {#each torrentView as { t, p } (t.infoHash)}
                <tr>
                  <td title={t.name}>
                    <span class="nm">{p.title || t.name}</span>
                    <span class="meta fs-xs tx-1">
                      {#if epTag(p)}<Badge variant="tag">{epTag(p)}</Badge>{/if}
                      {#each p.langs as l}<Badge>{l}</Badge>{/each}
                      {#if p.codec}<Badge>{p.codec}</Badge>{/if}
                      {#if p.hdr}<Badge variant="hdr">{p.hdr}</Badge>{/if}
                      {#if p.group}<Badge variant="dim">·{p.group}</Badge>{/if}
                      {#if t.source}<Badge variant="dim">{t.source}</Badge>{/if}
                    </span>
                  </td>
                  <td class="num">
                    {#if p.res}<Badge variant="quality">{p.res}</Badge
                      >{:else}<span class="tx-1">—</span>{/if}
                    {#if p.source}<span class="src fs-xs tx-1">{p.source}</span
                      >{/if}
                  </td>
                  <td class="num">{bytes(t.size)}</td>
                  <td class="num seed">{t.seeders}</td>
                  <td class="num tx-1">{t.leechers}</td>
                  <td class="num tx-1">{t.files || "—"}</td>
                  <td class="num tx-1">{t.added ? ago(t.added * 1000) : "—"}</td>
                  <td class="num">
                    <button
                      class="add ptr fs-xs"
                      class:done={added.has(t.infoHash)}
                      disabled={added.has(t.infoHash)}
                      onclick={() => addTorrent(t)}
                    >
                      {added.has(t.infoHash) ? "Added ✓" : "+ Add"}
                    </button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {:else if home.loading}
      <div class="skel-grid" aria-hidden="true">
        {#each Array(14) as _, i}
          <div class="skel" style="--i:{i}">
            <div class="skel-art"></div>
            <div class="skel-line"></div>
            <div class="skel-line short"></div>
          </div>
        {/each}
      </div>
    {:else if localResults !== null}
      <div class="search-header f al-ct g20">
        <span>
          Results for "<strong>{search}</strong>"
          <span class="tx-1">· {localResults.length}</span>
        </span>

        <button class="clear fs-xs" onclick={clearSearch}>✕ Clear</button>
      </div>

      <div class="grid spotlight">
        {#each localResults as item, idx (join(item.dir, item.name))}
          <MediaCard
            dir={item.dir}
            name={item.name}
            key={item.key}
            {idx}
            downloaded={home.downloadedSet.has(join(item.dir, item.name))}
          >
            {#snippet footer()}
              {#if item.dir !== "."}
                <div class="card-dir fs-xs trunc">
                  {item.dir}
                </div>
              {/if}
            {/snippet}
          </MediaCard>
        {/each}

        {#if localResults.length === 0}
          <EmptyState variant="hero" gridSpan icon="🔍">
            Nothing in the library matches. Press Enter to search torrents.
          </EmptyState>
        {/if}
      </div>
    {:else}
      {#if home.offline}
        <div class="offline-banner f al-ct g10">
          <span class="offline-dot"></span>
          Offline — showing downloaded videos
        </div>
      {:else if home.rows.length === 0 && home.continues.length === 0}
        <EmptyState variant="hero" icon="🎞">
          {#if home.disks.length === 0}
            No roots reachable on the server. Mount a drive (or check ~/Videos exists).
          {:else}
            No videos yet. Reachable roots: {home.disks
              .map((d) => d.root)
              .join(", ")}.
          {/if}
        </EmptyState>
      {/if}
      <Continues
        items={home.continues}
        downloadedSet={home.downloadedSet}
        onRemove={removeContinue}
      />
      {#each home.rows as row (row.kind === "dir" ? row.dir : row.key)}
        {#if row.kind === "show"}
          <ShowRow
            title={row.title}
            key={row.key}
            seasons={row.seasons}
            continues={home.continues}
            downloadedSet={home.downloadedSet}
          />
        {:else}
          {@const dir = row.dir}
          {@const files = row.files}
          <section class="row">
            <div class="head f al-ct g10">
              <h2 class="m0">
                {dir === "." ? "Movies" : clean(dir) || dir}
              </h2>
              <span class="count fs-xs tx-1">{files.length}</span>
            </div>

            <ScrollRow>
              <div
                class="cards spotlight flow-x-s g10 {dir === '.' ? 'multi' : 'f'}"
              >
                {#each files as f, idx (f.name)}
                  <MediaCard
                    {dir}
                    name={f.name}
                    key={f.key}
                    {idx}
                    downloaded={home.downloadedSet.has(join(dir, f.name))}
                  />
                {/each}
              </div>
            </ScrollRow>
          </section>
        {/if}
      {/each}
    {/if}
  </main>

  {#if home.conversions.length > 0 || home.inProg.length > 0}
    <div class="panel glass-strong p-fix flow-h">
      {#if home.conversions.length > 0}
        <div class="phead f al-ct j-bw">
          <span class="ptitle fw6">Converting</span>
          <span class="count-pill fs-xs fw7 cc">{home.conversions.length}</span>
        </div>
        {#each home.conversions as job (job.name)}
          <div class="item conv f-col g5">
            <div class="name fs-xs trunc">{clean(job.name)}</div>
            <ProgressBar value={job.percent} height="3px" />
          </div>
        {/each}
      {/if}

      {#if home.inProg.length > 0}
        <div class="phead f al-ct j-bw">
          <span class="ptitle fw6">Downloading</span>
          <span class="count-pill fs-xs fw7 cc">{home.inProg.length}</span>
        </div>
        {#each home.inProg as dl (dl.videoParam)}
          <div class="item f al-ct g10">
            <div class="name fs-xs trunc" style="flex:1">
              {clean(dl.title || dl.videoParam)}
            </div>
            <button
              class="cancel-btn fs-xs cc ptr"
              onclick={() => cancelDownload(dl.videoParam)}
              title="Cancel download">✕</button
            >
          </div>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  header {
    padding: 12px 48px;
    padding-top: calc(12px + var(--safe-t));
    z-index: 40;
  }
  .brand img {
    display: block;
    transition: opacity var(--motion, 0.3s) var(--ease-out);
  }

  header:has(.search-wrap input:focus) .brand img,
  header:has(.search-wrap input:focus) .tools {
    opacity: 0.35;
  }
  header:has(.search-wrap input:focus) .search-wrap {
    max-width: 620px;
  }

  .search-wrap {
    flex: 1;
    max-width: 400px;
    transition: max-width var(--motion, 0.32s) var(--ease-out);
  }
  .search-wrap input {
    padding-left: 38px;
  }
  .search-ic {
    left: 13px;
    top: 0;
    bottom: 0;
    color: var(--tx-1);
  }

  .hist {
    top: calc(100% + 8px);
    left: 0;
    z-index: 30;
    background: color-mix(in oklab, var(--bg-2) 92%, transparent);
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-md);
    box-shadow: var(--sh-3);
    backdrop-filter: blur(16px) saturate(140%);
    -webkit-backdrop-filter: blur(16px) saturate(140%);
    transition:
      opacity var(--motion, 0.2s) var(--ease-out),
      translate var(--motion, 0.2s) var(--ease-out);
  }
  @starting-style {
    .hist {
      opacity: 0;
      translate: 0 -8px;
    }
  }
  .hist-item {
    padding: 9px 14px;
    color: var(--tx-3);
    font-size: 13px;
    text-align: left;
    transition: background var(--motion-fast, 0.15s) var(--ease-out);
  }
  .hist-item:hover {
    background: var(--glass);
    color: var(--tx-5);
  }
  .hist-ic {
    color: var(--tx-1);
    flex-shrink: 0;
  }

  .tools {
    margin-left: auto;
    transition: opacity var(--motion, 0.3s) var(--ease-out);
  }
  .icon-btn {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    color: var(--tx-3);
    border-radius: var(--r-md);
    transition: color var(--motion-fast, 0.18s) var(--ease-out);
  }
  .icon-btn:hover {
    color: var(--tx-5);
  }

  .update-btn {
    padding: 7px 14px;
  }

  main {
    padding: 28px 0 80px;
    position: relative;
    z-index: 1;
  }
  .pad {
    padding: 0 48px;
  }

  .offline-banner {
    margin: 0 48px 20px;
    padding: 10px 16px;
    color: var(--tx-3);
    font-size: 13px;
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-md);
    backdrop-filter: blur(8px);
  }
  .offline-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--red);
    flex-shrink: 0;
    box-shadow: 0 0 8px var(--red-glow);
    animation: breathe 2s ease-in-out infinite;
  }

  .skel-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: 20px 16px;
    padding: 8px 48px;
  }
  .skel {
    opacity: 0;
    animation: fade-in 0.4s var(--ease-out) forwards;
    animation-delay: calc(var(--i) * 55ms);
  }
  .skel-art {
    aspect-ratio: 16 / 9;
    border-radius: var(--r-lg);
    margin-bottom: 10px;
  }
  .skel-line {
    height: 9px;
    border-radius: 999px;
    margin-bottom: 6px;
  }
  .skel-line.short {
    width: 45%;
  }
  .skel-art,
  .skel-line {
    background: linear-gradient(
      90deg,
      var(--bg-3) 0%,
      var(--bg-4) 45%,
      var(--bg-3) 90%
    );
    background-size: 380px 100%;
    animation: shimmer 1.7s linear infinite;
  }

  .scan {
    padding: 20px 0 0;
  }
  .scan-line {
    height: 44px;
    border-radius: var(--r-md);
    background: linear-gradient(
      90deg,
      var(--bg-3) 0%,
      var(--bg-4) 45%,
      var(--bg-3) 90%
    );
    background-size: 420px 100%;
    animation: shimmer 1.7s linear infinite;
    animation-delay: calc(var(--i) * -180ms);
    opacity: calc(1 - var(--i) * 0.13);
  }

  .search-header {
    padding: 8px 48px 20px;
    color: var(--tx-3);
    font-size: 15px;
  }
  .search-header strong {
    color: var(--tx-5);
    font-family: var(--font-display);
    font-weight: 600;
  }
  .clear {
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    color: var(--tx-3);
    padding: 5px 12px;
    border-radius: 999px;
    margin-left: auto;
    transition:
      color var(--motion-fast, 0.18s) var(--ease-out),
      background var(--motion-fast, 0.18s) var(--ease-out);
  }
  .clear:hover {
    color: var(--tx-5);
    background: var(--glass-2);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: 20px 16px;
    padding: 0 48px;
  }
  .card-dir {
    color: var(--tx-1);
    padding-top: 1px;
  }

  .row {
    margin-bottom: 40px;
  }
  .head {
    padding: 0 48px;
    margin-bottom: 12px;
  }
  .head h2 {
    font-size: 22px;
  }
  .count {
    font-variant-numeric: tabular-nums;
  }
  .head::after {
    content: "";
    flex: 1;
    height: 1px;
    background: linear-gradient(to right, var(--glass-bd), transparent);
  }

  .cards {
    padding: 4px 48px 14px;
    scrollbar-width: none;
  }
  .cards :global(.tile) {
    width: 220px;
  }
  /* Movies (dir === ".") is flat, so pack it column-major: ~3× density. */
  .cards.multi {
    display: grid;
    grid-template-rows: repeat(3, auto);
    grid-auto-flow: column;
    grid-auto-columns: 186px;
    overflow-x: auto;
  }
  .cards.multi :global(.tile) {
    width: 186px;
  }

  table.res {
    width: 100%;
    border-collapse: collapse;
    font-size: 14px;
  }
  .res th,
  .res td {
    padding: 10px 14px;
    text-align: left;
    border-bottom: 1px solid rgb(255 255 255 / 0.05);
    vertical-align: middle;
  }
  .res th {
    position: sticky;
    top: 0;
    z-index: 2;
    background: color-mix(in oklab, var(--bg-2) 92%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    color: var(--tx-1);
    font-weight: 500;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    user-select: none;
  }
  .res th.ptr:hover {
    color: var(--tx-4);
  }
  .res td.num,
  .res th.num {
    text-align: right;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .res tbody tr {
    transition: background var(--motion-fast, 0.15s) var(--ease-out);
  }
  .res tbody tr:hover {
    background: rgb(255 255 255 / 0.035);
  }
  .res tbody tr:has(.add.done) {
    opacity: 0.45;
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
    margin-top: 4px;
  }
  .src {
    display: block;
    margin-top: 2px;
  }
  .seed {
    color: var(--grn);
    font-weight: 600;
  }
  .add {
    padding: 6px 13px;
    border-radius: 999px;
    background: var(--red);
    color: #fff;
    font-family: inherit;
    white-space: nowrap;
    transition:
      filter var(--motion-fast, 0.18s) var(--ease-out),
      scale var(--motion-fast, 0.16s) var(--ease-snap);
  }
  .add:hover {
    filter: brightness(1.1);
  }
  .add:active {
    scale: 0.95;
  }
  .add.done {
    background: var(--glass);
    color: var(--tx-3);
    cursor: default;
  }

  .panel {
    bottom: calc(24px + var(--safe-b));
    right: 24px;
    width: 320px;
    max-height: 60vh;
    overflow-y: auto;
    border-radius: var(--r-xl);
    z-index: 200;
    box-shadow: var(--sh-4);
    transition:
      opacity var(--motion, 0.36s) var(--ease-out),
      translate var(--motion, 0.36s) var(--ease-spring);
  }
  @starting-style {
    .panel {
      opacity: 0;
      translate: 0 18px;
    }
  }
  .phead {
    padding: 12px 16px;
    background: rgb(255 255 255 / 0.02);
    border-bottom: 1px solid var(--glass-bd);
  }
  .ptitle {
    color: var(--tx-4);
    text-transform: uppercase;
    letter-spacing: 0.1em;
    font-size: 10px;
  }
  .count-pill {
    background: var(--red);
    color: #fff;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    font-variant-numeric: tabular-nums;
    box-shadow: 0 4px 12px -2px var(--red-glow);
  }
  .item {
    padding: 11px 16px;
    border-bottom: 1px solid rgb(255 255 255 / 0.04);
  }
  .item:last-child {
    border-bottom: none;
  }
  .name {
    color: var(--tx-4);
    margin-bottom: 6px;
  }
  .item.conv .name {
    margin-bottom: 0;
  }
  .cancel-btn {
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    color: var(--tx-3);
    width: 24px;
    height: 24px;
    flex-shrink: 0;
    font-size: 11px;
    border-radius: 50%;
    transition:
      background var(--motion-fast, 0.18s) var(--ease-out),
      color var(--motion-fast, 0.18s) var(--ease-out);
  }
  .cancel-btn:hover {
    background: var(--red);
    color: #fff;
    border-color: var(--red);
  }

  /* Chrome-only decoration; the header is complete without it. */
  @supports (animation-timeline: scroll()) {
    @media (prefers-reduced-motion: no-preference) {
      header::after {
        content: "";
        position: absolute;
        inset: 0;
        pointer-events: none;
        background: linear-gradient(
          to bottom,
          var(--bg-1) 0%,
          color-mix(in oklab, var(--bg-1) 70%, transparent) 100%
        );
        opacity: 0;
        z-index: -1;
        animation: settle linear both;
        animation-timeline: scroll(root block);
        animation-range: 0 140px;
      }
    }
  }
  @keyframes settle {
    to {
      opacity: 1;
    }
  }

  @media (max-width: 640px) {
    header {
      padding: 8px 16px;
      padding-top: calc(8px + var(--safe-t));
      gap: 10px;
    }
    header img {
      height: 30px;
    }
    .search-wrap {
      max-width: none;
    }
    .grid,
    .skel-grid {
      padding: 0 16px;
      gap: 14px 10px;
      grid-template-columns: repeat(auto-fill, minmax(132px, 1fr));
    }
    .head {
      padding: 0 16px;
    }
    .head h2 {
      font-size: 17px;
    }
    .cards {
      padding: 4px 16px 12px;
    }
    .cards :global(.tile) {
      width: 148px;
    }
    .cards.multi {
      grid-template-rows: repeat(2, auto);
      grid-auto-columns: 132px;
    }
    .cards.multi :global(.tile) {
      width: 132px;
    }
    .search-header,
    .pad {
      padding-left: 16px;
      padding-right: 16px;
    }
    .offline-banner {
      margin: 0 16px 14px;
    }
    main {
      padding: 12px 0 60px;
    }
    .icon-btn {
      width: 34px;
      height: 34px;
    }
    .update-btn {
      font-size: 11px;
      padding: 5px 10px;
    }
    .panel {
      right: 12px;
      left: 12px;
      width: auto;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .skel-art,
    .skel-line,
    .scan-line,
    .offline-dot {
      animation: none;
    }
    .hist,
    .panel,
    .search-wrap {
      transition-duration: 1ms;
    }
  }
</style>
