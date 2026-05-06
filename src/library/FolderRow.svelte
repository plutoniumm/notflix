<script lang="ts">
  import { clean } from "../core/video";
  import FileRow from "./FileRow.svelte";

  type FileEntry = { name: string; root: string; corrupt?: boolean };

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
        corrupt={f.corrupt}
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
    border: 1px solid var(--glass-bd);
    background: rgba(22, 19, 28, 0.5);
    backdrop-filter: blur(10px);
    -webkit-backdrop-filter: blur(10px);
    border-radius: var(--r-lg);
    margin-bottom: 10px;
    animation: slide-up 0.32s var(--ease-out) both;
    animation-delay: calc(var(--i) * 40ms);
    transition:
      opacity 0.2s,
      border-color 0.2s,
      box-shadow 0.25s var(--ease-out);
  }
  .folder[open] {
    box-shadow: var(--sh-2);
  }
  .folder[open] > .folder-hd {
    border-bottom: 1px solid var(--glass-bd);
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
      rgba(255, 255, 255, 0.04),
      rgba(255, 255, 255, 0.04) 8px,
      transparent 8px,
      transparent 16px
    );
  }

  .folder-hd {
    padding: 12px 16px;
    background: transparent;
    border-radius: var(--r-lg);
    user-select: none;
    transition: background 0.18s var(--ease-out);
  }
  .folder[open] > .folder-hd {
    border-radius: var(--r-lg) var(--r-lg) 0 0;
  }
  .folder-hd:hover {
    background: var(--glass);
  }
  .folder-hd:active {
    background: var(--glass-2);
  }
  @media (hover: hover) {
    .folder-hd:hover .btn-icon {
      opacity: 1;
    }
  }
  @media (max-width: 640px) {
    .folder-hd {
      padding: 12px 12px;
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
    box-shadow: 0 0 8px var(--red-glow);
    animation: breathe 1.4s ease-in-out infinite;
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
    padding: 5px 12px;
    border-radius: var(--r-md);
    font-family: inherit;
    box-shadow: 0 0 0 4px var(--red-soft);
  }
</style>
