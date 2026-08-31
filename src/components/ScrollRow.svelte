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
</div>
