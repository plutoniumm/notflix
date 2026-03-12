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
