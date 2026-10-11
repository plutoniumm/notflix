<script lang="ts">
  import Sect from "./Sect.svelte";
  import { bytes, pct } from "../core/fmt";
  import { clean } from "../core/video";

  // /api/manage/dirsizes already carries per-folder bytes and its root, so
  // "where did 500 GB go" needs no new request — only an allocation bar.
  let {
    disks,
    dirSizes,
    i = 0,
  }: {
    disks: DiskInfo[];
    dirSizes: Record<string, { bytes: number; root: string }>;
    i?: number;
  } = $props();

  const TOP = 6;

  let pick = $state<{ root: string; label: string; note: string } | null>(null);

  const label = (dir: string) => (dir === "." ? "Root" : clean(dir));

  let views = $derived(
    disks.map((d) => {
      const used = Math.max(0, d.total - d.free);
      const top = Object.entries(dirSizes)
        .filter(([, v]) => v.root === d.root && v.bytes > 0)
        .map(([dir, v]) => ({ dir, bytes: v.bytes }))
        .sort((a, b) => b.bytes - a.bytes)
        .slice(0, TOP);
      const rest = Math.max(0, used - top.reduce((n, x) => n + x.bytes, 0));

      const segs = top.map((s, k) => ({
        key: s.dir,
        name: label(s.dir),
        bytes: s.bytes,
        kind: "dir",
        w: `${86 - k * 11}%`,
      }));
      if (rest > 0)
        segs.push({
          key: "·rest",
          name: "Everything else",
          bytes: rest,
          kind: "other",
          w: "0%",
        });
      if (d.free > 0)
        segs.push({
          key: "·free",
          name: "Free",
          bytes: d.free,
          kind: "free",
          w: "0%",
        });

      return { ...d, used, segs };
    }),
  );

  const show =
    (root: string, name: string, b: number, total: number) => () =>
      (pick = {
        root,
        label: name,
        note: `${bytes(b)} · ${pct(total > 0 ? (b / total) * 100 : 0)} of disk`,
      });

</script>

{#if views.length > 0}
  <Sect {i} label="Storage" meta="largest {TOP} folders per disk">
    {#each views as v (v.root)}
      <div class="disk">
        <div class="top f al-ct g10">
          <span class="disk-tag fs-xs" data-root={v.root}>{v.root}</span>
          <span class="rule"></span>
          <span class="fs-xs tx-1 num">
            {bytes(v.used)} used · {bytes(v.free)} free of {bytes(v.total)}
          </span>
        </div>

        <div class="track f">
          {#each v.segs as s (s.key)}
            {@const at = show(v.root, s.name, s.bytes, v.total)}
            <button
              class="seg"
              class:other={s.kind === "other"}
              class:free={s.kind === "free"}
              style="flex-grow:{s.bytes}; --w:{s.w}"
              title={s.name}
              onpointerenter={at}
              onfocus={at}
            ></button>
          {/each}
        </div>

        <!-- One caption slot, always rendered, and it keeps the last band it
             was given: clearing on pointer-leave flickers it away on the move
             between two neighbouring bands, and a tap has no leave at all. -->
        <div class="cap fs-xs trunc">
          {#if pick && pick.root === v.root}
            <b class="tx-4 fw5">{pick.label}</b>
            <span class="tx-1 num">{pick.note}</span>
          {:else}
            <span class="tx-1">largest first</span>
          {/if}
        </div>
      </div>
    {/each}
  </Sect>
{/if}

<style>
  .disk + .disk {
    margin-top: 16px;
  }
  .top {
    padding-bottom: 6px;
  }
  .rule {
    flex: 1;
    min-width: 12px;
    height: 1px;
    background: var(--glass-bd);
  }

  .track {
    gap: 1px;
    height: 20px;
    border-radius: var(--r-sm);
    overflow: hidden;
    background: var(--bg-2);
  }

  /* One hue stepping darker left to right: rank is the message, and six
     arbitrary colours would read as six categories that do not exist. */
  .seg {
    flex-basis: 0;
    min-width: 5px;
    padding: 0;
    border-radius: 0;
    background: color-mix(in oklab, var(--cyan) var(--w, 50%), var(--bg-4));
    transition:
      flex-grow var(--motion-slow) var(--ease-out),
      filter var(--motion-fast) var(--ease-out);
  }
  .seg:hover,
  .seg:focus-visible {
    filter: brightness(1.3);
  }
  .seg.other {
    background: var(--bg-5);
  }
  .seg.free {
    background: repeating-linear-gradient(
      135deg,
      var(--glass) 0 5px,
      transparent 5px 10px
    );
  }

  .cap {
    padding-top: 6px;
    min-height: 18px;
  }

  @media (max-width: 640px) {
    .track {
      height: 24px;
    }
  }
</style>
