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

<header class="f fw al-ct g10 p-stx">
  <a href="/" class="back fs tx-3">←</a>
  <h1 class="m0 fw6">Manage Library</h1>
  <span class="fs tx-1">{rows.length} folders · {total} files</span>

  {#if onProcess}
    <button class="proc-btn rx2" onclick={onProcess} disabled={processing}>
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
    background: var(--bg-2);
    border-bottom: 1px solid var(--bg-3);
    top: 0;
    z-index: 10;
  }
  .back:hover {
    color: var(--tx-5);
  }
  .disk-bar {
    width: 80px;
    height: 4px;
    background: var(--bg-3);
    overflow: hidden;
    box-shadow: inset 0 1px 1px #0006;
  }
  .proc-btn {
    padding: 4px 12px;
    background: var(--bg-3);
    color: var(--tx-3);
    border: none;
    cursor: pointer;
    font-size: 13px;
  }
  .proc-btn:hover:not(:disabled) {
    background: var(--bg-4);
    color: var(--tx-5);
  }
  .proc-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .disk-fill {
    background: linear-gradient(90deg, #e11, #f63, #fa0);
    transition: width 0.4s cubic-bezier(0.2, 0.9, 0.3, 1);
    box-shadow: 0 0 8px -1px #e114;
  }
</style>
