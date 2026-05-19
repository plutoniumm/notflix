<script lang="ts">
  import type { Snippet } from "svelte";

  // Centered message block. `plain` = the Search hint/no-result text (was
  // `.msg tx-1`); `hero` = the Home grid empty (was `.no-results`,
  // display-font). gridSpan makes it span a CSS-grid row (Home sits in a
  // grid). Variants reproduce the prior CSS exactly.
  let {
    variant = "plain",
    gridSpan = false,
    children,
  }: {
    variant?: "plain" | "hero";
    gridSpan?: boolean;
    children?: Snippet;
  } = $props();
</script>

<p class="es {variant}" class:span={gridSpan}>{@render children?.()}</p>

<style>
  .es {
    text-align: center;
  }
  .plain {
    padding: 60px 40px;
    color: var(--tx-1);
  }
  .hero {
    padding: 60px 0;
    color: var(--tx-2);
    font-family: var(--font-display);
    font-size: 18px;
  }
  .span {
    grid-column: 1 / -1;
  }
</style>
