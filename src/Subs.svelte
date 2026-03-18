<script lang="ts">
  let { results, onSelect, onClose }: any = $props();
  let busy = $state<number | null>(null);

  async function pick(fid: number) {
    busy = fid;
    await onSelect(fid);
    busy = null;
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="backdrop p-fix cc" onclick={onClose}>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal f f-col rx5" onclick={(e) => e.stopPropagation()}>
    <div class="modal-hd f al-ct j-bw">
      <h3 class="m0 fw6">Select Subtitles</h3>
      <button class="p5 tx-3" onclick={onClose}>✕</button>
    </div>

    <ul class="list m0 p0 flow-y-s">
      {#each results as r (r.file_id)}
        <li>
          <button
            class="item f al-ct g10 w-100 fs ptr tl"
            class:busy={busy === r.file_id}
            onclick={() => pick(r.file_id)}
          >
            {#if r.hash_match}
              <span class="badge fs-xs rx2 sh-0"> ✓ match </span>
            {/if}

            <span class="release trunc">
              {r.release || "Unknown release"}
            </span>
            <span class="fs-sm tx-2 sh-0">
              {r.download_count?.toLocaleString() ?? 0} dl
            </span>

            {#if busy === r.file_id}
              <span class="spinner red">↻</span>
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  </div>
</div>

<style>
  .backdrop {
    inset: 0;
    background: #000c;
    z-index: 1000;
    animation: fade-in 0.2s ease;
  }

  .modal {
    background: var(--bg-3);
    border: 1px solid var(--bg-4);
    width: 560px;
    max-width: 90vw;
    max-height: 70vh;
    animation: slide-up 0.25s ease;
  }

  .modal-hd {
    padding: 16px 20px;
    border-bottom: 1px solid var(--bg-4);
  }

  .list {
    padding: 8px 0;
  }

  .item {
    padding: 10px 20px;
    border-bottom: 1px solid var(--bg-3);
    color: var(--tx-5);
    transition: background 0.1s;
  }
  .item:hover {
    background: var(--bg-3);
  }
  .item.busy {
    opacity: 0.7;
    pointer-events: none;
  }

  .badge {
    background: #152;
    color: var(--grn);
    padding: 2px 6px;
  }
  .release {
    flex: 1;
    color: var(--tx-4);
  }

  .spinner {
    animation: spin 0.8s linear infinite;
    display: inline-block;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
