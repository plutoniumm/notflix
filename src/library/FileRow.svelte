<script lang="ts">
  import { clean } from "../core/video";
  import { join } from "../core/path";
  import { pct } from "../core/fmt";
  import DeleteBtn from "../components/DeleteBtn.svelte";

  let {
    dir,
    f,
    armed = null,
    root = null,
    corrupt = false,
    job,
    editing,
    val = $bindable(""),
    startEdit,
    cancelEdit,
    confirmEdit,
    onDelete,
  }: any = $props();

  const fpath = $derived(join(dir, f));

  const ext = $derived((f.split(".").pop() ?? "").toUpperCase());

  function focus(el: HTMLInputElement) {
    el.focus();
    el.select();
  }
</script>

<li class="file" class:corrupt class:busy={!!job}>
  {#if job}
    <div class="conv-track p-abs">
      <div class="conv-fill h-100" style="width:{job.percent.toFixed(1)}%"></div>
    </div>
  {/if}

  <div class="row f al-ct g10">
    <span
      class="tag fs-xs sh-0 tc"
      class:skull={corrupt}
      title={corrupt
        ? "ffprobe failed to read this file — likely corrupt or truncated"
        : ext}
    >
      {corrupt ? "!" : ext}
    </span>

    {#if editing === fpath}
      <form class="form f" onsubmit={confirmEdit}>
        <input
          class="field input"
          bind:value={val}
          onkeydown={(e) => e.key === "Escape" && cancelEdit()}
          onblur={confirmEdit}
          use:focus
        />
      </form>
    {:else}
      <div class="names">
        <span class="d-b fs-sm tx-4 trunc">{clean(f)}</span>
        <span class="d-b fs-xs trunc raw">{f}</span>
      </div>
      {#if job}
        <span class="pctl fs-xs sh-0">{pct(job.percent)}</span>
      {/if}
      {#if root}
        <span class="disk-tag fs-xs sh-0" data-root={root}>{root}</span>
      {/if}
      <div class="actions f g2">
        <button
          class="btn-icon"
          title="Rename"
          onclick={() => startEdit(fpath, f)}
        >
          ✏️
        </button>
        <DeleteBtn armed={armed === fpath} onclick={onDelete} />
      </div>
    {/if}
  </div>
</li>

<style>
  .file {
    border-top: 1px solid rgb(255 255 255 / 0.04);
    transition: background var(--motion-fast, 0.15s) var(--ease-out);
    position: relative;
    overflow: hidden;
  }
  .file:hover {
    background: var(--glass);
  }
  .file:hover .actions,
  .file:hover :global(.btn-icon) {
    opacity: 1;
  }
  .file.corrupt {
    background: color-mix(in oklab, var(--red) 6%, transparent);
  }

  .conv-track {
    bottom: 0;
    left: 0;
    right: 0;
    height: 2px;
    background: rgb(255 255 255 / 0.06);
  }
  .conv-fill {
    background: linear-gradient(90deg, var(--red), var(--gold));
    box-shadow: 0 0 6px var(--red-glow);
    transition: width var(--motion-slow, 0.5s) var(--ease-out);
    min-width: 2px;
  }

  .row {
    padding: 8px 16px 8px 40px;
  }

  /* The container extension is the file's identity here — a fixed-width
     monospace slug keeps a long list scannable down one column. */
  .tag {
    width: 34px;
    padding: 2px 0;
    border-radius: var(--r-sm);
    background: rgb(255 255 255 / 0.05);
    color: var(--tx-1);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.06em;
  }
  .skull {
    color: var(--red);
    background: var(--red-soft);
    animation: breathe 1.8s ease-in-out infinite;
  }

  .names {
    flex: 1;
    min-width: 0;
  }
  .raw {
    color: var(--bg-5);
  }
  .pctl {
    color: var(--tx-2);
    font-variant-numeric: tabular-nums;
  }

  .actions {
    opacity: 0;
    transition: opacity var(--motion, 0.18s) var(--ease-out);
  }
  /* An opacity-0 wrapper still takes taps — on touch these were an invisible
     rename and an invisible delete sitting under the row. */
  @media (hover: none) {
    .actions {
      opacity: 1;
    }
  }
  .file.corrupt .actions,
  .file.corrupt :global(.btn-icon) {
    opacity: 1;
  }

  :global(.disk-tag) {
    margin-right: 6px;
  }

  .form {
    flex: 1;
    min-width: 0;
  }
  .input {
    flex: 1;
    width: 100%;
    border-color: var(--red);
    box-shadow: 0 0 0 4px var(--red-soft);
  }

  @media (max-width: 640px) {
    .row {
      padding: 8px 10px 8px 24px;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .skull,
    .conv-fill,
    .actions {
      animation: none;
      transition-duration: 1ms;
    }
  }
</style>
