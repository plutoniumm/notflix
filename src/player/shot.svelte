<script lang="ts">
  import Sheet from "./sheet.svelte";
  import { GET } from "../core/api";
  import { dur } from "../core/fmt";

  let {
    open,
    file,
    t,
    onClose,
  }: {
    open: boolean;
    file: string;
    t: number;
    onClose: () => void;
  } = $props();

  type Frame = { t: number; url: string; prev: string };
  type Shot = { at: number; w: number; h: number; fps: number; frames: Frame[] };

  let shot = $state<Shot | null>(null);
  let busy = $state(false);
  let failed = $state(false);
  let sel = $state(0);
  let cells = $state<HTMLButtonElement[]>([]);
  let save = $state<HTMLAnchorElement>();

  const frame = $derived(shot?.frames[sel] ?? null);
  const offset = $derived(shot ? sel - shot.at : 0);
  const label = $derived(
    offset === 0
      ? "this frame"
      : `${offset > 0 ? "+" : "−"}${Math.abs(offset)} frame${Math.abs(offset) > 1 ? "s" : ""}`,
  );

  // dur() stops at whole seconds; a frame picker has to show which frame.
  const stamp = (s: number) =>
    `${dur(s)}.${String(Math.floor((s % 1) * 1000)).padStart(3, "0")}`;

  // The video keeps playing behind the sheet, so the timestamp is taken once,
  // when the picker opens — not read live.
  $effect(() => {
    if (!open) {
      shot = null;
      failed = false;

      return;
    }

    let live = true;
    busy = true;
    failed = false;

    GET(`/api/frames?file=${encodeURIComponent(file)}&t=${t.toFixed(3)}`).then(
      (r) => {
        if (!live) return;
        busy = false;
        if (!r?.frames?.length) {
          failed = true;

          return;
        }
        shot = r as Shot;
        sel = r.at;
      },
    );

    return () => {
      live = false;
    };
  });

  function pick(i: number) {
    if (!shot) return;
    sel = Math.max(0, Math.min(shot.frames.length - 1, i));
    cells[sel]?.focus({ preventScroll: true });
  }

  $effect(() => {
    if (shot) cells[sel]?.focus({ preventScroll: true });
  });

  // Capture, because the player's own document-level hotkeys turn ← / → into
  // seeks and would scrub the film out from under the picker.
  $effect(() => {
    if (!open) return;

    const keys = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;

      const step: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1 };
      if (e.key in step) {
        e.preventDefault();
        e.stopPropagation();
        pick(sel + step[e.key]);
      } else if (e.key === "Enter") {
        e.preventDefault();
        e.stopPropagation();
        save?.click();
      }
    };
    window.addEventListener("keydown", keys, true);

    return () => window.removeEventListener("keydown", keys, true);
  });
</script>

<Sheet {open} {onClose} side="mid" title="Frame grab">
  {#if busy}
    <div class="pad cc tx-2 fs-sm">Pulling frames from the source…</div>
  {:else if failed}
    <div class="pad cc tx-2 fs-sm">Could not read a frame here.</div>
  {:else if shot && frame}
    <div class="stage stack rx10" style:--ar={`${shot.w} / ${shot.h}`}>
      <!-- The preview is already decoded, so it fills the frame while the
           full-resolution PNG (megabytes) is still on the wire. -->
      <img class="soft" src={frame.prev} alt="" />
      {#key frame.url}
        <img class="full" src={frame.url} alt="" />
      {/key}
    </div>

    <div class="meta f al-ct j-bw">
      <span class="pl-lbl" class:now={offset === 0}>{label}</span>
      <span class="pl-num fs-xs tx-2">
        {stamp(frame.t)} · {shot.w}×{shot.h} · {shot.fps.toFixed(3)} fps
      </span>
    </div>

    <div class="strip f g5" style:--ar={`${shot.w} / ${shot.h}`}>
      {#each shot.frames as f, i (f.url)}
        <button
          bind:this={cells[i]}
          class="cell ptr rx5"
          class:on={i === sel}
          class:mid={i === shot.at}
          onclick={() => pick(i)}
        >
          <img src={f.prev} alt="" />
        </button>
      {/each}
    </div>

    <div class="acts f al-ct j-bw g10">
      <a
        bind:this={save}
        class="go rx10 ptr"
        href={`${frame.url}&dl=1`}
        download
      >
        Save this frame
      </a>
      <span class="hint fs-xs tx-1">← → pick · Enter save · Esc close</span>
    </div>
  {/if}
</Sheet>

<style>
  .pad {
    min-height: 180px;
  }

  .stage {
    background: #000;
    border: 1px solid var(--glass-bd);
    aspect-ratio: var(--ar, 16 / 9);
    max-height: 46vh;
  }
  .stage img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .soft {
    filter: blur(6px);
    transform: scale(1.03);
  }
  .full {
    animation: fade var(--m, 260ms) var(--ease-out) both;
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }

  .meta {
    margin: 12px 2px 10px;
  }
  .now {
    color: var(--red);
  }

  .cell {
    flex: 1;
    padding: 0;
    min-height: 44px;
    background: var(--glass);
    border: 1px solid transparent;
    opacity: 0.5;
    transition:
      opacity var(--m-fast, 130ms) var(--ease-out),
      border-color var(--m-fast, 130ms) var(--ease-out);
  }
  .cell img {
    display: block;
    width: 100%;
    aspect-ratio: var(--ar, 16 / 9);
    object-fit: cover;
  }
  .cell:hover {
    opacity: 0.85;
  }
  .cell.on {
    opacity: 1;
    border-color: var(--red);
  }
  /* The frame that was actually on screen keeps a mark even once the
     selection has moved off it. */
  .cell.mid::after {
    content: "";
    position: absolute;
    left: 50%;
    bottom: 4px;
    width: 4px;
    height: 4px;
    margin-left: -2px;
    border-radius: 50%;
    background: var(--tx-5);
    box-shadow: 0 0 6px rgba(0, 0, 0, 0.9);
  }

  .acts {
    margin-top: 16px;
    flex-wrap: wrap;
  }

  .go {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 11px 22px;
    background: var(--red);
    color: var(--tx-5);
    font-family: var(--font-display);
    font-size: 14px;
    font-weight: 600;
    letter-spacing: -0.012em;
    transition: filter var(--m-fast, 130ms) var(--ease-out);
  }
  .go:hover {
    filter: brightness(1.12);
  }
  .go:active {
    transform: scale(0.97);
  }

  .hint {
    white-space: nowrap;
  }
  @media (max-width: 560px) {
    .hint {
      display: none;
    }
  }

  @starting-style {
    .stage,
    .strip,
    .acts {
      opacity: 0;
    }
  }
  .stage,
  .strip,
  .acts {
    transition: opacity var(--m, 260ms) var(--ease-out);
  }
</style>
