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
    runWhisper,
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
  <div class="bar p-fix f al-ct g10" class:hidden={view.state.hideUI}>
    <a href="/" class="back cc rx5 sh-0" aria-label="Back">
      <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M15 6l-6 6 6 6" />
      </svg>
    </a>

    <div class="spacer"></div>

    <div class="chips f g5 sh-0 al-ct">
      {#if view.audio.tracks.length > 1}
        <div class="btn-wrap p-rel">
          <!-- svelte-ignore a11y_consider_explicit_label -->
          <button
            class="ibtn cc rx5 ptr p5"
            class:active={view.audio.open}
            onclick={() => view.audio.toggle()}
          >
            <Icon name="audio" />
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
          <Icon name="captions" />
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
        <Icon name="whisper" />
      </button>

      <Down {videoParam} {title} />

    </div>
  </div>
{/if}

<style>
  .bar {
    top: 0;
    left: 0;
    right: 0;
    z-index: 10;
    /* viewport-fit=cover puts the page under the notch; max() keeps the real
       padding on devices where the inset resolves to 0. */
    padding: 12px 16px;
    padding-top: max(12px, env(safe-area-inset-top));
    padding-left: max(16px, env(safe-area-inset-left));
    padding-right: max(16px, env(safe-area-inset-right));
    background: linear-gradient(
      to bottom,
      rgba(7, 6, 10, 0.7) 0%,
      rgba(7, 6, 10, 0.3) 60%,
      transparent 100%
    );
    backdrop-filter: blur(12px) saturate(140%);
    -webkit-backdrop-filter: blur(12px) saturate(140%);
    transition: opacity 0.3s var(--ease-out);
    animation: slide-down 0.34s var(--ease-out);
  }
  .bar.hidden { opacity: 0; pointer-events: none; }

  .spacer { flex: 1; }

  .back {
    color: var(--tx-4);
    min-width: 36px;
    min-height: 36px;
    transition: color 0.18s var(--ease-out), background 0.18s var(--ease-out),
      transform 0.16s var(--ease-snap);
  }
  .back:hover { color: var(--tx-5); background: var(--glass); }
  .back:active { transform: scale(0.92); }

  .btn-wrap { display: flex; align-items: center; }

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

  @keyframes pulse {
    0%, 100% { color: var(--tx-3); }
    50% { color: var(--red); filter: drop-shadow(0 0 6px var(--red-glow)); }
  }
  @keyframes slide-down {
    from { opacity: 0; transform: translateY(-20%); }
    to   { opacity: 1; transform: translateY(0); }
  }
</style>
