<script lang="ts">
  import ProgressBar from "../components/ProgressBar.svelte";
  import { rate } from "../core/fmt";

  let {
    jobs,
    onRemove,
    onPause,
    onResume,
  }: {
    jobs: Downjob[];
    onRemove: (gid: string) => void;
    onPause: (gid: string) => void;
    onResume: (gid: string) => void;
  } = $props();
</script>

{#if jobs.length > 0}
  <section class="panel surface flow-h">
    <h3 class="phead m0 f al-ct g10">
      <span class="plabel">Downloading</span>
      <span class="pcount fs-xs">{jobs.length}</span>
    </h3>

    {#each jobs as j (j.gid)}
      <div class="item f al-ct g10" class:paused={j.status === "paused"}>
        <div class="info">
          <span class="name d-b fs-sm tx-4 trunc">{j.name || j.gid}</span>
          <div class="f al-ct g10">
            <div style="flex:1; min-width:0">
              <ProgressBar
                value={j.percent}
                variant={j.status !== "paused" ? "striped" : "default"}
                label
              />
            </div>
            <span class="speed fs-xs tx-1">
              {j.status === "paused" ? "paused" : rate(j.speed)}
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
    font-variant-numeric: tabular-nums;
  }
  .item {
    padding: 11px 16px;
    border-top: 1px solid rgb(255 255 255 / 0.04);
    transition: opacity var(--motion, 0.25s) var(--ease-out);
  }
  .item:first-of-type {
    border-top: none;
  }
  .item.paused {
    opacity: 0.6;
  }
  .info {
    flex: 1;
    min-width: 0;
  }
  .name {
    margin-bottom: 6px;
    font-weight: 500;
  }
  .speed {
    min-width: 68px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .item :global(.btn-icon) {
    opacity: 1;
  }

  @media (max-width: 640px) {
    .item {
      padding: 10px 8px;
    }
    .name {
      font-size: 12px;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .panel,
    .item {
      transition-duration: 1ms;
    }
  }
</style>
