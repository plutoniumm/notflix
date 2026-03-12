<script lang="ts">
  let {
    results,
    onSelect,
    onClose,
  }: {
    results: any[];
    onSelect: (fid: number) => void;
    onClose: () => void;
  } = $props();

  let busy = $state<number | null>(null);

  async function pick(fid: number) {
    busy = fid;
    await onSelect(fid);
    busy = null;
  }
</script>

<div class="backdrop p-fix cc" onclick={onClose} role="presentation">
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div
    class="modal f f-col rx5"
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
  >
    <div class="modal-hd f al-ct j-bw">
      <h3 class="m0 fw6">Select Subtitles</h3>
      <button class="close c-muted" onclick={onClose}>✕</button>
    </div>

    <ul class="list m0 p0 flow-y-s">
      {#each results as r (r.file_id)}
        <li>
          <button
            class="sub-item f al-ct g10 w-100 fs-base ptr"
            class:busy={busy === r.file_id}
            onclick={() => pick(r.file_id)}
          >
            {#if r.hash_match}
              <span class="badge fs-xs rx2 sh-0"> ✓ match </span>
            {/if}

            <span class="release trunc">
              {r.release || "Unknown release"}
            </span>
            <span class="fs-sm c-sub sh-0">
              {r.download_count?.toLocaleString() ?? 0} dl
            </span>

            {#if busy === r.file_id}
              <span class="spinner c-red">↻</span>
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
    background: rgba(0, 0, 0, 0.8);
    z-index: 1000;
  }

  .modal {
    background: #1f1f1f;
    border: 1px solid #333;
    width: 560px;
    max-width: 90vw;
    max-height: 70vh;
  }

  .modal-hd {
    padding: 16px 20px;
    border-bottom: 1px solid #333;
  }

  .close {
    background: none;
    border: none;
    font-size: 1rem;
    padding: 4px 8px;
  }
  .close:hover {
    color: #fff;
  }

  .list {
    list-style: none;
    padding: 8px 0;
  }
  li {
    list-style: none;
  }

  .sub-item {
    padding: 10px 20px;
    border-bottom: 1px solid #2a2a2a;
    background: none;
    border-top: none;
    border-left: none;
    border-right: none;
    color: #fff;
    text-align: left;
    transition: background 0.1s;
  }
  .sub-item:hover {
    background: #2a2a2a;
  }
  .sub-item.busy {
    opacity: 0.7;
    pointer-events: none;
  }

  .badge {
    background: #1a5c1a;
    color: #4caf50;
    padding: 2px 6px;
  }
  .release {
    flex: 1;
    color: #ddd;
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
