<script lang="ts">
  import Sheet from "./sheet.svelte";
  import { KEYS } from "./ux";

  let { open, onClose }: { open: boolean; onClose: () => void } = $props();

  // "Shift ← / →" is one chord, "d / s" is two keys: split on the separator so
  // each token gets its own cap, and keep the slash as plain text between them.
  const caps = (k: string) => k.split(" / ");
</script>

<Sheet {open} {onClose} side="mid" title="Keyboard & gestures">
  <div class="cols">
    {#each KEYS as grp (grp.g)}
      <section class="grp">
        <div class="pl-lbl">{grp.g}</div>
        {#each grp.keys as row (row.k)}
          <div class="row">
            <span class="keys">
              {#each caps(row.k) as c, i}
                {#if i > 0}<span class="or">/</span>{/if}<kbd>{c}</kbd>
              {/each}
            </span>
            <span class="act">{row.a}</span>
          </div>
        {/each}
      </section>
    {/each}
  </div>
</Sheet>

<style>
  .cols {
    columns: 2;
    column-gap: 34px;
  }
  @media (max-width: 720px) {
    .cols { columns: 1; }
  }

  .grp {
    break-inside: avoid;
    margin-bottom: 20px;
  }
  .grp :global(.pl-lbl) {
    padding-bottom: 7px;
    border-bottom: 1px solid var(--glass-bd);
    margin-bottom: 6px;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(96px, auto) 1fr;
    align-items: baseline;
    gap: 12px;
    padding: 4px 0;
  }

  .keys {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
  }
  .or {
    color: var(--tx-1);
    font-size: 11px;
  }

  kbd {
    font-family: var(--font-display);
    font-size: 11px;
    font-weight: 600;
    line-height: 1;
    color: var(--tx-5);
    background: var(--glass-2);
    border: 1px solid var(--glass-bd);
    border-bottom-width: 2px;
    border-radius: var(--r-sm);
    padding: 5px 7px;
    white-space: nowrap;
  }

  .act {
    font-size: 13px;
    color: var(--tx-3);
    text-wrap: pretty;
  }
</style>
