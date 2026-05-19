<script lang="ts">
  import Icon from "../components/Icon.svelte";

  let { level, visible }: any = $props();
</script>

<div class="pill p-fix f-col al-ct g10 p10 rx20 o-0" class:visible>
  <Icon name="volume" />

  <div class="track rx2 flow-h p-rel">
    <div class="fill p-abs rx2" style="height:{level * 100}%"></div>
  </div>
</div>

<style>
  .pill {
    left: 20px;
    top: 50%;
    transform: translateY(-50%);
    z-index: 10;
    background: #0009;
    backdrop-filter: blur(6px);
    color: var(--tx-5);
    pointer-events: none;
    transition: opacity 0.5s ease;
  }
  .pill.visible {
    opacity: 1;
    transition: opacity 0s;
  }

  .track {
    width: 4px;
    height: 80px;
    background: #fff4;
  }
  .fill {
    bottom: 0;
    left: 0;
    right: 0;
    background: var(--tx-5);
    transition: height 0.1s;
  }
</style>
