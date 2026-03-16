<script lang="ts">
  import { clean, vidURL } from "../lib/video";

  let {
    rows,
    dir,
    name,
    paused,
    embed,
  }: {
    rows: [string, any[]][];
    dir: string;
    name: string;
    paused: boolean;
    embed: boolean;
  } = $props();

  let contentEl: HTMLDivElement | undefined = $state();
  let isOpen = $state(false);
  let closing = $state(false);

  function toggle(e: MouseEvent) {
    e.preventDefault();
    if (isOpen) {
      close();
    } else {
      open();
    }
  }

  function open() {
    isOpen = true;
    requestAnimationFrame(() => {
      if (!contentEl) return;
      const height = contentEl.scrollHeight;
      contentEl.animate(
        [
          { height: "0px", opacity: 0 },
          { height: height + "px", opacity: 1 },
        ],
        { duration: 250, easing: "ease", fill: "forwards" },
      );
    });
  }

  function close() {
    if (!contentEl) {
      isOpen = false;
      return;
    }

    closing = true;
    const height = contentEl.scrollHeight;
    const anim = contentEl.animate(
      [
        { height: height + "px", opacity: 1 },
        { height: "0px", opacity: 0 },
      ],
      { duration: 200, easing: "ease", fill: "forwards" },
    );

    anim.onfinish = () => {
      isOpen = false;
      closing = false;
    };
  }
</script>

{#if !embed && rows.length > 0 && paused}
  {#each rows as [rowDir, files]}
    {#if rowDir === dir && files.length > 1}
      <details open={isOpen || closing}>
        <summary
          class="fs-sm c-muted m0 fw4 f al-ct g5 ptr p-fix p20"
          onclick={toggle}
        >
          <span class="chevron d-ib" class:open={isOpen && !closing}></span>
        </summary>

        <div class="content" bind:this={contentEl}>
          <div class="list f flow-x-s g10">
            {#each files as f (f.key)}
              <a
                href={vidURL(rowDir, f.name)}
                class="serie sh-0 rx2 flow-h"
                class:active={f.name === name}
              >
                <img
                  src="/images/{f.key}.jpg"
                  alt=""
                  loading="lazy"
                  class="w-100"
                />
                <span class="d-b fs-xs p5 c-muted trunc">{clean(f.name)}</span>
              </a>
            {/each}
          </div>
        </div>
      </details>
    {/if}
  {/each}
{/if}

<style>
  details {
    bottom: 0;
    left: 0;
    right: 0;
    z-index: 10;
    background: linear-gradient(to top, #000c 0%, transparent 100%);
    animation: slide-up 0.25s ease;
  }

  summary {
    user-select: none;
    margin-bottom: 10px;
  }
  summary::-webkit-details-marker {
    display: none;
  }

  .chevron {
    width: 8px;
    height: 8px;
    border: 2px solid currentColor;
    transform: rotate(135deg);
    transition: transform 0.25s ease;
  }
  .chevron.open {
    transform: rotate(-45deg);
  }

  .content {
    overflow: hidden;
  }

  .list {
    padding-bottom: 4px;
    scrollbar-width: none;
  }
  .list::-webkit-scrollbar {
    display: none;
  }

  .serie {
    width: 140px;
    border: 2px solid transparent;
    transition:
      border-color 0.15s,
      transform 0.15s;
  }
  .serie:hover {
    transform: scale(1.04);
  }
  .serie.active {
    border-color: #e50914;
  }
  .serie.active span {
    color: #fff;
  }
  .serie img {
    aspect-ratio: 16/9;
    background: #222;
  }
  .serie span {
    background: #111;
  }
</style>
