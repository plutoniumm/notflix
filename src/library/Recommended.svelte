<script lang="ts">
  import { onMount } from "svelte";
  import { GET } from "../core/api";
  import { join } from "../core/path";
  import { usePoll } from "../core/poll";
  import MediaCard from "../components/MediaCard.svelte";
  import ScrollRow from "../components/ScrollRow.svelte";

  type Rec = {
    dir: string;
    name: string;
    key: string;
    title: string;
    score: number;
    because: string;
    reason?: string;
  };

  let { downloadedSet = new Set<string>() }: { downloadedSet?: Set<string> } =
    $props();

  let items = $state<Rec[]>([]);

  // The first request finds nothing: it is what starts the embedding pass.
  // Poll slowly until the row fills, then stop — and stop regardless after a
  // few tries, because "no recommendations" is a legitimate resting state
  // (no ollama host, no finished titles) and not something to keep asking about.
  const RETRY_MS = 6000;
  const RETRIES = 5;

  onMount(() => {
    let tries = 0;
    let done = false;

    const stop = usePoll(
      async () => {
        if (done) return;

        const r = await GET("/api/reco", { silent: true });
        const got: Rec[] = Array.isArray(r?.items) ? r.items : [];

        if (got.length) items = got;

        if (got.length || ++tries >= RETRIES || !r?.building) {
          done = true;
          stop();
        }
      },
      RETRY_MS,
      { immediate: true },
    );

    return () => {
      done = true;
      stop();
    };
  });
</script>

{#if items.length > 0}
  <section class="row">
    <div class="head f al-ct g10">
      <h2 class="m0">Because you watched</h2>
      <span class="count fs-xs tx-1">{items.length}</span>
    </div>

    <ScrollRow>
      <div class="cards spotlight f flow-x-s g10">
        {#each items as item, idx (join(item.dir, item.name))}
          <MediaCard
            dir={item.dir}
            name={item.name}
            key={item.key}
            {idx}
            downloaded={downloadedSet.has(join(item.dir, item.name))}
          >
            {#snippet footer()}
              <div class="why fs-xs">
                <span class="src trunc">{item.because}</span>
                {#if item.reason}
                  <span class="line trunc">{item.reason}</span>
                {/if}
              </div>
            {/snippet}
          </MediaCard>
        {/each}
      </div>
    </ScrollRow>
  </section>
{/if}

<style>
  .row {
    margin-bottom: 40px;
  }
  .head {
    padding: 0 48px;
    margin-bottom: 14px;
  }
  h2 {
    color: var(--tx-5);
    font-size: 22px;
    font-weight: 600;
  }
  .count {
    font-variant-numeric: tabular-nums;
  }
  .head::after {
    content: "";
    flex: 1;
    height: 1px;
    background: linear-gradient(to right, var(--glass-bd), transparent);
  }

  .cards {
    padding: 4px 48px 12px;
    scrollbar-width: none;
  }
  .cards :global(.tile) {
    width: 190px;
  }

  .why {
    padding-top: 2px;
    min-width: 0;
  }
  .src {
    display: block;
    color: var(--tx-2);
  }
  .src::before {
    content: "↳ ";
    color: var(--tx-1);
  }
  .line {
    display: block;
    color: var(--tx-1);
  }

  @media (max-width: 640px) {
    .head {
      padding: 0 16px;
      margin-bottom: 8px;
    }
    h2 {
      font-size: 17px;
    }
    .cards {
      padding: 4px 16px 12px;
    }
    .cards :global(.tile) {
      width: 148px;
    }
  }
</style>
