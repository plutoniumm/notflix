<script lang="ts">
  import { onMount } from "svelte";
  import Sect from "./Sect.svelte";
  import DeleteBtn from "../components/DeleteBtn.svelte";
  import { api } from "../core/api";
  import { ago, bytes } from "../core/fmt";
  import { join } from "../core/path";
  import { clean } from "../core/video";
  import { reclaim, IDLE_DAYS } from "./reclaim";
  import type { HRow } from "./history";

  // Derived from the caller's live listing, so a delete there drops the row
  // here with no refetch — and a folder whose last unfinished file goes
  // becomes one folder row on its own.
  let {
    data,
    armed,
    onDelFile,
    onDelDir,
    i = 0,
  }: {
    data: Record<string, { name: string }[]>;
    armed: string | null;
    onDelFile: (dir: string, name: string) => void;
    onDelDir: (dir: string) => void;
    i?: number;
  } = $props();

  let rows = $state<HRow[]>([]);

  onMount(async () => {
    rows = (await api.history({ silent: true }))?.rows ?? [];
  });

  const picks = $derived(reclaim(rows, data));
  const total = $derived(picks.reduce((n, p) => n + p.size, 0));
  const top = $derived(picks[0]?.size || 1);
</script>

{#if picks.length > 0}
  <Sect {i} label="Reclaim space" meta="{bytes(total)} · finished, idle {IDLE_DAYS}+ days">
    {#each picks as p (p.id)}
      {@const key = p.name ? join(p.dir, p.name) : p.dir}
      <div class="led pick" style="--fill:{(p.size / top) * 100}%">
        <span class="f-col trunc">
          <span class="fs-sm tx-4 trunc">{clean(p.name ?? p.dir)}</span>
          <span class="fs-xs tx-1 trunc">
            {#if p.name}
              {p.dir === "." ? "Root" : clean(p.dir)} · finished {ago(p.lastAt)}
            {:else}
              {p.n}/{p.n} finished · last {ago(p.lastAt)}
            {/if}
            {#if p.redl}· <span class="redl">re-downloadable</span>{/if}
          </span>
        </span>
        <span class="fs-xs tx-3 num tr">{bytes(p.size)}</span>
        <DeleteBtn
          armed={armed === key}
          title={p.name ? "Delete file" : "Delete folder"}
          onclick={() => (p.name ? onDelFile(p.dir, p.name) : onDelDir(p.dir))}
        />
      </div>
    {/each}
  </Sect>
{/if}

<style>
  .pick {
    grid-template-columns: minmax(0, 1fr) auto auto;
    --led-c: color-mix(in oklab, var(--cyan) 14%, transparent);
  }
  /* Deleting is what this list is for, so its control is not hover-revealed. */
  .pick :global(.btn-icon) {
    opacity: 1;
  }
  .redl {
    color: var(--grn);
  }
</style>
