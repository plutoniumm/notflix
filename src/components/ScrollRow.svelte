<script lang="ts">
  import type { Snippet } from "svelte";

  // Arrow chrome around a horizontally scrolling strip. The caller supplies the
  // scrolling element itself (`.cards`) so each row keeps its own layout — a
  // plain flex strip, Home's column-major grid, ShowRow's animated swap.
  let { children }: { children: Snippet } = $props();

  function scroll(btn: HTMLElement, dir: number) {
    const el = (
      dir < 0 ? btn.nextElementSibling : btn.previousElementSibling
    ) as HTMLElement | null;

    el?.scrollBy({ left: dir * 400, behavior: "smooth" });
  }
</script>

<div class="scroll-row f al-ct p-rel">
  <button
    class="scroll-arrow cc h-100 p0 left"
    aria-label="Scroll left"
    onclick={(e) => scroll(e.currentTarget, -1)}>‹</button
  >

  {@render children()}

  <button
    class="scroll-arrow cc h-100 p0 right"
    aria-label="Scroll right"
    onclick={(e) => scroll(e.currentTarget, 1)}>›</button
  >

  <div class="fade start p-abs dead"></div>
  <div class="fade end p-abs dead"></div>
</div>

<style>
  /* The strip runs off both edges of the page rather than stopping at a hard
     crop, so a row reads as continuous rather than as a boxed carousel. */
  .fade {
    top: 0;
    bottom: 0;
    z-index: 2;
  }
  .fade.start {
    left: 0;
    width: var(--fade-s, 56px);
    background: linear-gradient(to right, var(--bg-2), transparent);
  }
  .fade.end {
    right: 0;
    width: var(--fade-e, 56px);
    background: linear-gradient(to left, var(--bg-2), transparent);
  }
  @media (max-width: 640px) {
    .fade.start {
      width: var(--fade-s, 20px);
    }
    .fade.end {
      width: var(--fade-e, 20px);
    }
  }
</style>
