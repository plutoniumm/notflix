<script lang="ts">
  import { onMount } from "svelte";
  import { clean, vidURL } from "./lib/video";
  import { GET } from "./lib";
  import { Down } from "./lib/dl";

  let data: VideoData = $state({});
  let search = $state("");
  let loading = $state(true);
  let jobs: Job[] = $state([]);
  let downloadedSet = $state(new Set<string>());

  async function pollJobs() {
    jobs = (await GET("/api/conversions")) || [];
  }

  onMount(() => {
    GET("/list/video").then((d) => (data = d));
    loading = false;

    pollJobs();

    Down.all().then((records) => {
      downloadedSet = new Set(
        records.filter((r) => r.status === "done").map((r) => r.videoParam),
      );
    });

    const unsubSW = Down.on((videoParam, record) => {
      const next = new Set(downloadedSet);
      if (record?.status === "done") {
        next.add(videoParam);
      } else {
        next.delete(videoParam);
      }
      downloadedSet = next;
    });

    const timer = setInterval(pollJobs, 2000);
    return () => {
      clearInterval(timer);
      unsubSW();
    };
  });

  let rows = $derived(
    Object.entries(data).filter(([, files]) => files?.length),
  );

  let results = $derived.by(() => {
    const q = search.trim().toLowerCase();
    if (!q) return null;

    return Object.entries(data).flatMap(([dir, files]) =>
      (files || [])
        .filter((f) => {
          const name = f.name.toLowerCase();

          clean(name).includes(q) || name.includes(q);
        })
        .map((f) => ({ dir, ...f })),
    );
  });

  function scrollRow(el: HTMLElement, dir: number) {
    el.scrollBy({ left: dir * 400, behavior: "smooth" });
  }
</script>

<div style="min-height: 100vh;">
  <header class="f al-ct g20 p-stx">
    <!-- <a href="/" class="logo fw7">NOTFLIX</a> -->
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
  </header>

  <main>
    {#if loading}
      <div class="loading tc">Loading…</div>
    {:else if results !== null}
      <div class="search-header f al-ct g20">
        <span>
          Results for "<strong>{search}</strong>"
        </span>

        <button class="clear fs-xs rx2" onclick={() => (search = "")}>
          ✕ Clear
        </button>
      </div>

      <div class="grid fw g10">
        {#each results as item (item.dir + "/" + item.name)}
          <a href={vidURL(item.dir, item.name)} class="card">
            <div class="thumb">
              <img
                src="/images/{item.key}.jpg"
                alt=""
                loading="lazy"
                onerror={(event) => {
                  const img = event.currentTarget as HTMLImageElement;
                  img.src = "/assets/tight.svg";
                  img.onerror = null;
                }}
              />
              <div class="play-icon">▶</div>
            </div>

            <div class="card-info">
              <span class="card-name">
                {clean(item.name)}
              </span>
              {#if item.dir !== "."}
                <span class="card-dir fs-xs d-b">
                  {item.dir}
                </span>
              {/if}
            </div>
          </a>
        {/each}

        {#if results.length === 0}
          <p class="no-results">Nothing found.</p>
        {/if}
      </div>
    {:else}
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

  {#if jobs.length > 0}
    <div class="panel p-fix rx10 flow-h">
      <div class="header f al-ct j-bw">
        <span class="title fw6">Converting</span>
        <span class="count fs-xs fw7 cc rx20">{jobs.length}</span>
      </div>

      {#each jobs as j (j.name)}
        <div class="item">
          <div class="name fs-xs flow-h">
            {j.name.replace(/\.(mkv|mov)$/i, "")}
          </div>

          <div class="row f al-ct g10">
            <div class="bar rx2 flow-h">
              <div
                class="fill h-100 rx2"
                style="width: {j.percent.toFixed(1)}%"
              ></div>
            </div>

            <span class="pct fs-xs tr">{Math.round(j.percent)}%</span>
          </div>
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
    background: #111;
    border: 1px solid #444;
    color: #fff;
    padding: 8px 14px;
    transition: border-color 0.15s;
  }
  .search-wrap input:focus {
    border-color: #fff;
  }
  .search-wrap input::placeholder {
    color: #666;
  }

  .manage {
    margin-left: auto;
    color: #ccc;
    padding: 6px 12px;
    border: 1px solid #444;
    transition:
      color 0.15s,
      border-color 0.15s;
  }
  .manage:hover {
    color: #fff;
    border-color: #888;
  }

  main {
    padding: 0 0 60px;
  }
  .loading {
    padding: 80px;
    color: #666;
  }

  .search-header {
    padding: 24px 48px 16px;
    color: #aaa;
  }
  .search-header strong {
    color: #fff;
  }
  .clear {
    background: none;
    border: 1px solid #444;
    color: #999;
    padding: 4px 10px;
  }
  .clear:hover {
    color: #fff;
    border-color: #888;
  }

  .grid {
    padding: 0 48px;
  }
  .grid .card {
    width: 180px;
  }
  .no-results {
    color: #666;
    padding: 40px 48px;
  }

  .row {
    margin-bottom: 32px;
  }
  .row h2 {
    padding: 0 48px;
    margin: 0 0 12px;
    color: #eee;
  }

  .arrow {
    position: absolute;
    z-index: 10;
    background: rgba(0, 0, 0, 0.7);
    color: #fff;
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
    background: rgba(0, 0, 0, 0.9);
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
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.8);
  }

  .thumb {
    aspect-ratio: 16/9 !important;
    background: #222;
  }

  .play-icon {
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
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
    background: #4ade80;
    box-shadow: 0 0 4px rgba(74, 222, 128, 0.8);
  }

  .card-name {
    color: #ccc;
    padding: 6px 4px 2px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    background: #1a1a1a;
  }

  .card-info {
    background: #1a1a1a;
    padding: 0 4px 6px;
  }
  .card-dir {
    color: #666;
  }

  .panel {
    bottom: 24px;
    right: 24px;
    width: 320px;
    background: #1a1a1a;
    border: 1px solid #333;
    z-index: 200;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.6);
    animation: slide-in-r 0.3s ease;
  }

  .header {
    padding: 10px 14px;
    background: #222;
    border-bottom: 1px solid #333;
  }

  .title {
    color: #ccc;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .count {
    background: #e50914;
    color: #fff;
    width: 20px;
    height: 20px;
  }

  .item {
    padding: 10px 14px;
    border-bottom: 1px solid #252525;
  }
  .item:last-child {
    border-bottom: none;
  }

  .name {
    color: #ddd;
    white-space: nowrap;
    text-overflow: ellipsis;
    margin-bottom: 6px;
  }

  .bar {
    flex: 1;
    height: 4px;
    background: #333;
  }

  .fill {
    background: #e50914;
    transition: width 0.5s ease;
    min-width: 2px;
  }

  .pct {
    color: #888;
    width: 30px;
    flex-shrink: 0;
  }
</style>
