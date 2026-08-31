<script lang="ts">
  import { clean, vidURL, thumbFail } from "../core/video";

  let { rows, dir, name, paused, embed }: any = $props();

  let open = $state(false);

  // Show only when paused and the current dir actually has siblings.
  const files = $derived(
    (rows as [string, any[]][])?.find(([d]) => d === dir)?.[1] ?? [],
  );
  const visible = $derived(!embed && paused && files.length > 1);
  const activeIdx = $derived(files.findIndex((f: any) => f.name === name));
</script>

{#if visible}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <aside
    class="rail p-fix flow-h"
    class:open
    onmouseenter={() => (open = true)}
    onmouseleave={() => (open = false)}
  >
    <button class="tab cc fw5 fs-xs ptr" onclick={() => (open = !open)}>
      <span class="chev d-ib" class:rot={open}></span>
      <span class="label">UP NEXT</span>
    </button>

    <div class="list flow-y-s f-col g10 p10">
      {#each files as f, i (f.key)}
        <a
          href={vidURL(dir, f.name)}
          class="card flow-h rx5"
          class:active={i === activeIdx}
        >
          <div class="thumb-wrap p-rel">
            <img
              src="/images/{f.key}.jpg"
              alt=""
              loading="lazy"
              class="w-100"
              onerror={thumbFail}
            />
            {#if i === activeIdx}<span class="now-pill">NOW</span>{/if}
          </div>
          <span class="title d-b fs-xs p5 trunc">{clean(f.name)}</span>
        </a>
      {/each}
    </div>
  </aside>
{/if}

<style>
  .rail {
    top: 64px;
    bottom: 96px;
    left: env(safe-area-inset-left);
    z-index: 9;
    width: 36px;
    display: flex;
    flex-direction: row;
    background: linear-gradient(
      to right,
      rgba(7, 6, 10, 0.85) 0%,
      rgba(7, 6, 10, 0.55) 80%,
      transparent 100%
    );
    border-right: 1px solid var(--glass-bd);
    backdrop-filter: blur(10px) saturate(140%);
    -webkit-backdrop-filter: blur(10px) saturate(140%);
    transition: width 0.24s var(--ease-out), background 0.24s var(--ease-out);
    animation: slide-r 0.3s var(--ease-out);
  }
  .rail.open { width: 220px; }
  /* Hover-only affordance. On a phone the 36px strip can never be opened but
     still swallows taps in the left edge — part of the back-10s zone. */
  @media (hover: none) {
    .rail { display: none; }
  }

  .tab {
    flex-direction: column;
    gap: 10px;
    width: 36px;
    color: var(--tx-3);
    padding: 14px 0;
    background: transparent;
    border: none;
    border-right: 1px solid var(--glass-bd);
    user-select: none;
    touch-action: manipulation;
  }
  .chev {
    width: 6px;
    height: 6px;
    border: 2px solid currentColor;
    border-left: 0;
    border-bottom: 0;
    transform: rotate(45deg);
    transition: transform 0.24s var(--ease-out);
  }
  .chev.rot { transform: rotate(225deg); }
  .label {
    writing-mode: vertical-rl;
    transform: rotate(180deg);
    letter-spacing: 0.12em;
    font-size: 10px;
    font-weight: 600;
    color: var(--tx-2);
  }

  .list {
    flex: 1;
    opacity: 0;
    transition: opacity 0.18s var(--ease-out) 0.06s;
    scrollbar-width: none;
  }
  .rail.open .list { opacity: 1; }
  .list::-webkit-scrollbar { display: none; }

  .card {
    background: var(--bg-2);
    border: 1px solid transparent;
    transition: border-color 0.15s var(--ease-out), transform 0.15s var(--ease-out);
    flex-shrink: 0;
  }
  .card:hover { transform: translateX(2px); }
  .card.active { border-color: var(--red); }
  .card.active .title { color: var(--tx-5); }

  .thumb-wrap { background: var(--bg-3); }
  .card img {
    aspect-ratio: 16/9;
    display: block;
  }
  .now-pill {
    position: absolute;
    top: 4px;
    left: 4px;
    padding: 1px 6px;
    border-radius: 99px;
    background: var(--red);
    color: #fff;
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.06em;
  }
  .title { color: var(--tx-3); }

  @keyframes slide-r {
    from { opacity: 0; transform: translateX(-12px); }
    to   { opacity: 1; transform: translateX(0); }
  }
</style>
