const REMOVES = [
  "nflx",
  "amzn",
  "hdtv",
  "hdrip",
  "bluray",
  "web-dl",
  "webrip",
  "web",
  "hevc-psa",
  "webdl",
  "eac3",
  "avi",
  "hdr",
  "mp4",
  "mkv",
  "dvdrip",
  "repack",
  "split",
  "scenes",
  "rq",
  "10bit",
  "atmos",
  "ddp5",
  "dd5",
  "ac3",
  "2025",
  "x264",
  "x265",
  "h264",
  "h265",
  "720p",
  "480p",
  "1080p",
  "2160p",
  "4k",
  "yify",
  "evo",
  "xvid",
  "rarbg",
  "hbo",
  "max",
  "hvec",
  "hevc",
  "bone",
];

const REMOVES_RE = REMOVES.map((r) => new RegExp(`\\b${r}\\b`, "gi"));
const MP4_RE = /\.mp4$/i;
const AAC_RE = /\baac\d?\b/gi;
const BRACKETS_RE = /\[.*?\]/g;
const PARENS_RE = /\(.*?\)/g;
const WS_RE = /\s+/g;

export function clean(name: string): string {
  let s = name.replace(MP4_RE, "");

  for (const re of REMOVES_RE) {
    re.lastIndex = 0;
    s = s.replace(re, "");
  }
  s = s.replace(AAC_RE, "");

  return s
    .replace(BRACKETS_RE, "")
    .replace(PARENS_RE, "")
    .replaceAll("_", " ")
    .replaceAll("-", " ")
    .replaceAll(".", " ")
    .replace(WS_RE, " ")
    .trim();
}

export function nextVid(
  data: VideoData,
  dir: string,
  name: string,
  autoplay: boolean,
): string | null {
  const files = data[dir] ?? [];
  const idx = files.findIndex((f) => f.name === name);
  if (idx === -1 || idx === files.length - 1) return null;

  return vidURL(dir, files[idx + 1].name, autoplay);
}

export function vidURL(dir: string, name: string, autoplay = false): string {
  const raw = dir === "." ? name : `${dir}/${name}`;

  return `/?video=${encodeURIComponent(raw)}` + (autoplay ? "&autoplay=1" : "");
}

export function parseRaw(raw: string): { dir: string; name: string } {
  const i = raw.lastIndexOf("/");

  if (i === -1)
    return {
      dir: ".",
      name: raw,
    };

  return {
    dir: raw.slice(0, i),
    name: raw.slice(i + 1),
  };
}
