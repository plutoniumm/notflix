<script lang="ts">
  // Two-step: the parent owns the armed key. One tap must never delete a file.
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
    color: #fff;
    background: var(--red);
    box-shadow: 0 0 0 3px var(--red-soft);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.02em;
    border-radius: 999px;
    animation: arm 0.26s var(--ease-spring);
  }

  @keyframes arm {
    from {
      scale: 0.8;
      box-shadow: 0 0 0 0 var(--red-soft);
    }
  }

  
</style>
