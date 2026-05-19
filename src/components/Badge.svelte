<script lang="ts">
  import type { Snippet } from "svelte";

  // Inline label/chip. Variants mirror the previously-duplicated styles 1:1
  // (Search chips/tag/quality/group, Subs provider badge) so the rendered
  // look is unchanged.
  let {
    variant = "chip",
    class: klass = "",
    children,
  }: {
    variant?: "chip" | "hdr" | "tag" | "dim" | "quality" | "accent";
    class?: string;
    children?: Snippet;
  } = $props();
</script>

<span class="bdg {variant} {klass}">{@render children?.()}</span>

<style>
  .chip {
    padding: 1px 6px;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.07);
  }
  .hdr {
    padding: 1px 6px;
    border-radius: 999px;
    background: rgba(230, 179, 74, 0.18);
    color: var(--gold, #e6b34a);
  }
  .tag {
    color: var(--gold, #e6b34a);
    font-weight: 600;
  }
  .dim {
    opacity: 0.55;
  }
  .quality {
    display: inline-block;
    padding: 2px 7px;
    border-radius: var(--r-sm, 4px);
    background: rgba(255, 255, 255, 0.08);
    font-size: 12px;
    font-weight: 600;
  }
  .accent {
    font-size: 10px;
    color: var(--cyan);
    background: rgba(94, 234, 212, 0.12);
    border: 1px solid rgba(94, 234, 212, 0.3);
    padding: 1px 7px;
    border-radius: 999px;
    flex-shrink: 0;
    letter-spacing: 0.04em;
    font-weight: 600;
  }
</style>
