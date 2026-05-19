<script lang="ts">
  // Central inline-SVG registry. Each entry carries the exact viewBox /
  // intrinsic size / stroke|fill setup of the original inline SVG; shapes are
  // real Svelte SVG nodes (NOT {@html} — that renders in the HTML namespace
  // inside <svg> and breaks). `color` drives currentColor (used by the
  // Download error state); omit to inherit. `size` overrides both dims.
  let {
    name,
    size,
    color,
  }: {
    name:
      | "audio"
      | "captions"
      | "whisper"
      | "download"
      | "search"
      | "settings"
      | "volume";
    size?: number;
    color?: string;
  } = $props();

  type Meta = {
    vb: string;
    w: number;
    h: number;
    stroke?: number | true;
    fill?: string;
    rnd?: boolean;
  };
  const META: Record<string, Meta> = {
    audio: { vb: "0 0 24 24", w: 18, h: 18, stroke: 2, rnd: true },
    captions: { vb: "0 0 20 15", w: 20, h: 15 },
    whisper: { vb: "0 0 24 24", w: 18, h: 18, stroke: true },
    download: { vb: "0 0 24 24", w: 18, h: 18, stroke: true },
    search: { vb: "0 0 24 24", w: 18, h: 18, stroke: 2, rnd: true },
    settings: { vb: "0 0 24 24", w: 18, h: 18, stroke: 2, rnd: true },
    volume: { vb: "0 0 24 24", w: 14, h: 14, fill: "currentColor" },
  };

  const m = $derived(META[name]);
  const w = $derived(size ?? m?.w ?? 18);
  const h = $derived(size ?? m?.h ?? 18);
</script>

{#if m}
  <svg
    width={w}
    height={h}
    viewBox={m.vb}
    fill={m.fill ?? "none"}
    stroke={m.stroke ? "currentColor" : null}
    stroke-width={typeof m.stroke === "number" ? m.stroke : null}
    stroke-linecap={m.rnd ? "round" : null}
    stroke-linejoin={m.rnd ? "round" : null}
    style:color={color}
  >
    {#if name === "audio"}
      <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
      <path d="M15.54 8.46a5 5 0 0 1 0 7.07" />
      <path d="M19.07 4.93a10 10 0 0 1 0 14.14" />
    {:else if name === "captions"}
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
      <rect x="10.5" y="9" width="7" height="2" rx="1" fill="currentColor" />
    {:else if name === "whisper"}
      <rect x="9" y="2" width="6" height="12" rx="3" />
      <path d="M5 10a7 7 0 0 0 14 0" />
      <line x1="12" y1="17" x2="12" y2="21" />
      <line x1="8" y1="21" x2="16" y2="21" />
    {:else if name === "download"}
      <path d="M12 3v13M5 12l7 7 7-7" />
      <line x1="4" y1="21" x2="20" y2="21" />
    {:else if name === "search"}
      <circle cx="11" cy="11" r="7" />
      <line x1="21" y1="21" x2="16.5" y2="16.5" />
    {:else if name === "settings"}
      <circle cx="12" cy="12" r="3" />
      <path
        d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
      />
    {:else if name === "volume"}
      <path
        d="M3 9v6h4l5 5V4L7 9H3zm13.5 3c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z"
      />
    {/if}
  </svg>
{/if}
