<script lang="ts">
  import { onMount } from "svelte";

  let visible = false;
  function toggleChat() {
    visible = !visible;
  }

  // create socket
  onMount(() => {
    const url = "ws://localhost:3000";
    const socket = new WebSocket(url);
    socket.onopen = () => {
      console.log("connected");
    };
    socket.onmessage = (event) => {
      console.log(event.data);
    };
    socket.onclose = () => {
      console.log("disconnected");
    };
  });
</script>

<main style="width:calc({visible ? 300 : 25}px + 10px);">
  <!-- svelte-ignore
        a11y-click-events-have-key-events
        a11y-no-static-element-interactions
  -->
  <div class="rpm-5 main" on:click={toggleChat}>
    <svg viewBox="0 0 32 32">
      {#if visible}
        <path d="M2 30 L30 2 M30 30 L2 2" />
      {:else}
        <path d="M2 4 L30 4 30 22 16 22 8 29 8 22 2 22 Z" />
      {/if}
    </svg>
  </div>
</main>

<style>
  main {
    background: linear-gradient(to top, #000, #222);
  }
  .main {
    color: #fff;
  }
  svg {
    width: 16px;
    height: 16px;
    fill: none;
    stroke: currentcolor;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-width: 2;
  }
</style>
