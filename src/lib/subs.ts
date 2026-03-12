import { GET } from '.';

const Enc = encodeURIComponent

export async function initSubs (player: any, raw: string, sub: string) {
  const info = await GET(`/api/subs/info?file=${Enc(raw)}`)

  if (!info || info.vtt) return

  if (info.srt) {
    reloadTrack(player, `/subs/${sub}`, 'English')
    return
  }

  if (info.embedded?.length > 0) {
    const eng =
      info.embedded.find((t: any) => ['en', 'eng'].includes(t.language)) ??
      info.embedded[0]
    await fetch('/api/subs/extract', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ file: raw, track: eng.index }),
    })

    reloadTrack(player, `/subs/${sub}`, 'English')
  }
}

export async function searchSubs (raw: string): Promise<any[] | null> {
  const res = await GET(`/api/subs/search?file=${Enc(raw)}`)

  return res?.results?.length ? res.results : null
}

export async function startWhisper (
  raw: string,
  onMsg: (s: string) => void,
  onDone: () => void,
) {
  onMsg('Generating subtitles with Whisper…')
  await fetch('/api/subs/whisper', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file: raw }),
  })

  const timer = setInterval(async () => {
    const s = await GET(`/api/subs/whisper/status?file=${Enc(raw)}`)
    if (!s) return

    if (s.status === 'done') {
      clearInterval(timer)
      onDone()
    } else if (s.status === 'error') {
      clearInterval(timer)
      onMsg('Whisper failed: ' + (s.error || 'unknown'))
    }
  }, 3000)
}

export function reloadTrack (
  player: any,
  src: string,
  label: string,
  show = label === 'English'
) {
  const tracks = player.remoteTextTracks()

  for (let i = tracks.length - 1; i >= 0; i--) {
    if (tracks[i].label === label)
      player.removeRemoteTextTrack(tracks[i])
  }

  player.addRemoteTextTrack({
    kind: 'captions',
    src,
    srclang: 'en',
    label,
    default: show
  }, false)

  if (show) {
    setTimeout(() => {
      const all = player.textTracks()
      for (let i = 0; i < all.length; i++) {
        if (all[i].label === label)
          all[i].mode = 'showing'
      }
    }, 100)
  }
}
