<script lang="ts">
  import { onMount } from 'svelte'
  import { cleanName, videoUrl, type VideoData, type VideoEntry } from './lib/video'

  interface Conversion { name: string; percent: number }

  let data: VideoData = $state({})
  let search = $state('')
  let loading = $state(true)
  let conversions: Conversion[] = $state([])

  onMount(async () => {
    data = await fetch('/list/video').then(r => r.json()).catch(() => ({}))
    loading = false

    // Poll for active conversions
    async function pollConversions() {
      conversions = await fetch('/api/conversions').then(r => r.json()).catch(() => [])
    }
    pollConversions()
    const timer = setInterval(pollConversions, 2000)
    return () => clearInterval(timer)
  })

  let rows = $derived(
    Object.entries(data).filter(([, files]) => files?.length > 0)
  )

  let searchResults = $derived.by(() => {
    const q = search.trim().toLowerCase()
    if (!q) return null
    return Object.entries(data).flatMap(([dir, files]) =>
      (files || [])
        .filter(f => cleanName(f.name).toLowerCase().includes(q) || f.name.toLowerCase().includes(q))
        .map(f => ({ dir, ...f }))
    )
  })

  function scrollRow(el: HTMLElement, dir: number) {
    el.scrollBy({ left: dir * 400, behavior: 'smooth' })
  }
</script>

<div class="page">
  <header>
    <a href="/" class="logo">NOTFLIX</a>
    <div class="search-wrap">
      <input
        type="search"
        placeholder="Search…"
        bind:value={search}
        autocomplete="off"
        spellcheck="false"
      />
    </div>
    <a href="/manage" class="manage-link">Manage</a>
  </header>

  <main>
    {#if loading}
      <div class="loading">Loading…</div>
    {:else if searchResults !== null}
      <div class="search-header">
        <span>Results for "<strong>{search}</strong>"</span>
        <button class="clear" onclick={() => search = ''}>✕ Clear</button>
      </div>
      <div class="grid">
        {#each searchResults as item (item.dir + '/' + item.name)}
          <a href={videoUrl(item.dir, item.name)} class="card">
            <div class="thumb">
              <img src="/images/{item.key}.jpg" alt="" loading="lazy" />
              <div class="play-icon">▶</div>
            </div>
            <div class="card-info">
              <span class="card-name">{cleanName(item.name)}</span>
              {#if item.dir !== '.'}<span class="card-dir">{item.dir}</span>{/if}
            </div>
          </a>
        {/each}
        {#if searchResults.length === 0}
          <p class="no-results">Nothing found.</p>
        {/if}
      </div>
    {:else}
      {#each rows as [dir, files]}
        {#if files?.length}
          {@const rowId = `row-${dir}`}
          <section class="row">
            <h2>{dir === '.' ? 'Movies' : (cleanName(dir) || dir)}</h2>
            <div class="row-wrap">
              <button class="arrow left" onclick={(e) => {
                const el = (e.currentTarget as HTMLElement).nextElementSibling as HTMLElement
                scrollRow(el, -1)
              }}>‹</button>
              <div class="cards" id={rowId}>
                {#each files as f (f.key)}
                  <a href={videoUrl(dir, f.name)} class="card">
                    <div class="thumb">
                      <img src="/images/{f.key}.jpg" alt="" loading="lazy" />
                      <div class="play-icon">▶</div>
                    </div>
                    <div class="card-name">{cleanName(f.name)}</div>
                  </a>
                {/each}
              </div>
              <button class="arrow right" onclick={(e) => {
                const el = (e.currentTarget as HTMLElement).previousElementSibling as HTMLElement
                scrollRow(el, 1)
              }}>›</button>
            </div>
          </section>
        {/if}
      {/each}
    {/if}
  </main>
  {#if conversions.length > 0}
    <div class="conv-panel">
      <div class="conv-header">
        <span class="conv-title">Converting</span>
        <span class="conv-count">{conversions.length}</span>
      </div>
      {#each conversions as c (c.name)}
        <div class="conv-item">
          <div class="conv-name">{c.name.replace(/\.(mkv|mov)$/i, '')}</div>
          <div class="conv-row">
            <div class="conv-bar">
              <div class="conv-fill" style="width: {c.percent.toFixed(1)}%"></div>
            </div>
            <span class="conv-pct">{Math.round(c.percent)}%</span>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .page { min-height: 100vh; }

  header {
    position: sticky;
    top: 0;
    z-index: 100;
    background: linear-gradient(to bottom, #000 80%, transparent);
    display: flex;
    align-items: center;
    gap: 24px;
    padding: 18px 48px;
  }

  .logo {
    color: #e50914;
    font-size: 1.8rem;
    font-weight: 900;
    letter-spacing: -1px;
    flex-shrink: 0;
  }

  .search-wrap {
    flex: 1;
    max-width: 340px;
  }

  .search-wrap input {
    width: 100%;
    background: #111;
    border: 1px solid #444;
    color: #fff;
    padding: 8px 14px;
    border-radius: 4px;
    font-size: 14px;
    transition: border-color 0.15s;
  }
  .search-wrap input:focus { outline: none; border-color: #fff; }
  .search-wrap input::placeholder { color: #666; }

  .manage-link {
    margin-left: auto;
    font-size: 13px;
    color: #ccc;
    padding: 6px 12px;
    border: 1px solid #444;
    border-radius: 4px;
    transition: color 0.15s, border-color 0.15s;
  }
  .manage-link:hover { color: #fff; border-color: #888; }

  main { padding: 0 0 60px; }

  .loading { text-align: center; padding: 80px; color: #666; }

  /* Search results */
  .search-header {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 24px 48px 16px;
    font-size: 14px;
    color: #aaa;
  }
  .search-header strong { color: #fff; }
  .clear {
    background: none;
    border: 1px solid #444;
    color: #999;
    padding: 4px 10px;
    border-radius: 3px;
    font-size: 12px;
  }
  .clear:hover { color: #fff; border-color: #888; }

  .grid {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    padding: 0 48px;
  }
  .grid .card { width: 180px; }
  .no-results { color: #666; padding: 40px 48px; }

  /* Rows */
  .row { margin-bottom: 32px; }

  .row h2 {
    padding: 0 48px;
    margin: 0 0 12px;
    font-size: 1.1rem;
    font-weight: 600;
    color: #e5e5e5;
  }

  .row-wrap {
    position: relative;
    display: flex;
    align-items: center;
  }

  .arrow {
    position: absolute;
    z-index: 10;
    background: rgba(0,0,0,0.7);
    border: none;
    color: #fff;
    font-size: 2rem;
    width: 44px;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0;
    transition: opacity 0.2s;
    padding: 0;
  }
  .arrow.left { left: 0; }
  .arrow.right { right: 0; }
  .row-wrap:hover .arrow { opacity: 1; }
  .arrow:hover { background: rgba(0,0,0,0.9); }

  .cards {
    display: flex;
    gap: 6px;
    overflow-x: auto;
    padding: 8px 48px 16px;
    scrollbar-width: none;
  }
  .cards::-webkit-scrollbar { display: none; }

  /* Cards */
  .card {
    flex-shrink: 0;
    width: 200px;
    cursor: pointer;
    border-radius: 4px;
    overflow: hidden;
    transition: transform 0.2s, box-shadow 0.2s;
    position: relative;
    z-index: 1;
  }
  .card:hover {
    transform: scale(1.08);
    z-index: 10;
    box-shadow: 0 8px 24px rgba(0,0,0,0.8);
  }

  .thumb {
    position: relative;
    aspect-ratio: 16/9;
    background: #222;
    overflow: hidden;
  }
  .thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
    transition: opacity 0.2s;
  }
  .play-icon {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 2rem;
    opacity: 0;
    background: rgba(0,0,0,0.4);
    transition: opacity 0.2s;
  }
  .card:hover .play-icon { opacity: 1; }

  .card-name {
    font-size: 12px;
    color: #ccc;
    padding: 6px 4px 2px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    background: #1a1a1a;
  }

  .card-info { background: #1a1a1a; padding: 0 4px 6px; }
  .card-dir { font-size: 11px; color: #666; display: block; }

  /* Conversion panel */
  .conv-panel {
    position: fixed;
    bottom: 24px;
    right: 24px;
    width: 320px;
    background: #1a1a1a;
    border: 1px solid #333;
    border-radius: 8px;
    overflow: hidden;
    z-index: 200;
    box-shadow: 0 8px 32px rgba(0,0,0,0.6);
  }

  .conv-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 14px;
    background: #222;
    border-bottom: 1px solid #333;
  }

  .conv-title {
    font-size: 12px;
    font-weight: 600;
    color: #ccc;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .conv-count {
    background: #e50914;
    color: #fff;
    font-size: 11px;
    font-weight: 700;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .conv-item {
    padding: 10px 14px;
    border-bottom: 1px solid #252525;
  }
  .conv-item:last-child { border-bottom: none; }

  .conv-name {
    font-size: 12px;
    color: #ddd;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    margin-bottom: 6px;
  }

  .conv-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .conv-bar {
    flex: 1;
    height: 4px;
    background: #333;
    border-radius: 2px;
    overflow: hidden;
  }

  .conv-fill {
    height: 100%;
    background: #e50914;
    border-radius: 2px;
    transition: width 0.5s ease;
    min-width: 2px;
  }

  .conv-pct {
    font-size: 11px;
    color: #888;
    width: 30px;
    text-align: right;
    flex-shrink: 0;
  }
</style>
