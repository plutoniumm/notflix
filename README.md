<div align="center">
<img src="./public/assets/tight.svg" height="150" alt="Notflix" />
<hr/>
</div>

Self-hosted Netflix-like video streaming. Go backend, Svelte 5 frontend.


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

- **ffmpeg** and **ffprobe** — transcoding, thumbnails, subtitle extraction
- **aria2c** (optional) — torrent/magnet downloads via JSON-RPC
- **faster-whisper** (optional) — AI subtitle generation; Python in conda env `global`
- **ollama** (optional) — local LLM for non-English Whisper translation

**Environment variables** (`.env`)
```
WHISPER_MODEL=/path/to/model.bin
OPENSUBTITLES_API_KEY=...
OPENSUBTITLES_USER=...
OPENSUBTITLES_PASS=...
SUBDL_API_KEY=...          # optional fallback provider
OLLAMA_HOST=http://...     # defaults to the author's tailnet box
OLLAMA_MODEL=qwen2.5:7b    # model used for translation
```

## Build & Run

```sh
make run
```

> `make` handles CGO flags and build tags. Don't invoke `go build` directly.

## License
This was once hand written. Its now mostly vibe written.

MIT 2026 plutoniumm
