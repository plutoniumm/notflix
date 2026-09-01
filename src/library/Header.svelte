<script lang="ts">
  import BackLink from "../components/BackLink.svelte";
  import { bytes, pct } from "../core/fmt";

  let {
    rows,
    total,
    disks,
    processing = false,
    onProcess,
  }: {
    rows: [string, { name: string; root: string }[]][];
    total: number;
    disks: DiskInfo[];
    processing?: boolean;
    onProcess?: () => void;
  } = $props();

  const used = (d: DiskInfo) => (d.total > 0 ? (1 - d.free / d.total) * 100 : 0);
</script>

<header class="glass-header f fw al-ct g10">
  <BackLink />
  <h1 class="m0 fw6">Manage Library</h1>
  <span class="tally fs-sm tx-1">
    <b>{rows.length}</b> folders · <b>{total}</b> files
  </span>

  {#if onProcess}
    <button
      class="proc-btn glass glass-hover ptr"
      class:busy={processing}
      onclick={onProcess}
      disabled={processing}
    >
      {processing ? "Processing…" : "Process Library"}
    </button>
  {/if}

  {#if disks.length > 0}
    <div class="disks">
      {#each disks as d}
        <span class="disk-tag fs-xs" data-root={d.root}>{d.root}</span>
        <div class="meter p-rel flow-h" title="{pct(used(d))} used">
          <div class="meter-fill p-abs" style="--p:{used(d)}%"></div>
        </div>
        <span class="free fs-xs tx-1">{bytes(d.free)} free</span>
      {/each}
    </div>
  {/if}
</header>

<style>
  header {
    padding: 16px 40px;
    padding-top: calc(16px + var(--safe-t));
    z-index: 40;
  }
  header h1 {
    font-size: 20px;
  }
  .tally {
    font-variant-numeric: tabular-nums;
  }
  .tally b {
    color: var(--tx-4);
    font-weight: 600;
  }

  .proc-btn {
    padding: 6px 14px;
    color: var(--tx-4);
    font-family: inherit;
    font-size: 13px;
    font-weight: 500;
    border-radius: var(--r-md);
  }
  .proc-btn:disabled {
    cursor: default;
  }
  .proc-btn.busy {
    color: var(--tx-3);
    background: linear-gradient(
        90deg,
        transparent,
        color-mix(in oklab, var(--red) 26%, transparent),
        transparent
      )
      0 0 / 180px 100% repeat-x;
    animation: shimmer 1.4s linear infinite;
  }

  .disks {
    margin-left: auto;
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 6px 10px;
  }
  .meter {
    width: 120px;
    height: 6px;
    border-radius: 999px;
    background: rgb(255 255 255 / 0.07);
  }
  /* The scale lives on the bar and the fill is a clip window onto it, so
     fullness maps to colour with no branch in the markup. */
  .meter-fill {
    inset: 0;
    border-radius: 999px;
    background: linear-gradient(
      90deg,
      var(--grn) 0%,
      var(--cyan) 30%,
      var(--gold) 72%,
      var(--red) 96%
    );
    clip-path: inset(0 calc(100% - var(--p, 0%)) 0 0);
    transition: clip-path var(--motion-slow, 0.55s) var(--ease-out);
  }
  .free {
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  @media (max-width: 640px) {
    header {
      padding: 12px 16px;
      padding-top: calc(12px + var(--safe-t));
    }
    .disks {
      margin-left: 0;
      width: 100%;
    }
    .meter {
      width: auto;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .proc-btn.busy {
      animation: none;
    }
    .meter-fill {
      transition-duration: 1ms;
    }
  }
</style>
