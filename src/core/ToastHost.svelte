<script lang="ts">
  import { toast } from "./toast.svelte";
</script>

<div class="host">
  {#each toast.items as t (t.id)}
    <button
      class="toast glass-strong {t.kind}"
      onclick={() => toast.dismiss(t.id)}
      aria-label="Dismiss"
    >
      <span class="ico">
        {#if t.kind === "err"}!{:else if t.kind === "ok"}✓{:else}i{/if}
      </span>
      <span class="msg">{t.msg}</span>
    </button>
  {/each}
</div>

<style>
  .host {
    position: fixed;
    bottom: 16px;
    right: 16px;
    z-index: 10000;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: min(420px, calc(100vw - 32px));
    pointer-events: none;
  }

  .toast {
    pointer-events: auto;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    border-radius: var(--r-lg);
    font-size: 13px;
    line-height: 1.35;
    color: var(--tx-5);
    box-shadow: var(--sh-3);
    animation: slide-in-r 0.28s var(--ease-out);
    text-align: left;
    cursor: pointer;
    transition: transform 0.16s var(--ease-snap),
      background 0.18s var(--ease-out), border-color 0.18s var(--ease-out);
  }
  .toast:active { transform: scale(0.97); }

  .toast.err {
    border-color: rgba(255, 77, 109, 0.4);
    background: linear-gradient(
      180deg,
      rgba(60, 12, 24, 0.85),
      rgba(22, 19, 28, 0.85)
    );
    box-shadow: var(--sh-3), 0 0 0 1px rgba(255, 77, 109, 0.15);
  }
  .toast.ok {
    border-color: rgba(52, 211, 153, 0.4);
    background: linear-gradient(
      180deg,
      rgba(10, 50, 32, 0.85),
      rgba(22, 19, 28, 0.85)
    );
    box-shadow: var(--sh-3), 0 0 0 1px rgba(52, 211, 153, 0.15);
  }

  .ico {
    flex-shrink: 0;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    font-weight: 700;
    font-size: 12px;
    background: var(--glass-2);
  }
  .toast.err .ico {
    background: var(--red);
    box-shadow: 0 0 12px var(--red-glow);
  }
  .toast.ok .ico {
    background: var(--grn);
    color: #042618;
    box-shadow: 0 0 12px rgba(52, 211, 153, 0.5);
  }

  .msg {
    flex: 1;
    word-break: break-word;
  }
</style>
