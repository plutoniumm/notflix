<script lang="ts">
  import { clean } from "../core/video";

  let {
    dir,
    f,
    root = null,
    job,
    editing,
    val = $bindable(""),
    startEdit,
    cancelEdit,
    confirmEdit,
    onDelete,
  }: any = $props();

  const fpath = $derived(dir === "." ? f : `${dir}/${f}`);

  function icon(name: string) {
    const ext = name.split(".").pop()?.toLowerCase();

    return ext === "mkv" || ext === "mov" ? "⟳" : "▶";
  }

  function focus(el: HTMLInputElement) {
    el.focus();
    el.select();
  }
</script>

<li class="file">
  {#if job}
    <div class="conv-track">
      <div class="conv-fill" style="width:{job.percent.toFixed(1)}%"></div>
    </div>
  {/if}

  <div class="row f al-ct g10">
    <span
      class="fs-xs tx-1 sh-0 tc icon"
      title={f.split(".").pop()?.toUpperCase()}
    >
      {icon(f)}
    </span>

    {#if editing === fpath}
      <form class="form f" onsubmit={confirmEdit}>
        <input
          class="input"
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
        <button class="btn-icon" onclick={() => startEdit(fpath, f)}>✏️</button>
        <button class="btn-icon danger" onclick={onDelete}> 🗑 </button>
      </div>
    {/if}
  </div>
</li>

<style>
  .file {
    border-top: 1px solid var(--bg-3);
    transition: background 0.1s;
    position: relative;
    overflow: hidden;
  }
  .file:hover {
    background: var(--bg-3);
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
    background: var(--bg-4);
  }
  .conv-fill {
    height: 100%;
    background: var(--red);
    transition: width 0.5s;
    min-width: 2px;
  }

  .row {
    padding: 7px 14px 7px 42px;
  }

  .icon {
    width: 16px;
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
    transition: opacity 0.15s;
  }

  .disk-tag {
    padding: 1px 6px;
    border-radius: 3px;
    background: var(--bg-4);
    color: var(--tx-4);
    letter-spacing: 0.02em;
    margin-right: 6px;
  }
  .disk-tag[data-root="Ravan"] {
    background: #3a2a2a;
    color: #f2a3a3;
  }
  .disk-tag[data-root="Oni"] {
    background: #2a323a;
    color: #a3c8f2;
  }
  .disk-tag[data-root="Kumbhakarn"] {
    background: #2a3a2d;
    color: #a3f2b8;
  }

  .form {
    flex: 1;
    min-width: 0;
  }
  .input {
    flex: 1;
    width: 100%;
    background: var(--bg-2);
    border: 1px solid var(--red);
    color: var(--tx-5);
    padding: 3px 10px;
    border-radius: 3px;
  }
</style>
