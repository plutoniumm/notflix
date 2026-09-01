<script lang="ts">
  import { clean } from "../core/video";
  import { bytes, pct } from "../core/fmt";
  import DeleteBtn from "../components/DeleteBtn.svelte";
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
    armed = null,
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
    armed?: string | null;
  } = $props();

  const share = $derived(diskTotal > 0 ? (sizeBytes / diskTotal) * 100 : 0);

  const converting = $derived(
    jobs.some((j) => files.some((f) => f.name === j.name)),
  );

  const broken = $derived(files.filter((f) => f.corrupt).length);

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

<details
  class="folder flow-h"
  class:is-hidden={hidden}
  class:converting
  class:broken={broken > 0}
  style="--i:{idx}"
>
  <summary class="folder-hd f al-ct g10 ptr">
    <span class="caret fs-xs sh-0 dead">▸</span>

    {#if editing === dir}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <form
        class="rename-form f"
        onsubmit={confirmEdit}
        onclick={(e) => e.stopPropagation()}
      >
        <input
          class="field rename-input"
          bind:value={val}
          onkeydown={(e) => e.key === "Escape" && cancelEdit()}
          onblur={confirmEdit}
          use:focus
        />
      </form>
    {:else}
      <span class="fw5 fs-md name trunc">
        {dir === "." ? "Root" : clean(dir) || dir}
      </span>

      {#if uniformRoot}
        <span class="disk-tag fs-xs" data-root={uniformRoot}>{uniformRoot}</span
        >
      {/if}
      {#if broken > 0}
        <span class="flag broken-flag fs-xs" title="{broken} unplayable">
          {broken} corrupt
        </span>
      {/if}
      {#if converting}
        <span class="flag conv-flag fs-xs">converting</span>
      {/if}

      <span class="stat fs-xs tx-1">
        {#if sizeBytes > 0}
          <span class="num">{bytes(sizeBytes)}</span>
          <span class="share" style="--p:{Math.min(100, share)}%"></span>
          <span class="num">{pct(share, 1)}</span>
        {/if}
        <span class="num files">{files.length}</span>
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
          title="Rename folder"
          onclick={(e) => {
            e.stopPropagation();
            startEdit(dir, dir);
          }}
        >
          ✏️
        </button>
        <DeleteBtn
          armed={armed === dir}
          title="Delete folder"
          onclick={(e) => {
            e.stopPropagation();
            onDelDir();
          }}
        />
      {:else}
        <div style="width:32px">&nbsp;</div>
      {/if}
    {/if}
  </summary>

  <ul class="m0 p0">
    {#each files as f (f.name + "@" + f.root)}
      <FileRow
        {dir}
        {armed}
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
    position: relative;
    border: 1px solid var(--glass-bd);
    background: color-mix(in oklab, var(--bg-3) 55%, transparent);
    backdrop-filter: blur(10px);
    -webkit-backdrop-filter: blur(10px);
    border-radius: var(--r-lg);
    margin-bottom: 8px;
    animation: slide-up var(--motion, 0.32s) var(--ease-out) both;
    animation-delay: calc(min(var(--i, 0), 12) * 34ms);
    transition:
      opacity var(--motion, 0.2s),
      border-color var(--motion, 0.2s),
      box-shadow var(--motion, 0.25s) var(--ease-out);
  }
  .folder[open] {
    box-shadow: var(--sh-2);
    border-color: rgb(255 255 255 / 0.12);
  }
  .folder[open] > .folder-hd {
    border-bottom: 1px solid var(--glass-bd);
    border-radius: var(--r-lg) var(--r-lg) 0 0;
  }

  .folder::before {
    content: "";
    position: absolute;
    left: 0;
    top: 8px;
    bottom: 8px;
    width: 2px;
    border-radius: 999px;
    background: transparent;
    transition: background var(--motion, 0.25s) var(--ease-out);
  }
  .folder.converting::before {
    background: var(--red);
    box-shadow: 0 0 10px var(--red-glow);
  }
  .folder.broken::before {
    background: var(--gold);
    box-shadow: 0 0 10px color-mix(in oklab, var(--gold) 60%, transparent);
  }

  .folder.is-hidden {
    opacity: 0.5;
  }
  .folder.is-hidden:hover {
    opacity: 0.85;
  }
  .folder.is-hidden .folder-hd {
    background: repeating-linear-gradient(
      -45deg,
      rgb(255 255 255 / 0.035),
      rgb(255 255 255 / 0.035) 8px,
      transparent 8px,
      transparent 16px
    );
  }
  .eye-icon {
    animation: pop-in 0.25s var(--ease-spring);
    display: inline-block;
  }

  .folder-hd {
    padding: 11px 16px 11px 14px;
    border-radius: var(--r-lg);
    user-select: none;
    list-style: none;
    transition: background var(--motion-fast, 0.18s) var(--ease-out);
  }
  .folder-hd::-webkit-details-marker {
    display: none;
  }
  .folder-hd:focus-within,
  .folder-hd:hover {
    background: var(--glass);
  }
  .folder-hd:active {
    background: var(--glass-2);
  }
  @media (hover: hover) {
    .folder-hd:focus-within :global(.btn-icon),
    .folder-hd:hover :global(.btn-icon) {
      opacity: 1;
    }
  }

  .caret {
    width: 12px;
    color: var(--tx-1);
    transition: rotate var(--motion, 0.24s) var(--ease-snap);
  }
  .folder[open] .caret {
    rotate: 90deg;
    color: var(--tx-3);
  }

  .name {
    min-width: 0;
  }

  .flag {
    flex-shrink: 0;
    padding: 2px 8px;
    border-radius: 999px;
    font-weight: 600;
    letter-spacing: 0.02em;
  }
  .broken-flag {
    color: var(--gold);
    background: color-mix(in oklab, var(--gold) 16%, transparent);
    border: 1px solid color-mix(in oklab, var(--gold) 34%, transparent);
  }
  .conv-flag {
    color: var(--red);
    background: var(--red-soft);
    border: 1px solid color-mix(in oklab, var(--red) 34%, transparent);
    animation: breathe 1.8s ease-in-out infinite;
  }

  .stat {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .num {
    font-variant-numeric: tabular-nums;
    min-width: 52px;
    text-align: right;
  }
  .files::after {
    content: " files";
    color: var(--bg-5);
  }
  .share {
    width: 46px;
    height: 4px;
    border-radius: 999px;
    background: rgb(255 255 255 / 0.07);
    background-image: linear-gradient(
      90deg,
      var(--tx-2) var(--p, 0%),
      transparent var(--p, 0%)
    );
  }

  .rename-form {
    flex: 1;
    min-width: 0;
  }
  .rename-input {
    flex: 1;
    width: 100%;
    border-color: var(--red);
    box-shadow: 0 0 0 4px var(--red-soft);
  }

  /* Chrome-only disclosure animation; the folder opens correctly without it. */
  @supports selector(::details-content) {
    .folder {
      interpolate-size: allow-keywords;
    }
    .folder::details-content {
      block-size: 0;
      overflow: hidden;
      transition:
        block-size var(--motion, 0.3s) var(--ease-out),
        content-visibility var(--motion, 0.3s) allow-discrete;
    }
    .folder[open]::details-content {
      block-size: auto;
    }
  }

  @media (max-width: 640px) {
    .folder-hd {
      padding: 11px 10px;
      flex-wrap: wrap;
    }
    .stat {
      margin-left: auto;
    }
    .share {
      display: none;
    }
    .num {
      min-width: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .folder,
    .caret,
    .eye-icon,
    .conv-flag {
      animation: none;
      transition-duration: 1ms;
    }
  }
</style>
