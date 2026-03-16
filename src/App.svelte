<script lang="ts">
  import Home from "./Home.svelte";
  import Player from "./Player.svelte";
  import Manage from "./Manage.svelte";

  const loc = window.location;

  const path = loc.pathname;
  const vidParam = new URLSearchParams(loc.search).get("video");
</script>

{#if path === "/manage"}
  <Manage />
{:else if vidParam}
  <Player videoParam={vidParam} />
{:else}
  <Home />
{/if}

<style lang="scss">
</style>
