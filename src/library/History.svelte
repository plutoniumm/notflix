<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../core/api";
  import { ago, bytes, pct, durWords } from "../core/fmt";
  import { local } from "../core/store";
  import BackLink from "../components/BackLink.svelte";
  import {
    build,
    sortNodes,
    agg,
    progress,
    seen,
    day,
    playURL,
    type HRow,
    type Node,
    type SortKey,
  } from "./history";

  // ONE table, not two. A download and a watch are two columns on the same
  // file, and the interesting states — downloaded but never watched, watched
  // but since deleted — only exist as the contrast between them. Two tables
  // would print every title twice and hide exactly those rows.
  type Stats = {
    rows: number;
    torrents: number;
    torrentsBad: number;
    diskFiles: number;
    watchLines: number;
    watchVideos: number;
    missing: number;
    scanMs: number;
  };

  type Filter = "all" | "dl" | "watched" | "unwatched" | "gone";

  const MATCH: Record<Filter, (r: HRow) => boolean> = {
    all: () => true,
    dl: (r) => !!r.dlAt,
    watched: (r) => (r.sess ?? 0) > 0,
    unwatched: (r) => !(r.sess ?? 0),
    gone: (r) => !r.disk,
  };

  const CHIPS: [Filter, string][] = [
    ["all", "Everything"],
    ["dl", "Downloaded"],
    ["watched", "Watched"],
    ["unwatched", "Never watched"],
    ["gone", "Off disk"],
  ];

  const COLS: [SortKey, string][] = [
    ["title", "Title"],
    ["dl", "Downloaded"],
    ["watch", "Last watched"],
    ["size", "Size"],
  ];

  const sortPref = local<{ k: SortKey; desc: boolean }>("histSort", {
    k: "watch",
    desc: true,
  });
  const filterPref = local<Filter>("histFilter", "all");

  let rows = $state<HRow[]>([]);
  let stats = $state<Stats | null>(null);
  let loading = $state(true);
  let q = $state("");
  let sort = $state(sortPref.get());
  let filter = $state(filterPref.get());
  // Reassigned rather than mutated: $state does not proxy a Set.
  let open = $state(new Set<string>());

  const nodes = $derived.by(() => {
    const needle = q.trim().toLowerCase();
    const keep = rows.filter(
      (r) =>
        MATCH[filter](r) &&
        (!needle ||
          r.name.toLowerCase().includes(needle) ||
          (r.tor ?? "").toLowerCase().includes(needle)),
    );

    return sortNodes(build(keep), sort.k, sort.desc);
  });

  async function load() {
    loading = true;
    const r = await api.history();
    if (r) {
      rows = r.rows ?? [];
      stats = r.stats ?? null;
    }
    loading = false;
  }

  onMount(load);

  function toggle(id: string) {
    const next = new Set(open);
    if (!next.delete(id)) next.add(id);
    open = next;
  }

  function sortBy(k: SortKey) {
    sort = sort.k === k ? { k, desc: !sort.desc } : { k, desc: k !== "title" };
    sortPref.set(sort);
  }

  function setFilter(f: Filter) {
    filter = f;
    filterPref.set(f);
  }

  const marks = (r: HRow) => [...(r.marks ?? []), ...(r.hls ? ["hls"] : [])];
</script>

<div class="page">
  <header class="glass-header f fw al-ct g10">
    <BackLink />
    <h1 class="m0 fw6">History</h1>
    {#if stats}
      <span class="tally fs-sm tx-1 num">
        <b>{stats.rows}</b> files · <b>{stats.diskFiles}</b> on disk ·
        <b>{stats.missing}</b> off disk · <b>{stats.torrents}</b> torrents ·
        <b>{stats.watchVideos}</b> watched · {stats.watchLines} pings
      </span>
    {/if}
    <button class="act glass glass-hover ptr fs-xs" onclick={load} disabled={loading}>
      {loading ? "Loading…" : "Refresh"}
    </button>
  </header>

  <main>
    <div class="bar f fw al-ct g10">
      <input class="field" placeholder="Filter by title or torrent…" bind:value={q} />
      <div class="chips f fw al-ct g5">
        {#each CHIPS as [f, label]}
          <button class="chip ptr fs-xs" class:on={filter === f} onclick={() => setFilter(f)}>
            {label}
          </button>
        {/each}
      </div>
    </div>

    {#snippet branch(n: Node, depth: number)}
      {@const a = agg(n.rows)}
      {@const r = n.leaf}
      {@const shown = open.has(n.id)}
      <button
        type="button"
        class="row num"
        class:group={!r}
        class:leaf={!!r}
        class:shown
        style="--d:{depth}"
        onclick={() => toggle(n.id)}
      >
        <span class="cell name">
          <span class="caret tx-3">{shown ? "▾" : "▸"}</span>
          <span class="lbl">
            <span class="trunc">{n.label}</span>
            {#if n.sub}<span class="sub fs-xs tx-3 trunc">{n.sub}</span>{/if}
            {#if !r}<span class="sub fs-xs tx-3">{a.n} files · {a.disk} on disk</span>{/if}
          </span>
        </span>

        <span class="cell fs-xs" title={day(r ? r.dlAt : a.dlAt)}>
          {(r ? r.dlAt : a.dlAt) ? ago(r ? r.dlAt! : a.dlAt) : "—"}
        </span>

        <span class="cell fs-xs" title={day(r ? r.lastAt : a.lastAt)}>
          {#if (r ? r.lastAt : a.lastAt)}
            {ago(r ? r.lastAt! : a.lastAt)}
            <span class="tx-3">· {r ? (r.sess ?? 0) : a.sess}×</span>
          {:else}
            —
          {/if}
        </span>

        <span
          class="cell fs-xs"
          title={r && r.dur ? pct(progress(r)) : a.n ? `${a.watched} of ${a.n} past halfway` : ""}
        >
          {#if r}
            {#if !r.dur || !(r.sess ?? 0)}
              —
            {:else if seen(r)}
              <span class="seen">✓</span>
            {:else}
              <span class="part">·</span>
            {/if}
          {:else if a.watched}
            <span class="seen num">{a.watched}</span><span class="tx-3 num"
              >/{a.n}</span
            >
          {:else}
            —
          {/if}
        </span>

        <span class="cell fs-xs">{a.size ? bytes(a.size) : "—"}</span>

        <span class="cell fs-xs tags">
          {#if r}
            {#if !r.disk}<span class="tag gone">off disk</span>{/if}
            {#each marks(r) as m}<span class="tag">{m}</span>{/each}
          {:else if a.disk < a.n}
            <span class="tag gone">{a.n - a.disk} off disk</span>
          {/if}
        </span>

        <span class="cell fs-xs tx-3 trunc">{(r ? r.dev : "") || "—"}</span>
      </button>

      {#if shown}
        {#if r}
          <div class="detail fs-xs" style="--d:{depth}">
            <dl class="kv m0">
              <dt>File</dt>
              <dd class="trunc">{r.rel ?? r.name}</dd>
              <dt>Torrent</dt>
              <dd class="trunc">{r.tor ?? "—"}{r.ih ? ` · ${r.ih.slice(0, 12)}` : ""}</dd>
              <dt>Downloaded</dt>
              <dd>{day(r.dlAt)}</dd>
              <dt>Watched</dt>
              <dd>
                {r.firstAt ? `${day(r.firstAt)} → ${day(r.lastAt)}` : "—"}
                {#if r.sess}· {r.sess} sessions{/if}
                {#if r.dur}· {durWords(r.t ?? 0)} of {durWords(r.dur)}{/if}
              </dd>
              <dt>Processed</dt>
              <dd>
                {#if r.steps && Object.keys(r.steps).length}
                  {Object.entries(r.steps)
                    .map(([k, v]) => `${k}=${v}`)
                    .join(" · ")}
                  {#if r.procAt}· {day(r.procAt)}{/if}
                {:else}
                  —
                {/if}
              </dd>
            </dl>
            {#if r.disk && r.rel}
              <a class="act glass glass-hover fs-xs" href={playURL(r.rel)}>Play</a>
            {/if}
          </div>
        {:else}
          {#each n.kids as k (k.id)}
            {@render branch(k, depth + 1)}
          {/each}
        {/if}
      {/if}
    {/snippet}

    <div class="wrap flow-x-s">
      <div class="tbl">
        <div class="row head num">
          {#each COLS as [k, label], i}
            <span class="cell" class:name={i === 0}>
              <button class="sorter ptr fs-xs up" onclick={() => sortBy(k)}>
                {label}{sort.k === k ? (sort.desc ? " ↓" : " ↑") : ""}
              </button>
            </span>
            {#if i === 2}<span class="cell fs-xs up tx-3">Watched</span>{/if}
          {/each}
          <span class="cell fs-xs up tx-3">State</span>
          <span class="cell fs-xs up tx-3">Device</span>
        </div>

        {#each nodes as n (n.id)}
          {@render branch(n, 0)}
        {/each}
      </div>
    </div>

    {#if !loading && !nodes.length}
      <p class="empty tx-3">Nothing matches.</p>
    {/if}
    {#if stats}
      <p class="foot fs-xs tx-3 num">
        {stats.torrentsBad} unreadable torrents · joined in {stats.scanMs} ms
      </p>
    {/if}
  </main>
</div>

<style>
  /* Binary, not a bar: scanning 90/92/94% told you nothing. The exact figure
     is on the cell's title for when it is actually wanted. */
  .seen {
    color: var(--grn);
    font-weight: 700;
  }
  .part {
    color: var(--tx-2);
    font-weight: 700;
  }

  .page {
    min-height: 100vh;
    min-height: 100dvh;
  }
  header {
    padding: 16px 40px;
    padding-top: calc(16px + var(--safe-t));
    z-index: 40;
  }
  header h1 {
    font-size: 20px;
  }
  .tally b {
    color: var(--tx-4);
    font-weight: 600;
  }
  .act {
    margin-left: auto;
    padding: 7px 14px;
    color: var(--tx-4);
    font-family: inherit;
    border-radius: var(--r-md);
    text-decoration: none;
  }

  main {
    padding: 16px 40px calc(40px + var(--safe-b));
  }
  .bar {
    margin-bottom: 12px;
  }
  .chip {
    padding: 6px 11px;
    color: var(--tx-2);
    font-family: inherit;
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    border-radius: 999px;
  }
  .chip.on {
    color: var(--tx-5);
    background: color-mix(in oklab, var(--red) 24%, transparent);
  }

  /* The table owns its horizontal scroll so the page body never slides
     sideways on a phone. */
  .wrap {
    border-top: 1px solid var(--bg-4);
  }
  .tbl {
    min-width: 900px;
    display: grid;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(300px, 1fr) 110px 150px 120px 90px 170px 130px;
    align-items: center;
    gap: 0 10px;
    width: 100%;
    padding: 0 8px;
    color: var(--tx-2);
    font-family: inherit;
    font-size: 13px;
    text-align: left;
    background: none;
    border: 0;
    border-bottom: 1px solid color-mix(in oklab, var(--bg-4) 55%, transparent);
  }
  .row.head {
    position: sticky;
    top: 0;
    z-index: 2;
    background: var(--bg-2);
  }
  .row.group {
    color: var(--tx-4);
  }
  .row:not(.head):hover {
    background: var(--glass);
  }
  .row:focus-visible {
    outline: 2px solid var(--red);
    outline-offset: -2px;
  }
  .cell {
    min-height: 34px;
    display: flex;
    align-items: center;
    gap: 6px;
    overflow: hidden;
    white-space: nowrap;
  }
  .name {
    padding-left: calc(var(--d, 0) * 18px);
  }
  .caret {
    width: 12px;
    flex-shrink: 0;
    font-size: 10px;
  }
  .row.leaf .caret {
    visibility: hidden;
  }
  .lbl {
    display: grid;
    overflow: hidden;
  }
  .sub {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .sorter {
    padding: 0;
    color: var(--tx-3);
    font-family: inherit;
    font-weight: 600;
    letter-spacing: 0.08em;
    background: none;
    border: 0;
  }
  .sorter:focus-visible {
    outline: 1px solid var(--red);
  }


  .tags {
    flex-wrap: wrap;
    gap: 3px;
  }
  .tag {
    padding: 1px 6px;
    color: var(--tx-3);
    font-size: 10px;
    background: var(--bg-3);
    border-radius: 4px;
  }
  .tag.gone {
    color: var(--red);
    background: color-mix(in oklab, var(--red) 18%, transparent);
  }

  .detail {
    padding: 10px 14px 12px calc(14px + var(--d, 0) * 18px);
    display: flex;
    flex-wrap: wrap;
    gap: 10px 20px;
    align-items: start;
    color: var(--tx-2);
    background: color-mix(in oklab, var(--bg-3) 55%, transparent);
    border-bottom: 1px solid var(--bg-4);
  }
  .kv {
    flex: 1;
    min-width: 260px;
    display: grid;
    grid-template-columns: 90px minmax(0, 1fr);
    gap: 2px 10px;
  }
  .kv dt {
    color: var(--tx-3);
  }
  .kv dd {
    margin: 0;
    overflow: hidden;
  }

  .empty,
  .foot {
    padding: 14px 2px;
  }

  @media (max-width: 640px) {
    header {
      padding: 12px 16px;
      padding-top: calc(12px + var(--safe-t));
    }
    main {
      padding: 12px 16px calc(28px + var(--safe-b));
    }
    .act {
      margin-left: 0;
    }
  }

  /* A row is the tap target for expanding a branch, and a chip for filtering:
     both have to clear 44px on a touch screen. */
  @media (hover: none) {
    .row .cell {
      min-height: 44px;
    }
    .chip {
      min-height: 44px;
    }
    .sorter {
      min-height: 44px;
    }
  }
</style>
