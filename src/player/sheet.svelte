<script lang="ts">
  import type { Snippet } from "svelte";

  let {
    open,
    title,
    side = "right",
    onClose,
    children,
  }: {
    open: boolean;
    title: string;
    side?: "right" | "mid";
    onClose: () => void;
    children: Snippet;
  } = $props();

  let el = $state<HTMLElement>();
  let restore: HTMLElement | null = null;

  // The top layer brings Escape and light-dismiss and can't be trapped by an
  // ancestor's stacking context; without it, `.show` drives a plain fixed panel.
  const popOk =
    typeof HTMLElement !== "undefined" && "popover" in HTMLElement.prototype;

  $effect(() => {
    const node = el as any;
    if (!node?.isConnected) return;

    if (open) restore = document.activeElement as HTMLElement;

    if (popOk) {
      try {
        if (open) node.showPopover?.();
        else node.hidePopover?.();
      } catch {
        // showPopover throws if the state already matches; nothing to repair.
      }
    }

    if (open) node.focus?.({ preventScroll: true });
    else restore?.focus?.({ preventScroll: true });
  });

  // The fallback path has no top layer, so Escape is ours to handle. Capture,
  // so the player's own keydown handler doesn't also see it.
  $effect(() => {
    if (!open || popOk) return;

    const esc = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      onClose();
    };
    window.addEventListener("keydown", esc, true);

    return () => window.removeEventListener("keydown", esc, true);
  });
</script>

<aside
  bind:this={el}
  class="sheet pl-pane {side}"
  class:show={open}
  tabindex="-1"
  inert={!open}
  popover={popOk ? "auto" : undefined}
  ontoggle={(e: any) => e.newState === "closed" && open && onClose()}
>
  <div class="hd f al-ct j-bw">
    <span class="ttl">{title}</span>
    <button class="x ptr tx-1" aria-label="Close" onclick={onClose}>✕</button>
  </div>

  {@render children()}
</aside>

<style>
  .sheet {
    position: fixed;
    z-index: 14;
    margin: 0;
    border: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    transition:
      transform var(--m, 260ms) var(--ease-out),
      opacity var(--m, 260ms) var(--ease-out);
  }
  .sheet:focus {
    outline: none;
  }

  .sheet.right {
    top: 0;
    right: 0;
    bottom: 0;
    /* The UA popover sets `inset: 0`; without left:auto the element is
       over-constrained and pins to the left edge. */
    left: auto;
    width: min(320px, 86vw);
    padding: calc(16px + var(--safe-t, 0px)) 18px
      calc(18px + var(--safe-b, 0px));
    border-left: 1px solid var(--glass-bd);
    border-radius: 0;
    transform: translateX(101%);
  }
  .sheet.right.show {
    transform: translateX(0);
  }

  .sheet.mid {
    inset: auto;
    top: 50%;
    left: 50%;
    width: min(760px, 92vw);
    max-height: min(78vh, 720px);
    padding: 20px 24px calc(22px + var(--safe-b, 0px));
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-xl);
    opacity: 0;
    transform: translate(-50%, -46%) scale(0.97);
  }
  .sheet.mid.show {
    opacity: 1;
    transform: translate(-50%, -50%) scale(1);
  }

  /* No top layer on the fallback path, so a closed sheet has to remove itself:
     inert stops it taking clicks but a centred one still sits over the picture,
     and an off-screen one still widens the page. */
  @supports not selector(:popover-open) {
    .sheet {
      transition-property: transform, opacity, display;
      transition-behavior: allow-discrete;
    }
    .sheet:not(.show) {
      display: none;
    }
  }

  /* The UA hides a closed popover outright, so the motion has to survive a
     discrete display change. The fallback path never sees these rules. */
  .sheet[popover] {
    transition-property: transform, opacity, display, overlay;
    transition-duration: var(--m, 260ms);
    transition-timing-function: var(--ease-out);
    transition-behavior: allow-discrete;
  }
  .sheet.right:popover-open {
    transform: translateX(0);
  }
  .sheet.mid:popover-open {
    opacity: 1;
    transform: translate(-50%, -50%) scale(1);
  }
  @starting-style {
    .sheet.right:popover-open {
      transform: translateX(101%);
    }
    .sheet.mid:popover-open {
      opacity: 0;
      transform: translate(-50%, -46%) scale(0.97);
    }
  }
  .sheet[popover]::backdrop {
    background: rgba(3, 2, 6, 0.42);
    backdrop-filter: blur(3px);
    -webkit-backdrop-filter: blur(3px);
    transition:
      opacity var(--m, 260ms) var(--ease-out),
      display var(--m, 260ms) allow-discrete;
  }

  .hd {
    margin-bottom: 10px;
  }
  .ttl {
    font-family: var(--font-display);
    font-size: 16px;
    font-weight: 600;
    letter-spacing: -0.02em;
    color: var(--tx-5);
  }
  .x {
    background: none;
    border: none;
    font-size: 14px;
    min-width: 32px;
    min-height: 32px;
    border-radius: var(--r-sm);
    transition: color var(--m-fast, 130ms) var(--ease-out);
  }
  .x:hover {
    color: var(--tx-5);
  }
  @media (hover: none) {
    .x { min-width: 44px; min-height: 44px; }
  }
</style>
