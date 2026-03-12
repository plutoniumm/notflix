<script lang="ts">
  import { onMount } from 'svelte'
  import videojs from 'video.js'
  import 'video.js/dist/video-js.css'
  import { cleanName, subPath, parseRaw, videoUrl, type VideoData } from './lib/video'
  import { Tracker } from './lib/tracker'
  import { initSubtitles, fetchSubResults, startWhisper, reloadTrack } from './lib/subs'
  import { addHotkeys } from './lib/hotkeys'
  import SubsPicker from './SubsPicker.svelte'

  let { videoParam }: { videoParam: string } = $props()

  const { dir, name } = parseRaw(videoParam)
  const displayName = cleanName(name)
  const sub = subPath(videoParam)

  let videoEl = $state<HTMLVideoElement | undefined>(undefined)
  let player: any = null
  let subsResults = $state<any[] | null>(null)
  let whisperMsg = $state('')
  let nextUrl = $state<string | null>(null)
  let rows = $state<[string, any[]][]>([])
  let paused = $state(true)
  let hideUi = $state(false)
  let uiTimer: ReturnType<typeof setTimeout>

  const tracker = new Tracker()
  const autoplay = new URLSearchParams(window.location.search).get('autoplay') === '1'
  const isEmbed = window.location.pathname === '/embed'

  function showUi() {
    hideUi = false
    clearTimeout(uiTimer)
    if (!paused) uiTimer = setTimeout(() => { hideUi = true }, 3000)
  }

  onMount(() => {
    document.title = `${displayName} | Notflix`

    player = videojs(videoEl!, {
      controls: true,
      preload: 'auto',
      fill: true,
      poster: '/assets/home.png',
      playbackRates: [0.5, 1, 1.25, 1.5, 2],
    })

    player.src({ src: `/video/${videoParam}`, type: 'video/mp4' })

    player.addRemoteTextTrack(
      { kind: 'captions', src: `/subs/${sub}`, srclang: 'en', label: 'English', default: true },
      false,
    )

    player.on('play', () => {
      paused = false
      showUi()
    })
    player.on('pause', () => {
      paused = true
      hideUi = false
      clearTimeout(uiTimer)
    })

    player.ready(async () => {
      if (autoplay) player.play()
      const saved = tracker.get(videoParam)
      if (saved > 0) player.currentTime(saved)

      addHotkeys(
        player,
        () => { if (nextUrl) window.location.href = nextUrl },
        () => {
          // Disable any active subtitle track before whisper starts
          const tracks = player.textTracks()
          for (let i = 0; i < tracks.length; i++) {
            const t = tracks[i]
            if ((t.kind === 'captions' || t.kind === 'subtitles') && t.mode === 'showing') {
              t.mode = 'hidden'
            }
          }
          handleWhisper()
        },
      )

      setInterval(() => {
        const t = player.currentTime() ?? 0
        const d = player.duration() ?? 0
        tracker.set(videoParam, t)
        if (d - t < 5 * 60) tracker.del(videoParam)
      }, 2000)
    })

    initSubtitles(player, videoParam, sub)

    if (isEmbed) {
      setInterval(async () => {
        const cmd = await fetch('/cmd').then(r => r.text()).catch(() => '')
        if (!cmd.trim()) return
        if (cmd === 'tog') {
          player.paused() ? player.play() : player.pause()
        } else if (cmd.startsWith('+')) {
          seekBy(parseFloat(cmd.slice(1)))
        } else if (cmd.startsWith('-')) {
          seekBy(-parseFloat(cmd.slice(1)))
        }
      }, 1000)
    }

    if (!isEmbed) {
      fetch('/list/video').then(r => r.json()).then((data: VideoData) => {
        rows = Object.entries(data).filter(([, files]) => files?.length > 0)
        nextUrl = getNext(data)
      }).catch(() => {})
    }

    return () => {
      clearTimeout(uiTimer)
      player?.dispose()
    }
  })

  function seekBy(n: number) {
    const t = player.currentTime() ?? 0
    const d = player.duration() ?? 0
    player.currentTime(Math.max(0, Math.min(d - 0.1, t + n)))
  }

  function getNext(data: VideoData): string | null {
    const files = data[dir] ?? []
    const idx = files.findIndex(f => f.name === name)
    if (idx === -1 || idx === files.length - 1) return null
    const next = files[idx + 1]
    return videoUrl(dir, next.name, autoplay)
  }

  async function handleFetchSubs() {
    const results = await fetchSubResults(videoParam)
    if (!results) { alert('No subtitles found on OpenSubtitles.'); return }
    subsResults = results
  }

  async function handleSubSelect(fileId: number) {
    const res = await fetch('/api/subs/download', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ file_id: fileId, file: videoParam }),
    }).then(r => r.json()).catch(() => ({ ok: false }))

    if (res.ok) {
      subsResults = null
      reloadTrack(player, `/subs/${sub}`, 'English')
    }
  }

  async function handleWhisper() {
    await startWhisper(
      videoParam,
      (msg) => { whisperMsg = msg },
      () => {
        whisperMsg = ''
        reloadTrack(player, `/subs/${sub.replace(/\.vtt$/, '.whisper.vtt')}`, 'Whisper')
      },
    )
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="player-page"
  class:playing={!paused}
  class:hide-ui={hideUi}
  class:embed={isEmbed}
  onmousemove={showUi}
>
  {#if !isEmbed}
    <div class="player-bar">
      <a href="/" class="back">← Back</a>
      <h1 class="title">{dir !== '.' ? `${dir} / ` : ''}{displayName}</h1>
      <div class="sub-controls">
        <button onclick={handleFetchSubs}>Fetch Subs</button>
        <button onclick={handleWhisper}>Whisper</button>
        {#if nextUrl}
          <a href={nextUrl} class="next-btn">Next →</a>
        {/if}
      </div>
    </div>
  {/if}

  <div class="video-wrap">
    <video bind:this={videoEl} class="video-js vjs-default-skin vjs-big-play-centered"></video>
  </div>

  {#if whisperMsg}
    <div class="whisper-msg">{whisperMsg}</div>
  {/if}

  {#if !isEmbed && rows.length > 0 && paused}
    <div class="related">
      {#each rows as [rowDir, files]}
        {#if rowDir === dir && files.length > 1}
          <h2>{rowDir === '.' ? 'More Movies' : rowDir}</h2>
          <div class="related-list">
            {#each files as f (f.key)}
              <a
                href={videoUrl(rowDir, f.name)}
                class="related-item"
                class:active={f.name === name}
              >
                <img src="/images/{f.key}.jpg" alt="" loading="lazy" />
                <span>{cleanName(f.name)}</span>
              </a>
            {/each}
          </div>
        {/if}
      {/each}
    </div>
  {/if}
</div>

{#if subsResults}
  <SubsPicker
    results={subsResults}
    onSelect={handleSubSelect}
    onClose={() => subsResults = null}
  />
{/if}

<style>
  .player-page {
    background: #000;
    min-height: 100vh;
  }

  /* When playing: fill entire viewport */
  .player-page.playing {
    position: fixed;
    inset: 0;
    overflow: hidden;
    z-index: 50;
  }

  .player-page.playing .video-wrap {
    position: absolute;
    inset: 0;
  }

  /* Hide cursor when playing and idle */
  .player-page.playing.hide-ui {
    cursor: none;
  }

  /* Player bar */
  .player-bar {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 14px 32px;
    background: #0a0a0a;
    border-bottom: 1px solid #222;
  }

  /* When playing, overlay bar at top with gradient */
  .player-page.playing .player-bar {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    z-index: 10;
    background: linear-gradient(to bottom, rgba(0,0,0,0.85) 0%, transparent 100%);
    border-bottom: none;
    padding: 20px 32px 40px;
    transition: opacity 0.3s;
  }

  .player-page.playing.hide-ui .player-bar {
    opacity: 0;
    pointer-events: none;
  }

  .back {
    color: #aaa;
    font-size: 13px;
    white-space: nowrap;
    flex-shrink: 0;
    transition: color 0.15s;
  }
  .back:hover { color: #fff; }

  .title {
    flex: 1;
    margin: 0;
    font-size: 1rem;
    font-weight: 500;
    color: #e5e5e5;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub-controls {
    display: flex;
    gap: 8px;
    flex-shrink: 0;
  }

  .sub-controls button, .next-btn {
    background: rgba(255,255,255,0.1);
    border: 1px solid #444;
    color: #ccc;
    padding: 6px 14px;
    border-radius: 3px;
    font-size: 13px;
    transition: background 0.15s, color 0.15s;
  }
  .sub-controls button:hover, .next-btn:hover {
    background: rgba(255,255,255,0.2);
    color: #fff;
  }

  /* Video wrap — default state */
  .video-wrap {
    width: 100%;
    aspect-ratio: 16/9;
    background: #000;
  }

  .whisper-msg {
    padding: 10px 32px;
    font-size: 13px;
    color: #e50914;
  }

  .related { padding: 32px; }
  .related h2 {
    font-size: 1rem;
    color: #aaa;
    margin: 0 0 16px;
    font-weight: 400;
  }

  .related-list {
    display: flex;
    gap: 10px;
    overflow-x: auto;
    padding-bottom: 12px;
    scrollbar-width: thin;
  }

  .related-item {
    flex-shrink: 0;
    width: 160px;
    border-radius: 3px;
    overflow: hidden;
    border: 2px solid transparent;
    transition: border-color 0.15s, transform 0.15s;
  }
  .related-item:hover { transform: scale(1.04); }
  .related-item.active { border-color: #e50914; }

  .related-item img {
    width: 100%;
    aspect-ratio: 16/9;
    object-fit: cover;
    display: block;
    background: #222;
  }

  .related-item span {
    display: block;
    font-size: 11px;
    color: #aaa;
    padding: 4px 4px 6px;
    background: #1a1a1a;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .related-item.active span { color: #fff; }
</style>
