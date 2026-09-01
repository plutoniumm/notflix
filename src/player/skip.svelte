<script lang="ts" module>
  export function activeSkip(
    t: number,
    intro: [number, number] | null,
    outro: [number, number] | null,
  ): "intro" | "outro" | null {
    if (intro && t >= intro[0] && t < intro[1] - 1) return "intro";
    if (outro && t >= outro[0] && t < outro[1] - 1) return "outro";
    return null;
  }
</script>

<script lang="ts">
  import type { PlayerState } from "../core/events.svelte";

  let {
    ps,
    intro,
    outro,
    onSeek,
    onNext,
  }: {
    ps: PlayerState;
    intro: [number, number] | null;
    outro: [number, number] | null;
    onSeek: (t: number) => void;
    onNext: () => void;
  } = $props();

  const which = $derived(activeSkip(ps.currentTime, intro, outro));
  const nextish = $derived(which === "outro" && !!ps.nextURL);
  const label = $derived(
    which === "intro" ? "Skip intro" : nextish ? "Next episode" : "Skip credits",
  );

  // Rendered always, hidden by `.out`, so it can leave with the same motion it
  // arrived with. Every action is guarded — `which` is null most of the time.
  function act() {
    if (which === "intro") onSeek(intro![1]);
    else if (nextish) onNext();
    else if (which === "outro") onSeek(outro![1]);
  }
</script>

<button
  class="skip pl-enter rx5 ptr"
  class:out={!which}
  aria-hidden={!which}
  tabindex={which ? 0 : -1}
  onclick={act}
>
  <span class="lbl">{label}</span>
  <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor"><path d="M5 4l12 8L5 20zM19 4h2v16h-2z" /></svg>
</button>

<style>
  .skip {
    position: fixed;
    /* Offsets, not padding: this is pinned by right/bottom, so the inset has
       to move the button rather than inflate it. env() is 0 without a notch. */
    right: calc(28px + env(safe-area-inset-right));
    bottom: calc(110px + var(--safe-b, env(safe-area-inset-bottom)));
    z-index: 12;
    display: flex;
    align-items: center;
    gap: 9px;
    min-height: 44px;
    padding: 11px 20px;
    background: rgba(3, 2, 6, 0.72);
    border: 1px solid var(--glass-bd);
    border-radius: var(--r-xl);
    color: var(--tx-5);
    font-family: var(--font-display);
    font-size: 14px;
    font-weight: 500;
    letter-spacing: -0.012em;
    backdrop-filter: blur(16px) saturate(150%);
    -webkit-backdrop-filter: blur(16px) saturate(150%);
    box-shadow:
      var(--sh-3),
      inset 0 1px 0 rgba(255, 255, 255, 0.07);
  }
  .skip.out {
    display: none;
    opacity: 0;
    transform: translateY(10px) scale(0.96);
  }
  @starting-style {
    .skip {
      opacity: 0;
      transform: translateY(10px) scale(0.96);
    }
  }

  .lbl {
    white-space: nowrap;
  }

  .skip:hover {
    background: var(--red);
    border-color: transparent;
  }
  .skip:active {
    transform: scale(0.96);
  }
</style>
