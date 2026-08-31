<script lang="ts">
  // Two-step delete. The parent owns the armed key (one at a time, auto-
  // disarming) — a single tap on a phone must never take a file off the disk.
  let {
    armed = false,
    title = "Delete",
    onclick,
  }: {
    armed?: boolean;
    title?: string;
    onclick: (e: MouseEvent) => void;
  } = $props();
</script>

<button
  class="btn-icon danger sh-0"
  class:armed
  title={armed ? "Tap again to delete" : title}
  {onclick}
>
  {armed ? "Delete?" : "🗑"}
</button>

<style>
  .armed {
    opacity: 1;
    color: var(--red);
    background: var(--red-soft);
    font-size: 11px;
    font-weight: 600;
  }
</style>
