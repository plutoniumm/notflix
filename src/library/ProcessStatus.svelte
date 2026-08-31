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
    whisper: "Subtitles (Whisper)",
  };

  const icons: Record<PhaseState, string> = {
    pending: "○",
    running: "●",
    done: "✓",
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
  <section class="ps surface flow-h">
    <h3 class="m0 p10 fs-sm fw6 f al-ct g10">
      <span>
        {status.running ? "◐ Processing library" : "✓ Last run"}
      </span>
      {#if totalElapsed}
        <span class="fs-xs tx-1">· {totalElapsed}</span>
      {/if}
    </h3>

    <div class="phases">
      {#each status.phases as p (p.name)}
        <div class="phase f al-ct g10 st-{p.state}">
          <span class="ic fs-sm sh-0">{icons[p.state]}</span>
          <span class="lbl fs-sm sh-0">{labels[p.name] ?? p.name}</span>

          <div class="mid">
            {#if p.state === "running" || p.state === "done"}
              {#if p.total && p.total > 0}
                <div class="bar">
                  <ProgressBar
                    value={(p.done / p.total) * 100}
                    variant={p.state === "running" ? "striped" : "default"}
                    label={false}
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
        <div class="jobs-hd fs-xs tx-1 p10">Active conversions ({jobs.length})</div>
        {#each jobs as j (j.name)}
          <div class="job f al-ct g10">
            <span class="jn fs-xs tx-4 trunc">{clean(j.name)}</span>
            <div class="jb">
              <ProgressBar value={j.percent} variant="striped" label={false} />
            </div>
            <span class="jp fs-xs tx-1 sh-0">{pct(j.percent)}</span>
          </div>
        {/each}
      </div>
    {/if}
  </section>
{/if}

<style>
  .ps { margin-bottom: 24px; }
  .ps h3 {
    background: rgba(255, 255, 255, 0.02);
    color: var(--tx-4);
    border-bottom: 1px solid var(--glass-bd);
  }
  .phases { padding: 6px 10px; }
  .phase { padding: 6px 6px; border-top: 1px solid rgba(255,255,255,0.03); }
  .phase:first-child { border-top: none; }
  .ic { width: 16px; text-align: center; }
  .st-pending .ic { color: var(--tx-1); }
  .st-running .ic { color: var(--accent, #4a9eff); animation: pulse 1.2s ease-in-out infinite; }
  .st-done .ic { color: var(--green, #4ade80); }
  .st-pending .lbl { color: var(--tx-1); }
  .lbl { min-width: 160px; }
  .mid { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .bar { width: 100%; }
  .note { display: block; }
  .cnt { min-width: 60px; text-align: right; font-variant-numeric: tabular-nums; }
  .el { min-width: 46px; text-align: right; font-variant-numeric: tabular-nums; }

  .jobs { border-top: 1px solid var(--glass-bd); }
  .jobs-hd { text-transform: uppercase; letter-spacing: 0.04em; }
  .job { padding: 4px 10px; }
  .jn { flex: 1; min-width: 0; }
  .jb { width: 120px; flex-shrink: 0; }
  .jp { min-width: 34px; text-align: right; font-variant-numeric: tabular-nums; }

  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.35; }
  }
</style>
