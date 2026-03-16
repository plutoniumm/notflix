<script lang="ts">
  import DownloadButton from "../Download.svelte";

  let {
    title,
    hideUI,
    embed,
    videoKey,
    videoParam,
    onfetchSubs,
    onrunWhisper,
  }: {
    title: string;
    hideUI: boolean;
    embed: boolean;
    videoKey: string;
    videoParam: string;
    onfetchSubs: () => void;
    onrunWhisper: () => void;
  } = $props();
</script>

{#if !embed}
  <div class="player-bar f al-ct g10" class:hidden={hideUI}>
    <a href="/" class="back fs sh-0">←</a>
    <h1 class="title fw5 m0 trunc">{title}</h1>

    <div class="f g5 sh-0 al-ct">
      <button class="icon-btn" onclick={onfetchSubs} title="Fetch subtitles">
        <svg
          width="20"
          height="15"
          viewBox="0 0 20 15"
          fill="none"
          aria-hidden="true"
        >
          <rect
            x="0.75"
            y="0.75"
            width="18.5"
            height="13.5"
            rx="2"
            stroke="currentColor"
            stroke-width="1.5"
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

      <button
        class="icon-btn"
        onclick={onrunWhisper}
        title="Whisper transcription"
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <rect x="9" y="2" width="6" height="12" rx="3" />
          <path d="M5 10a7 7 0 0 0 14 0" />
          <line x1="12" y1="17" x2="12" y2="21" />
          <line x1="8" y1="21" x2="16" y2="21" />
        </svg>
      </button>

      {#if videoKey}
        <DownloadButton {videoParam} {title} key={videoKey} />
      {/if}
    </div>
  </div>
{/if}

<style>
  .player-bar {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 10;
    padding: 20px 32px 48px;
    background: linear-gradient(
      to bottom,
      rgba(0, 0, 0, 0.85) 0%,
      transparent 100%
    );
    transition: opacity 0.3s;
    animation: slide-down 0.3s ease;
  }
  .player-bar.hidden {
    opacity: 0;
    pointer-events: none;
  }

  .back {
    color: #ddd;
    white-space: nowrap;
    transition: color 0.15s;
  }
  .back:hover {
    color: #fff;
  }

  .title {
    flex: 1;
    font-size: 1rem;
    color: #e5e5e5;
  }

  .icon-btn {
    background: none;
    border: none;
    color: #ccc;
    cursor: pointer;
    padding: 5px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    transition:
      color 0.15s,
      background 0.15s;
  }
  .icon-btn:hover {
    color: #fff;
    background: rgba(255, 255, 255, 0.12);
  }
</style>
