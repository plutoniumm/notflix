<script lang="ts">
  import { onMount } from "svelte";
  import { messages } from "./messages";
  import Message from "./message/Message.svelte";

  const url = "ws://localhost:3001";
  let //
    visible = true,
    value,
    sendMessage;

  const toggleChat = () => (visible = !visible);

  // create socket
  onMount(() => {
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

    sendMessage = (e) => {
      socket.send(value);
      messages.update((messages) => [...messages, { value }]);
      value = "";
    };
  });
</script>

<main style="width:calc({visible ? 300 : 25}px + 10px);">
  <!-- svelte-ignore
        a11y-click-events-have-key-events
        a11y-no-static-element-interactions
  -->
  <div class="rpm-5 main p-rel f-col j-bw">
    <div class="w-100 d-b f" on:click={toggleChat}>
      <svg
        viewBox="0 0 32 32"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        stroke="currentColor"
        fill="none"
      >
        {#if visible}
          <path d="M2 30 L30 2 M30 30 L2 2" />
        {:else}
          <path d="M2 4 L30 4 30 22 16 22 8 29 8 22 2 22 Z" />
        {/if}
      </svg>
    </div>
    <section style="flow-y-s">
      {#each $messages as message}
        <Message {message} />
      {/each}
    </section>
    <form
      action=""
      class="p-abs f p5 rx5"
      on:submit|preventDefault={sendMessage}
    >
      <input style="flex:6;" bind:value placeholder="Type a message..." />
      <label for="submit">
        <input type="submit" id="submit" value=">" class="d-n p-rel" />
        <img
          class="m5"
          src="/send.svg"
          alt="submit"
          height="20px"
          width="20px"
        />
      </label>
    </form>
  </div>
</main>

<style lang="scss">
  form {
    left: 0;
    bottom: 0;
    border: 1px solid #888;
    width: calc(100% - 14px);
    background: #000;
    box-shadow: 0 -15px 15px #000;
    input[type="text"] {
      color: #fff;
      background: transparent;
    }
    label {
      flex: 1;
    }
  }
  main {
    background: linear-gradient(to top, #000, #222);
  }
  .main {
    color: #fff;
    height: calc(100% - 20px) !important;
  }
  svg {
    width: 16px;
    height: 16px;
  }
</style>
