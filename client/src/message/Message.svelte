<script>
  import { marked } from "marked";

  export let message = {
    id: 1,
    text: "Hello world",
    user: {
      name: "John Doe",
      image: {},
    },
  };

  const noSpace = (str) => str.replace(/\s/g, "");
</script>

<div class="f p5 m5 message" id={noSpace(message.user.name + message.id)}>
  <div>User stuff</div>
  <div class="p5 m5">
    {@html marked(message.text)}
  </div>
</div>
<hr class="mx-a w-50" />

<style>
  hr {
    opacity: 0.05;
  }
  .message {
    width: calc(100% - 10px);
  }
</style>
