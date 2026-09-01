<script lang="ts">
  import Down from "./Download.svelte";
  import SubsDropdown from "./Subs.svelte";
  import AudioPicker from "./AudioPicker.svelte";
  import Icon from "../components/Icon.svelte";
  import type { PlayerView } from "./view.svelte";

  let {
    view,
    embed,
    videoParam,
    title,
    tuneOpen = false,
    helpOpen = false,
    runWhisper,
    onHelp,
    onTune,
    onToggleSubs,
    onSelectLocal,
    onSelectEmbedded,
    onSelectOnline,
    onSubsOff,
    onSelectAudio,
  }: {
    view: PlayerView;
    embed: boolean;
    videoParam: string;
    title: string;
    tuneOpen?: boolean;
    helpOpen?: boolean;
    runWhisper: () => void;
    onHelp: () => void;
    onTune: () => void;
    onToggleSubs: () => void;
    onSelectLocal: (file: string, label: string) => void;
    onSelectEmbedded: (idx: number, lang: string) => Promise<void>;
    onSelectOnline: (pick: any) => Promise<void>;
    onSubsOff: () => void;
    onSelectAudio: (track: number) => void;
  } = $props();
</script>

{#if !embed}
  <div class="bar f al-ct g10" class:hidden={view.state.hideUI}>
    <a href="/" class="back cc rx5 sh-0" aria-label="Back">
      <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M15 6l-6 6 6 6" />
      </svg>
    </a>

    <div class="spacer"></div>

    <div class="chips f g5 sh-0 al-ct">
      {#if view.audio.tracks.length > 1}
        <div class="btn-wrap p-rel">
          <button
            class="ibtn cc rx5 ptr p5"
            aria-label="Audio track"
            class:active={view.audio.open}
            onclick={() => view.audio.toggle()}
          >
            <Icon name="audio" />
          </button>
          <AudioPicker
            open={view.audio.open}
            tracks={view.audio.tracks}
            activeTrack={view.audio.active}
            onSelect={onSelectAudio}
            onClose={() => view.audio.close()}
          />
        </div>
      {/if}

      <div class="btn-wrap p-rel">
        <button
          class="ibtn cc rx5 ptr p5"
          aria-label="Subtitles"
          class:active={view.subs.open || view.state.hasSubs}
          onclick={onToggleSubs}
        >
          <Icon name="captions" />
        </button>

        <SubsDropdown
          open={view.subs.open}
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
      </div>

      <button
        class="ibtn cc rx5 ptr p5"
        aria-label="Transcribe with Whisper"
        class:pulse={!!view.state.wMsg}
        onclick={runWhisper}
      >
        <Icon name="whisper" />
      </button>

      <button
        class="ibtn cc rx5 ptr p5"
        aria-label="Picture and audio"
        class:active={tuneOpen}
        onclick={onTune}
      >
        <Icon name="settings" />
      </button>

      <button
        class="ibtn qmark cc rx5 ptr p5"
        aria-label="Keyboard shortcuts"
        title="Keyboard & gestures (?)"
        class:active={helpOpen}
        onclick={onHelp}
      >
        ?
      </button>

      <Down {videoParam} {title} />

    </div>
  </div>
{/if}

<style>
  .bar {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 10;
    /* max() keeps the real padding where the safe-area inset resolves to 0. */
    padding: 12px 16px;
    padding-top: max(12px, var(--safe-t, env(safe-area-inset-top)));
    padding-left: max(16px, env(safe-area-inset-left));
    padding-right: max(16px, env(safe-area-inset-right));
    background: linear-gradient(
      to bottom,
      rgba(3, 2, 6, 0.72) 0%,
      rgba(3, 2, 6, 0.28) 62%,
      transparent 100%
    );
    backdrop-filter: blur(14px) saturate(150%);
    -webkit-backdrop-filter: blur(14px) saturate(150%);
    transition:
      opacity var(--m, 260ms) var(--ease-out),
      transform var(--m, 260ms) var(--ease-out);
  }
  @starting-style {
    .bar {
      opacity: 0;
      transform: translateY(-22%);
    }
  }
  .bar.hidden {
    opacity: 0;
    transform: translateY(-14%);
    pointer-events: none;
  }

  .spacer { flex: 1; }

  .back {
    color: var(--tx-4);
    min-width: 36px;
    min-height: 36px;
    transition:
      color var(--m-fast, 130ms) var(--ease-out),
      background var(--m-fast, 130ms) var(--ease-out),
      transform var(--m-fast, 130ms) var(--ease-snap);
  }
  .back:hover { color: var(--tx-5); background: var(--glass); }
  .back:active { transform: scale(0.92); }
  @media (hover: none) {
    .back { min-width: 44px; min-height: 44px; }
  }

  .btn-wrap { display: flex; align-items: center; }

  .ibtn {
    color: var(--tx-3);
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--r-md) !important;
    transition:
      color var(--m-fast, 130ms) var(--ease-out),
      background var(--m-fast, 130ms) var(--ease-out),
      border-color var(--m-fast, 130ms) var(--ease-out),
      transform var(--m-fast, 130ms) var(--ease-snap);
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
  @media (hover: none) { .ibtn { min-width: 44px; min-height: 44px; } }
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
  .ibtn.pulse { animation: pulse 1.5s ease infinite; }

  .qmark {
    font-family: var(--font-display);
    font-size: 15px;
    font-weight: 600;
    line-height: 1;
  }

  @keyframes pulse {
    0%, 100% { color: var(--tx-3); }
    50% { color: var(--red); filter: drop-shadow(0 0 6px var(--red-glow)); }
  }
</style>
