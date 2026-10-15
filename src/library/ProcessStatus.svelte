<script lang="ts">
  import Sect from "./Sect.svelte";
  import { clean } from "../core/video";
  import { durWords, pct } from "../core/fmt";
  import { TICK_MS } from "../core/events.svelte";
  import { usePoll } from "../core/poll";
  import { room } from "../core/room.svelte";

  let {
    status,
    jobs = [],
    i = 0,
  }: {
    status: ProcessStatus | null;
    jobs?: Job[];
    i?: number;
  } = $props();

  // ProcessAll's real shape. CleanAll runs alone first so later phases walk a
  // settled tree; these chains then fan out concurrently. A flat ten-row list
  // claims ten sequential steps, which is a lie about the machine — and the
  // lie is why the old panel read as a form rather than as something working.
  const GATE = "clean";
  const CHAINS = [
    ["convert"],
    ["scan", "repair"],
    ["thumbs", "sprites", "posters", "anim"],
    ["whisper"],
    ["blurbs"],
    ["skips"],
  ];

  const labels: Record<string, string> = {
    clean: "Organise",
    convert: "Convert",
    scan: "Scan",
    repair: "Repair",
    thumbs: "Thumbs",
    sprites: "Sprites",
    posters: "Posters",
    anim: "Animation",
    blurbs: "Blurbs",
    whisper: "Whisper",
    skips: "Intro/outro",
  };

  // One cell per file up to this, then one cell per batch. A count is
  // subitisable and a bar is not: 60% of a bar could be three files or three
  // hundred, and that magnitude is the thing the old bars threw away.
  const CELLS = 56;

  let now = $state(Date.now());
  $effect(() => usePoll(() => (now = Date.now()), TICK_MS));
  $effect(() => {
    room.busy = !!status?.running;
  });

  const elapsed = (ms: number) => (ms < 1000 ? "<1s" : durWords(ms / 1000));

  let phases = $derived(status?.phases ?? []);
  const at = (name: string) => phases.find((p) => p.name === name);

  const span = (p: PhaseInfo) =>
    p.startedAt ? elapsed((p.endedAt ?? now) - p.startedAt) : "";

  function field(p: PhaseInfo) {
    const total = p.total ?? 0;
    const done = p.done || 0;

    if (total > 0) {
      const n = Math.min(total, CELLS);
      return { n, lit: Math.round((done / total) * n), per: total / n };
    }
    // No total but work recorded: the count is the magnitude, all of it done.
    if (done > 0) {
      const n = Math.min(done, CELLS);
      return { n, lit: n, per: done / n };
    }

    return { n: 0, lit: 0, per: 1 };
  }

  const count = (p: PhaseInfo) =>
    p.total ? `${p.done}/${p.total}` : p.done ? String(p.done) : "—";

  let settled = $derived(phases.filter((p) => p.state === "done").length);
  let touched = $derived(phases.reduce((n, p) => n + (p.done || 0), 0));
  let visible = $derived(
    !!status && (status.running || phases.some((p) => p.state !== "pending")),
  );
  let meta = $derived(
    [
      `${settled}/${phases.length}`,
      touched > 0 ? `${touched} files` : "",
      status?.startedAt
        ? elapsed((status.endedAt ?? now) - status.startedAt)
        : "",
    ]
      .filter(Boolean)
      .join(" · "),
  );
  let convertActive = $derived(at("convert")?.state === "running");
</script>

{#snippet step(p: PhaseInfo)}
  {@const f = field(p)}
  <div
    class="step st-{p.state}"
    class:idle={p.state === "done" && !f.n}
    title="{labels[p.name] ?? p.name} · {count(p)}{f.per > 1
      ? ` · 1 mark = ${Math.round(f.per)} files`
      : ''} {span(p)}"
  >
    <span class="nm fs-xs trunc">{labels[p.name] ?? p.name}</span>
    {#if f.n}
      <span class="units">
        {#each { length: f.n } as _, c}
          <i class:on={c < f.lit} class:edge={c === f.lit} style="--i:{c}"></i>
        {/each}
      </span>
      <span class="ct fs-xs num">{count(p)}</span>
    {:else}
      <span class="units"></span>
      <span class="ct fs-xs num tx-1">—</span>
    {/if}
  </div>
{/snippet}

{#if visible && status}
  <Sect
    {i}
    label={status.running ? "Working" : "Last run"}
    {meta}
    fill={phases.length ? (settled / phases.length) * 100 : 0}
  >
    <div class="rig" class:live={status.running}>
      {#if at(GATE)}
        <div class="gate">
          {@render step(at(GATE)!)}
          {#if at(GATE)!.note}
            <span class="note fs-xs trunc">{clean(at(GATE)!.note!)}</span>
          {/if}
        </div>
      {/if}

      <div class="lanes">
        {#each CHAINS as chain, li (chain[0])}
          {@const live = chain.some((n) => at(n)?.state === "running")}
          {@const note = chain.map((n) => at(n)?.note).find(Boolean)}
          <div class="lane" class:hot={live} style="--i:{li}">
            <div class="steps">
              {#each chain as name (name)}
                {#if at(name)}{@render step(at(name)!)}{/if}
              {/each}
            </div>
            {#if note}<span class="note fs-xs trunc">{clean(note)}</span>{/if}
          </div>
        {/each}
      </div>

      <!-- A single file's conversion IS a continuous fraction, so here a bar is
           the honest mark — unlike a phase, where it hid the count. -->
      {#if convertActive && jobs.length > 0}
        <div class="subs">
          {#each jobs as j (j.name)}
            <div class="sub" style="--fill:{j.percent}%">
              <span class="fs-xs tx-3 trunc">{clean(j.name)}</span>
              <span class="fs-xs num tx-1">{pct(j.percent)}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </Sect>
{/if}

<style>
  .rig {
    display: grid;
    gap: 6px;
  }

  /* The gate is the one phase nothing overlaps, so it gets the full width and
     everything else hangs off it. */
  .gate {
    display: grid;
    gap: 2px;
    padding: 7px 10px 8px;
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-md);
    background: linear-gradient(
      180deg,
      var(--glass) 0%,
      color-mix(in oklab, var(--glass) 30%, transparent) 100%
    );
  }

  /* The fan-out as a board rather than a drawn tree: at full width the lanes
     sit side by side, which is what concurrent actually looks like. */
  .lanes {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(330px, 1fr));
    gap: 2px 22px;
    align-content: start;
  }

  .lane {
    position: relative;
    padding: 3px 8px 3px 11px;
    border-radius: var(--r-sm);
    transition: background var(--motion) var(--ease-out);
  }
  /* The rail is the lane's own state, carried in the margin where it is read
     without being looked at. */
  .lane::before {
    content: "";
    position: absolute;
    inset: 3px auto 3px 2px;
    width: 2px;
    border-radius: 2px;
    background: var(--glass-bd);
    transition:
      background var(--motion) var(--ease-out),
      box-shadow var(--motion) var(--ease-out);
  }
  .lane.hot {
    background: color-mix(in oklab, var(--red) 7%, transparent);
  }
  .lane.hot::before {
    background: var(--red);
    box-shadow: 0 0 10px var(--red-glow);
  }

  .steps {
    display: grid;
    gap: 1px;
  }

  .step {
    display: grid;
    grid-template-columns: 76px minmax(0, 1fr) 58px;
    align-items: center;
    gap: 9px;
    min-height: 20px;
  }
  .nm {
    color: var(--tx-3);
  }
  .st-pending .nm,
  .idle .nm {
    color: var(--tx-1);
  }
  .st-running .nm {
    color: var(--tx-4);
  }
  .ct {
    text-align: right;
    color: var(--tx-3);
  }

  /* The unit field. Each mark is work, so the row's width is its size and the
     lit run is its progress — one mark carrying both. */
  .units {
    display: flex;
    align-items: center;
    gap: 2px;
    height: 13px;
    min-width: 0;
    overflow: hidden;
  }
  .units i {
    width: 4px;
    height: 4px;
    flex-shrink: 0;
    border-radius: 1px;
    background: var(--glass-2);
    transition:
      height var(--motion) var(--ease-spring),
      background var(--motion) var(--ease-out);
    /* A batch finishing lights as one sweep rather than ten separate blinks:
       co-moving marks are read as a single event. */
    transition-delay: calc(var(--i) * 9ms);
  }
  .units i.on {
    height: 13px;
    background: var(--grn);
  }
  .st-running .units i.on {
    background: var(--red);
  }
  /* The frontier mark breathes on the same phase as the room, so the panel and
     the page are one thing rather than a widget sitting on a background. */
  .st-running .units i.edge {
    height: calc(5px + var(--room-live, 0) * 8px);
    background: color-mix(
      in oklab,
      var(--red) calc(30% + var(--room-live, 0) * 70%),
      var(--glass-2)
    );
    transition-delay: 0ms;
  }

  .idle .units,
  .st-pending .units {
    opacity: 0.4;
  }
  .idle {
    opacity: 0.55;
  }

  .note {
    grid-column: 1 / -1;
    padding-left: 85px;
    color: var(--tx-2);
  }

  .subs {
    display: grid;
    gap: 1px;
    margin: 2px 0 0 19px;
  }
  .sub {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 3px 8px;
    border-radius: var(--r-sm);
    background: linear-gradient(
      90deg,
      color-mix(in oklab, var(--red) 16%, transparent) 0 var(--fill),
      var(--glass) var(--fill)
    );
    transition: --fill var(--motion-slow) linear;
  }

  @media (max-width: 640px) {
    .step {
      grid-template-columns: 64px minmax(0, 1fr) 54px;
      gap: 6px;
    }
    .note {
      padding-left: 70px;
    }
    .lanes {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
