<script lang="ts">
  import Home from "./library/Home.svelte";
  import Player from "./player/Player.svelte";
  import Manage from "./library/Manage.svelte";
  import Search from "./library/Search.svelte";
  import Toast from "./core/ToastHost.svelte";

  const loc = window.location;

  const path = loc.pathname;
  const vidParam = new URLSearchParams(loc.search).get("video");
</script>

{#if path === "/manage"}
  <Manage />
{:else if path === "/search"}
  <Search />
{:else if vidParam}
  <Player videoParam={vidParam} />
{:else}
  <Home />
{/if}

<Toast />

<style lang="scss">
</style>
