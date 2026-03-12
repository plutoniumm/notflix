<script lang="ts">
  import { onMount } from 'svelte'
  import videojs from 'video.js'
  import 'video.js/dist/video-js.css'
  import { cleanName, subPath, parseRaw, vidURL } from './lib/video'
  import { Tracker } from './lib/tracker'
  import { initSubs, searchSubs, startWhisper, reloadTrack } from './lib/subs'
  import { addHotkeys } from './lib/hotkeys'
  import SubsPicker from './SubsPicker.svelte'

  let { videoParam }: { videoParam: string } = $props()

  const { dir, name } = parseRaw(videoParam)
  const title = cleanName(name)
  const sub = subPath(videoParam)

  let videoEl = $state<HTMLVideoElement | undefined>(undefined)
  let player: any = null
  let subs = $state<any[] | null>(null)
  let wMsg = $state('')
  let nextURL = $state<string | null>(null)
  let rows = $state<[string, any[]][]>([])
  let paused = $state(true)
  let hideUI = $state(false)
  let timer: ReturnType<typeof setTimeout>

  const tracker = new Tracker()
  const autoplay = new URLSearchParams(window.location.search).get('autoplay') === '1'
  const embed = window.location.pathname === '/embed'

  function showUI() {
    hideUI = false
    clearTimeout(timer)
    if (!paused) timer = setTimeout(() => { hideUI = true }, 3000)
  }

  onMount(() => {
    document.title = `${title} | Notflix`

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

    player.on('play', () => { paused = false; showUI() })
    player.on('pause', () => { paused = true; hideUI = false; clearTimeout(timer) })

    player.ready(async () => {
      if (autoplay) player.play()
      const saved = tracker.get(videoParam)
      if (saved > 0) player.currentTime(saved)

      addHotkeys(
        player,
        () => { if (nextURL) window.location.href = nextURL },
        () => {
          const tracks = player.textTracks()
          for (let i = 0; i < tracks.length; i++) {
            const t = tracks[i]
            if ((t.kind === 'captions' || t.kind === 'subtitles') && t.mode === 'showing') {
              t.mode = 'hidden'
            }
          }
          runWhisper()
        },
      )

      setInterval(() => {
        const t = player.currentTime() ?? 0
        const d = player.duration() ?? 0
        tracker.set(videoParam, t)
        if (d - t < 5 * 60) tracker.del(videoParam)
      }, 2000)
    })

    initSubs(player, videoParam, sub)

    if (embed) {
      setInterval(async () => {
        const cmd = await fetch('/cmd').then(r => r.text()).catch(() => '')
        if (!cmd.trim()) return
        if (cmd === 'tog') {
          player.paused() ? player.play() : player.pause()
        } else if (cmd.startsWith('+')) {
          seek(parseFloat(cmd.slice(1)))
        } else if (cmd.startsWith('-')) {
          seek(-parseFloat(cmd.slice(1)))
        }
      }, 1000)
    }

    if (!embed) {
      fetch('/list/video').then(r => r.json()).then((data: VideoData) => {
        rows = Object.entries(data).filter(([, files]) => files?.length > 0)
        nextURL = nextVid(data)
      }).catch(() => {})
    }

    return () => { clearTimeout(timer); player?.dispose() }
  })

  function seek(n: number) {
    const t = player.currentTime() ?? 0
    const d = player.duration() ?? 0
    player.currentTime(Math.max(0, Math.min(d - 0.1, t + n)))
  }

  function nextVid(data: VideoData): string | null {
    const files = data[dir] ?? []
    const idx = files.findIndex(f => f.name === name)
    if (idx === -1 || idx === files.length - 1) return null
    return vidURL(dir, files[idx + 1].name, autoplay)
  }

  async function fetchSubs() {
    const results = await searchSubs(videoParam)
    if (!results) { alert('No subtitles found on OpenSubtitles.'); return }
    subs = results
  }

  async function selectSub(fid: number) {
    const res = await fetch('/api/subs/download', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ file_id: fid, file: videoParam }),
    }).then(r => r.json()).catch(() => ({ ok: false }))

    if (res.ok) {
      subs = null
      reloadTrack(player, `/subs/${sub}`, 'English')
    }
  }

  async function runWhisper() {
    await startWhisper(
      videoParam,
      (msg) => { wMsg = msg },
      () => {
        wMsg = ''
        reloadTrack(player, `/subs/${sub.replace(/\.vtt$/, '.whisper.vtt')}`, 'Whisper', true)
      },
    )
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="player-page"
  class:hide-ui={hideUI}
  class:embed
  onmousemove={showUI}
>
  {#if !embed}
    <div class="player-bar">
      <a href="/" class="back">← Back</a>
      <h1 class="title">{dir !== '.' ? `${dir} / ` : ''}{title}</h1>
      <div class="sub-controls">
        <button onclick={fetchSubs}>Fetch Subs</button>
        <button onclick={runWhisper}>Whisper</button>
      </div>
    </div>
  {/if}

  <div class="video-wrap">
    <video bind:this={videoEl} class="video-js vjs-default-skin vjs-big-play-centered"></video>
  </div>

  {#if wMsg}
    <div class="whisper-msg">{wMsg}</div>
  {/if}

  {#if !embed && rows.length > 0 && paused}
    <div class="related">
      {#each rows as [rowDir, files]}
        {#if rowDir === dir && files.length > 1}
          <h2>{rowDir === '.' ? 'More Movies' : rowDir}</h2>
          <div class="related-list">
            {#each files as f (f.key)}
              <a
                href={vidURL(rowDir, f.name)}
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

{#if subs}
  <SubsPicker
    results={subs}
    onSelect={selectSub}
    onClose={() => subs = null}
  />
{/if}

<style>
  .player-page {
    position: fixed;
    inset: 0;
    background: #000;
    overflow: hidden;
  }

  .player-page.hide-ui { cursor: none; }

  .video-wrap {
    position: absolute;
    inset: 0;
  }

  .player-bar {
    position: absolute;
    top: 0; left: 0; right: 0;
    z-index: 10;
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 20px 32px 48px;
    background: linear-gradient(to bottom, rgba(0,0,0,0.85) 0%, transparent 100%);
    transition: opacity 0.3s;
  }

  .player-page.hide-ui .player-bar {
    opacity: 0;
    pointer-events: none;
  }

  .back { color: #ddd; font-size: 13px; white-space: nowrap; flex-shrink: 0; transition: color 0.15s; }
  .back:hover { color: #fff; }

  .title {
    flex: 1; margin: 0; font-size: 1rem; font-weight: 500;
    color: #e5e5e5; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }

  .sub-controls { display: flex; gap: 8px; flex-shrink: 0; }

  .sub-controls button {
    background: rgba(255,255,255,0.1);
    border: 1px solid rgba(255,255,255,0.3);
    color: #ccc; padding: 6px 14px; border-radius: 3px; font-size: 13px;
    transition: background 0.15s, color 0.15s;
  }
  .sub-controls button:hover { background: rgba(255,255,255,0.2); color: #fff; }

  .whisper-msg {
    position: absolute; bottom: 80px; left: 32px; z-index: 10;
    font-size: 13px; color: #e50914;
    background: rgba(0,0,0,0.6); padding: 6px 12px; border-radius: 4px;
  }

  .related {
    position: absolute; bottom: 0; left: 0; right: 0; z-index: 10;
    padding: 16px 32px 24px;
    background: linear-gradient(to top, rgba(0,0,0,0.9) 0%, transparent 100%);
  }

  .related h2 { font-size: 0.9rem; color: #aaa; margin: 0 0 10px; font-weight: 400; }

  .related-list {
    display: flex; gap: 10px; overflow-x: auto;
    padding-bottom: 4px; scrollbar-width: none;
  }
  .related-list::-webkit-scrollbar { display: none; }

  .related-item {
    flex-shrink: 0; width: 140px; border-radius: 3px; overflow: hidden;
    border: 2px solid transparent; transition: border-color 0.15s, transform 0.15s;
  }
  .related-item:hover { transform: scale(1.04); }
  .related-item.active { border-color: #e50914; }

  .related-item img {
    width: 100%; aspect-ratio: 16/9; object-fit: cover; display: block; background: #222;
  }

  .related-item span {
    display: block; font-size: 11px; color: #aaa; padding: 4px 4px 6px;
    background: #1a1a1a; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .related-item.active span { color: #fff; }
</style>
