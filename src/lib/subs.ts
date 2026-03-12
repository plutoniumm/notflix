export async function initSubtitles(player: any, raw: string, sub: string) {
  const info = await fetch(`/api/subs/info?file=${encodeURIComponent(raw)}`)
    .then(r => r.json())
    .catch(() => null)
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

export async function fetchSubResults(raw: string): Promise<any[] | null> {
  const res = await fetch(`/api/subs/search?file=${encodeURIComponent(raw)}`)
    .then(r => r.json())
    .catch(() => ({ results: [] }))
  return res.results?.length ? res.results : null
}

export async function startWhisper(
  raw: string,
  onStatus: (s: string) => void,
  onDone: () => void,
) {
  onStatus('Generating subtitles with Whisper…')
  await fetch('/api/subs/whisper', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file: raw }),
  })

  const poll = setInterval(async () => {
    const s = await fetch(`/api/subs/whisper/status?file=${encodeURIComponent(raw)}`)
      .then(r => r.json())
      .catch(() => null)
    if (!s) return
    if (s.status === 'done') {
      clearInterval(poll)
      onDone()
    } else if (s.status === 'error') {
      clearInterval(poll)
      onStatus('Whisper failed: ' + (s.error || 'unknown'))
    }
  }, 3000)
}

export function reloadTrack(player: any, src: string, label: string) {
  const tracks = player.remoteTextTracks()
  for (let i = tracks.length - 1; i >= 0; i--) {
    if (tracks[i].label === label) player.removeRemoteTextTrack(tracks[i])
  }
  player.addRemoteTextTrack(
    { kind: 'captions', src, srclang: 'en', label, default: label === 'English' },
    false,
  )
}
