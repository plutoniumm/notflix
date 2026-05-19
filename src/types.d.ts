interface VideoEntry {
  name: string;
  key: string;
}

type DiskInfo = {
  root: string;
  path: string;
  free: number;
  total: number;
};

type Downjob = {
  gid: string;
  name: string;
  status: string;
  total: number;
  done: number;
  percent: number;
  speed: number;
};

type Torrent = {
  name: string;
  magnet: string;
  infoHash: string;
  size: number;
  seeders: number;
  leechers: number;
  files: number;
  added: number;
  category: string;
  status: string;
};

interface VideoGroup {
  dir: string;
  files: VideoEntry[];
}

type VideoData = VideoGroup[];

interface Job {
  name: string;
  percent: number;
}

interface DownloadRecord {
  videoParam: string;
  title: string;
  key: string;
  size: number;
  status: "downloading" | "done" | "error";
  downloadedAt: number | null;
  bgFetchId: string | null;
}
