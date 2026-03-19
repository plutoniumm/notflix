<script lang="ts">
  let {
    disks,
    fmtBytes,
    onAdd,
  }: {
    disks: DiskInfo[];
    fmtBytes: (b: number) => string;
    onAdd: (magnet: string, dir: string) => void;
  } = $props();

  let magnet = $state("");
  let dir = $state(disks[0]?.path ?? "");

  $effect(() => {
    if (disks.length > 0 && !dir) dir = disks[0].path;
  });

  function submit() {
    if (!magnet.trim()) return;

    onAdd(magnet.trim(), dir);
    magnet = "";
  }
</script>

<div class="form-wrap f al-ct g10 p-stx">
  <input
    class="input bg-3 rx5 fs-sm tx-5"
    placeholder="magnet:?xt=…"
    bind:value={magnet}
    onkeydown={(e) => e.key === "Enter" && submit()}
  />
  <select class="bg-3 rx5 fs-sm tx-5" bind:value={dir}>
    {#each disks as d}
      <option value={d.path}>
        {d.root} ({fmtBytes(d.free)} free)
      </option>
    {/each}
  </select>

  <button class="btn ptr rx5 fs-xs" onclick={submit}> Add </button>
</div>

<style>
  .form-wrap {
    padding: 10px 40px;
    background: var(--bg-2);
    border-bottom: 1px solid var(--bg-3);
  }
  .input {
    flex: 1;
    min-width: 0;
    border: 1px solid var(--bg-4);
    padding: 6px 12px;
  }
  select {
    border: 1px solid var(--bg-4);
    padding: 6px 10px;
    white-space: nowrap;
  }
  .btn {
    background: var(--red);
    color: #fff;
    padding: 6px 16px;
    white-space: nowrap;
  }
  .btn:hover {
    opacity: 0.85;
  }
</style>
