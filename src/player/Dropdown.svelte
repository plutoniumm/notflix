<script lang="ts">
  import { onMount } from "svelte";
  import { clickOutside } from "../core/clickOutside";
  import type { Snippet } from "svelte";

  let {
    onClose,
    width = "260px",
    maxHeight = "none",
    children,
  }: {
    onClose: () => void;
    width?: string;
    maxHeight?: string;
    children: Snippet;
  } = $props();

  onMount(() => clickOutside(onClose));
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="dropdown"
  style:width
  style:max-height={maxHeight}
  onclick={(e) => e.stopPropagation()}
>
  {@render children()}
</div>

<style>
  .dropdown {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    overflow-y: auto;
    background: rgba(13, 11, 18, 0.78);
    backdrop-filter: blur(20px) saturate(140%);
    -webkit-backdrop-filter: blur(20px) saturate(140%);
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-lg);
    z-index: 100;
    box-shadow: var(--sh-3);
    transform-origin: top right;
    padding: 4px;
    animation: dropdown-in 0.22s var(--ease-snap);
  }

  .dropdown :global(.item) {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 9px 11px;
    border-radius: var(--r-md);
    text-align: left;
    color: var(--tx-3);
    font-size: 13px;
    font-weight: 500;
    transition: background 0.15s var(--ease-out), color 0.15s var(--ease-out);
  }
  .dropdown :global(.item:hover) {
    background: var(--glass);
    color: var(--tx-5);
  }
  .dropdown :global(.item.active) {
    color: var(--tx-5);
    background: var(--glass-2);
  }
  .dropdown :global(.item.busy) {
    opacity: 0.6;
    pointer-events: none;
  }

  .dropdown :global(.bullet) {
    width: 14px;
    text-align: center;
    flex-shrink: 0;
    color: var(--red);
  }

  .dropdown :global(.section-hd) {
    padding: 10px 12px 6px;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.1em;
    font-weight: 600;
    color: var(--tx-2);
  }
  .dropdown :global(.section-hd.row) {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .dropdown :global(.divider) {
    height: 1px;
    background: var(--glass-bd);
    margin: 6px 4px;
  }

  @keyframes dropdown-in {
    from {
      opacity: 0;
      transform: translateY(-6px) scale(0.94);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }
</style>
