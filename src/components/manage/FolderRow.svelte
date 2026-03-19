<script lang="ts">
  import { clean } from "../../lib/video";
  import FileRow from "./FileRow.svelte";

  let {
    dir,
    files,
    jobs,
    idx,
    editing,
    val = $bindable(""),
    startEdit,
    cancelEdit,
    confirmEdit,
    onDelFile,
    onDelDir,
  }: {
    dir: string;
    files: string[];
    jobs: Job[];
    idx: number;
    editing: string | null;
    val: string;
    startEdit: (path: string, name: string) => void;
    cancelEdit: () => void;
    confirmEdit: (e?: Event) => void;
    onDelFile: (f: string) => void;
    onDelDir: () => void;
  } = $props();

  const converting = $derived(jobs.some((j) => files.includes(j.name)));

  function focus(el: HTMLInputElement) {
    el.focus();
    el.select();
  }
</script>

<details class="folder rx5 flow-h" style="--i:{idx}">
  <summary class="folder-hd f al-ct g10 ptr">
    <span class="folder-icon p-rel">
      📁
      {#if converting}
        <span class="conv-dot p-abs"></span>
      {/if}
    </span>

    {#if editing === dir}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <form
        class="rename-form f"
        onsubmit={confirmEdit}
        onclick={(e) => e.stopPropagation()}
      >
        <input
          class="rename-input fs-sm"
          bind:value={val}
          onkeydown={(e) => e.key === "Escape" && cancelEdit()}
          onblur={confirmEdit}
          use:focus
        />
      </form>
    {:else}
      <span class="fw5 fs-md">
        {dir === "." ? "Root" : clean(dir) || dir}
      </span>
      <span class="folder-raw fs-xs tx-1 trunc">
        {dir === "." ? "" : dir}
      </span>
      <span class="fs-sm tx-1 sh-0" style="margin-left:auto">
        ({files.length} files)
      </span>

      {#if dir !== "."}
        <button
          class="btn-icon"
          onclick={(e) => {
            e.stopPropagation();
            startEdit(dir, dir);
          }}
        >
          ✏️
        </button>
        <button
          class="btn-icon danger"
          title="Delete folder"
          onclick={(e) => {
            e.stopPropagation();
            onDelDir();
          }}
        >
          🗑
        </button>
      {:else}
        <div style="width:32px">&nbsp;</div>
      {/if}
    {/if}
  </summary>

  <ul class="m0 p0">
    {#each files as f (f)}
      <FileRow
        {dir}
        {f}
        job={jobs.find((j) => j.name === f) ?? null}
        {editing}
        bind:val
        {startEdit}
        {cancelEdit}
        {confirmEdit}
        onDelete={() => onDelFile(f)}
      />
    {/each}
  </ul>
</details>

<style>
  .folder {
    border: 1px solid var(--bg-3);
    margin-bottom: 8px;
    animation: slide-up 0.25s ease both;
    animation-delay: calc(var(--i) * 40ms);
  }

  .folder-hd {
    padding: 10px 14px;
    background: var(--bg-3);
    user-select: none;
    transition: background 0.1s;
  }
  .folder-hd:hover .btn-icon {
    opacity: 1;
  }

  .folder-icon {
    line-height: 1;
  }

  .conv-dot {
    top: -2px;
    right: -4px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--red);
    box-shadow: 0 0 4px var(--red);
  }

  .folder-raw {
    flex: 1;
  }

  .rename-form {
    flex: 1;
    min-width: 0;
  }
  .rename-input {
    flex: 1;
    width: 100%;
    background: var(--bg-2);
    border: 1px solid var(--red);
    color: var(--tx-5);
    padding: 3px 10px;
    border-radius: 3px;
  }
</style>
