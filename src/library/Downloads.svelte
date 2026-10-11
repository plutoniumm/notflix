<script lang="ts">
  import Sect from "./Sect.svelte";
  import { bytes, rate, durWords } from "../core/fmt";

  let {
    jobs,
    onRemove,
    onPause,
    onResume,
    i = 0,
  }: {
    jobs: Downjob[];
    onRemove: (gid: string) => void;
    onPause: (gid: string) => void;
    onResume: (gid: string) => void;
    i?: number;
  } = $props();

  const KEEP = 24;
  const W = 54;
  const H = 14;

  // The history is a plain Map, not $state: appending to it would make the
  // effect depend on what it writes and loop forever.
  const hist = new Map<string, number[]>();
  let spark = $state<Record<string, string>>({});

  $effect(() => {
    const next: Record<string, string> = {};
    for (const j of jobs) {
      const h = [...(hist.get(j.gid) ?? []), Math.max(0, j.speed)].slice(-KEEP);
      hist.set(j.gid, h);
      next[j.gid] = trace(h);
    }
    for (const gid of [...hist.keys()]) if (!(gid in next)) hist.delete(gid);
    spark = next;
  });

  // Normalised to the window's own peak: the shape answers "is it moving or
  // stalled", which an absolute scale hides on a slow torrent.
  function trace(h: number[]): string {
    if (h.length < 2) return "";
    const top = Math.max(...h, 1);

    return h
      .map((v, k) => {
        const x = (k / (h.length - 1)) * W;

        return `${x.toFixed(1)},${(H - 1 - (v / top) * (H - 2)).toFixed(1)}`;
      })
      .join(" ");
  }

  const left = (j: Downjob) =>
    j.speed > 0 && j.total > j.done ? durWords((j.total - j.done) / j.speed) : "";

  let moving = $derived(jobs.filter((j) => j.status !== "paused"));
  let pull = $derived(moving.reduce((n, j) => n + Math.max(0, j.speed), 0));
  let meta = $derived(
    [
      `${jobs.length} queued`,
      moving.length ? rate(pull) : jobs.length ? "all paused" : "",
    ]
      .filter(Boolean)
      .join(" · "),
  );
</script>

{#if jobs.length > 0}
  <Sect {i} label="Downloading" {meta}>
    <div class="tbl">
      <div class="led hrow fs-xs tx-1">
        <span>Torrent</span>
        <span class="sp">Trend</span>
        <span class="tr sz">Size</span>
        <span class="tr">Speed</span>
        <span class="tr lf">Left</span>
        <span></span>
        <span></span>
      </div>

      {#each jobs as j (j.gid)}
        {@const paused = j.status === "paused"}
        <div class="led row" class:paused style="--fill:{j.percent}%">
          <span class="nm fs-sm tx-4 trunc">{j.name || j.gid}</span>

          <span class="sp">
            {#if spark[j.gid] && !paused}
              <svg viewBox="0 0 {W} {H}" width={W} height={H}>
                <polyline points={spark[j.gid]} fill="none" stroke-width="1.25" />
              </svg>
            {/if}
          </span>

          <span class="tr sz fs-xs tx-1 num">
            {bytes(j.done)} / {bytes(j.total)}
          </span>
          <!-- 0 B/s on an active torrent means no peers, not "almost done". -->
          <span class="tr fs-xs num" class:red={!paused && j.speed === 0}>
            {paused ? "paused" : rate(j.speed)}
          </span>
          <span class="tr lf fs-xs tx-1 num">{left(j) || "—"}</span>

          {#if paused}
            <button class="btn-icon sh-0" title="Resume" onclick={() => onResume(j.gid)}>
              ▶
            </button>
          {:else}
            <button class="btn-icon sh-0" title="Pause" onclick={() => onPause(j.gid)}>
              ⏸
            </button>
          {/if}
          <button
            class="btn-icon danger sh-0"
            title="Remove"
            onclick={() => onRemove(j.gid)}
          >
            ✕
          </button>
        </div>
      {/each}
    </div>
  </Sect>
{/if}

<style>
  .tbl :global(.led) {
    grid-template-columns:
      minmax(0, 1fr) 54px 104px 78px 52px
      auto auto;
  }
  .hrow {
    padding-block: 2px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    font-size: 10px;
    border-bottom: 1px solid var(--glass-bd);
    border-radius: 0;
    margin-bottom: 2px;
  }
  .row {
    padding-block: 9px;
  }
  .nm {
    font-weight: 500;
  }

  .sp {
    height: 14px;
  }
  .sp svg {
    display: block;
    overflow: visible;
    color: var(--grn);
  }

  .paused {
    opacity: 0.6;
    --led-c: var(--glass);
  }
  .row :global(.btn-icon) {
    opacity: 1;
  }

  @media (max-width: 640px) {
    .tbl :global(.led) {
      grid-template-columns: minmax(0, 1fr) 72px auto auto;
    }
    .sp,
    .sz,
    .lf {
      display: none;
    }
    .nm {
      font-size: 13px;
    }
  }
</style>
