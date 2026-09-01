<script lang="ts">
  import Icon from "../components/Icon.svelte";

  let { level, visible }: any = $props();
</script>

<div class="pill pl-pane f-col al-ct g10 p10 o-0" class:visible>
  <Icon name="volume" />

  <div class="track flow-h p-rel">
    <div class="fill p-abs" style="height:{level * 100}%"></div>
  </div>

  <span class="pct pl-num">{Math.round(level * 100)}</span>
</div>

<style>
  .pill {
    position: fixed;
    left: calc(20px + env(safe-area-inset-left));
    top: 50%;
    transform: translateY(-50%);
    z-index: 10;
    border-radius: var(--r-xl);
    color: var(--tx-5);
    pointer-events: none;
    transition: opacity var(--m-slow, 520ms) var(--ease-out);
  }
  .pill.visible {
    opacity: 1;
    transition: opacity 0s;
  }

  .track {
    width: 4px;
    height: 84px;
    border-radius: 99px;
    background: rgba(255, 255, 255, 0.18);
  }
  .fill {
    bottom: 0;
    left: 0;
    right: 0;
    border-radius: 99px;
    background: linear-gradient(
      to top,
      color-mix(in oklab, var(--tx-5) 70%, var(--art-1)),
      var(--tx-5)
    );
    transition: height var(--m-fast, 130ms) var(--ease-out);
  }
  .pct {
    font-size: 11px;
    color: var(--tx-3);
  }
</style>
