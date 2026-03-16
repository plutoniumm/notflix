<script lang="ts">
  import Down from "./Download.svelte";

  let { title, hidden, embed, videoKey, videoParam, getSubs, runWhisper }: any =
    $props();
</script>

{#if !embed}
  <div class="bar p-fix p20 f al-ct g10" class:hidden>
    <a href="/" class="back fs sh-0">←</a>
    <div class="title fw5 trunc">{title}</div>

    <div class="f g5 sh-0 al-ct">
      <!-- svelte-ignore a11y_consider_explicit_label -->
      <button class="ibtn cc rx5 ptr p5" onclick={getSubs}>
        <svg width="20" height="15" viewBox="0 0 20 15" fill="none">
          <rect
            x="0.75"
            y="0.75"
            width="18.5"
            height="13.5"
            rx="2"
            stroke="currentColor"
            stroke-width="2"
          />
          <rect x="2.5" y="9" width="6" height="2" rx="1" fill="currentColor" />
          <rect
            x="10.5"
            y="9"
            width="7"
            height="2"
            rx="1"
            fill="currentColor"
          />
        </svg>
      </button>

      <!-- svelte-ignore a11y_consider_explicit_label -->
      <button class="ibtn cc rx5 ptr p5" onclick={runWhisper}>
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
        >
          <rect x="9" y="2" width="6" height="12" rx="3" />
          <path d="M5 10a7 7 0 0 0 14 0" />
          <line x1="12" y1="17" x2="12" y2="21" />
          <line x1="8" y1="21" x2="16" y2="21" />
        </svg>
      </button>

      {#if videoKey}
        <Down {videoParam} {title} key={videoKey} />
      {/if}
    </div>
  </div>
{/if}

<style>
  .bar {
    top: 0;
    left: 0;
    right: 0;
    z-index: 10;
    background: linear-gradient(to bottom, #000c 0%, transparent 100%);
    transition: opacity 0.3s;
    animation: slide-down 0.3s ease;
  }
  .bar.hidden {
    opacity: 0;
    pointer-events: none;
  }

  .back {
    color: var(--tx-4);
    white-space: nowrap;
    transition: color 0.15s;
  }
  .back:hover {
    color: var(--tx-5);
  }

  .title {
    flex: 1;
  }

  .ibtn {
    color: var(--tx-4);
    transition:
      color 0.15s,
      background 0.15s;
  }
  .ibtn:hover {
    color: var(--tx-5);
    background: #fff2;
  }
</style>
