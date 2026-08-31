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

<section class="organize surface flow-h">
  <h3 class="m0 p10 fs-sm fw6 f al-ct g10">
    <span>✨ Organize with AI</span>
    <span class="fs-xs tx-1" style="margin-left:auto">
      groups loose movies into folders — nothing moves until you apply
    </span>
  </h3>

  {#if moves === null}
    <div class="p10 f al-ct g10">
      <button class="act rx5 ptr fs-sm" onclick={suggest} disabled={loading}>
        {loading ? "Asking the model…" : "Suggest organization"}
      </button>
      {#if loading}
        <span class="fs-xs tx-1">{stage || "starting…"}</span>
      {:else if note}
        <span class="fs-xs tx-1">{note}</span>
      {/if}
    </div>
  {:else if moves.length > 0}
    {#each groups as [folder, files] (folder)}
      <div class="group">
        <div class="fs-sm fw6 tx-4 p10 pb0">📁 {folder}</div>
        {#each files as m (key(m))}
          <label class="item f al-ct g10 ptr">
            <input
              type="checkbox"
              checked={!excluded.has(key(m))}
              onchange={() => toggle(m)}
            />
            <span class="fs-sm tx-4 trunc">{clean(m.file)}</span>
            <span class="fs-xs tx-1" style="margin-left:auto">{m.root}</span>
          </label>
        {/each}
      </div>
    {/each}
    <div class="p10 f al-ct g10">
      <button
        class="act rx5 ptr fs-sm"
        onclick={apply}
        disabled={applying || !selected.length}
      >
        {applying ? "Moving…" : `Apply ${selected.length} moves`}
      </button>
      <button class="ghost rx5 ptr fs-sm" onclick={() => (moves = null)}>
        Discard
      </button>
    </div>
  {/if}
</section>

<style>
  .organize {
    margin-bottom: 24px;
  }
  .organize h3 {
    background: rgba(255, 255, 255, 0.02);
    color: var(--tx-4);
    border-bottom: 1px solid var(--glass-bd);
  }

  .group {
    border-top: 1px solid rgba(255, 255, 255, 0.04);
  }
  .pb0 {
    padding-bottom: 0;
  }
  .item {
    padding: 6px 16px 6px 24px;
  }
  .item:hover {
    background: var(--glass);
  }

  .act {
    background: var(--bg-5);
    color: var(--tx-5);
    border: 1px solid var(--glass-bd);
    padding: 8px 16px;
  }
  .act:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .ghost {
    background: transparent;
    color: var(--tx-1);
    border: 1px solid var(--glass-bd);
    padding: 8px 16px;
  }
</style>
