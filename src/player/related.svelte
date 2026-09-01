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
  const pad = (n: number) => String(n).padStart(2, "0");
</script>

{#if !embed && files.length > 1}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <aside
    class="rail flow-h"
    class:open
    class:out={!visible}
    onmouseenter={() => (open = true)}
    onmouseleave={() => (open = false)}
  >
    <button class="tab cc ptr" onclick={() => (open = !open)}>
      <span class="chev d-ib" class:rot={open}></span>
      <span class="label">UP NEXT</span>
      <span class="count pl-num">{pad(files.length)}</span>
    </button>

    <div class="list flow-y-s f-col g10 p10">
      {#each files as f, i (f.key)}
        <a
          href={vidURL(dir, f.name)}
          class="card flow-h"
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
          <span class="meta f al-ct g5">
            <span class="idx pl-num">{pad(i + 1)}</span>
            <span class="title trunc">{clean(f.name)}</span>
          </span>
        </a>
      {/each}
    </div>
  </aside>
{/if}

<style>
  .rail {
    position: fixed;
    top: 64px;
    bottom: 96px;
    left: env(safe-area-inset-left);
    z-index: 9;
    width: 40px;
    display: flex;
    flex-direction: row;
    background: linear-gradient(
      to right,
      rgba(3, 2, 6, 0.88) 0%,
      rgba(3, 2, 6, 0.5) 80%,
      transparent 100%
    );
    border-right: 1px solid var(--glass-bd);
    border-radius: 0 var(--r-lg) var(--r-lg) 0;
    backdrop-filter: blur(16px) saturate(150%);
    -webkit-backdrop-filter: blur(16px) saturate(150%);
    transition:
      width var(--m, 260ms) var(--ease-out),
      opacity var(--m, 260ms) var(--ease-out),
      transform var(--m, 260ms) var(--ease-out);
  }
  @supports (transition-behavior: allow-discrete) {
    .rail {
      transition-property: width, opacity, transform, display;
      transition-behavior: allow-discrete;
    }
  }
  .rail.open { width: 244px; }
  .rail.out {
    display: none;
    opacity: 0;
    transform: translateX(-14px);
  }
  @starting-style {
    .rail {
      opacity: 0;
      transform: translateX(-14px);
    }
  }
  /* Hover-only affordance. On a phone the strip can never be opened but
     still swallows taps in the left edge — part of the back-10s zone. */
  @media (hover: none) {
    .rail { display: none; }
  }

  .tab {
    flex-direction: column;
    gap: 12px;
    width: 40px;
    color: var(--tx-3);
    padding: 16px 0;
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
    transition: transform var(--m, 260ms) var(--ease-out);
  }
  .chev.rot { transform: rotate(225deg); }
  .label {
    writing-mode: vertical-rl;
    transform: rotate(180deg);
    font-family: var(--font-display);
    letter-spacing: 0.18em;
    font-size: 10px;
    font-weight: 600;
    color: var(--tx-2);
  }
  .count {
    font-size: 10px;
    color: var(--tx-1);
    margin-top: auto;
  }

  .list {
    flex: 1;
    opacity: 0;
    transition: opacity var(--m-fast, 130ms) var(--ease-out) 60ms;
    scrollbar-width: none;
  }
  .rail.open .list { opacity: 1; }
  .list::-webkit-scrollbar { display: none; }

  .card {
    background: var(--bg-2);
    border: 1px solid transparent;
    border-radius: var(--r-md);
    overflow: hidden;
    transition:
      border-color var(--m-fast, 130ms) var(--ease-out),
      transform var(--m-fast, 130ms) var(--ease-out),
      background var(--m-fast, 130ms) var(--ease-out);
    flex-shrink: 0;
  }
  .card:hover {
    transform: translateX(3px);
    background: var(--bg-3);
  }
  .card.active {
    border-color: color-mix(in oklab, var(--red) 70%, var(--art-1));
    box-shadow: 0 0 18px -6px var(--red-glow);
  }
  .card.active .title { color: var(--tx-5); }

  .thumb-wrap { background: var(--bg-3); }
  .card img {
    aspect-ratio: 16/9;
    display: block;
  }
  .now-pill {
    position: absolute;
    top: 5px;
    left: 5px;
    padding: 2px 7px;
    border-radius: 99px;
    background: var(--red);
    color: #fff;
    font-family: var(--font-display);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.08em;
  }

  .meta { padding: 7px 9px 8px; }
  .idx {
    font-size: 10px;
    color: var(--tx-1);
    flex-shrink: 0;
  }
  .title {
    font-size: 12px;
    line-height: 1.3;
    letter-spacing: -0.008em;
    color: var(--tx-3);
  }
</style>
