<script>
  import { marked } from "marked";

  // id: number;
  // room: uuid;
  // text: string;
  // user:
  //    name: string;
  //    image: Image;

  export let message;
</script>

<div class="f p5 m5 message" id={`${message.room}-${message.id}`}>
  <div>User stuff</div>
  <div class="p5 m5">
    {@html marked(message.text)}
  </div>
</div>
<hr class="mx-a w-50" />

<style>
  hr {
    opacity: 0.1;
  }
  .message {
    width: calc(100% - 10px);
  }
</style>
