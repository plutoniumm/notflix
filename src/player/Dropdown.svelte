<script lang="ts">
  import { onMount } from "svelte";
  import { clickOutside } from "../core/clickOutside";
  import type { Snippet } from "svelte";

  let {
    open,
    onClose,
    width = "260px",
    maxHeight = "none",
    children,
  }: {
    open: boolean;
    onClose: () => void;
    width?: string;
    maxHeight?: string;
    children: Snippet;
  } = $props();

  let el = $state<HTMLElement>();

  // Always mounted, hidden by `.out`, so the panel leaves with the motion it
  // arrived with. Dismissal only listens while it is open.
  $effect(() => {
    if (!open) return;

    const from = document.activeElement as HTMLElement | null;

    // onClose is read only inside the closures, so the effect depends on `open`
    // alone — a re-rendered parent must not tear the listeners down.
    const drop = clickOutside(() => onClose());

    // Capture, so the player's own keydown handler doesn't also see these.
    const keys = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
        return;
      }

      const items = [...(el?.querySelectorAll<HTMLElement>(".item") ?? [])];
      if (!items.length) return;

      const i = items.indexOf(document.activeElement as HTMLElement);
      let to: number;
      if (e.key === "ArrowDown") to = (i + 1) % items.length;
      else if (e.key === "ArrowUp") to = (i <= 0 ? items.length : i) - 1;
      else if (e.key === "Home") to = 0;
      else if (e.key === "End") to = items.length - 1;
      else return;

      e.preventDefault();
      e.stopPropagation();
      items[to].focus();
    };
    window.addEventListener("keydown", keys, true);

    return () => {
      drop();
      window.removeEventListener("keydown", keys, true);
      // Only if the closing panel still owns focus — a click elsewhere has
      // already put it where the user wanted it.
      if (el?.contains(document.activeElement)) from?.focus?.();
    };
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={el}
  class="dropdown pl-pane pl-enter"
  class:out={!open}
  inert={!open}
  style:width
  style:max-height={maxHeight}
  onclick={(e) => e.stopPropagation()}
>
  {@render children()}
</div>

<style>
  .dropdown {
    position: absolute;
    top: calc(100% + 10px);
    right: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    border-radius: var(--r-lg);
    z-index: 100;
    transform-origin: top right;
    padding: 5px;
    scrollbar-width: thin;
  }
  .dropdown.out {
    display: none;
    opacity: 0;
    transform: translateY(-8px) scale(0.94);
  }
  @starting-style {
    .dropdown {
      opacity: 0;
      transform: translateY(-8px) scale(0.94);
    }
  }

  .dropdown :global(.item) {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 9px 11px;
    min-height: 38px;
    border-radius: var(--r-md);
    text-align: left;
    color: var(--tx-3);
    font-size: 13px;
    font-weight: 500;
    letter-spacing: -0.006em;
    transition:
      background var(--m-fast, 130ms) var(--ease-out),
      color var(--m-fast, 130ms) var(--ease-out);
  }
  @media (hover: none) {
    .dropdown :global(.item) { min-height: 44px; }
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
    padding: 11px 12px 6px;
    font-family: var(--font-display);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
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
</style>
