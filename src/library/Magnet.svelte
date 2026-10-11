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
  let drag = $state(false);
  let fileInput: HTMLInputElement | undefined;

  $effect(() => {
    if (disks.length > 0 && !dir) dir = disks[0].path;
  });

  // The button used to say "Add" and silently open a file picker when empty.
  // Naming what it will do costs one derived value.
  let kind = $derived(
    !magnet.trim()
      ? ""
      : /^magnet:/i.test(magnet)
        ? "magnet"
        : /^https?:\/\//i.test(magnet)
          ? "url"
          : "?",
  );
  let action = $derived(
    kind === "" ? "Upload .torrent" : kind === "url" ? "Fetch" : "Add",
  );

  function submit() {
    if (!magnet.trim()) {
      fileInput?.click();
      return;
    }
    onAdd(magnet.trim(), dir);
    magnet = "";
  }

  function take(file: File | null | undefined) {
    if (!file) return;
    onAddTorrent(file, dir);
  }

  function onFileSelected(e: Event) {
    const input = e.target as HTMLInputElement;
    take(input.files?.[0]);
    input.value = "";
  }

  function onDrop(e: DragEvent) {
    e.preventDefault();
    drag = false;
    take(
      [...(e.dataTransfer?.files ?? [])].find((f) => f.name.endsWith(".torrent")),
    );
  }
</script>

<div class="form-wrap f al-ct g10 p-stx" class:drag>
  <!-- The drop handlers live on the field, not the bar: it is the obvious
       target and an input needs no invented role to take a pointer event. -->
  <input
    class="field url"
    placeholder={drag
      ? "drop it"
      : "magnet:?xt=…, https://…, or a YouTube URL — or drop a .torrent here"}
    bind:value={magnet}
    onkeydown={(e) => e.key === "Enter" && submit()}
    ondragover={(e) => {
      e.preventDefault();
      drag = true;
    }}
    ondragleave={() => (drag = false)}
    ondrop={onDrop}
  />
  <input
    bind:this={fileInput}
    type="file"
    accept=".torrent"
    style="display:none"
    onchange={onFileSelected}
  />

  {#if kind}
    <span class="kind fs-xs num" class:bad={kind === "?"}>{kind}</span>
  {/if}

  <select class="field ptr" bind:value={dir}>
    {#each disks as d}
      <option value={d.path}>
        {d.root} ({bytes(d.free)} free)
      </option>
    {/each}
  </select>

  <button class="btn-action ptr fs-xs" onclick={submit}>{action}</button>
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
    transition:
      background var(--motion) var(--ease-out),
      box-shadow var(--motion) var(--ease-out);
  }
  .form-wrap:has(.field:focus) {
    background: color-mix(in oklab, var(--bg-1) 88%, transparent);
  }
  .form-wrap:has(.field:focus) select {
    border-color: var(--glass-bd);
  }
  .form-wrap.drag {
    background: color-mix(in oklab, var(--bg-1) 92%, transparent);
    box-shadow: inset 0 0 0 1px var(--red-glow);
  }
  select {
    white-space: nowrap;
  }

  .kind {
    padding: 3px 8px;
    border-radius: 999px;
    background: var(--glass);
    border: 1px solid var(--glass-bd);
    color: var(--tx-3);
    letter-spacing: 0.06em;
    flex-shrink: 0;
    transition: color var(--motion-fast) var(--ease-out);
  }
  .kind.bad {
    color: var(--red);
    border-color: color-mix(in oklab, var(--red) 40%, transparent);
  }

  /* Was an inline flex:1, which sets flex-basis:0 and beats any stylesheet
     rule — so on a phone the field collapsed to about two characters wide
     instead of wrapping the disk picker and button onto the next line. */
  .url {
    flex: 1;
    min-width: 0;
  }

  @media (max-width: 640px) {
    .form-wrap {
      padding: 10px 16px;
      flex-wrap: wrap;
    }
    .url {
      flex-basis: 100%;
    }
  }

  
</style>
