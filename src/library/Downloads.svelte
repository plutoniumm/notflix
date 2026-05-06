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
  <section class="surface flow-h">
    <h3 class="m0 p10 fs-sm fw6">
      Downloading ({jobs.length})
    </h3>

    {#each jobs as j (j.gid)}
      <div class="item f al-ct g10">
        <div class="info">
          <span class="name d-b fs-sm tx-4 flow-h">{j.name || j.gid}</span>
          <div class="f al-ct g10">
            <div class="bar bar-track">
              <div
                class="bar-fill"
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
          <button
            class="btn-icon sh-0"
            title="Resume"
            onclick={() => onResume(j.gid)}
          >
            ▶
          </button>
        {:else}
          <button
            class="btn-icon sh-0"
            title="Pause"
            onclick={() => onPause(j.gid)}
          >
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
  </section>
{/if}

<style>
  section {
    margin-bottom: 24px;
  }
  section h3 {
    background: rgba(255, 255, 255, 0.02);
    color: var(--tx-4);
    border-bottom: 1px solid var(--glass-bd);
  }
  .item {
    padding: 12px 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.04);
  }
  .item:first-child { border-top: none; }
  .info { flex: 1; min-width: 0; }
  .name {
    white-space: nowrap;
    text-overflow: ellipsis;
    margin-bottom: 6px;
    font-weight: 500;
  }
  .item :global(.btn-icon) { opacity: 1; }
  .bar { flex: 1; height: 6px; }
  .bar-fill.active {
    background-image: linear-gradient(
      45deg,
      var(--red) 0 25%,
      #ff7849 25% 50%,
      var(--red) 50% 75%,
      #ff7849 75% 100%
    );
    background-size: 28px 28px;
    animation: stripe-shift 0.8s linear infinite;
  }
  @media (max-width: 640px) {
    .item { padding: 10px 8px; }
    .name { font-size: 12px; }
  }
</style>
