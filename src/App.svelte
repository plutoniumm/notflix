<script lang="ts">
  import Home from './Home.svelte'
  import Player from './Player.svelte'
  import Manage from './Manage.svelte'

  const path = window.location.pathname
  const params = new URLSearchParams(window.location.search)
  const videoParam = params.get('video')
</script>

{#if path === '/manage'}
  <Manage />
{:else if videoParam}
  <Player {videoParam} />
{:else}
  <Home />
{/if}

<style>
  :global(*) { box-sizing: border-box; }

  :global(body) {
    background: #141414;
    color: #fff;
    font-family: 'Helvetica Neue', Helvetica, Arial, sans-serif;
    margin: 0;
    min-height: 100vh;
  }

  :global(a) { color: inherit; text-decoration: none; }
  :global(button) { cursor: pointer; font-family: inherit; }

  :global(::-webkit-scrollbar) { width: 6px; height: 6px; }
  :global(::-webkit-scrollbar-track) { background: transparent; }
  :global(::-webkit-scrollbar-thumb) { background: #444; border-radius: 3px; }
  :global(::-webkit-scrollbar-thumb:hover) { background: #666; }

  /* video.js overrides */
  :global(.video-js) { width: 100% !important; height: auto !important; aspect-ratio: 16/9; }
  :global(.vjs-poster) { background-size: cover; }
  :global(.video-js.hideSubs .vjs-text-track-display) { display: none !important; }
  :global(video.bright) { filter: brightness(1.4); }
</style>
