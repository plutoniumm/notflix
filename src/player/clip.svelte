<script lang="ts">
  import { onDestroy } from "svelte";
  import { fmtTime } from "../core/events.svelte";
  import type { PlayerState } from "../core/events.svelte";
  import { api } from "../core/api";
  import { usePoll } from "../core/poll";
  import { toast } from "../core/toast.svelte";
  import ProgressBar from "../components/ProgressBar.svelte";

  // Mirrors media.ClipMaxSec. Duplicated so the button can refuse before the
  // round trip; the server is still the one that enforces it.
  const MAX = 120;
  const POLL_MS = 700;
  // Comfortably past the server's own per-clip ffmpeg timeout.
  const GIVE_UP_MS = 9 * 60 * 1000;

  let {
    ps,
    file,
    open = $bindable(false),
    marks = $bindable<[number | null, number | null]>([null, null]),
    onSeek,
  }: {
    ps: PlayerState;
    file: string;
    open?: boolean;
    marks?: [number | null, number | null];
    onSeek: (sec: number) => void;
  } = $props();

  type Stage = "idle" | "working" | "ready";

  let stage = $state<Stage>("idle");
  let pct = $state(0);
  let href = $state("");
  let name = $state("");
  let stopPoll: (() => void) | null = null;

  const dur = $derived(ps.duration);
  const inAt = $derived(marks[0]);
  const outAt = $derived(marks[1]);

  // Marks are two independent points, so either can be dropped first; the
  // selection is whatever lies between them.
  const lo = $derived(
    inAt !== null && outAt !== null ? Math.min(inAt, outAt) : inAt ?? outAt,
  );
  const hi = $derived(
    inAt !== null && outAt !== null ? Math.max(inAt, outAt) : null,
  );
  const span = $derived(hi !== null && lo !== null ? hi - lo : 0);
  const tooLong = $derived(span > MAX);
  const ready = $derived(span > 0.05 && !tooLong);

  const at = (t: number) => (dur > 0 ? (t / dur) * 100 : 0);

  function endPoll() {
    stopPoll?.();
    stopPoll = null;
  }

  function reset() {
    endPoll();
    marks = [null, null];
    stage = "idle";
    pct = 0;
    href = "";
    name = "";
  }

  // A mark after a finished clip starts a new one: the old file stays on the
  // server, keyed by its own range, so nothing is lost by dropping the link.
  function mark(which: 0 | 1) {
    if (stage !== "idle") reset();
    open = true;

    const next: [number | null, number | null] = [marks[0], marks[1]];
    next[which] = Math.max(0, Math.min(dur, ps.currentTime));
    marks = next;
  }

  function close() {
    open = false;
    reset();
  }

  async function save() {
    if (!ready || lo === null || hi === null) return;

    stage = "working";
    pct = 0;
    const job = await api.clip.start(file, lo, hi);
    if (!job?.id) {
      stage = "idle";
      return;
    }

    name = job.name ?? "";
    if (job.stage === "done") {
      land(job.url);
      return;
    }

    // A restart forgets the job and every poll answers 404 from then on, so
    // the wait is bounded rather than open-ended.
    const deadline = Date.now() + GIVE_UP_MS;

    stopPoll = usePoll(async () => {
      const s = await api.clip.status(job.id, { silent: true });

      if (!s) {
        if (Date.now() > deadline) fail("Lost track of that clip");
        return;
      }

      pct = s.pct ?? 0;
      if (s.stage === "done") land(s.url);
      else if (s.stage === "error") fail(s.error || "Could not cut this clip");
      else if (Date.now() > deadline) fail("That clip is taking too long");
    }, POLL_MS);
  }

  function fail(msg: string) {
    endPoll();
    stage = "idle";
    pct = 0;
    toast.err(msg);
  }

  // Content-Disposition does the saving, so a same-tab navigation downloads
  // without leaving the player — which is the only route Safari honours. The
  // link stays as the fallback for when the assignment is blocked.
  function land(url: string) {
    endPoll();
    pct = 100;
    href = url;
    stage = "ready";
    toast.ok("Clip ready");
    window.location.href = url;
  }

  // The player hides its chrome on an idle pointer; marking an out-point is
  // not idling, so the tool pins the chrome up for as long as it is open.
  $effect(() => {
    ps.showUI(open || ps.paused);
  });

  $effect(() => {
    file;
    reset();
    open = false;
  });

  function key(e: KeyboardEvent) {
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (
      (e.target as HTMLElement)?.closest?.(
        "input,button,a,select,textarea,[contenteditable]",
      )
    )
      return;

    const k = e.key.toLowerCase();

    if (k === "i" || k === "o") {
      e.preventDefault();
      mark(k === "i" ? 0 : 1);
      return;
    }
    if (!open) return;

    if (e.key === "Escape") {
      // Player's own Escape ladder would close a different panel behind this.
      e.stopPropagation();
      close();
    } else if (e.key === "Backspace" || e.key === "Delete") {
      e.preventDefault();
      marks = [null, null];
    } else if (e.key === "Enter" && stage === "idle") {
      e.preventDefault();
      save();
    }
  }

  $effect(() => {
    document.addEventListener("keydown", key, true);

    return () => document.removeEventListener("keydown", key, true);
  });

  onDestroy(endPoll);

  // The picture's click/double-click handlers live on the page element and
  // skip a fixed list of chrome selectors this panel is not in, so it stops
  // its own gestures rather than toggling playback under itself.
  const eat = (e: Event) => e.stopPropagation();
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="clip pl-enter"
  class:out={!open || dur <= 0}
  onpointerup={eat}
  ondblclick={eat}
  ontouchend={eat}
>
  <div class="row f al-ct g10">
    <span class="pl-lbl sh-0">Clip</span>

    <span class="read pl-num sh-0" class:over={tooLong}>
      {#if lo !== null && hi !== null}
        {fmtTime(lo)} → {fmtTime(hi)}
        <b class="len">{span.toFixed(1)}s</b>
      {:else if lo !== null}
        {fmtTime(lo)} → …
      {:else}
        press <kbd>i</kbd> then <kbd>o</kbd>
      {/if}
    </span>

    <span class="gap"></span>

    {#if stage === "working"}
      <ProgressBar value={pct} label width="140px" height="4px" />
    {:else if stage === "ready"}
      <a class="go done ptr" {href} download={name}>Save to device</a>
    {:else}
      <button class="mk ptr" onclick={() => mark(0)}>In</button>
      <button class="mk ptr" onclick={() => mark(1)}>Out</button>
      <button
        class="mk ptr"
        disabled={lo === null}
        onclick={() => (marks = [null, null])}>Clear</button
      >
      <button class="go ptr" disabled={!ready} onclick={save}>
        {tooLong ? `Over ${MAX}s` : "Save clip"}
      </button>
    {/if}

    <button class="x ptr" onclick={close}>✕</button>
  </div>

  <!-- Same left/right basis as the dock's beam (safe-area + 16px), so a mark
       here sits directly over the same instant on the scrub track below. -->
  <div class="strip">
    <div class="base"></div>

    {#if lo !== null && hi !== null}
      <div
        class="sel"
        class:over={tooLong}
        style="left:{at(lo)}%;width:{at(hi - lo)}%"
      ></div>
    {/if}

    {#if inAt !== null}
      <button
        class="pin a ptr"
        style="left:{at(inAt)}%"
        title="In — {fmtTime(inAt)}"
        onclick={() => onSeek(inAt)}
      ></button>
    {/if}

    {#if outAt !== null}
      <button
        class="pin b ptr"
        style="left:{at(outAt)}%"
        title="Out — {fmtTime(outAt)}"
        onclick={() => onSeek(outAt)}
      ></button>
    {/if}

    <div class="now" style="left:{at(ps.currentTime)}%"></div>
  </div>
</div>

<style>
  .clip {
    position: fixed;
    left: 0;
    right: 0;
    /* Clears the dock, the way .skip does — same stack, same constant family. */
    bottom: calc(100px + var(--safe-b, env(safe-area-inset-bottom)));
    z-index: 11;
    padding: 8px calc(16px + env(safe-area-inset-right)) 10px
      calc(16px + env(safe-area-inset-left));
    color: var(--tx-4);
    /* Continues the dock's gradient upward rather than sitting on it as a
       second box: the two read as one instrument. */
    background: linear-gradient(
      to top,
      rgba(3, 2, 6, 0.62) 0%,
      rgba(3, 2, 6, 0.28) 60%,
      transparent 100%
    );
  }
  .clip.out {
    display: none;
    opacity: 0;
    transform: translateY(12px);
  }
  @starting-style {
    .clip {
      opacity: 0;
      transform: translateY(12px);
    }
  }

  .row {
    margin-bottom: 9px;
  }
  .gap {
    flex: 1;
  }

  .read {
    font-size: 12px;
    color: var(--tx-3);
    white-space: nowrap;
  }
  .read.over {
    color: var(--red);
  }
  .len {
    color: var(--tx-5);
    font-weight: 600;
    margin-left: 4px;
  }
  kbd {
    font-family: var(--font-display);
    font-size: 10px;
    padding: 1px 5px;
    border-radius: var(--r-sm);
    background: var(--glass-2);
    color: var(--tx-4);
  }

  .mk,
  .go,
  .x {
    min-height: 32px;
    padding: 6px 13px;
    border-radius: var(--r-md);
    border: 1px solid var(--glass-bd);
    background: var(--glass);
    color: var(--tx-4);
    font-family: var(--font-display);
    font-size: 12px;
    font-weight: 500;
    white-space: nowrap;
    transition:
      background var(--m-fast, 130ms) var(--ease-out),
      color var(--m-fast, 130ms) var(--ease-out),
      transform var(--m-fast, 130ms) var(--ease-snap);
  }
  @media (hover: none) {
    .mk,
    .go,
    .x {
      min-height: 44px;
      min-width: 44px;
      padding: 6px 16px;
    }
  }
  .mk:hover,
  .x:hover {
    background: var(--glass-2);
    color: var(--tx-5);
  }
  .mk:active,
  .go:active,
  .x:active {
    transform: scale(0.94);
  }
  .mk:disabled {
    opacity: 0.35;
    cursor: default;
  }

  .go {
    background: var(--red);
    border-color: transparent;
    color: var(--tx-5);
    font-weight: 600;
  }
  .go:hover {
    background: color-mix(in oklab, var(--red) 82%, var(--tx-5));
  }
  .go:disabled {
    background: var(--glass-2);
    color: var(--tx-2);
    cursor: default;
  }
  .go.done {
    background: var(--grn);
    color: #06110c;
    display: inline-flex;
    align-items: center;
  }

  .x {
    padding: 6px 11px;
    color: var(--tx-2);
  }

  .strip {
    position: relative;
    height: 14px;
  }
  .base,
  .sel {
    position: absolute;
    top: 50%;
    height: 4px;
    transform: translateY(-50%);
    border-radius: 99px;
  }
  .base {
    left: 0;
    right: 0;
    background: rgba(255, 255, 255, 0.14);
  }
  .sel {
    background: linear-gradient(90deg, var(--art-1) 0%, var(--red) 100%);
    box-shadow: 0 0 12px -2px var(--red-glow);
  }
  .sel.over {
    background: repeating-linear-gradient(
      45deg,
      var(--red) 0 6px,
      rgba(3, 2, 6, 0.55) 6px 12px
    );
  }

  .pin {
    position: absolute;
    top: 50%;
    width: 3px;
    height: 14px;
    padding: 0;
    border: 0;
    border-radius: 2px;
    background: var(--tx-5);
    transform: translate(-50%, -50%);
    box-shadow: 0 0 0 2px rgba(3, 2, 6, 0.7);
    transition: height var(--m-fast, 130ms) var(--ease-snap);
  }
  /* The visible pin stays 3px wide; the tap target does not. */
  .pin::before {
    content: "";
    position: absolute;
    inset: -15px -14px;
  }
  .pin:hover {
    height: 20px;
  }
  .pin.b {
    background: var(--gold);
  }

  .now {
    position: absolute;
    top: 50%;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--tx-5);
    transform: translate(-50%, -50%);
    box-shadow: 0 0 0 2px rgba(3, 2, 6, 0.7);
    pointer-events: none;
  }

  @media (max-width: 560px) {
    .read {
      font-size: 11px;
    }
    .mk,
    .go {
      padding: 6px 10px;
    }
  }
</style>
