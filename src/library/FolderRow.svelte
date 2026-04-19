<script lang="ts">
  import { clean } from "../core/video";
  import FileRow from "./FileRow.svelte";

  type FileEntry = { name: string; root: string };

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
    sizeBytes = 0,
    diskTotal = 0,
    hidden = false,
    onToggleHidden,
    fmtBytes,
  }: {
    dir: string;
    files: FileEntry[];
    jobs: Job[];
    idx: number;
    editing: string | null;
    val: string;
    startEdit: (path: string, name: string) => void;
    cancelEdit: () => void;
    confirmEdit: (e?: Event) => void;
    onDelFile: (f: string) => void;
    onDelDir: () => void;
    sizeBytes?: number;
    diskTotal?: number;
    hidden?: boolean;
    onToggleHidden?: () => void;
    fmtBytes?: (b: number) => string;
  } = $props();

  const pct = $derived(diskTotal > 0 ? (sizeBytes / diskTotal) * 100 : 0);

  const converting = $derived(
    jobs.some((j) => files.some((f) => f.name === j.name)),
  );

  const uniformRoot = $derived.by(() => {
    if (!files.length) return null;
    const r = files[0].root;
    return files.every((f) => f.root === r) ? r : null;
  });

  function focus(el: HTMLInputElement) {
    el.focus();
    el.select();
  }
</script>

<details class="folder rx5 flow-h" class:is-hidden={hidden} style="--i:{idx}">
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
      {#if uniformRoot}
        <span class="disk-tag fs-xs" data-root={uniformRoot}>{uniformRoot}</span
        >
      {/if}
      <span class="fs-sm tx-1 sh-0" style="margin-left:auto">
        {#if sizeBytes > 0 && fmtBytes}
          {fmtBytes(sizeBytes)} · {pct.toFixed(1)}% of disk ·
        {/if}
        {files.length} files
      </span>

      {#if onToggleHidden}
        <button
          class="btn-icon eye-btn"
          title={hidden ? "Unhide from Home" : "Hide from Home"}
          onclick={(e) => {
            e.stopPropagation();
            onToggleHidden?.();
          }}
        >
          {#key hidden}
            <span class="eye-icon d-b">{hidden ? "🙈" : "👁"}</span>
          {/key}
        </button>
      {/if}
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
    {#each files as f (f.name + "@" + f.root)}
      <FileRow
        {dir}
        f={f.name}
        root={uniformRoot ? null : f.root}
        job={jobs.find((j) => j.name === f.name) ?? null}
        {editing}
        bind:val
        {startEdit}
        {cancelEdit}
        {confirmEdit}
        onDelete={() => onDelFile(f.name)}
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
    transition:
      opacity 0.2s,
      border-color 0.2s;
  }
  .folder[open] > .folder-hd {
    border-bottom: 1px solid var(--bg-4);
  }
  .folder.is-hidden {
    opacity: 0.55;
  }
  .folder.is-hidden:hover {
    opacity: 0.85;
  }
  .eye-icon {
    animation: pop-in 0.25s cubic-bezier(0.2, 0.9, 0.3, 1.4);
    display: inline-block;
  }
  .folder.is-hidden .folder-hd {
    background: repeating-linear-gradient(
      -45deg,
      var(--bg-3),
      var(--bg-3) 8px,
      var(--bg-2) 8px,
      var(--bg-2) 16px
    );
  }

  .folder-hd {
    padding: 10px 14px;
    background: var(--bg-3);
    user-select: none;
    transition: background 0.15s;
  }
  .folder-hd:hover {
    background: var(--bg-4);
  }
  .folder-hd:active {
    background: var(--bg-5);
  }
  @media (hover: hover) {
    .folder-hd:hover .btn-icon {
      opacity: 1;
    }
  }
  @media (max-width: 640px) {
    .folder-hd {
      padding: 12px 10px;
    }
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
    animation: breathe 1.4s ease-in-out infinite;
  }

  .disk-tag {
    padding: 1px 6px;
    border-radius: 3px;
    background: var(--bg-4);
    color: var(--tx-4);
    letter-spacing: 0.02em;
    flex-shrink: 0;
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
