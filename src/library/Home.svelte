<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { clean, vidURL } from "../core/video";
  import { GET, POST } from "../core/api";
  import { Down } from "./dl";

  let data: VideoData = $state({});
  let search = $state("");
  let loading = $state(true);
  let offline = $state(false);
  let swUpdate = $state<ServiceWorkerRegistration | null>(null);
  let downloadedSet = $state(new Set<string>());
  let inProg: DownloadRecord[] = $state([]);
  let continues: {
    dir: string;
    name: string;
    key: string;
    t: number;
  }[] = $state([]);

  async function loadState(d: VideoData) {
    const allParams: {
      dir: string;
      name: string;
      key: string;
      param: string;
    }[] = [];

    for (const [dir, files] of Object.entries(d)) {
      for (const f of files ?? []) {
        const param = dir === "." ? f.name : `${dir}/${f.name}`;
        allParams.push({
          dir,
          name: f.name,
          key: f.key,
          param,
        });
      }
    }
    if (!allParams.length) return;

    const qs = allParams
      .map((v) => `key=${encodeURIComponent("watched:" + v.param)}`)
      .join("&");
    const kv: Record<string, { t: number; at: number } | null> =
      (await GET(`/kv/get?${qs}`)) ?? {};

    continues = allParams
      .filter((v) => {
        const val = kv[`watched:${v.param}`];
        return val && val.t > 60;
      })
      .sort((a, b) => {
        const at = kv[`watched:${a.param}`]?.at ?? 0;
        const bt = kv[`watched:${b.param}`]?.at ?? 0;
        return bt - at;
      })
      .slice(0, 20)
      .map((v) => ({
        dir: v.dir,
        name: v.name,
        key: v.key,
        t: kv[`watched:${v.param}`]!.t,
      }));
  }

  function removeContinue(e: Event, dir: string, name: string) {
    e.preventDefault();
    e.stopPropagation();
    const param = dir === "." ? name : `${dir}/${name}`;
    POST("/kv/set", { key: `watched:${param}`, value: null });
    continues = continues.filter(
      (c) => !(c.dir === dir && c.name === name),
    );
  }

  async function cancelDownload(videoParam: string) {
    await Down.del(videoParam);

    inProg = inProg.filter((r) => r.videoParam !== videoParam);
  }

  function buildOfflineData(records: DownloadRecord[]): VideoData {
    const out: VideoData = {};
    for (const r of records) {
      if (r.status !== "done") continue;
      const slash = r.videoParam.lastIndexOf("/");
      const dir = slash >= 0 ? r.videoParam.slice(0, slash) : ".";
      const name = slash >= 0 ? r.videoParam.slice(slash + 1) : r.videoParam;
      if (!out[dir]) out[dir] = [];
      out[dir].push({ name, key: r.key });
    }
    return out;
  }

  onMount(() => {
    const recordsP = Down.all();

    (async () => {
      const d = await GET("/list/video");
      if (d && Object.keys(d).length) {
        data = d;
        loadState(d);
      } else {
        offline = true;
        data = buildOfflineData(await recordsP);
      }
      loading = false;
    })();

    recordsP.then((records) => {
      downloadedSet = new Set(
        records.filter((r) => r.status === "done").map((r) => r.videoParam),
      );
      inProg = records.filter((r) => r.status === "downloading");
    });

    Down.recover();

    const unsubSW = Down.on((videoParam, record) => {
      const next = new Set(downloadedSet);
      if (record?.status === "done") {
        next.add(videoParam);
        inProg = inProg.filter((r) => r.videoParam !== videoParam);
      } else if (record?.status === "downloading") {
        inProg = [...inProg.filter((r) => r.videoParam !== videoParam), record];
      } else {
        next.delete(videoParam);
        inProg = inProg.filter((r) => r.videoParam !== videoParam);
      }
      downloadedSet = next;
    });

    const onSwUpdate = (e: Event) => {
      swUpdate = (e as CustomEvent).detail;
    };
    window.addEventListener("sw-update", onSwUpdate);

    return () => {
      unsubSW();
      window.removeEventListener("sw-update", onSwUpdate);
    };
  });

  let rows = $derived(
    Object.entries(data).filter(([, files]) => files?.length),
  );

  let debouncedSearch = $state("");
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  $effect(() => {
    const q = search;
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => { debouncedSearch = q; }, 150);
  });

  let results = $derived.by(() => {
    const q = debouncedSearch.trim().toLowerCase();
    if (!q) return null;

    return Object.entries(data).flatMap(([dir, files]) =>
      (files || [])
        .filter((f) => {
          const name = f.name.toLowerCase();

          return clean(name).includes(q) || name.includes(q);
        })
        .map((f) => ({ dir, ...f })),
    );
  });

  function applyUpdate() {
    swUpdate?.waiting?.postMessage({ type: "SKIP_WAITING" });
  }

  function scrollRow(el: HTMLElement, dir: number) {
    el.scrollBy({ left: dir * 400, behavior: "smooth" });
  }
</script>

<div style="min-height: 100vh;">
  <header class="f al-ct g20 p-stx">
    <a href="/">
      <img src="/assets/tight.svg" alt="Notflix" height="50" />
    </a>

    <div class="search-wrap">
      <input
        class="rx5 w-100"
        type="search"
        placeholder="Search…"
        bind:value={search}
        autocomplete="off"
        spellcheck="false"
      />
    </div>

    <a href="/manage" class="manage rx5">Manage</a>

    {#if swUpdate}
      <button class="update-btn rx5 ptr fs-xs fw5" onclick={applyUpdate}>
        Update available — tap to reload
      </button>
    {/if}
  </header>

  <main>
    {#if loading}
      <div class="loading tc"><span class="spinner"></span></div>
    {:else if results !== null}
      <div class="search-header f al-ct g20">
        <span>
          Results for "<strong>{search}</strong>"
        </span>

        <button class="clear fs-xs rx2" onclick={() => (search = "")}>
          ✕ Clear
        </button>
      </div>

      <div class="grid">
        {#each results as item, idx (item.dir + "/" + item.name)}
          {@const vidParam = item.dir === "." ? item.name : `${item.dir}/${item.name}`}
          <a href={vidURL(item.dir, item.name)} class="card ptr rx5 flow-h p-rel" style="--i: {idx}">
            <div class="thumb p-rel flow-h">
              <img
                class="h-100 w-100 d-b"
                src="/images/{item.key}.jpg"
                alt=""
                loading="lazy"
                onerror={(e) => { const i = e.currentTarget as HTMLImageElement; i.src = "/assets/tight.svg"; i.onerror = null; }}
              />
              <div class="play-icon p-abs cc o-0 fs-lg">▶</div>
              {#if downloadedSet.has(vidParam)}
                <div class="dot p-abs"></div>
              {/if}
            </div>

            <div class="card-name">
              {clean(item.name)}
            </div>
            {#if item.dir !== "."}
              <div class="card-dir fs-xs">
                {item.dir}
              </div>
            {/if}
          </a>
        {/each}

        {#if results.length === 0}
          <p class="no-results">Nothing found.</p>
        {/if}
      </div>
    {:else}
      {#if offline}
        <div class="offline-banner f al-ct g10">
          <span class="offline-dot"></span>
          Offline — showing downloaded videos
        </div>
      {/if}
      {#if continues.length > 0}
        <section class="row">
          <h2>Continue Watching</h2>
          <div class="row-wrap f al-ct p-rel">
            <button
              class="arrow cc o-0 h-100 p0 left"
              onclick={(e) => {
                const el: any = e.currentTarget.nextElementSibling;
                scrollRow(el, -1);
              }}>‹</button
            >
            <div class="cards f flow-x-s g5">
              {#each continues as item, idx}
                {@const vidParam =
                  item.dir === "." ? item.name : `${item.dir}/${item.name}`}
                <a
                  href={vidURL(item.dir, item.name)}
                  class="card ptr rx5 flow-h p-rel"
                  style="--i: {idx}"
                >
                  <div class="thumb p-rel flow-h">
                    <img
                      class="h-100 w-100 d-b"
                      src="/images/{item.key}.jpg"
                      alt=""
                      loading="lazy"
                      onerror={(e) => { const i = e.currentTarget as HTMLImageElement; i.src = "/assets/tight.svg"; i.onerror = null; }}
                    />
                    <div class="play-icon p-abs cc o-0 fs-lg">▶</div>
                    {#if downloadedSet.has(vidParam)}
                      <div class="dot p-abs"></div>
                    {/if}
                    <!-- svelte-ignore a11y_consider_explicit_label -->
                    <button
                      class="cw-remove p-abs cc o-0 ptr"
                      onclick={(e) => removeContinue(e, item.dir, item.name)}
                    >✕</button>
                    <div class="cw-bar p-abs"></div>
                  </div>
                  <div class="card-name">
                    {clean(item.name)}
                  </div>
                  <div class="cw-time fs-xs">
                    {Math.floor(item.t / 3600) > 0
                      ? `${Math.floor(item.t / 3600)}h ${Math.floor((item.t % 3600) / 60)}m`
                      : `${Math.floor(item.t / 60)}m`}
                  </div>
                </a>
              {/each}
            </div>
            <button
              class="arrow cc o-0 h-100 p0 right"
              onclick={(e) => {
                const el: any = e.currentTarget.previousElementSibling;
                scrollRow(el, 1);
              }}>›</button
            >
          </div>
        </section>
      {/if}
      {#each rows as [dir, files]}
        {#if files?.length}
          <section class="row">
            <h2>
              {dir === "." ? "Movies" : clean(dir) || dir}
            </h2>

            <div class="row-wrap f al-ct p-rel">
              <button
                class="arrow cc o-0 h-100 p0 left"
                onclick={(e) => {
                  const el: any = e.currentTarget.nextElementSibling;
                  scrollRow(el, -1);
                }}>‹</button
              >

              <div class="cards f flow-x-s g5">
                {#each files as f, idx}
                  {@const vidParam = dir === "." ? f.name : `${dir}/${f.name}`}
                  <a
                    href={vidURL(dir, f.name)}
                    class="card ptr rx5 flow-h p-rel"
                    style="--i: {idx}"
                  >
                    <div class="thumb p-rel flow-h">
                      <img
                        class="h-100 w-100 d-b"
                        src="/images/{f.key}.jpg"
                        alt=""
                        loading="lazy"
                        onerror={(e) => { const i = e.currentTarget as HTMLImageElement; i.src = "/assets/tight.svg"; i.onerror = null; }}
                      />
                      <div class="play-icon p-abs cc o-0 fs-lg">▶</div>
                      {#if downloadedSet.has(vidParam)}
                        <div class="dot p-abs"></div>
                      {/if}
                    </div>

                    <div class="card-name">
                      {clean(f.name)}
                    </div>
                  </a>
                {/each}
              </div>

              <button
                class="arrow cc o-0 h-100 p0 right"
                onclick={(e) => {
                  const el: any = e.currentTarget.previousElementSibling;
                  scrollRow(el, 1);
                }}>›</button
              >
            </div>
          </section>
        {/if}
      {/each}
    {/if}
  </main>

  {#if inProg.length > 0}
    <div class="panel p-fix rx10 flow-h">
      <div class="header f al-ct j-bw">
        <span class="title fw6">Downloading</span>
        <span class="count fs-xs fw7 cc rx20">{inProg.length}</span>
      </div>
      {#each inProg as dl (dl.videoParam)}
        <div class="item f al-ct g10">
          <div class="name fs-xs flow-h" style="flex:1">
            {clean(dl.title || dl.videoParam)}
          </div>
          <button
            class="cancel-btn fs-xs cc ptr rx3"
            onclick={() => cancelDownload(dl.videoParam)}
            title="Cancel download">✕</button
          >
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  header {
    top: 0;
    z-index: 100;
    background: linear-gradient(to bottom, #000 80%, transparent);
    padding: 18px 48px;
  }

  .search-wrap {
    flex: 1;
    max-width: 340px;
  }

  .search-wrap input {
    background: var(--bg-2);
    border: 1px solid var(--bg-5);
    color: var(--tx-5);
    padding: 8px 14px;
    transition: border-color 0.15s;
  }
  .search-wrap input:focus {
    border-color: var(--tx-5);
  }
  .search-wrap input::placeholder {
    color: var(--tx-1);
  }

  .update-btn {
    background: var(--red);
    color: #fff;
    border: none;
    padding: 6px 14px;
    white-space: nowrap;
    animation: pulse-bg 2s ease infinite;
  }

  @keyframes pulse-bg {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.7; }
  }

  .manage {
    margin-left: auto;
    color: var(--tx-4);
    padding: 6px 12px;
    border: 1px solid var(--bg-5);
    transition:
      color 0.15s,
      border-color 0.15s;
  }
  .manage:hover {
    color: var(--tx-5);
    border-color: var(--tx-2);
  }

  main {
    padding: 0 0 60px;
  }

  .offline-banner {
    padding: 10px 48px;
    color: var(--tx-3);
    font-size: 13px;
  }
  .offline-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--tx-2);
    flex-shrink: 0;
  }
  .loading {
    padding: 80px;
    color: var(--tx-1);
  }
  .spinner {
    display: inline-block;
    width: 28px;
    height: 28px;
    border: 3px solid var(--bg-5);
    border-top-color: var(--tx-3);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .search-header {
    padding: 24px 48px 16px;
    color: var(--tx-3);
  }
  .search-header strong {
    color: var(--tx-5);
  }
  .clear {
    background: none;
    border: 1px solid var(--bg-5);
    color: var(--tx-2);
    padding: 4px 10px;
  }
  .clear:hover {
    color: var(--tx-5);
    border-color: var(--tx-2);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
    gap: 10px;
    padding: 0 48px;
  }
  .grid .card {
    width: auto;
  }
  .no-results {
    grid-column: 1 / -1;
    color: var(--tx-1);
    padding: 40px 0;
  }

  .row {
    margin-bottom: 32px;
  }
  .row h2 {
    padding: 0 48px;
    margin: 0 0 12px;
    color: var(--tx-4);
  }

  .arrow {
    position: absolute;
    z-index: 10;
    background: #000b;
    color: var(--tx-5);
    font-size: 2rem;
    width: 44px;
    transition: opacity 0.2s;
  }
  .arrow.left {
    left: 0;
  }
  .arrow.right {
    right: 0;
  }
  .row-wrap:hover .arrow {
    opacity: 1;
  }
  @media (hover: none) {
    .arrow {
      opacity: 0.5;
    }
  }
  .arrow:hover {
    background: #000e;
  }

  .cards {
    padding: 8px 48px 16px;
    scrollbar-width: none;
    animation-delay: calc(var(--i) * 50ms);
  }

  .card {
    flex-shrink: 0;
    width: 200px;
    transition:
      transform 0.2s,
      box-shadow 0.2s;
    z-index: 1;
    animation: slide-up 0.3s ease both;
  }
  .card:hover {
    transform: scale(1.08);
    z-index: 10;
    box-shadow: 0 8px 24px #000c;
  }

  .thumb {
    aspect-ratio: 16/9 !important;
    background: var(--bg-3);
  }

  .play-icon {
    inset: 0;
    background: #0006;
    transition: opacity 0.2s;
  }
  .card:hover .play-icon {
    opacity: 1;
  }

  .dot {
    bottom: 6px;
    right: 6px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--grn);
    box-shadow: 0 0 4px #4d8a;
  }

  .cw-remove {
    top: 4px;
    right: 4px;
    width: 22px;
    height: 22px;
    font-size: 11px;
    background: #000b;
    color: var(--tx-3);
    border-radius: 4px;
    border: none;
    z-index: 2;
    transition: opacity 0.15s, background 0.15s, color 0.15s;
  }
  .card:hover .cw-remove {
    opacity: 1;
  }
  .cw-remove:hover {
    background: var(--red);
    color: #fff;
  }

  .cw-bar {
    bottom: 0;
    left: 0;
    right: 0;
    height: 3px;
    background: var(--red);
    opacity: 0.8;
  }

  .cw-time {
    color: var(--tx-2);
    padding: 2px 4px 4px;
    background: var(--bg-3);
  }

  .cancel-btn {
    background: #fff1;
    border: 1px solid #fff2;
    color: var(--tx-3);
    width: 22px;
    height: 22px;
    flex-shrink: 0;
    font-size: 11px;
    transition:
      background 0.15s,
      color 0.15s;
  }
  .cancel-btn:hover {
    background: var(--red);
    color: #fff;
    border-color: var(--red);
  }

  .card-name {
    color: var(--tx-4);
    padding: 6px 4px 2px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    background: var(--bg-3);
  }

  .card-dir {
    color: var(--tx-1);
    padding: 0 4px 4px;
    background: var(--bg-3);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .panel {
    bottom: 24px;
    right: 24px;
    width: 320px;
    background: var(--bg-3);
    border: 1px solid var(--bg-4);
    z-index: 200;
    box-shadow: 0 8px 32px #0009;
    animation: slide-in-r 0.3s ease;
  }

  .header {
    padding: 10px 14px;
    background: var(--bg-3);
    border-bottom: 1px solid var(--bg-4);
  }

  .title {
    color: var(--tx-4);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .count {
    background: var(--red);
    color: var(--tx-5);
    width: 20px;
    height: 20px;
  }

  .item {
    padding: 10px 14px;
    border-bottom: 1px solid var(--bg-3);
  }
  .item:last-child {
    border-bottom: none;
  }

  .name {
    color: var(--tx-4);
    white-space: nowrap;
    text-overflow: ellipsis;
    margin-bottom: 6px;
  }
</style>
