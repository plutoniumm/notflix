<script lang="ts">
  // The "←" header back link — was byte-identical (markup + .back/.back:hover
  // CSS) in Search and Manage's Header. Self-contained so no Svelte scoping
  // breakage (unlike trying to extract the whole <header>).
  let { href = "/" }: { href?: string } = $props();
</script>

<a {href} class="back fs tx-3" aria-label="Back">←</a>

<style>
  .back {
    padding: 4px 10px;
    border-radius: var(--r-md);
    transition:
      color 0.18s var(--ease-out),
      background 0.18s var(--ease-out);
  }
  .back:hover {
    color: var(--tx-5);
    background: var(--glass);
  }
</style>
