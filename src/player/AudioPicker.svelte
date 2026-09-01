<script lang="ts">
  import Dropdown from "./Dropdown.svelte";

  export type AudioTrack = {
    track: number;
    language: string;
    codec: string;
    channels: number;
  };

  let {
    open,
    tracks,
    activeTrack,
    onSelect,
    onClose,
  }: {
    open: boolean;
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
</script>

<Dropdown {open} {onClose}>
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
</Dropdown>
