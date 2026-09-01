<script lang="ts">
  import ProgressBar from "../components/ProgressBar.svelte";
  import { clean } from "../core/video";
  import { durWords, pct } from "../core/fmt";
  import { TICK_MS } from "../core/events.svelte";
  import { usePoll } from "../core/poll";

  let {
    status,
    jobs = [],
  }: {
    status: ProcessStatus | null;
    jobs?: Job[];
  } = $props();

  const labels: Record<string, string> = {
    convert: "Convert",
    clean: "Organize",
    scan: "Scan for corruption",
    repair: "Repair",
    thumbs: "Thumbnails",
    sprites: "Trickplay sprites",
    posters: "Poster frames",
    whisper: "Subtitles (Whisper)",
    skips: "Intro/outro detection",
  };

  const elapsed = (ms: number) => (ms < 1000 ? "<1s" : durWords(ms / 1000));

  function phaseElapsed(p: PhaseInfo, now: number): string {
    if (!p.startedAt) return "";

    return elapsed((p.endedAt ?? now) - p.startedAt);
  }

  let now = $state(Date.now());
  $effect(() => usePoll(() => (now = Date.now()), TICK_MS));

  let visible = $derived(status?.running || status?.phases?.some((p) => p.state !== "pending"));
  let totalElapsed = $derived(
    status?.startedAt ? elapsed((status.endedAt ?? now) - status.startedAt) : "",
  );

  let convertActive = $derived(
    status?.phases?.find((p) => p.name === "convert")?.state === "running",
  );
</script>

{#if status && visible}
  <section class="panel surface flow-h" class:live={status.running}>
    <h3 class="phead m0 f al-ct g10">
      <span class="plabel">
        {status.running ? "Processing library" : "Last run"}
      </span>
      {#if totalElapsed}
        <span class="pcount fs-xs">{totalElapsed}</span>
      {/if}
    </h3>

    <div class="phases">
      {#each status.phases as p (p.name)}
        <div class="phase f al-ct g10 st-{p.state}">
          <span class="mark sh-0 cc"></span>
          <span class="lbl fs-sm sh-0 trunc">{labels[p.name] ?? p.name}</span>

          <div class="mid">
            {#if p.state === "running" || p.state === "done"}
              {#if p.total && p.total > 0}
                <div class="bar">
                  <ProgressBar
                    value={(p.done / p.total) * 100}
                    variant={p.state === "running" ? "striped" : "default"}
                    label={false}
                    height="4px"
                  />
                </div>
              {/if}
              {#if p.state === "running" && p.note}
                <span class="note fs-xs tx-1 trunc">{clean(p.note)}</span>
              {/if}
            {/if}
          </div>

          <span class="cnt fs-xs tx-1 sh-0">
            {#if p.total && p.total > 0}
              {p.done}/{p.total}
            {:else if p.state === "done" && p.done > 0}
              {p.done}
            {/if}
          </span>
          <span class="el fs-xs tx-1 sh-0">{phaseElapsed(p, now)}</span>
        </div>
      {/each}
    </div>

    {#if convertActive && jobs.length > 0}
      <div class="jobs">
        <div class="jobs-hd fs-xs tx-1">Active conversions ({jobs.length})</div>
        {#each jobs as j (j.name)}
          <div class="job f al-ct g10">
            <span class="jn fs-xs tx-4 trunc">{clean(j.name)}</span>
            <div class="jb">
              <ProgressBar value={j.percent} variant="striped" label={false} height="4px" />
            </div>
            <span class="jp fs-xs tx-1 sh-0">{pct(j.percent)}</span>
          </div>
        {/each}
      </div>
    {/if}
  </section>
{/if}

<style>
  .panel {
    margin-bottom: 20px;
    transition:
      opacity var(--motion, 0.32s) var(--ease-out),
      translate var(--motion, 0.32s) var(--ease-out);
  }
  @starting-style {
    .panel {
      opacity: 0;
      translate: 0 10px;
    }
  }
  .panel.live {
    border-color: color-mix(in oklab, var(--red) 32%, transparent);
  }
  .phead {
    padding: 10px 16px;
    background: rgb(255 255 255 / 0.02);
    border-bottom: 1px solid var(--glass-bd);
  }
  .plabel {
    color: var(--tx-3);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }
  .pcount {
    color: var(--tx-1);
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }

  .phases {
    padding: 8px 16px 10px;
  }
  /* A stepper, not seven independent rows: one rail runs behind the markers so
     the order of the pipeline is visible even when nothing is moving. */
  .phase {
    padding: 5px 0;
    position: relative;
  }
  .phase::before {
    content: "";
    position: absolute;
    left: 5px;
    top: 0;
    bottom: 0;
    width: 1px;
    background: var(--glass-bd);
  }
  .phase:first-child::before {
    top: 50%;
  }
  .phase:last-child::before {
    bottom: 50%;
  }

  .mark {
    width: 11px;
    height: 11px;
    border-radius: 50%;
    border: 1px solid var(--bg-5);
    background: var(--bg-2);
    z-index: 1;
  }
  .st-running .mark {
    border-color: var(--red);
    background: var(--red);
    box-shadow: 0 0 0 4px var(--red-soft);
    animation: breathe 1.4s ease-in-out infinite;
  }
  .st-done .mark {
    border-color: var(--grn);
    background: color-mix(in oklab, var(--grn) 70%, transparent);
  }

  .lbl {
    min-width: 150px;
    color: var(--tx-4);
  }
  .st-pending .lbl {
    color: var(--tx-1);
  }
  .mid {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .bar {
    width: 100%;
  }
  .note {
    display: block;
  }
  .cnt {
    min-width: 62px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .el {
    min-width: 48px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .jobs {
    border-top: 1px solid var(--glass-bd);
    padding-bottom: 6px;
  }
  .jobs-hd {
    padding: 8px 16px 4px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    font-weight: 600;
  }
  .job {
    padding: 3px 16px;
  }
  .jn {
    flex: 1;
    min-width: 0;
  }
  .jb {
    width: 120px;
    flex-shrink: 0;
  }
  .jp {
    min-width: 36px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  @media (max-width: 640px) {
    .lbl {
      min-width: 0;
      flex: 1;
    }
    .mid {
      display: none;
    }
    .phases {
      padding: 8px 10px 10px;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .panel,
    .st-running .mark {
      animation: none;
      transition-duration: 1ms;
    }
  }
</style>
