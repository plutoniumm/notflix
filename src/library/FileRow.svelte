<script lang="ts">
  import { clean } from "../core/video";
  import { join } from "../core/path";
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

  function icon(name: string) {
    const ext = name.split(".").pop()?.toLowerCase();

    return ext === "mkv" || ext === "mov" ? "⟳" : "▶";
  }

  function focus(el: HTMLInputElement) {
    el.focus();
    el.select();
  }
</script>

<li class="file" class:corrupt>
  {#if job}
    <div class="conv-track">
      <div class="conv-fill" style="width:{job.percent.toFixed(1)}%"></div>
    </div>
  {/if}

  <div class="row f al-ct g10">
    {#if corrupt}
      <span
        class="fs-xs sh-0 tc icon skull"
        title="ffprobe failed to read this file — likely corrupt or truncated"
      >
        💀
      </span>
    {:else}
      <span
        class="fs-xs tx-1 sh-0 tc icon"
        title={f.split(".").pop()?.toUpperCase()}
      >
        {icon(f)}
      </span>
    {/if}

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
        <span class="d-b fs tx-4 trunc">{clean(f)}</span>
        <span class="d-b fs-xs trunc raw">{f}</span>
      </div>
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
    border-top: 1px solid rgba(255, 255, 255, 0.04);
    transition: background 0.15s var(--ease-out);
    position: relative;
    overflow: hidden;
  }
  .file:hover {
    background: var(--glass);
  }
  .file:hover .actions,
  .file:hover .btn-icon {
    opacity: 1;
  }

  .conv-track {
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    height: 2px;
    background: rgba(255, 255, 255, 0.06);
  }
  .conv-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--red) 0%, #ff7849 100%);
    box-shadow: 0 0 6px var(--red-glow);
    transition: width 0.5s var(--ease-out);
    min-width: 2px;
  }

  .row {
    padding: 9px 16px 9px 44px;
  }

  .icon {
    width: 16px;
  }
  .skull {
    filter: drop-shadow(0 0 4px var(--red));
    animation: pulse 1.6s ease-in-out infinite;
  }
  @keyframes pulse {
    0%,
    100% {
      filter: drop-shadow(0 0 3px var(--red));
    }
    50% {
      filter: drop-shadow(0 0 7px var(--red));
    }
  }

  .names {
    flex: 1;
    min-width: 0;
  }
  .raw {
    color: var(--bg-5);
  }

  .actions {
    opacity: 0;
    transition: opacity 0.18s var(--ease-out);
  }
  /* An opacity-0 wrapper still takes taps — on touch these were an invisible
     rename and an invisible delete sitting under the row. */
  @media (hover: none) {
    .actions {
      opacity: 1;
    }
  }
  .file.corrupt .actions,
  .file.corrupt .btn-icon {
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
</style>
