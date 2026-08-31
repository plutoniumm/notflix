<script lang="ts">
  import type { Snippet } from "svelte";
  import { clean, thumbFail, vidURL } from "../core/video";

  // The library's one video tile. `overlay` draws inside the thumb (badges,
  // per-card actions), `footer` below the title — leaving it empty keeps the
  // .card-name:last-child rounding.
  let {
    dir,
    name,
    key,
    idx = 0,
    downloaded = false,
    overlay,
    footer,
  }: {
    dir: string;
    name: string;
    key: string;
    idx?: number;
    downloaded?: boolean;
    overlay?: Snippet;
    footer?: Snippet;
  } = $props();
</script>

<a
  href={vidURL(dir, name)}
  class="media-card ptr flow-h p-rel"
  style="--i: {idx}"
>
  <div class="thumb p-rel flow-h">
    <img
      class="h-100 w-100 d-b"
      src="/images/{key}.jpg"
      alt=""
      loading="lazy"
      onerror={thumbFail}
    />
    <div class="play-icon p-abs cc o-0 fs-lg">▶</div>
    {#if downloaded}
      <div class="dot p-abs"></div>
    {/if}
    {@render overlay?.()}
  </div>

  <div class="card-name">{clean(name)}</div>
  {@render footer?.()}
</a>
