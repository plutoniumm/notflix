interface VideoEntry {
  name: string
  key: string
}

type VideoData = Record<string, VideoEntry[]>

interface Job {
  name: string
  percent: number
}

interface DiskInfo {
  root: string
  free: number
  total: number
}

interface DownloadRecord {
  videoParam: string
  title: string
  key: string
  size: number
  status: 'downloading' | 'done' | 'error'
  downloadedAt: number | null
  bgFetchId: string | null
}
