<script lang="ts">
  let visible = false;
  function toggleChat() {
    visible = !visible;
  }
</script>

<main style="width:calc({visible ? 300 : 25}px - 10px)">
  <!-- svelte-ignore
        a11y-click-events-have-key-events
        a11y-no-static-element-interactions
  -->
  <div class="rpm-5" on:click={toggleChat}>
    {#if visible}
      &rarr;
    {:else}
      &larr;
    {/if}
  </div>
</main>

<style>
  main {
    background: #666;
    width: 25px;
  }
</style>
