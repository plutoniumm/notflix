<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../core/api";
  import { clean } from "../core/video";
  import Icon from "../components/Icon.svelte";

  type Range = "week" | "month" | "year" | "all";

  type TopItem = { key: string; hours: number };
  type IPTile = {
    ip: string;
    user: string;
    device?: string;
    host?: string;
    os?: string;
    login?: string;
    tags?: string[];
    online?: boolean;
    label: string;
    hours: number;
    videos: number;
  };
  type GanttBar = {
    ip: string;
    user: string;
    label: string;
    vp: string;
    startHour: number;
    durHours: number;
  };
  type DayBars = { date: string; bars: GanttBar[] };
  type UserHeatmap = {
    user: string;
    ips: IPTile[];
    cells: string[][];     // 7×24 of dominant IP per cell ("" = empty)
    intensity: number[][]; // 7×24 of ping counts
  };
  type Summary = {
    range: Range;
    hours: number;
    videos: number;
    sessions: number;
    longestSessionMin: number;
    streakDays: number;
    activeDays: number;
    topShows: TopItem[];
    topIPs: IPTile[];
    gantt: DayBars[];
    userHeatmaps: UserHeatmap[];
    filteredIP?: string;
    dailyMinutes: number[];
    firstAt: number;
    lastAt: number;
  };

  let range = $state<Range>("year");
  let activeIP = $state<string>(""); // "" = everyone
  let data = $state<Summary | null>(null);
  let loading = $state(true);

  async function load() {
    loading = true;
    data = (await api.stats.summary(range, activeIP || undefined)) as Summary | null;
    loading = false;
  }

  onMount(load);
  $effect(() => {
    void range;
    void activeIP;
    load();
  });

  // Stable colour per IP — same IP gets the same hue across daily rows.
  // djb2 → 360° hue; saturation/lightness held constant for the warm UI.
  function ipColour(ip: string): string {
    let h = 5381;
    for (let i = 0; i < ip.length; i++) h = ((h << 5) + h + ip.charCodeAt(i)) | 0;
    const hue = Math.abs(h) % 360;
    return `hsl(${hue}, 70%, 58%)`;
  }

  // One-char OS glyph for compact chips. Tailscale gives us the OS string
  // in a small set: macOS, iOS, android, windows, linux, freebsd. Unknown
  // → empty (the chip just omits the dot rather than showing "?").
  function osGlyph(os?: string): string {
    switch ((os || "").toLowerCase()) {
      case "macos": return "";
      case "ios": return "";
      case "android": return "🤖";
      case "windows": return "⊞";
      case "linux": return "🐧";
      case "freebsd": return "😈";
      default: return "";
    }
  }

  function fmtDayLabel(iso: string): string {
    // "2026-05-25" → "May 25"
    const [_, m, d] = iso.split("-").map(Number);
    return new Date(2026, m - 1, d).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
    });
  }
  function fmtBarTip(b: GanttBar): string {
    const start = `${String(Math.floor(b.startHour)).padStart(2, "0")}:${String(
      Math.floor((b.startHour % 1) * 60),
    ).padStart(2, "0")}`;
    const mins = Math.round(b.durHours * 60);
    return `${b.label} · ${b.vp.split("/").pop()} · ${start} · ${mins}min`;
  }

  // Wrapped-y headline: "X hours" with a smart unit.
  function fmtHours(h: number): { n: string; unit: string } {
    if (h < 1) return { n: Math.round(h * 60).toString(), unit: "minutes" };
    if (h < 100) return { n: h.toFixed(1), unit: "hours" };
    return { n: Math.round(h).toString(), unit: "hours" };
  }

  // dir name → leaf path component, prettified.
  function dirLabel(k: string): string {
    if (k === "." || k === "") return "Movies";
    const last = k.split("/").pop() || k;
    return clean(last) || last;
  }

  // Per-cell alpha against a per-user max — each grid breathes at its own
  // scale instead of being dominated by the heaviest user's peaks.
  function heatAlpha(v: number, maxV: number): number {
    if (!v) return 0;
    return 0.18 + 0.82 * (Math.log1p(v) / Math.log1p(maxV));
  }

  let dailyMax = $derived(
    data ? Math.max(1, ...data.dailyMinutes) : 1,
  );

  const DOW = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  const HOURS = Array.from({ length: 24 }, (_, i) => i);

  // For the daily strip: render the last 90 days max so the chart doesn't go fuzzy.
  let dailyTail = $derived(
    data ? data.dailyMinutes.slice(-90) : [],
  );

  let heroNum = $derived(data ? fmtHours(data.hours) : { n: "0", unit: "minutes" });
  let showsMax = $derived(data && data.topShows.length ? data.topShows[0].hours || 1 : 1);
</script>

<div style="min-height: 100vh;">
  <header class="glass-header f al-ct g20">
    <a href="/">
      <img src="/assets/tight.svg" alt="Notflix" height="50" />
    </a>
    <h1 class="title">Your Stats</h1>
    <nav class="actions f al-ct g8">
      <div class="range-pick f g5">
        {#each ["week", "month", "year", "all"] as r}
          <button
            class="rbtn fs-xs ptr rx2"
            class:active={range === r}
            onclick={() => (range = r as Range)}
          >
            {r}
          </button>
        {/each}
      </div>
      <a href="/" class="icon-btn glass glass-hover" aria-label="Home" title="Home">
        <Icon name="search" />
      </a>
    </nav>
  </header>

  <main class="pad">
    {#if loading}
      <div class="skel-grid">
        {#each Array(6) as _, i}
          <div class="skel-card" style="--i:{i}"></div>
        {/each}
      </div>
    {:else if !data || data.videos === 0}
      <div class="empty glass cc f-col g10">
        <div class="fs-lg">No watch data yet.</div>
        <div class="fs-xs tx-2">
          Play a video for more than a minute — stats start accruing after that.
        </div>
      </div>
    {:else}
      <!-- DEVICE CHIPS: one per distinct client IP in the window. Multi-IP
           users get "<user> · device" labels; click toggles the filter.
           Shown whenever there's any data so the unknown bucket is at
           least visible — users tend to want to know what their stats
           contain. -->
      {#if data.topIPs.length}
        <section class="userbar f al-ct g8 flow-x-s">
          <button
            class="chip ptr rx20 fs-xs"
            class:active={!activeIP}
            onclick={() => (activeIP = "")}
          >
            Everyone
            <span class="chip-h fs-xs">{data.hours.toFixed(1)}h</span>
          </button>
          {#each data.topIPs as t}
            <button
              class="chip ptr rx20 fs-xs"
              class:active={activeIP === t.ip}
              class:off={t.online === false}
              title={[
                t.host,
                t.login,
                t.tags?.length ? "tags: " + t.tags.join(", ") : "",
                t.ip,
                t.online === false ? "(last known: offline)" : "",
              ].filter(Boolean).join(" · ")}
              onclick={() => (activeIP = activeIP === t.ip ? "" : t.ip)}
            >
              <span class="chip-dot" style="background: {ipColour(t.ip)}"></span>
              {#if osGlyph(t.os)}<span class="chip-os">{osGlyph(t.os)}</span>{/if}
              {t.label}
              <span class="chip-h fs-xs">{t.hours.toFixed(1)}h</span>
            </button>
          {/each}
        </section>
      {/if}

      <!-- HERO: the big number. -->
      <section class="hero glass-strong">
        {#if activeIP}
          {@const tile = data.topIPs.find((x) => x.ip === activeIP)}
          <div class="hero-ctx fs-xs tx-2">
            Showing <strong>{tile?.label ?? activeIP}</strong>
            {#if tile?.os}<span class="tx-1"> · {tile.os}</span>{/if}
            {#if tile?.login}<span class="tx-1"> · {tile.login}</span>{/if}
            {#if tile?.ip && tile.ip !== "unknown"}<span class="tx-1"> · {tile.ip}</span>{/if}
          </div>
        {/if}
        <div class="hero-num">{heroNum.n}</div>
        <div class="hero-unit fs-lg tx-3">{heroNum.unit} watched</div>
        <div class="hero-sub fs-xs tx-2">
          {data.videos} {data.videos === 1 ? "video" : "videos"}
          · {data.sessions} {data.sessions === 1 ? "session" : "sessions"}
          · {data.activeDays} active {data.activeDays === 1 ? "day" : "days"}
          {#if data.streakDays > 1}· {data.streakDays}-day streak{/if}
        </div>
      </section>

      <!-- STAT TILES -->
      <section class="tiles">
        <div class="tile glass">
          <div class="tnum">{Math.round(data.longestSessionMin)}<span class="tu">min</span></div>
          <div class="tlbl fs-xs tx-2">Longest session</div>
        </div>
        <div class="tile glass">
          <div class="tnum">{data.streakDays}<span class="tu">day{data.streakDays === 1 ? "" : "s"}</span></div>
          <div class="tlbl fs-xs tx-2">Best streak</div>
        </div>
        <div class="tile glass">
          <div class="tnum">{(data.hours / Math.max(1, data.activeDays)).toFixed(1)}<span class="tu">h/day</span></div>
          <div class="tlbl fs-xs tx-2">Avg on active days</div>
        </div>
        <div class="tile glass">
          <div class="tnum">{Math.round(data.hours * 60 / Math.max(1, data.sessions))}<span class="tu">min</span></div>
          <div class="tlbl fs-xs tx-2">Avg session length</div>
        </div>
      </section>

      <!-- DAILY STRIP -->
      {#if dailyTail.length > 1}
        <section class="card glass">
          <h2 class="card-h">Daily minutes</h2>
          <div class="bars f al-end g2">
            {#each dailyTail as v, i}
              <div
                class="bar"
                style="height: {Math.max(2, (v / dailyMax) * 100)}%"
                title="{Math.round(v)} min"
                aria-label="day {i + 1}"
              ></div>
            {/each}
          </div>
        </section>
      {/if}

      <!-- GANTT: one row per day. Within the row, each session is a
           coloured bar positioned by hour-of-day, length = duration.
           Same IP keeps the same hue across days. -->
      {#if data.gantt.length}
        <section class="card glass">
          <h2 class="card-h">Sessions by day</h2>
          <div class="gantt">
            <div class="ghours fs-xs tx-2">
              {#each [0, 3, 6, 9, 12, 15, 18, 21, 24] as h}
                <span style="left: {(h / 24) * 100}%">{h}</span>
              {/each}
            </div>
            {#each data.gantt as day}
              <div class="grow">
                <div class="gdate fs-xs tx-3">{fmtDayLabel(day.date)}</div>
                <div class="gtrack">
                  {#each [6, 12, 18] as h}
                    <div class="gtick" style="left: {(h / 24) * 100}%"></div>
                  {/each}
                  {#each day.bars as b}
                    <div
                      class="gbar"
                      style="
                        left: {(b.startHour / 24) * 100}%;
                        width: max(2px, {(b.durHours / 24) * 100}%);
                        background: {ipColour(b.ip)};
                      "
                      title={fmtBarTip(b)}
                    ></div>
                  {/each}
                </div>
              </div>
            {/each}
          </div>
        </section>
      {/if}

      <!-- TOP SHOWS (kept — folders persist across deletions) -->
      {#if data.topShows.length}
        <section class="card glass">
          <h2 class="card-h">Top shows / folders</h2>
          <ul class="rank">
            {#each data.topShows as s, i}
              <li class="row">
                <div class="idx fs-xs tx-2">{i + 1}</div>
                <div class="rname flow-h">{dirLabel(s.key)}</div>
                <div class="rbar"><div class="rfill" style="width: {(s.hours / showsMax) * 100}%"></div></div>
                <div class="rval fs-xs tx-3">{s.hours.toFixed(1)}h</div>
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      <!-- PER-USER HEATMAPS: one 7×24 grid per user, cells coloured by
           the dominant device for that slot (same hue as the gantt + the
           chip dots). Cell alpha = log-scaled ping count vs that user's
           own max, so a quieter user still reads cleanly. -->
      {#each data.userHeatmaps as uh}
        {@const maxV = Math.max(1, ...uh.intensity.flat())}
        <section class="card glass">
          <h2 class="card-h f al-ct g10 j-bw">
            <span>When {uh.user} watches</span>
            <span class="legend f al-ct g8 fs-xs">
              {#each uh.ips ?? [] as ip}
                <span
                  class="leg-item f al-ct g5"
                  title={[ip.host, ip.os, ip.login, ip.ip].filter(Boolean).join(" · ")}
                >
                  <span class="leg-dot" style="background: {ipColour(ip.ip)}"></span>
                  {#if osGlyph(ip.os)}<span class="chip-os">{osGlyph(ip.os)}</span>{/if}
                  <span class="tx-3">{ip.device || ip.label}</span>
                </span>
              {/each}
            </span>
          </h2>
          <div class="heat">
            <div class="hcol haxis">
              <div class="hempty"></div>
              {#each DOW as d}
                <div class="hlabel fs-xs tx-2">{d}</div>
              {/each}
            </div>
            {#each HOURS as h}
              <div class="hcol">
                <div class="hhour fs-xs tx-2">{h % 6 === 0 ? h : ""}</div>
                {#each DOW as _, dow}
                  {@const v = uh.intensity[dow][h]}
                  {@const dom = uh.cells[dow][h]}
                  <div
                    class="hcell"
                    style="--a: {heatAlpha(v, maxV)}; --c: {dom ? ipColour(dom) : 'transparent'};"
                    title="{DOW[dow]} {h}:00 — {v} pings{dom ? ' · ' + dom : ''}"
                  ></div>
                {/each}
              </div>
            {/each}
          </div>
        </section>
      {/each}
    {/if}
  </main>
</div>

<style>
  header {
    padding: 16px 48px;
  }
  .title {
    margin: 0;
    font-family: var(--font-display);
    color: var(--tx-5);
    font-size: 22px;
    font-weight: 600;
  }
  .actions {
    margin-left: auto;
  }
  .icon-btn {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    color: var(--tx-4);
    border-radius: var(--r-md);
  }

  .range-pick {
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-md);
    padding: 3px;
  }
  .rbtn {
    background: transparent;
    border: 0;
    color: var(--tx-3);
    padding: 5px 10px;
    text-transform: capitalize;
    border-radius: var(--r-sm);
  }
  .rbtn.active {
    background: linear-gradient(135deg, var(--red) 0%, #ff7849 100%);
    color: #fff;
    box-shadow: 0 4px 12px -2px var(--red-glow);
  }

  .pad {
    padding: 24px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .skel-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 12px;
  }
  .skel-card {
    height: 120px;
    border-radius: var(--r-lg);
    background: linear-gradient(90deg, var(--bg-3) 0%, var(--bg-4) 40%, var(--bg-3) 80%);
    background-size: 400px 100%;
    opacity: 0;
    animation:
      shimmer 1.6s linear infinite,
      fade-in 0.3s ease forwards;
    animation-delay: calc(var(--i) * 60ms), calc(var(--i) * 60ms);
  }

  .empty {
    min-height: 200px;
    padding: 40px;
    border-radius: var(--r-lg);
    border: 1px solid var(--glass-bd);
    color: var(--tx-4);
  }

  .userbar {
    padding: 4px 2px;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .userbar::-webkit-scrollbar {
    display: none;
  }
  .chip {
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    color: var(--tx-3);
    padding: 7px 14px;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    transition: background 0.15s var(--ease-out), color 0.15s var(--ease-out),
      border-color 0.15s var(--ease-out);
  }
  .chip:hover {
    color: var(--tx-5);
    background: var(--glass-2);
    border-color: rgba(255, 255, 255, 0.16);
  }
  .chip.active {
    background: linear-gradient(135deg, var(--red) 0%, #ff7849 100%);
    color: #fff;
    border-color: transparent;
    box-shadow: 0 4px 12px -2px var(--red-glow);
  }
  .chip-h {
    opacity: 0.7;
    font-weight: 400;
  }
  .chip-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .chip-os {
    font-size: 12px;
    line-height: 1;
    opacity: 0.85;
  }
  .chip.off {
    opacity: 0.55;
  }
  .chip.off:hover {
    opacity: 0.85;
  }
  .hero-ctx {
    margin-bottom: 10px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }
  .hero-ctx strong {
    color: var(--tx-4);
    font-weight: 600;
  }

  .hero {
    padding: 40px 24px 36px;
    text-align: center;
    border-radius: var(--r-xl);
  }
  .hero-num {
    font-family: var(--font-display);
    font-size: 96px;
    font-weight: 700;
    line-height: 1;
    background: linear-gradient(135deg, var(--red) 0%, #ff7849 100%);
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
  }
  .hero-unit {
    margin-top: 6px;
    font-weight: 500;
  }
  .hero-sub {
    margin-top: 12px;
  }

  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 12px;
  }
  .tile {
    padding: 18px 20px;
    border-radius: var(--r-lg);
    border: 1px solid var(--glass-bd);
  }
  .tnum {
    font-family: var(--font-display);
    font-size: 36px;
    font-weight: 600;
    color: var(--tx-5);
    line-height: 1.1;
  }
  .tu {
    font-size: 14px;
    font-weight: 400;
    color: var(--tx-2);
    margin-left: 6px;
  }
  .tlbl {
    margin-top: 4px;
  }

  .card {
    padding: 20px 22px;
    border-radius: var(--r-lg);
    border: 1px solid var(--glass-bd);
  }
  .card-h {
    margin: 0 0 14px;
    font-family: var(--font-display);
    font-size: 16px;
    font-weight: 600;
    color: var(--tx-4);
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .bars {
    height: 80px;
  }
  .bar {
    flex: 1;
    min-width: 3px;
    background: linear-gradient(180deg, #ff7849 0%, var(--red) 100%);
    border-radius: 2px 2px 0 0;
    opacity: 0.85;
    transition: opacity 0.15s;
  }
  .bar:hover {
    opacity: 1;
  }

  /* Gantt */
  .gantt {
    display: flex;
    flex-direction: column;
    gap: 4px;
    position: relative;
  }
  .ghours {
    position: relative;
    height: 14px;
    margin-left: 56px;
    color: var(--tx-1);
  }
  .ghours span {
    position: absolute;
    transform: translateX(-50%);
    font-size: 10px;
  }
  .grow {
    display: grid;
    grid-template-columns: 52px 1fr;
    align-items: center;
    gap: 4px;
  }
  .gdate {
    text-align: right;
    padding-right: 4px;
    white-space: nowrap;
  }
  .gtrack {
    position: relative;
    height: 14px;
    background: var(--bg-3);
    border-radius: 3px;
    overflow: hidden;
  }
  .gtick {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 1px;
    background: rgba(255, 255, 255, 0.04);
  }
  .gbar {
    position: absolute;
    top: 1px;
    bottom: 1px;
    border-radius: 2px;
    opacity: 0.88;
    transition: opacity 0.15s, transform 0.15s var(--ease-out);
    cursor: default;
  }
  .gbar:hover {
    opacity: 1;
    transform: scaleY(1.18);
    z-index: 2;
  }

  .rank {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .row {
    display: grid;
    grid-template-columns: 20px 1fr 120px 50px;
    gap: 10px;
    align-items: center;
  }
  .idx {
    text-align: right;
  }
  .rname {
    color: var(--tx-4);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    text-decoration: none;
  }
  .rbar {
    height: 6px;
    background: var(--bg-3);
    border-radius: 3px;
    overflow: hidden;
  }
  .rfill {
    height: 100%;
    background: linear-gradient(90deg, var(--red) 0%, #ff7849 100%);
    border-radius: 3px;
  }
  .rval {
    text-align: right;
  }
  @media (max-width: 640px) {
    .row {
      grid-template-columns: 20px 1fr 60px 45px;
    }
  }

  .heat {
    display: grid;
    grid-template-columns: 38px repeat(24, 1fr);
    gap: 3px;
  }
  .hcol {
    display: grid;
    grid-template-rows: 14px repeat(7, 1fr);
    gap: 3px;
  }
  .haxis {
    color: var(--tx-2);
  }
  .hempty {
    height: 14px;
  }
  .hhour {
    height: 14px;
    text-align: center;
    line-height: 14px;
  }
  .hlabel {
    height: 18px;
    line-height: 18px;
    text-align: right;
    padding-right: 4px;
  }
  .hcell {
    height: 18px;
    border-radius: 3px;
    /* color comes from --c (per-cell dominant IP); alpha from --a.
       color-mix lets us re-mix the hue with the bg at the cell's alpha
       without losing the underlying contrast. */
    background: color-mix(in srgb, var(--c, transparent) calc(var(--a, 0) * 100%), transparent);
    border: 1px solid rgba(255, 255, 255, 0.04);
  }
  .legend {
    flex-wrap: wrap;
    text-transform: none;
    letter-spacing: 0;
    font-weight: 400;
  }
  .leg-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  @media (max-width: 640px) {
    header {
      padding: 10px 16px;
      gap: 10px;
    }
    .pad {
      padding: 16px 14px 40px;
      gap: 14px;
    }
    .hero-num {
      font-size: 64px;
    }
    .heat {
      grid-template-columns: 28px repeat(24, 1fr);
      gap: 2px;
    }
    .hcell, .hhour, .hempty {
      height: 12px;
    }
    .hlabel {
      height: 12px;
      line-height: 12px;
      font-size: 9px;
    }
  }
</style>
