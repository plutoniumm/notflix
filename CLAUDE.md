# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```sh
make run          # build (Go + frontend) and start server on :4242
make build        # just compile
pnpm run dev      # frontend watch mode (Vite)
pnpm run build    # frontend production build → public/assets/
go build -o notflix .   # Go binary only
```

`make run` is the canonical way to build and start the server. The Makefile cleans, compiles Go, builds the Svelte frontend, then runs the binary.

There are no tests in this project.

## Architecture

**Go backend (Gin)** serves both the API and the static frontend assets from `public/`.

- `main.go` — entry point, all route definitions, port 4242, video root dirs (`/Volumes/Ravan`, `/Volumes/Oni`, `/Volumes/Kumbhakarn`)
- `server/*.go` — handler logic, each file owns a domain (hls, subs, convert, aria2, whisper, etc.)

**Svelte 5 frontend** compiled by Vite into `public/assets/`. Entry: `src/main.ts` → `src/App.svelte`.

- Routing is hash-based in `App.svelte` (Home / Player / Manage)
- `src/lib/events.svelte.ts` — central reactive player state (`PlayerState` class with Svelte 5 `$state`)
- `src/lib/ux.ts` — keyboard shortcuts and touch gestures, bound to the player
- `src/lib/subs.ts` — subtitle waterfall logic (local → embedded → OpenSubtitles → Whisper)
- `src/player/*.svelte` — composable player UI pieces (bar, progress, volume, sync, whisper, etc.)

## Key Patterns

**Video streaming** has two paths:
1. Direct MP4 → HTTP 206 range requests (`server/player.go`)
2. HLS adaptive → master playlist with quality levels 144p–2160p, 4s MPEG-TS segments generated on-demand by ffmpeg, cached to `./cache/` (`server/hls.go`)

**Background conversion** (`server/convert.go`): non-MP4 formats auto-convert on startup. Max 3 concurrent ffmpeg processes. Progress parsed from ffmpeg stderr via regex.

**Subtitle waterfall** (`server/subs.go` + `src/lib/subs.ts`): tries local VTT → SRT → embedded extraction → OpenSubtitles API (hash, then title) → Whisper transcription. Each step is attempted only if prior steps fail.

**Whisper** (`server/whisper.go` + `tools/stream_whisper.py`): async transcription via faster-whisper Python subprocess. SSE streams cues to the frontend in real-time. Python runs in conda env `global`.

**Reactive state**: `PlayerState` in `events.svelte.ts` is the single source of truth for all player UI — current time, subtitles, volume, UI visibility, etc. Components read from it; mutations happen through its methods.

## External Dependencies

- **ffmpeg / ffprobe** — required for HLS segments, conversion, thumbnails, subtitle extraction
- **aria2c** — optional, torrent/magnet downloads via JSON-RPC on localhost:6800
- **faster-whisper** — optional, Python package in conda `global` env for AI subtitle generation

## Environment Variables (`.env`)

- `WHISPER_MODEL` — path to whisper model file
- `OPENSUBTITLES_API_KEY`, `OPENSUBTITLES_USER`, `OPENSUBTITLES_PASS` — OpenSubtitles API credentials
