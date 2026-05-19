<script lang="ts">
  // Standard progress bar. Renders the global .bar-track/.bar-fill standard so
  // every progress UI is structurally identical; variant picks the accent
  // (default red, success green→cyan, striped animated). Value is clamped.
  let {
    value = 0,
    variant = "default",
    label = false,
    height = "6px",
    width = "",
  }: {
    value?: number;
    variant?: "default" | "success" | "striped";
    label?: boolean;
    height?: string;
    width?: string;
  } = $props();

  const pct = $derived(Math.max(0, Math.min(100, value || 0)));
</script>

<div class="pb f al-ct g10" style:width={width || null}>
  <div class="bar-track" style:height>
    <div
      class="bar-fill"
      class:success={variant === "success"}
      class:striped={variant === "striped"}
      style:width="{pct}%"
    ></div>
  </div>
  {#if label}<span class="pct fs-xs tx-1">{Math.round(pct)}%</span>{/if}
</div>

<style>
  .pb {
    width: 100%;
  }
  .bar-track {
    flex: 1;
  }
  .pct {
    flex-shrink: 0;
    min-width: 30px;
    text-align: right;
  }
</style>
