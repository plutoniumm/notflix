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

<div class="backdrop" onclick={onClose} role="presentation">
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div
    class="modal"
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
  >
    <div class="modal-header">
      <h3>Select Subtitles</h3>
      <button class="close" onclick={onClose}>✕</button>
    </div>
    <ul class="list">
      {#each results as r (r.file_id)}
        <li>
          <button
            class="sub-item"
            class:downloading={busy === r.file_id}
            onclick={() => pick(r.file_id)}
          >
            {#if r.hash_match}<span class="badge">✓ exact match</span>{/if}
            <span class="release">{r.release || "Unknown release"}</span>
            <span class="count"
              >{r.download_count?.toLocaleString() ?? 0} dl</span
            >
            {#if busy === r.file_id}
              <span class="spinner">↻</span>
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.8);
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .modal {
    background: #1f1f1f;
    border: 1px solid #333;
    border-radius: 6px;
    width: 560px;
    max-width: 90vw;
    max-height: 70vh;
    display: flex;
    flex-direction: column;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 16px 20px;
    border-bottom: 1px solid #333;
  }

  h3 {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
  }

  .close {
    background: none;
    border: none;
    color: #999;
    font-size: 1rem;
    padding: 4px 8px;
  }
  .close:hover {
    color: #fff;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 8px 0;
    overflow-y: auto;
  }

  li {
    list-style: none;
  }

  .sub-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 20px;
    cursor: pointer;
    border-bottom: 1px solid #2a2a2a;
    transition: background 0.1s;
    font-size: 13px;
    width: 100%;
    background: none;
    border-top: none;
    border-left: none;
    border-right: none;
    color: #fff;
    font-family: inherit;
    text-align: left;
  }
  .sub-item:hover {
    background: #2a2a2a;
  }
  .sub-item.downloading {
    opacity: 0.7;
    pointer-events: none;
  }

  .badge {
    background: #1a5c1a;
    color: #4caf50;
    font-size: 11px;
    padding: 2px 6px;
    border-radius: 3px;
    flex-shrink: 0;
  }

  .release {
    flex: 1;
    color: #ddd;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    color: #666;
    font-size: 12px;
    flex-shrink: 0;
  }

  .spinner {
    color: #e50914;
    animation: spin 0.8s linear infinite;
    display: inline-block;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
