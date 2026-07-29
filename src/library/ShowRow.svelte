<script lang="ts">
  import { clean, vidURL } from "../core/video";
  import type { SeasonEntry, ContinueItem } from "./homeData.svelte";

  let {
    title,
    key,
    seasons,
    continues,
    downloadedSet,
  }: {
    title: string;
    key: string;
    seasons: SeasonEntry[];
    continues: ContinueItem[];
    downloadedSet: Set<string>;
  } = $props();

  let picked = $state<string | null>(null);

  function stored(): { dir: string; at: number } | null {
    try {
      const s = JSON.parse(localStorage.getItem(`season:${key}`) ?? "null");
      return s?.dir ? s : null;
    } catch {
      return null;
    }
  }

  // Freshest signal wins: an explicit pill click this session, else the most
  // recent of {continue-watching entry, last stored pick}, else season one.
  const sel = $derived.by(() => {
    if (picked && seasons.some((s) => s.dir === picked)) return picked;
    let dir = "";
    let at = -1;
    const c = continues.find((c) => seasons.some((s) => s.dir === c.dir));
    if (c) {
      dir = c.dir;
      at = c.at;
    }
    const st = stored();
    if (st && st.at > at && seasons.some((s) => s.dir === st.dir)) dir = st.dir;
    return dir || seasons[0].dir;
  });
  const season = $derived(seasons.find((s) => s.dir === sel) ?? seasons[0]);
  const noun = $derived(seasons.every((s) => s.n < 1000) ? "seasons" : "parts");

  function pick(dir: string) {
    picked = dir;
    localStorage.setItem(`season:${key}`, JSON.stringify({ dir, at: Date.now() }));
  }

  const short = (label: string) =>
    label.replace(/^Season /, "S").replace(/^Ep /, "");

  function scrollRow(el: HTMLElement, dir: number) {
    el.scrollBy({ left: dir * 400, behavior: "smooth" });
  }
</script>

<section class="row">
  <div class="head f al-ct g10">
    <h2 class="m0">{title}</h2>
    <span class="count fs-xs tx-1">{seasons.length} {noun}</span>
    <div class="pills f al-ct g5">
      {#each seasons as s (s.dir)}
        <button
          class="pill fs-xs ptr"
          class:on={s.dir === sel}
          title={s.label}
          onclick={() => pick(s.dir)}
        >
          {short(s.label)}
        </button>
      {/each}
    </div>
  </div>

  <div class="scroll-row f al-ct p-rel">
    <button
      class="scroll-arrow cc h-100 p0 left"
      onclick={(e) => {
        const el: any = e.currentTarget.nextElementSibling;
        scrollRow(el, -1);
      }}>‹</button
    >

    {#key season.dir}
      <div class="cards flow-x-s g5 f">
        {#each season.files as f, idx}
          {@const vidParam = `${season.dir}/${f.name}`}
          <a
            href={vidURL(season.dir, f.name)}
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
    {/key}

    <button
      class="scroll-arrow cc h-100 p0 right"
      onclick={(e) => {
        const el: any = e.currentTarget.previousElementSibling;
        scrollRow(el, 1);
      }}>›</button
    >
  </div>
</section>

<style>
  .row {
    margin-bottom: 36px;
  }
  .head {
    padding: 0 48px;
    margin-bottom: 14px;
    flex-wrap: wrap;
  }
  h2 {
    font-size: 22px;
  }
  .count {
    white-space: nowrap;
  }
  .pills {
    margin-left: auto;
  }
  .pill {
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-md);
    color: var(--tx-3);
    padding: 4px 12px;
    min-height: 28px;
    white-space: nowrap;
    transition:
      color 0.18s var(--ease-out),
      border-color 0.18s var(--ease-out);
  }
  .pill:hover {
    background: var(--glass-2);
    color: var(--tx-4);
  }
  .pill.on {
    border-color: var(--red);
    color: var(--tx-5);
    box-shadow: 0 0 10px var(--red-glow);
  }

  .cards {
    padding: 8px 48px 16px;
    scrollbar-width: none;
    animation: season-in 0.18s var(--ease-out);
  }
  @keyframes season-in {
    from {
      opacity: 0;
      transform: translateX(8px);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }

  @media (max-width: 640px) {
    .head {
      padding: 0 16px;
      margin-bottom: 8px;
    }
    h2 {
      font-size: 17px;
    }
    .pills {
      margin-left: 0;
      width: 100%;
      flex-wrap: nowrap;
      overflow-x: auto;
      scrollbar-width: none;
      padding: 4px 0;
    }
    .cards {
      padding: 6px 16px 12px;
    }
  }
</style>
