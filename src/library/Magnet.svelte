<script lang="ts">
  import { bytes } from "../core/fmt";

  let {
    disks,
    onAdd,
    onAddTorrent,
  }: {
    disks: DiskInfo[];
    onAdd: (magnet: string, dir: string) => void;
    onAddTorrent: (file: File, dir: string) => void;
  } = $props();

  let magnet = $state("");
  let dir = $state("");
  let fileInput: HTMLInputElement | undefined;

  $effect(() => {
    if (disks.length > 0 && !dir) dir = disks[0].path;
  });

  function submit() {
    if (!magnet.trim()) {
      fileInput?.click();
      return;
    }
    onAdd(magnet.trim(), dir);
    magnet = "";
  }

  function onFileSelected(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    onAddTorrent(file, dir);
    input.value = "";
  }
</script>

<div class="form-wrap f al-ct g10 p-stx">
  <input
    class="field"
    style="flex:1; min-width:0"
    placeholder="magnet:?xt=…, https://…, or YouTube URL — click Add to upload .torrent"
    bind:value={magnet}
    onkeydown={(e) => e.key === "Enter" && submit()}
  />
  <input
    bind:this={fileInput}
    type="file"
    accept=".torrent"
    style="display:none"
    onchange={onFileSelected}
  />
  <select class="field ptr" bind:value={dir}>
    {#each disks as d}
      <option value={d.path}>
        {d.root} ({bytes(d.free)} free)
      </option>
    {/each}
  </select>

  <button class="btn-action ptr fs-xs" onclick={submit}>Add</button>
</div>

<style>
  .form-wrap {
    padding: 10px 40px;
    background: color-mix(in oklab, var(--bg-1) 62%, transparent);
    backdrop-filter: blur(12px) saturate(140%);
    -webkit-backdrop-filter: blur(12px) saturate(140%);
    border-bottom: 1px solid var(--glass-bd);
    top: 0;
    z-index: 9;
    transition: background var(--motion, 0.3s) var(--ease-out);
  }
  /* The bar only asserts itself while it's being used — the parent can't know
     a field three nodes down has focus without :has(). */
  .form-wrap:has(.field:focus) {
    background: color-mix(in oklab, var(--bg-1) 88%, transparent);
  }
  .form-wrap:has(.field:focus) select {
    border-color: var(--glass-bd);
  }
  select {
    white-space: nowrap;
  }

  @media (max-width: 640px) {
    .form-wrap {
      padding: 10px 16px;
      flex-wrap: wrap;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .form-wrap {
      transition-duration: 1ms;
    }
  }
</style>
