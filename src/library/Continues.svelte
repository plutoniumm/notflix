<script lang="ts">
  import { clean, vidURL } from "../core/video";

  type Item = { dir: string; name: string; key: string; t: number };

  let {
    items,
    downloadedSet,
    onRemove,
  }: {
    items: Item[];
    downloadedSet: Set<string>;
    onRemove: (e: Event, dir: string, name: string) => void;
  } = $props();

  function scrollSibling(e: Event, dir: number) {
    const btn = e.currentTarget as HTMLElement;
    const el = (dir < 0 ? btn.nextElementSibling : btn.previousElementSibling) as HTMLElement | null;
    el?.scrollBy({ left: dir * 400, behavior: "smooth" });
  }

  function fmt(t: number) {
    const h = Math.floor(t / 3600);
    return h > 0 ? `${h}h ${Math.floor((t % 3600) / 60)}m` : `${Math.floor(t / 60)}m`;
  }

  function vidParam(item: Item) {
    return item.dir === "." ? item.name : `${item.dir}/${item.name}`;
  }
</script>

{#if items.length > 0}
  <section class="row">
    <h2>Continue Watching</h2>
    <div class="scroll-row f al-ct p-rel">
      <button class="scroll-arrow cc h-100 p0 left" onclick={(e) => scrollSibling(e, -1)}>‹</button>
      <div class="cards f flow-x-s g5">
        {#each items as item, idx}
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
              {#if downloadedSet.has(vidParam(item))}
                <div class="dot p-abs"></div>
              {/if}
              <!-- svelte-ignore a11y_consider_explicit_label -->
              <button
                class="cw-remove p-abs cc o-0 ptr"
                onclick={(e) => onRemove(e, item.dir, item.name)}>✕</button
              >
              <div class="cw-bar p-abs"></div>
            </div>
            <div class="card-name">{clean(item.name)}</div>
            <div class="cw-time fs-xs">{fmt(item.t)}</div>
          </a>
        {/each}
      </div>
      <button class="scroll-arrow cc h-100 p0 right" onclick={(e) => scrollSibling(e, 1)}>›</button>
    </div>
  </section>
{/if}

<style>
  .row {
    margin-bottom: 36px;
  }
  .row h2 {
    padding: 0 48px;
    margin: 0 0 14px;
    color: var(--tx-5);
    font-size: 22px;
    font-weight: 600;
  }
  @media (max-width: 640px) {
    .row h2 {
      padding: 0 16px;
      font-size: 17px;
    }
    .cards {
      padding: 6px 16px 12px;
    }
  }

  .cards {
    padding: 8px 48px 16px;
    scrollbar-width: none;
  }
  :global(.media-card) {
    width: 220px;
  }

  .cw-remove {
    top: 6px;
    right: 6px;
    width: 24px;
    height: 24px;
    font-size: 12px;
    background: rgba(13, 11, 18, 0.7);
    backdrop-filter: blur(8px);
    color: var(--tx-3);
    border-radius: 50%;
    border: 1px solid var(--glass-bd);
    z-index: 2;
    transition: opacity 0.15s, background 0.18s var(--ease-out),
      color 0.18s var(--ease-out), transform 0.16s var(--ease-snap);
  }
  :global(.media-card):hover .cw-remove {
    opacity: 1;
  }
  .cw-remove:hover {
    background: var(--red);
    color: #fff;
    border-color: var(--red);
  }
  .cw-remove:active {
    transform: scale(0.88);
  }

  .cw-bar {
    bottom: 0;
    left: 0;
    right: 0;
    height: 3px;
    background: linear-gradient(90deg, var(--red) 0%, #ff7849 100%);
    box-shadow: 0 0 8px var(--red-glow);
  }

  .cw-time {
    color: var(--tx-2);
    padding: 0 10px 8px;
    background: var(--bg-3);
    border-radius: 0 0 var(--r-lg) var(--r-lg);
  }
</style>
