<div align="center">
<img src="./public/assets/tight.svg" height="150" alt="Notflix" />
<hr/>
</div>

Self-hosted Netflix-like video streaming. Go backend, Svelte 5 frontend.

## Features

**Streaming**
- Adaptive bitrate HLS (144p–2160p) — auto-selects quality based on network speed
- Direct MP4 range serving for offline-downloaded files
- 4-second segments cached to disk, deduplicated on concurrent requests

**Library Management**
- Watches configured video root directories
- Auto-converts MKV, MOV, AVI, WMV, FLV, and other formats to MP4 on startup
  - Copies h264/HEVC streams directly; re-encodes everything else
  - Max 3 concurrent conversions; real-time progress in the UI
- Auto-flattens library: moves lone videos out of subdirectories, removes junk files
- Thumbnail generation via ffprobe/ffmpeg (lazy, sampled at video midpoint)

**Subtitles** (waterfall, tries in order)
1. Local `.vtt` file
2. Local `.srt` file (auto-converted)
3. Embedded subtitle track (extracted via ffmpeg)
4. OpenSubtitles API (hash match, then title search)
5. Whisper.cpp AI transcription (async, streams cues live via SSE)

**Offline Downloads**
- BackgroundFetch API — resumable downloads that survive closing the app
- Stores video + subtitles in CacheStorage; metadata in IndexedDB
- Downloaded videos play locally without streaming

**Torrent/Magnet Downloads**
- Add magnets or torrents from the Manage panel
- Powered by aria2c (must be running separately)
- Live download progress with speed and completion percentage

**Player**
- Video.js with HLS adaptive streaming
- Watch progress saved to localStorage; resumes on return
- Next video auto-play with 1MB prefetch
- Related videos sidebar on pause
- Picture-in-picture

**Keyboard Shortcuts**

| Key | Action |
| --- | --- |
| `Space` | play / pause |
| `←` / `→` | seek ±5s |
| `Shift + ←/→` | seek ±30s |
| `0`–`9` | jump to 0%–90% |
| `f` | fullscreen |
| `p` | picture-in-picture |
| `m` | mute |
| `n` | next video |
| `d` / `s` | speed +0.1 / −0.1 |
| `c` | toggle subtitles |
| `w` | generate Whisper subtitles |
| `b` | brightness toggle |

**Touch Gestures**
- Double-tap left third: −10s
- Double-tap right third: +10s
- Double-tap center: play/pause

## Requirements

- **ffmpeg** and **ffprobe** — transcoding and thumbnails
- **aria2c** (optional) — torrent/magnet downloads
- **whisper.cpp** (optional) — AI subtitle generation; set `WHISPER_MODEL` env var

**Environment variables**
```
WHISPER_MODEL=/path/to/model.bin
OPENSUBTITLES_API_KEY=...
OPENSUBTITLES_USER=...
OPENSUBTITLES_PASS=...
```

## Build & Run

```sh
make run
```

> Uses `-tags whisper` and CGO flags for whisper.cpp. Don't use `go build` directly.

## License

MIT 2023 plutoniumm
