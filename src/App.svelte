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
  :global {
    * {
      box-sizing: border-box;
    }

    body {
      background: #141414;
      color: #fff;
      font-family: "Helvetica Neue", Helvetica, Arial, sans-serif;
      margin: 0;
      min-height: 100vh;
    }

    a {
      color: inherit;
      text-decoration: none;
    }
    button {
      cursor: pointer;
      font-family: inherit;
    }

    .video-js {
      width: 100% !important;
      height: auto !important;
      aspect-ratio: 16/9;
    }
    .video-wrap .video-js {
      height: 100% !important;
      aspect-ratio: unset !important;
    }
    .vjs-poster {
      background-size: cover;
    }
    .video-js.hideSubs .vjs-text-track-display {
      display: none !important;
    }
    video.bright {
      filter: brightness(1.4);
    }
  }
</style>
