<script lang="ts">
  let {
    disks,
    fmtBytes,
    onAdd,
    onAddTorrent,
  }: {
    disks: DiskInfo[];
    fmtBytes: (b: number) => string;
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
  <select class="field" bind:value={dir}>
    {#each disks as d}
      <option value={d.path}>
        {d.root} ({fmtBytes(d.free)} free)
      </option>
    {/each}
  </select>

  <button class="btn-action ptr fs-xs" onclick={submit}>Add</button>
</div>

<style>
  .form-wrap {
    padding: 12px 40px;
    background: rgba(13, 11, 18, 0.5);
    backdrop-filter: blur(10px) saturate(140%);
    -webkit-backdrop-filter: blur(10px) saturate(140%);
    border-bottom: 1px solid var(--glass-bd);
    top: 0;
    z-index: 9;
  }
  select {
    cursor: pointer;
    white-space: nowrap;
  }
</style>
