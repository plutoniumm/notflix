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
  .bdg {
    white-space: nowrap;
  }
  .chip {
    padding: 1px 7px;
    border-radius: 999px;
    background: rgb(255 255 255 / 0.07);
  }
  .hdr {
    padding: 1px 7px;
    border-radius: 999px;
    background: color-mix(in oklab, var(--gold) 18%, transparent);
    color: var(--gold);
  }
  .tag {
    color: var(--gold);
    font-weight: 600;
  }
  .dim {
    opacity: 0.55;
  }
  .quality {
    display: inline-block;
    padding: 2px 8px;
    border-radius: var(--r-sm);
    background: rgb(255 255 255 / 0.08);
    font-size: 12px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .accent {
    font-size: 10px;
    color: var(--cyan);
    background: color-mix(in oklab, var(--cyan) 14%, transparent);
    border: 1px solid color-mix(in oklab, var(--cyan) 32%, transparent);
    padding: 1px 7px;
    border-radius: 999px;
    flex-shrink: 0;
    letter-spacing: 0.04em;
    font-weight: 600;
  }
</style>
