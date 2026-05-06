<script lang="ts">
  let {
    rows,
    total,
    disks,
    fmtBytes,
    processing = false,
    onProcess,
  }: {
    rows: [string, { name: string; root: string }[]][];
    total: number;
    disks: DiskInfo[];
    fmtBytes: (b: number) => string;
    processing?: boolean;
    onProcess?: () => void;
  } = $props();
</script>

<header class="glass-header f fw al-ct g10">
  <a href="/" class="back fs tx-3">←</a>
  <h1 class="m0 fw6">Manage Library</h1>
  <span class="fs tx-1">{rows.length} folders · {total} files</span>

  {#if onProcess}
    <button class="proc-btn glass glass-hover" onclick={onProcess} disabled={processing}>
      {processing ? "Processing…" : "Process Library"}
    </button>
  {/if}

  {#if disks.length > 0}
    <div class="f g20 sh-0" style="margin-left:auto">
      {#each disks as d}
        {@const pct = Math.round((1 - d.free / d.total) * 100)}
        <div class="f al-ct g5">
          <span class="fs-xs tx-1 up" style="white-space:nowrap">{d.root}</span>
          <div class="disk-bar rx2 flow-h">
            <div class="disk-fill h-100 rx2" style="width:{pct}%"></div>
          </div>
          <span class="fs-xs tx-1" style="white-space:nowrap">
            {fmtBytes(d.free)} free
          </span>
        </div>
      {/each}
    </div>
  {/if}
</header>

<style>
  header {
    padding: 20px 40px;
  }
  header h1 {
    font-size: 22px;
  }
  .back {
    padding: 4px 10px;
    border-radius: var(--r-md);
    transition: color 0.18s var(--ease-out), background 0.18s var(--ease-out);
  }
  .back:hover {
    color: var(--tx-5);
    background: var(--glass);
  }
  .disk-bar {
    width: 88px;
    height: 5px;
    background: rgba(255, 255, 255, 0.06);
    border-radius: 999px;
    overflow: hidden;
  }
  .proc-btn {
    padding: 6px 14px;
    color: var(--tx-4);
    font-family: inherit;
    font-size: 13px;
    font-weight: 500;
    border-radius: var(--r-md);
    cursor: pointer;
  }
  .proc-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .disk-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--red), #ff7849, var(--gold));
    border-radius: 999px;
    transition: width 0.4s var(--ease-out);
  }
</style>
