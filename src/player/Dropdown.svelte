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
    top: calc(100% + 6px);
    right: 0;
    overflow-y: auto;
    background: #0d0d0dd9;
    backdrop-filter: blur(14px) saturate(130%);
    border: 1px solid #fff2;
    border-radius: 10px;
    z-index: 100;
    box-shadow: 0 12px 40px #0009, 0 0 0 1px #0006;
    transform-origin: top right;
    animation: dropdown-in 0.18s cubic-bezier(0.2, 0.9, 0.3, 1.2);
  }

  .dropdown :global(.item) {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 10px 12px;
    text-align: left;
    color: var(--tx-4);
    font-size: 13px;
    transition: background 0.1s;
  }
  .dropdown :global(.item:hover) {
    background: #fff1;
    color: var(--tx-5);
  }
  .dropdown :global(.item.active) {
    color: var(--tx-5);
  }
  .dropdown :global(.item.busy) {
    opacity: 0.6;
    pointer-events: none;
  }

  .dropdown :global(.bullet) {
    width: 14px;
    text-align: center;
    flex-shrink: 0;
  }

  .dropdown :global(.section-hd) {
    padding: 8px 12px 4px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--tx-3);
  }
  .dropdown :global(.section-hd.row) {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .dropdown :global(.divider) {
    height: 1px;
    background: #fff1;
    margin: 4px 0;
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
