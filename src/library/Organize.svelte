<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { api } from "../core/api";
  import { toast } from "../core/toast.svelte";
  import { clean } from "../core/video";
  import { ORGANIZE_POLL_MS } from "../core/events.svelte";
  import { usePoll } from "../core/poll";

  type Move = { file: string; root: string; folder: string };
  type Plan = {
    stage: string;
    run: number;
    runs: number;
    note: string;
    moves: Move[];
    total: number;
    error: string;
  };

  let { onApplied }: { onApplied: () => void } = $props();

  let loading = $state(false);
  let applying = $state(false);
  let moves = $state<Move[] | null>(null);
  let excluded = $state(new Set<string>());
  let note = $state("");
  let stage = $state("");
  let stop: (() => void) | null = null;

  const key = (m: Move) => `${m.root}/${m.file}`;

  const groups = $derived.by(() => {
    const map = new Map<string, Move[]>();
    for (const m of moves ?? []) {
      if (!map.has(m.folder)) map.set(m.folder, []);
      map.get(m.folder)!.push(m);
    }
    return [...map.entries()];
  });

  const selected = $derived((moves ?? []).filter((m) => !excluded.has(key(m))));

  const stageText = (s: Plan) =>
    s.stage === "loading model"
      ? "loading model into memory…"
      : s.stage === "merging"
        ? "merging…"
        : s.run
          ? `classifying — run ${s.run} of ${s.runs}…`
          : "classifying…";

  function stopPoll() {
    stop?.();
    stop = null;
  }

  function finish(s: Plan) {
    stopPoll();
    loading = false;
    stage = "";
    if (s.stage === "error") {
      note = s.error || "the model didn't return a usable plan — try again";
      return;
    }
    if (!s.moves?.length) {
      note = `nothing to organize — ${s.total ?? 0} files considered`;
      return;
    }
    moves = s.moves;
  }

  async function check() {
    const s: Plan | null = await api.organize.status({ silent: true });
    if (!s) return;
    if (s.stage === "done" || s.stage === "error") finish(s);
    else if (s.stage === "idle") {
      stopPoll();
      loading = false;
      stage = "";
    } else stage = stageText(s);
  }

  function startPoll() {
    loading = true;
    stopPoll();
    stop = usePoll(check, ORGANIZE_POLL_MS);
  }

  async function suggest() {
    loading = true;
    moves = null;
    note = "";
    excluded = new Set();
    const res: Plan | null = await api.organize.plan({ tag: "organize" });
    if (!res) {
      loading = false;
      return;
    }
    stage = stageText(res);
    startPoll();
  }

  onMount(() => {
    api.organize.warm({ silent: true });
    api.organize.status({ silent: true }).then((s: Plan | null) => {
      if (!s) return;
      if (s.stage === "done" && s.moves?.length) moves = s.moves;
      else if (s.stage !== "idle" && s.stage !== "done" && s.stage !== "error") {
        stage = stageText(s);
        startPoll();
      }
    });
  });
  onDestroy(stopPoll);

  function toggle(m: Move) {
    const next = new Set(excluded);
    const k = key(m);
    if (next.has(k)) next.delete(k);
    else next.add(k);
    excluded = next;
  }

  async function apply() {
    if (!selected.length) return;
    applying = true;
    const res = await api.organize.apply(selected);
    applying = false;
    if (!res) return;

    if (res.errors?.length) {
      toast.err(`Moved ${res.moved}, failed ${res.errors.length}: ${res.errors[0]}`);
    } else {
      toast.ok(`Moved ${res.moved} videos`);
    }
    moves = null;
    onApplied();
  }
</script>

<section class="panel surface flow-h">
  <h3 class="phead m0 f al-ct g10">
    <span class="plabel">Organize with AI</span>
    <span class="pnote fs-xs tx-1">
      groups loose movies into folders — nothing moves until you apply
    </span>
  </h3>

  {#if moves === null}
    <div class="bar f al-ct g10">
      <button class="act ptr fs-sm" onclick={suggest} disabled={loading}>
        {loading ? "Asking the model…" : "Suggest organization"}
      </button>
      {#if loading}
        <span class="stage fs-xs tx-1 f al-ct g5">
          <span class="spin"></span>{stage || "starting…"}
        </span>
      {:else if note}
        <span class="fs-xs tx-1">{note}</span>
      {/if}
    </div>
  {:else if moves.length > 0}
    <div class="plan">
      {#each groups as [folder, files] (folder)}
        <div class="group">
          <div class="gname fs-xs f al-ct g5">
            <span class="arrow">→</span>{folder}
            <span class="gcount">{files.length}</span>
          </div>
          {#each files as m (key(m))}
            <label class="item f al-ct g10 ptr" class:off={excluded.has(key(m))}>
              <input
                type="checkbox"
                checked={!excluded.has(key(m))}
                onchange={() => toggle(m)}
              />
              <span class="fs-sm tx-4 trunc">{clean(m.file)}</span>
              <span class="fs-xs tx-1 root">{m.root}</span>
            </label>
          {/each}
        </div>
      {/each}
    </div>
    <div class="bar f al-ct g10">
      <button
        class="act ptr fs-sm"
        onclick={apply}
        disabled={applying || !selected.length}
      >
        {applying ? "Moving…" : `Apply ${selected.length} moves`}
      </button>
      <button class="ghost ptr fs-sm" onclick={() => (moves = null)}>
        Discard
      </button>
      <span class="fs-xs tx-1 count">
        {selected.length} of {moves.length} selected
      </span>
    </div>
  {/if}
</section>

<style>
  .panel {
    margin-bottom: 20px;
    transition:
      opacity var(--motion, 0.32s) var(--ease-out),
      translate var(--motion, 0.32s) var(--ease-out);
  }
  @starting-style {
    .panel {
      opacity: 0;
      translate: 0 10px;
    }
  }
  .phead {
    padding: 10px 16px;
    background: rgb(255 255 255 / 0.02);
    border-bottom: 1px solid var(--glass-bd);
  }
  .plabel {
    color: var(--tx-3);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }
  .pnote {
    margin-left: auto;
    text-align: right;
  }

  .bar {
    padding: 12px 16px;
  }
  .stage {
    font-variant-numeric: tabular-nums;
  }
  .spin {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    border: 1.5px solid var(--glass-bd);
    border-top-color: var(--red);
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin {
    to {
      rotate: 360deg;
    }
  }

  .plan {
    max-height: 46vh;
    overflow-y: auto;
  }
  .group {
    border-top: 1px solid rgb(255 255 255 / 0.04);
  }
  .gname {
    padding: 10px 16px 4px;
    color: var(--tx-4);
    font-weight: 600;
    letter-spacing: 0.02em;
  }
  .arrow {
    color: var(--tx-1);
  }
  .gcount {
    margin-left: auto;
    color: var(--tx-1);
    font-variant-numeric: tabular-nums;
  }
  .item {
    padding: 5px 16px 5px 30px;
    transition:
      background var(--motion-fast, 0.15s) var(--ease-out),
      opacity var(--motion-fast, 0.18s) var(--ease-out);
  }
  .item:hover {
    background: var(--glass);
  }
  .item.off {
    opacity: 0.35;
  }
  .item input {
    accent-color: var(--red);
    flex-shrink: 0;
  }
  .root {
    margin-left: auto;
    flex-shrink: 0;
  }

  .act {
    background: var(--bg-5);
    color: var(--tx-5);
    border: 1px solid var(--glass-bd);
    padding: 8px 16px;
    border-radius: var(--r-md);
    transition:
      background var(--motion-fast, 0.18s) var(--ease-out),
      scale var(--motion-fast, 0.16s) var(--ease-snap);
  }
  .act:hover:not(:disabled) {
    background: var(--red);
    border-color: var(--red);
  }
  .act:active:not(:disabled) {
    scale: 0.97;
  }
  .act:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .ghost {
    background: transparent;
    color: var(--tx-1);
    border: 1px solid var(--glass-bd);
    padding: 8px 16px;
    border-radius: var(--r-md);
  }
  .ghost:hover {
    color: var(--tx-4);
  }
  .count {
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }

  @media (prefers-reduced-motion: reduce) {
    .panel,
    .item {
      transition-duration: 1ms;
    }
    .spin {
      animation: none;
    }
  }
</style>
