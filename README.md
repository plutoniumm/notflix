<div align="center">
<img src="./public/assets/tight.svg" height="150" alt="Notflix" />
<hr/>
</div>

Self-hosted Netflix-like video streaming. Go backend, Svelte 5 frontend.

## Controls

Every one of these is also reachable by hand — the player renders the whole
table behind `?` (and the `?` button in the top bar), so nothing below is
discoverable only by reading this file. The table itself is generated from
`KEYS` in `src/player/ux.ts`; `tools/parity.test.ts` fails if the two drift.

<!-- keys -->
### Playback

| Key | Action |
| --- | --- |
| `Space` | play / pause |
| `← / →` | seek 5s |
| `Shift ← / →` | seek 30s |
| `Alt ← / →` | nudge 0.1s |
| `0 – 9` | jump to 0% – 90% |
| `x` | skip intro / credits |
| `n` | next episode |
| `d / s` | speed +0.1 / −0.1 |

### Sound

| Key | Action |
| --- | --- |
| `↑ / ↓` | volume ±10% |
| `m` | mute |
| `[ / ]` | audio delay −50ms / +50ms |

### Picture

| Key | Action |
| --- | --- |
| `f` | fullscreen |
| `p` | picture in picture |
| `c` | subtitles on / off |
| `w` | transcribe with Whisper |

### Panels

| Key | Action |
| --- | --- |
| `v` | tune — picture, audio delay, speed |
| `t` | up next |
| `?` | this help |
| `Esc` | close whatever is open |
| `l` | back to the library |

### Library

| Key | Action |
| --- | --- |
| `/` | focus search |
| `Enter` | search torrents for what you typed |
| `Esc` | clear the search |
| `?` | this help |

### Pointer

| Key | Action |
| --- | --- |
| `Click` | play / pause |
| `Double-click` | fullscreen |
| `Tap` | play / pause |
| `Double-tap left third / right third` | seek −10s / +10s |
| `Double-tap centre` | fullscreen |
| `Scrub track` | drag to scrub; focus it for ← → Home End PageUp PageDown |
<!-- /keys -->

## Requirements

- **ffmpeg** and **ffprobe** — transcoding, thumbnails, sprites, subtitle extraction
- **aria2c** (optional) — torrent/magnet downloads via JSON-RPC
- **yt-dlp** (optional) — non-torrent URL downloads
- **faster-whisper** (optional) — AI subtitle generation; Python in conda env `global`
- **ollama** (optional) — local LLM for Whisper translation, blurbs, poster picking

**Environment variables** (`.env`)
```
OPENSUBTITLES_API_KEY=...
OPENSUBTITLES_USER=...
OPENSUBTITLES_PASS=...
SUBDL_API_KEY=...            # optional fallback provider
OLLAMA_HOST=http://...       # defaults to the author's tailnet box
OLLAMA_MODEL=aya-expanse:8b  # translation, blurbs
POSTER_VISION_MODEL=...      # vision model for poster picking; POSTER_VISION=0 disables
```

## Build & Run

```sh
make run
```

> `make` handles CGO flags and build tags. Don't invoke `go build` directly.

```sh
go test ./...               # backend
node --test tools/*.test.ts # frontend (build first — some assert on the bundle)
```

## License
This was once hand written. Its now mostly vibe written.

MIT 2026 plutoniumm
