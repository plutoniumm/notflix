<script lang="ts">
  let { msg }: { msg: string } = $props();
</script>

{#if msg}
  <div class="p-fix p10 fs red rx5">{msg}</div>
{/if}

<style>
  div {
    bottom: 80px;
    left: 32px;
    z-index: 10;
    background: rgba(0, 0, 0, 0.6);
  }
</style>
