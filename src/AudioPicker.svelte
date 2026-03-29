<script lang="ts">
  import { onMount } from "svelte";
  import { clickOutside } from "./lib/clickOutside";

  export type AudioTrack = {
    track: number;
    language: string;
    codec: string;
    channels: number;
  };

  let {
    tracks,
    activeTrack,
    onSelect,
    onClose,
  }: {
    tracks: AudioTrack[];
    activeTrack: number;
    onSelect: (track: number) => void;
    onClose: () => void;
  } = $props();

  function label(t: AudioTrack) {
    const lang = t.language ? t.language.toUpperCase() : `Track ${t.track + 1}`;
    const ch =
      t.channels === 2
        ? "2.0"
        : t.channels === 6
          ? "5.1"
          : t.channels === 8
            ? "7.1"
            : `${t.channels}ch`;
    const codec = t.codec?.toUpperCase() ?? "";
    return `${lang} — ${codec} ${ch}`;
  }

  onMount(() => clickOutside(onClose));
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="dropdown" onclick={(e) => e.stopPropagation()}>
  <div class="section-hd">Audio</div>
  {#each tracks as t}
    <button
      class="item"
      class:active={activeTrack === t.track}
      onclick={() => {
        onSelect(t.track);
        onClose();
      }}
    >
      <span class="bullet">{activeTrack === t.track ? "●" : "○"}</span>
      {label(t)}
    </button>
  {/each}
</div>

<style>
  .dropdown {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    width: 260px;
    background: #111c;
    backdrop-filter: blur(8px);
    border: 1px solid #fff2;
    border-radius: 8px;
    z-index: 100;
    animation: fade-in 0.15s ease;
  }

  .section-hd {
    padding: 8px 12px 4px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--tx-3);
  }

  .item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 12px;
    text-align: left;
    color: var(--tx-4);
    font-size: 13px;
    transition: background 0.1s;
  }
  .item:hover {
    background: #fff1;
    color: var(--tx-5);
  }
  .item.active {
    color: var(--tx-5);
  }

  .bullet {
    width: 14px;
    text-align: center;
    flex-shrink: 0;
  }

  @keyframes fade-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
</style>
