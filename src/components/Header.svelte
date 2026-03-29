<script lang="ts">
  let {
    rows,
    total,
    disks,
    fmtBytes,
  }: {
    rows: [string, string[]][];
    total: number;
    disks: DiskInfo[];
    fmtBytes: (b: number) => string;
  } = $props();
</script>

<header class="f fw al-ct g10 p-stx">
  <a href="/" class="back fs tx-3">←</a>
  <h1 class="m0 fw6">Manage Library</h1>
  <span class="fs tx-1">{rows.length} folders · {total} files</span>

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
    height: 3px;
    background: var(--bg-3);
  }
  .disk-fill {
    background: var(--red);
    transition: width 0.3s;
  }
</style>
