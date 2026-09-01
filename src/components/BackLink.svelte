<script lang="ts">
  let { href = "/" }: { href?: string } = $props();
</script>

<a {href} class="back fs tx-3" aria-label="Back">←</a>

<style>
  .back {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border-radius: 50%;
    border: 1px solid transparent;
    transition:
      color var(--motion-fast, 0.18s) var(--ease-out),
      background var(--motion-fast, 0.18s) var(--ease-out),
      border-color var(--motion-fast, 0.18s) var(--ease-out),
      translate var(--motion-fast, 0.18s) var(--ease-snap);
  }
  .back:hover {
    color: var(--tx-5);
    background: var(--glass);
    border-color: var(--glass-bd);
    translate: -2px 0;
  }
  .back:active {
    scale: 0.92;
  }

  @media (prefers-reduced-motion: reduce) {
    .back {
      transition-duration: 1ms;
    }
  }
</style>
