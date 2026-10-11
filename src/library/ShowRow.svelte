<script lang="ts">
  import { local } from "../core/store";
  import SeriesStrip from "./SeriesStrip.svelte";
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

  const seen = $derived(
    local<{ dir: string; at: number } | null>(`season:${key}`, null),
  );

  function stored() {
    const s = seen.get();

    return s?.dir ? s : null;
  }

  // Freshest signal wins: a pill click this session, else the more recent of
  // {continue-watching entry, stored pick}, else season one.
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
    seen.set({ dir, at: Date.now() });
  }

  const short = (label: string) =>
    label.replace(/^Season /, "S").replace(/^Ep /, "");
</script>

<section class="row">
  <div class="head f al-ct g10">
    <h2 class="m0">{title}</h2>
    <span class="count fs-xs tx-1">{seasons.length} {noun}</span>
    <div class="pills f al-ct g2" role="tablist" aria-label="{title} seasons">
      {#each seasons as s (s.dir)}
        <button
          class="pill fs-xs ptr"
          class:on={s.dir === sel}
          role="tab"
          aria-selected={s.dir === sel}
          title={s.label}
          onclick={() => pick(s.dir)}
        >
          {short(s.label)}
        </button>
      {/each}
    </div>
  </div>

  {#key season.dir}
    <SeriesStrip
      dir={season.dir}
      files={season.files}
      {continues}
      {downloadedSet}
    />
  {/key}
</section>

<style>
  .row {
    margin-bottom: 40px;
  }
  .head {
    padding: 0 48px;
    margin-bottom: 12px;
    flex-wrap: wrap;
  }
  h2 {
    font-size: 22px;
  }
  .count {
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .pills {
    margin-left: auto;
    padding: 3px;
    border-radius: 999px;
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
  }
  .pill {
    border-radius: 999px;
    color: var(--tx-2);
    padding: 4px 12px;
    min-height: 26px;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
    transition:
      color var(--motion-fast, 0.18s) var(--ease-out),
      background var(--motion-fast, 0.18s) var(--ease-out);
  }
  .pill:hover {
    color: var(--tx-4);
    background: var(--glass-2);
  }
  .pill.on {
    color: var(--tx-5);
    background: color-mix(in oklab, var(--red) 78%, transparent);
    box-shadow: 0 2px 10px -2px var(--red-glow);
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
      border-radius: var(--r-md);
    }
  }
</style>
