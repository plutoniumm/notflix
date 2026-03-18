interface VideoEntry {
  name: string
  key: string
}

type DiskInfo = {
  root: string;
  path: string;
  free: number;
  total: number
};

type Downjob = {
  gid: string;
  name: string;
  status: string;
  total: number;
  done: number;
  percent: number;
  speed: number
};

type VideoData = Record<string, VideoEntry[]>

interface Job {
  name: string
  percent: number
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
