const REMOVES = [
  'nflx','amzn','hevc','hdtv','hdrip','bluray','web-dl','webrip','web',
  'hevc-psa','webdl','eac3','avi','hdr','mp4','mkv','dvdrip','repack',
  'split','scenes','rq','aac','10bit','atmos','ddp5','dd5','ac3','2025',
  'x264','x265','h264','h265','720p','480p','1080p','2160p','4k',
]

export function cleanName(name: string): string {
  let s = name.replace(/\.mp4$/i, '')
  for (const r of REMOVES) {
    s = s.replace(new RegExp(`\\b${r}\\b`, 'gi'), '')
  }
  return s.replaceAll('-', ' ').replaceAll('.', ' ').replace(/\s+/g, ' ').trim()
}

export function vidURL(dir: string, name: string, autoplay = false): string {
  const raw = dir === '.' ? name : `${dir}/${name}`
  return `/?video=${encodeURIComponent(raw)}` + (autoplay ? '&autoplay=1' : '')
}

export function subPath(raw: string): string {
  return raw.replace(/\.mp4$/i, '.vtt')
}

export function parseRaw(raw: string): { dir: string; name: string } {
  const i = raw.lastIndexOf('/')
  if (i === -1) return { dir: '.', name: raw }
  return { dir: raw.slice(0, i), name: raw.slice(i + 1) }
}
