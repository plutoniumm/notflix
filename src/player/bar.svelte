<script lang="ts">
  import Down from "./Download.svelte";
  import SubsDropdown from "./Subs.svelte";
  import AudioPicker from "./AudioPicker.svelte";
  import type { PlayerView } from "./view.svelte";

  let {
    view,
    title,
    embed,
    videoParam,
    runWhisper,
    onToggleSubs,
    onSelectLocal,
    onSelectEmbedded,
    onSelectOnline,
    onSubsOff,
    onSelectAudio,
  }: {
    view: PlayerView;
    title: string;
    embed: boolean;
    videoParam: string;
    runWhisper: () => void;
    onToggleSubs: () => void;
    onSelectLocal: (file: string, label: string) => void;
    onSelectEmbedded: (idx: number, lang: string) => Promise<void>;
    onSelectOnline: (pick: any) => Promise<void>;
    onSubsOff: () => void;
    onSelectAudio: (track: number) => void;
  } = $props();
</script>

{#if !embed}
  <div class="bar p-fix p20 f al-ct g10" class:hidden={view.state.hideUI}>
    <a href="/" class="back fs sh-0">←</a>
    <div class="title fw5 trunc">{title}</div>

    <div class="f g5 sh-0 al-ct">
      {#if view.audio.tracks.length > 1}
        <div class="btn-wrap p-rel">
          <!-- svelte-ignore a11y_consider_explicit_label -->
          <button
            class="ibtn cc rx5 ptr p5"
            class:active={view.audio.open}
            onclick={() => view.audio.toggle()}
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
            >
              <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
              <path d="M15.54 8.46a5 5 0 0 1 0 7.07" />
              <path d="M19.07 4.93a10 10 0 0 1 0 14.14" />
            </svg>
          </button>
          {#if view.audio.open}
            <AudioPicker
              tracks={view.audio.tracks}
              activeTrack={view.audio.active}
              onSelect={onSelectAudio}
              onClose={() => view.audio.close()}
            />
          {/if}
        </div>
      {/if}

      <div class="btn-wrap p-rel">
        <!-- svelte-ignore a11y_consider_explicit_label -->
        <button
          class="ibtn cc rx5 ptr p5"
          class:active={view.subs.open || view.state.hasSubs}
          onclick={onToggleSubs}
        >
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
            <rect
              x="2.5"
              y="9"
              width="6"
              height="2"
              rx="1"
              fill="currentColor"
            />
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

        {#if view.subs.open}
          <SubsDropdown
            info={view.subs.info}
            onlineResults={view.subs.onlineResults}
            searching={view.subs.searching}
            activeSub={view.subs.activeSub}
            {onSelectLocal}
            {onSelectEmbedded}
            {onSelectOnline}
            {onSubsOff}
            onClose={() => view.subs.close()}
          />
        {/if}
      </div>

      <!-- svelte-ignore a11y_consider_explicit_label -->
      <button
        class="ibtn cc rx5 ptr p5"
        class:pulse={!!view.state.wMsg}
        onclick={runWhisper}
      >
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

      {#if view.state.videoKey}
        <Down {videoParam} {title} key={view.state.videoKey} />
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
    background: linear-gradient(
      to bottom,
      rgba(7, 6, 10, 0.78) 0%,
      rgba(7, 6, 10, 0.4) 50%,
      transparent 100%
    );
    backdrop-filter: blur(14px) saturate(140%);
    -webkit-backdrop-filter: blur(14px) saturate(140%);
    transition: opacity 0.3s var(--ease-out);
    animation: slide-down 0.34s var(--ease-out);
  }
  .bar.hidden {
    opacity: 0;
    pointer-events: none;
  }

  .back {
    color: var(--tx-4);
    white-space: nowrap;
    font-size: 22px;
    padding: 4px 10px;
    border-radius: var(--r-md);
    transition: color 0.18s var(--ease-out), background 0.18s var(--ease-out),
      transform 0.16s var(--ease-snap);
  }
  .back:hover {
    color: var(--tx-5);
    background: var(--glass);
  }
  .back:active {
    transform: scale(0.92);
  }

  .title {
    flex: 1;
    font-family: var(--font-display);
    font-weight: 500;
    font-size: 16px;
    letter-spacing: -0.012em;
  }

  .btn-wrap {
    display: flex;
    align-items: center;
  }

  .ibtn {
    color: var(--tx-3);
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--r-md) !important;
    transition:
      color 0.18s var(--ease-out),
      background 0.18s var(--ease-out),
      border-color 0.18s var(--ease-out),
      transform 0.16s var(--ease-snap);
    min-width: 38px;
    min-height: 38px;
  }
  @media (hover: hover) {
    .ibtn:hover {
      color: var(--tx-5);
      background: var(--glass);
      border-color: var(--glass-bd);
    }
  }
  @media (hover: none) {
    .ibtn {
      min-width: 44px;
      min-height: 44px;
    }
  }
  .ibtn:active {
    color: var(--tx-5);
    background: var(--glass-2);
    transform: scale(0.9);
  }
  .ibtn.active {
    color: var(--tx-5);
    background: var(--glass-2);
    border-color: var(--glass-bd);
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.05);
  }

  .ibtn.pulse {
    animation: pulse 1.5s ease infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      color: var(--tx-3);
    }
    50% {
      color: var(--red);
      filter: drop-shadow(0 0 6px var(--red-glow));
    }
  }
</style>
