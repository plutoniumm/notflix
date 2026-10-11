<script lang="ts">
  import Home from "./library/Home.svelte";
  import Player from "./player/Player.svelte";
  import Manage from "./library/Manage.svelte";
  import History from "./library/History.svelte";
  import Toast from "./core/ToastHost.svelte";
  import { onMount } from "svelte";
  import { mountRoom } from "./core/room.svelte";

  const loc = window.location;

  const path = loc.pathname;
  const vidParam = new URLSearchParams(loc.search).get("video");

  // The player paints --art-1/--art-2 from the frame itself and dims --room;
  // a second driver would fight it for the same properties.
  onMount(() => (vidParam ? undefined : mountRoom()));
</script>

{#if path === "/manage"}
  <Manage />
{:else if path === "/history"}
  <History />
{:else if vidParam}
  <Player videoParam={vidParam} />
{:else}
  <Home />
{/if}

<Toast />
