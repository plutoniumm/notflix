<script lang="ts">
  import { onMount } from "svelte";
  import { send, watchState } from "./bus";
  import { GET } from "../core/api";
  import { dur as fmt } from "../core/fmt";
  import { matches } from "../core/fuzzy";
  import { join } from "../core/path";
  import { clean, thumbFail } from "../core/video";

  type State = {
    file: string;
    title: string;
    pos: number;
    dur: number;
    paused: boolean;
    volume: number;
    screens: number;
  };

  type Item = { file: string; dir: string; name: string; key: string; title: string };

  let st = $state<State>({
    file: "",
    title: "",
    pos: 0,
    dur: 0,
    paused: true,
    volume: 1,
    screens: 0,
    muted: false,
    full: false,
    subs: [],
    sub: -1,
  });

  let items = $state<Item[]>([]);
  let err = $state("");
  let q = $state("");
  // Until the bus has answered once, a screen count of 0 is ignorance, not
  // news — claiming "no screen" on first paint would cry wolf every load.
  let seen = $state(false);

  // Two modes, not two pages. Once something is playing the remote IS the
  // transport — a one-handed thing in a dark room — and the library is a place
  // you visit to change what that transport is driving. Browsing is an
  // explicit, reversible detour, so Back never stops playback.
  let browsing = $state(false);
  const playing = $derived(!!st.file);
  const nowView = $derived(playing && !browsing);

  // A drag is the thumb's truth; incoming state must not fight it mid-gesture.
  let drag = $state<number | null>(null);
  let vdrag = $state<number | null>(null);
  // The screen reports position on its own clock, so the seek we just sent
  // reads as stale for one report. Hold the commanded value until the screen
  // confirms it, or 1.5s passes.
  // ponytail: a time-based fallback, not a command ack — the bus has no ack and
  // one would be a protocol change for a cosmetic snap-back.
  let pend = $state<{ v: number; at: number } | null>(null);

  const SHOW = 60;

  const hits = $derived(items.filter((i) => matches(q, i.title, i.name, i.dir)));
  const list = $derived(hits.slice(0, SHOW));

  const pos = $derived(
    drag !== null
      ? drag
      : pend && Date.now() - pend.at < 1500
        ? pend.v
        : st.pos,
  );
  const vol = $derived(vdrag ?? st.volume);
  const seekable = $derived(st.dur > 0);
  const title = $derived(st.title || clean(st.file.split("/").pop() ?? ""));

  async function go(cmd: Record<string, unknown>) {
    // The fanout count is the only honest answer to "did that do anything?",
    // and it arrives even when the screen never reports state back.
    const n = (await send(cmd as any)) ?? 0;
    st.screens = n;
    seen = true;

    return n;
  }

  function toggle() {
    if (!st.file) return;
    const next = !st.paused;
    st.paused = next;
    // Optimistic, then honest: with no screen listening nothing changed, and a
    // play icon that flipped anyway is the lie this remote must not tell.
    go({ cmd: next ? "pause" : "resume" }).then((n) => {
      if (!n) st.paused = !next;
    });
  }

  function play(i: Item) {
    st = { ...st, file: i.file, title: i.title, pos: 0, dur: 0, paused: false };
    pend = null;
    go({ cmd: "play", file: i.file });
  }

  function by(d: number) {
    if (!seekable) return;
    pend = { v: Math.max(0, Math.min(st.dur, pos + d)), at: Date.now() };
    go({ cmd: "seekBy", d });
  }

  function scrub(e: Event) {
    drag = +(e.currentTarget as HTMLInputElement).value;
  }

  // change fires once, on release, for both touch and mouse — one seek per
  // gesture. An input-driven seek restarts the screen's segment fetch dozens
  // of times a second (see src/player/dock.svelte).
  function commit(e: Event) {
    const v = +(e.currentTarget as HTMLInputElement).value;
    drag = null;
    pend = { v, at: Date.now() };
    go({ cmd: "seek", t: v });
  }

  function volIn(e: Event) {
    vdrag = +(e.currentTarget as HTMLInputElement).value;
  }

  function volSet(e: Event) {
    const v = +(e.currentTarget as HTMLInputElement).value;
    vdrag = v;
    go({ cmd: "volume", v });
  }

  onMount(() => {
    GET("/list/video", { silent: true }).then((data: VideoData | null) => {
      if (!data) {
        err = "Could not reach the library";

        return;
      }
      items = data.flatMap((g) =>
        (g.files ?? []).map((f) => ({
          file: join(g.dir, f.name),
          dir: g.dir,
          name: f.name,
          key: f.key,
          title: clean(f.name),
        })),
      );
    });

    return watchState((s: State) => {
      if (pend && Math.abs(s.pos - pend.v) < 2) pend = null;
      if (vdrag !== null && Math.abs(s.volume - vdrag) < 0.02) vdrag = null;
      st = s;
      seen = true;
    });
  });
</script>

<div class="rc" class:now-only={nowView}>
  <header>
    {#if playing}
      <button
        class="nav"
        aria-label={browsing ? "Back to what's playing" : "Browse the library"}
        onclick={() => (browsing = !browsing)}
      >
        {browsing ? "‹ Now playing" : "Library"}
      </button>
    {/if}
    <input
      class="q"
      type="search"
      inputmode="search"
      placeholder={items.length ? `Search ${items.length} titles` : "Search"}
      bind:value={q}
    />
    <span class="screens" class:dead={seen && st.screens === 0}>
      {!seen
        ? "…"
        : st.screens === 0
          ? "no screen"
          : `${st.screens} screen${st.screens > 1 ? "s" : ""}`}
    </span>
  </header>

  {#if seen && st.screens === 0}
    <p class="warn">No screen is listening. Open notflix on the TV — taps do nothing until it connects.</p>
  {/if}
  {#if err}
    <p class="warn">{err}</p>
  {/if}

  <ul class="list">
    {#each list as i (i.file)}
      <li>
        <button class="row" class:now={i.file === st.file} onclick={() => { play(i); browsing = false; }}>
          <img src="/images/{i.key}.jpg" alt="" loading="lazy" onerror={thumbFail} />
          <span class="meta">
            <span class="name">{i.title || i.name}</span>
            {#if i.dir !== "."}<span class="dir">{i.dir}</span>{/if}
          </span>
        </button>
      </li>
    {/each}
    {#if hits.length > SHOW}
      <li class="more">{hits.length - SHOW} more — keep typing</li>
    {:else if !hits.length && items.length}
      <li class="more">Nothing matches “{q}”</li>
    {/if}
  </ul>

  <section class="dock">
    <div class="np">{st.file ? title : "Nothing playing"}</div>

    {#if playing && st.subs?.length}
      <!-- A TV has no subtitle menu, so this is the only place the choice can
           be made. Shown only when the title actually has tracks: an empty row
           of nothing is worse than no row. -->
      <div class="subs">
        <button class="sub" class:on={st.sub === -1} onclick={() => send({ cmd: "sub", sub: -1 })}>
          Off
        </button>
        {#each st.subs as label, i}
          <button class="sub" class:on={st.sub === i} onclick={() => send({ cmd: "sub", sub: i })}>
            {label}
          </button>
        {/each}
      </div>
    {/if}

    {#if playing}
      <button
        class="full"
        class:on={st.full}
        onclick={() => send({ cmd: "fullscreen" })}
      >
        {st.full ? "Exit full screen" : "Full screen"}
      </button>
    {/if}

    {#if st.muted && playing}
      <button class="unmute" onclick={() => send({ cmd: "unmute" })}>
        Muted — tap for sound
      </button>
    {/if}

    <input
      class="bar"
      type="range"
      min="0"
      max={st.dur || 1}
      step="1"
      value={pos}
      disabled={!seekable}
      aria-label="Position"
      style="--p:{seekable ? (pos / st.dur) * 100 : 0}%"
      oninput={scrub}
      onchange={commit}
    />

    <div class="times">
      <span>{fmt(pos)}</span>
      <span>{seekable ? fmt(st.dur) : "--:--"}</span>
    </div>

    <div class="pad">
      <!-- Two, not four. Asymmetric on purpose: back is for "what did they
           say", which is seconds, and forward is for skipping something, which
           is not. Four buttons on a phone is four things to aim at in the dark
           to save one extra tap. -->
      <button class="jump" disabled={!seekable} onclick={() => by(-10)}>−10</button>
      <button class="big" disabled={!st.file} onclick={toggle} aria-label={st.paused ? "Play" : "Pause"}>
        {#if st.paused}
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 5v14l11-7z" /></svg>
        {:else}
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 5h4v14H6zm8 0h4v14h-4z" /></svg>
        {/if}
      </button>
      <button class="jump" disabled={!seekable} onclick={() => by(30)}>+30</button>
    </div>

    <div class="tail">
      <button class="stop" onclick={() => go({ cmd: "stop" })} aria-label="Stop">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 6h12v12H6z" /></svg>
      </button>

      <svg class="spk" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M3 10v4h4l5 4V6L7 10H3z" />
      </svg>
      <input
        class="bar vol"
        type="range"
        min="0"
        max="1"
        step="0.02"
        value={vol}
        aria-label="Volume"
        style="--p:{vol * 100}%"
        oninput={volIn}
        onchange={volSet}
      />
      <span class="pctn">{Math.round(vol * 100)}</span>
    </div>
  </section>
</div>

<style>
  /* Now-playing view: the list is not hidden behind the dock, it is GONE, and
     the dock stops being a dock. A transport you are holding in the dark wants
     the whole screen; a scrubber that is 40% of the way up a phone is a
     scrubber you miss. */
  .rc.now-only .list {
    display: none;
  }
  /* The search box is a library control. In now-view it is a dead 44px strip
     across the top of a one-handed transport, and a keyboard you can summon by
     mis-tapping while reaching for Back. */
  .rc.now-only .q {
    display: none;
  }
  .rc.now-only header {
    border-bottom: none;
  }
  .rc.now-only .nav {
    flex: 1;
    text-align: left;
  }
  .rc.now-only .dock {
    position: static;
    flex: 1;
    display: flex;
    flex-direction: column;
    /* Bottom-weighted, not centred: this is held low in one hand, and the
       buttons that get pressed most should sit where the thumb already is. */
    justify-content: flex-end;
    padding-bottom: max(28px, env(safe-area-inset-bottom));
    gap: 20px;
    border-top: none;
  }
  .rc.now-only .np {
    font-size: 22px;
    line-height: 1.25;
    text-align: center;
  }

  .nav {
    flex: 0 0 auto;
    min-height: 44px;
    padding: 0 12px;
    border: none;
    border-radius: var(--r-md, 10px);
    background: color-mix(in oklab, var(--tx-5, #fff) 8%, transparent);
    color: var(--tx-4, #ddd);
    font: inherit;
    font-size: 14px;
  }

  /* Requesting fullscreen wants a gesture in the TV's own document, which a
     relayed tap is not. It usually works anyway on a TV browser; when it does
     not, st.full stays false and this button keeps saying "Full screen" rather
     than lying about it. */
  .subs {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 8px;
  }
  .sub {
    min-height: 44px;
    padding: 0 16px;
    border: 1px solid var(--glass-bd);
    border-radius: 999px;
    background: var(--glass);
    color: var(--tx-3);
    font: inherit;
    font-size: 15px;
    font-weight: 600;
    letter-spacing: 0.03em;
  }
  .sub.on {
    color: #fff;
    background: var(--red);
    border-color: transparent;
  }

  .full {
    align-self: center;
    min-height: 44px;
    padding: 0 18px;
    border: 1px solid var(--glass-bd);
    border-radius: 999px;
    background: var(--glass);
    color: var(--tx-4);
    font: inherit;
    font-size: 15px;
  }
  .full.on {
    color: #fff;
    background: color-mix(in oklab, var(--red) 30%, transparent);
    border-color: transparent;
  }

  .unmute {
    align-self: center;
    min-height: 44px;
    padding: 0 18px;
    border: none;
    border-radius: 999px;
    background: var(--red, #e50914);
    color: #fff;
    font: inherit;
    font-size: 15px;
    font-weight: 600;
  }

  .rc {
    display: flex;
    flex-direction: column;
    height: 100vh;
    height: 100dvh;
    background: var(--bg-1, oklch(11.5% 0.012 300));
    color: var(--tx-5, #fff);
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: calc(10px + var(--safe-t, 0px)) 12px 10px;
    border-bottom: 1px solid var(--glass-bd);
  }

  .q {
    flex: 1;
    min-height: 44px;
    /* 16px or iOS zooms in on focus and never zooms back. */
    font-size: 16px;
    font-family: inherit;
    color: var(--tx-5);
    background: var(--bg-3);
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-md, 10px);
    padding: 0 12px;
  }

  .screens {
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    color: var(--tx-3);
    white-space: nowrap;
  }
  .screens.dead {
    color: var(--red, #f66);
  }

  .warn {
    margin: 0;
    padding: 10px 14px;
    font-size: 14px;
    line-height: 1.35;
    color: var(--tx-5);
    background: var(--red-soft, rgba(255, 90, 90, 0.16));
    border-bottom: 1px solid var(--red-glow);
  }

  .list {
    flex: 1;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    margin: 0;
    padding: 6px 0;
    list-style: none;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 60px;
    padding: 6px 12px;
    text-align: left;
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
  }
  .row:active {
    background: var(--active);
  }
  .row.now {
    background: var(--red-soft, rgba(255, 90, 90, 0.16));
  }

  .row img {
    width: 72px;
    height: 44px;
    object-fit: cover;
    border-radius: var(--r-sm, 6px);
    background: var(--bg-3);
    flex: none;
  }

  .meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .name {
    font-size: 15px;
    line-height: 1.2;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dir {
    font-size: 12px;
    color: var(--tx-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .more {
    padding: 14px 12px;
    font-size: 13px;
    color: var(--tx-2);
  }

  .dock {
    flex: none;
    padding: 12px 14px calc(12px + var(--safe-b, 0px));
    background: var(--bg-2, #16131c);
    border-top: 1px solid var(--glass-bd);
    box-shadow: var(--sh-3);
  }

  .np {
    font-family: var(--font-display, inherit);
    font-size: 17px;
    font-weight: 600;
    padding-bottom: 8px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* A 28px thumb on a 10px rail, inside a 44px-tall input: the rail is thin
     for looks, but a tap anywhere in the input's box moves the slider, so the
     touch target is the full 44px band and not the 10px line. */
  .bar {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 56px;
    margin: 0;
    background: none;
  }
  /* NOT grouped with the -moz- twin: one unknown pseudo-element invalidates
     the whole selector list, so a shared rule styles the track nowhere. */
  .bar::-webkit-slider-runnable-track {
    height: 14px;
    border-radius: 99px;
    background: linear-gradient(
      to right,
      var(--red, #f66) var(--p),
      var(--bg-4, #2a2633) var(--p)
    );
  }
  .bar::-moz-range-track {
    height: 14px;
    border-radius: 99px;
    background: linear-gradient(
      to right,
      var(--red, #f66) var(--p),
      var(--bg-4, #2a2633) var(--p)
    );
  }
  .bar::-webkit-slider-thumb {
    -webkit-appearance: none;
    width: 36px;
    height: 36px;
    margin-top: -9px;
    border-radius: 99px;
    border: 0;
    background: var(--tx-5, #fff);
    box-shadow: var(--sh-2);
  }
  .bar::-moz-range-thumb {
    width: 36px;
    height: 36px;
    border-radius: 99px;
    border: 0;
    background: var(--tx-5, #fff);
  }
  .bar:disabled {
    opacity: 0.4;
  }

  .times {
    display: flex;
    justify-content: space-between;
    font-size: 13px;
    font-variant-numeric: tabular-nums;
    color: var(--tx-3);
    padding-bottom: 10px;
  }

  .pad {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  /* 44px is the ACCESSIBILITY FLOOR, not a target size. This is used
     one-handed, in the dark, usually without looking at it — the thumb is
     aiming from memory, so the buttons are sized for that rather than for the
     minimum that passes a checklist. */
  .jump,
  .big,
  .stop {
    display: grid;
    place-items: center;
    min-width: 78px;
    min-height: 78px;
    color: var(--tx-5);
    background: var(--glass-2);
    border: 1px solid var(--glass-bd);
    border-radius: 99px;
    font: inherit;
    font-size: 21px;
    font-variant-numeric: tabular-nums;
    transition: transform var(--motion-fast, 130ms) var(--ease-spring);
  }
  .jump:active,
  .big:active,
  .stop:active {
    transform: scale(0.92);
  }
  .jump:disabled,
  .big:disabled {
    opacity: 0.35;
  }

  .big {
    width: 104px;
    height: 104px;
    background: var(--red, #f66);
    border-color: transparent;
    box-shadow: var(--sh-2);
  }
  .big svg {
    width: 46px;
    height: 46px;
    fill: currentColor;
  }

  .tail {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-top: 12px;
  }
  .stop svg {
    width: 22px;
    height: 22px;
    fill: currentColor;
  }
  .spk {
    width: 20px;
    height: 20px;
    flex: none;
    fill: var(--tx-3);
  }
  .vol {
    flex: 1;
  }
  .pctn {
    width: 3ch;
    text-align: right;
    font-size: 13px;
    font-variant-numeric: tabular-nums;
    color: var(--tx-3);
  }
</style>
