<script lang="ts">
  import { onMount } from "svelte";
  import { clean, vidURL } from "../core/video";
  import { toast } from "../core/toast.svelte";
  import { HomeData } from "./homeData.svelte";
  import Continues from "./Continues.svelte";

  const home = new HomeData();
  let search = $state("");
  let swUpdate = $state<ServiceWorkerRegistration | null>(null);

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

  let debouncedSearch = $state("");
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  $effect(() => {
    const q = search;
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      debouncedSearch = q;
    }, 150);
  });

  let results = $derived(home.filter(debouncedSearch));

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
    swUpdate?.waiting?.postMessage({ type: "SKIP_WAITING" });
  }

  function scrollRow(el: HTMLElement, dir: number) {
    el.scrollBy({ left: dir * 400, behavior: "smooth" });
  }
</script>

<div style="min-height: 100vh;">
  <header class="glass-header f al-ct g20">
    <a href="/">
      <img src="/assets/tight.svg" alt="Notflix" height="50" />
    </a>

    <div class="search-wrap">
      <input
        class="field w-100"
        type="search"
        placeholder="Search…"
        bind:value={search}
        autocomplete="off"
        spellcheck="false"
      />
    </div>

    <a href="/manage" class="manage glass glass-hover">Manage</a>

    {#if swUpdate}
      <button class="update-btn btn-action ptr fs-xs" onclick={applyUpdate}>
        Update available — tap to reload
      </button>
    {/if}
  </header>

  <main>
    {#if home.loading}
      <div class="skel-grid">
        {#each Array(14) as _, i}
          <div class="skel-card" style="--i:{i}"></div>
        {/each}
      </div>
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
          {@const vidParam =
            item.dir === "." ? item.name : `${item.dir}/${item.name}`}
          <a
            href={vidURL(item.dir, item.name)}
            class="media-card ptr flow-h p-rel"
            style="--i: {idx}"
          >
            <div class="thumb p-rel flow-h">
              <img
                class="h-100 w-100 d-b"
                src="/images/{item.key}.jpg"
                alt=""
                loading="lazy"
                onerror={(e) => {
                  const i = e.currentTarget as HTMLImageElement;
                  i.src = "/assets/tight.svg";
                  i.onerror = null;
                }}
              />
              <div class="play-icon p-abs cc o-0 fs-lg">▶</div>
              {#if home.downloadedSet.has(vidParam)}
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
      {#if home.offline}
        <div class="offline-banner f al-ct g10">
          <span class="offline-dot"></span>
          Offline — showing downloaded videos
        </div>
      {/if}
      <Continues
        items={home.continues}
        downloadedSet={home.downloadedSet}
        onRemove={removeContinue}
      />
      {#each home.rows as [dir, files]}
        {#if files?.length}
          <section class="row">
            <h2>
              {dir === "." ? "Movies" : clean(dir) || dir}
            </h2>

            <div class="scroll-row f al-ct p-rel">
              <button
                class="scroll-arrow cc h-100 p0 left"
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
                    class="media-card ptr flow-h p-rel"
                    style="--i: {idx}"
                  >
                    <div class="thumb p-rel flow-h">
                      <img
                        class="h-100 w-100 d-b"
                        src="/images/{f.key}.jpg"
                        alt=""
                        loading="lazy"
                        onerror={(e) => {
                          const i = e.currentTarget as HTMLImageElement;
                          i.src = "/assets/tight.svg";
                          i.onerror = null;
                        }}
                      />
                      <div class="play-icon p-abs cc o-0 fs-lg">▶</div>
                      {#if home.downloadedSet.has(vidParam)}
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
                class="scroll-arrow cc h-100 p0 right"
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

  {#if home.inProg.length > 0}
    <div class="panel glass-strong p-fix flow-h">
      <div class="header f al-ct j-bw">
        <span class="title fw6">Downloading</span>
        <span class="count fs-xs fw7 cc rx20">{home.inProg.length}</span>
      </div>
      {#each home.inProg as dl (dl.videoParam)}
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
    padding: 16px 48px;
  }
  .search-wrap {
    flex: 1;
    max-width: 380px;
  }
  .manage {
    margin-left: auto;
    color: var(--tx-4);
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 500;
    border-radius: var(--r-md);
  }

  .update-btn {
    padding: 7px 14px;
    animation: pulse-bg 2s ease infinite;
  }
  @keyframes pulse-bg {
    0%, 100% { opacity: 1; }
    50%      { opacity: 0.85; }
  }

  main {
    padding: 24px 0 60px;
    position: relative;
    z-index: 1;
  }

  .offline-banner {
    margin: 0 48px 16px;
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
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 12px;
    padding: 24px 48px;
  }
  .skel-card {
    aspect-ratio: 16 / 9;
    border-radius: var(--r-lg);
    background: linear-gradient(90deg, var(--bg-3) 0%, var(--bg-4) 40%, var(--bg-3) 80%);
    background-size: 400px 100%;
    opacity: 0;
    animation:
      shimmer 1.6s linear infinite,
      fade-in 0.3s ease forwards;
    animation-delay: calc(var(--i) * 60ms), calc(var(--i) * 60ms);
  }

  .search-header {
    padding: 24px 48px 16px;
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
    padding: 5px 11px;
    border-radius: var(--r-md);
  }
  .clear:hover {
    color: var(--tx-5);
    background: var(--glass-2);
    border-color: rgba(255, 255, 255, 0.16);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 12px;
    padding: 0 48px;
  }
  .grid :global(.media-card) {
    width: auto;
  }
  .no-results {
    grid-column: 1 / -1;
    color: var(--tx-2);
    padding: 60px 0;
    text-align: center;
    font-family: var(--font-display);
    font-size: 18px;
  }

  .row {
    margin-bottom: 36px;
  }
  .row h2 {
    padding: 0 48px;
    margin: 0 0 14px;
    font-size: 22px;
  }

  .cards {
    padding: 8px 48px 16px;
    scrollbar-width: none;
    animation-delay: calc(var(--i) * 50ms);
  }
  :global(.media-card) {
    width: 220px;
  }

  .cancel-btn {
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    color: var(--tx-3);
    width: 22px;
    height: 22px;
    flex-shrink: 0;
    font-size: 11px;
    border-radius: var(--r-sm);
  }
  .cancel-btn:hover {
    background: var(--red);
    color: #fff;
    border-color: var(--red);
  }

  .card-dir {
    color: var(--tx-2);
    padding: 0 10px 8px;
    background: var(--bg-3);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    border-radius: 0 0 var(--r-lg) var(--r-lg);
  }

  .panel {
    bottom: 24px;
    right: 24px;
    width: 320px;
    border-radius: var(--r-xl);
    z-index: 200;
    box-shadow: var(--sh-4);
    animation: slide-in-r 0.36s var(--ease-out);
  }
  .header {
    padding: 12px 16px;
    background: rgba(255, 255, 255, 0.02);
    border-bottom: 1px solid var(--glass-bd);
  }
  .title {
    color: var(--tx-4);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    font-size: 11px;
  }
  .count {
    background: linear-gradient(135deg, var(--red) 0%, #ff7849 100%);
    color: var(--tx-5);
    width: 22px;
    height: 22px;
    border-radius: 50%;
    box-shadow: 0 4px 12px -2px var(--red-glow);
  }
  .item {
    padding: 11px 16px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
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

  @media (max-width: 640px) {
    header {
      padding: 10px 16px;
      gap: 10px;
    }
    header img {
      height: 32px;
    }
    .grid {
      padding: 0 16px;
      gap: 8px;
      grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    }
    .row h2 {
      padding: 0 16px;
      font-size: 17px;
    }
    .cards {
      padding: 6px 16px 12px;
    }
    .search-header {
      padding: 16px 16px 12px;
    }
    main {
      padding: 0 0 40px;
    }
    .manage {
      padding: 5px 10px;
      font-size: 12px;
    }
    .update-btn {
      font-size: 11px;
      padding: 5px 10px;
    }
  }
</style>
