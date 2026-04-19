<script lang="ts">
  let {
    jobs,
    fmtBytes,
    onRemove,
    onPause,
    onResume,
  }: {
    jobs: Downjob[];
    fmtBytes: (b: number) => string;
    onRemove: (gid: string) => void;
    onPause: (gid: string) => void;
    onResume: (gid: string) => void;
  } = $props();
</script>

{#if jobs.length > 0}
  <section class="rx5 flow-h">
    <h3 class="m0 p10 fs-sm fw6 bg-3">
      Downloading ({jobs.length})
    </h3>

    {#each jobs as j (j.gid)}
      <div class="item f al-ct g10">
        <div class="info">
          <span class="name d-b fs-sm tx-4 flow-h">{j.name || j.gid}</span>
          <div class="f al-ct g10">
            <div class="bar rx2">
              <div
                class="fill h-100 rx2"
                class:active={j.status !== "paused"}
                style="width:{j.percent.toFixed(1)}%"
              ></div>
            </div>
            <span class="fs-xs tx-1">{Math.round(j.percent)}%</span>
            <span class="fs-xs tx-1">
              {j.status === "paused" ? "paused" : `${fmtBytes(j.speed)}/s`}
            </span>
          </div>
        </div>
        {#if j.status === "paused"}
          <button class="btn-icon sh-0" title="Resume" onclick={() => onResume(j.gid)}>
            ▶
          </button>
        {:else}
          <button class="btn-icon sh-0" title="Pause" onclick={() => onPause(j.gid)}>
            ⏸
          </button>
        {/if}
        <button class="btn-icon danger sh-0" title="Remove" onclick={() => onRemove(j.gid)}>
          ✕
        </button>
      </div>
    {/each}
  </section>
{/if}

<style>
  section {
    margin-bottom: 20px;
    border: 1px solid var(--bg-3);
  }
  .item {
    padding: 10px 14px;
    border-top: 1px solid var(--bg-3);
  }
  .info {
    flex: 1;
    min-width: 0;
  }
  .name {
    white-space: nowrap;
    text-overflow: ellipsis;
    margin-bottom: 4px;
  }
  .item .btn-icon {
    opacity: 1;
  }
  .bar {
    flex: 1;
    height: 5px;
    background: var(--bg-4);
    overflow: hidden;
    box-shadow: inset 0 1px 1px #0005;
  }
  .fill {
    background: linear-gradient(90deg, #e11, #f63);
    transition: width 0.5s;
    box-shadow: 0 0 8px -2px #e114;
  }
  .fill.active {
    background-image:
      linear-gradient(
        45deg,
        #e11 0 25%,
        #f63 25% 50%,
        #e11 50% 75%,
        #f63 75% 100%
      );
    background-size: 28px 28px;
    animation: stripe-shift 0.8s linear infinite;
  }
</style>
