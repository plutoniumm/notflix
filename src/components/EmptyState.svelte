<script lang="ts">
  import type { Snippet } from "svelte";

  // Centered message block. `plain` = an inline hint inside a flow of results;
  // `hero` = a screen with nothing on it, which gets a mark and room to
  // breathe. gridSpan makes it span a CSS-grid row (Home's grid).
  let {
    variant = "plain",
    gridSpan = false,
    icon = "",
    children,
  }: {
    variant?: "plain" | "hero";
    gridSpan?: boolean;
    icon?: string;
    children?: Snippet;
  } = $props();
</script>

<div class="es {variant}" class:span={gridSpan}>
  {#if icon && variant === "hero"}
    <div class="mark dead" aria-hidden="true">{icon}</div>
  {/if}
  <p class="msg m0">{@render children?.()}</p>
</div>

<style>
  .es {
    text-align: center;
    transition: opacity var(--motion-slow, 0.5s) var(--ease-out);
  }
  @starting-style {
    .es {
      opacity: 0;
    }
  }
  .plain {
    padding: 56px 40px;
    color: var(--tx-1);
    font-size: 14px;
  }
  .hero {
    padding: 88px 24px;
    color: var(--tx-2);
  }
  .hero .msg {
    font-family: var(--font-display);
    font-size: 17px;
    font-weight: 500;
    max-width: 34ch;
    margin: 0 auto;
    text-wrap: balance;
    line-height: 1.5;
  }
  /* The mark is a shape in the dark, not an illustration competing with the
     message — heavily dimmed and blurred into the surface behind it. */
  .mark {
    font-size: 52px;
    line-height: 1;
    margin-bottom: 20px;
    opacity: 0.28;
    filter: grayscale(0.5) drop-shadow(0 8px 24px rgb(0 0 0 / 0.6));
  }
  .span {
    grid-column: 1 / -1;
  }

  @media (prefers-reduced-motion: reduce) {
    .es {
      transition-duration: 1ms;
    }
  }
</style>
