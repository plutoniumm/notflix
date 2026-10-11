<script lang="ts">
  import type { Snippet } from "svelte";

  // A box used to mean nothing here — every section had one. Now only a
  // problem gets a surface, so a box means "look at this"; everything else is
  // a label, a hairline and aligned columns. The hairline doubles as the
  // section's own progress when the caller has one.
  let {
    label,
    meta = "",
    fill = 0,
    bad = false,
    i = 0,
    children,
  }: {
    label: string;
    meta?: string;
    fill?: number;
    bad?: boolean;
    i?: number;
    children: Snippet;
  } = $props();
</script>

<section class="sect" class:bad style="--i:{i}">
  <div class="hd f al-ct g10">
    <span class="lab">{label}</span>
    <span class="rule" style="--fill:{Math.max(0, Math.min(100, fill))}%"></span>
    {#if meta}<span class="meta fs-xs tx-1 num">{meta}</span>{/if}
  </div>
  <div class="body" class:surface={bad}>{@render children()}</div>
</section>

<style>
  .sect {
    margin-bottom: 26px;
    transition:
      opacity var(--motion) var(--ease-out),
      translate var(--motion) var(--ease-out);
    transition-delay: calc(var(--i) * 45ms);
  }
  @starting-style {
    .sect {
      opacity: 0;
      translate: 0 8px;
    }
  }

  .hd {
    padding: 0 2px 7px;
  }
  .lab {
    font-family: var(--font-display);
    color: var(--tx-3);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.1em;
    white-space: nowrap;
  }
  .rule {
    flex: 1;
    min-width: 16px;
    height: 1px;
    background: linear-gradient(
      90deg,
      var(--red) 0 var(--fill),
      var(--glass-bd) var(--fill)
    );
    transition: --fill var(--motion-slow) var(--ease-out);
  }
  .meta {
    white-space: nowrap;
  }

  .bad .lab {
    color: var(--red);
  }
  .bad .rule {
    background: color-mix(in oklab, var(--red) 40%, transparent);
  }
  .bad .body {
    padding: 4px;
    border-radius: var(--r-lg);
    border-color: color-mix(in oklab, var(--red) 40%, transparent);
    box-shadow: inset 2px 0 0 var(--red);
  }
</style>
