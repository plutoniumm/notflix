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

**Go backend (Gin)** serves both the API and the static frontend assets from `public/`. The backend lives in three sub-packages, each owning a domain:

- `server/library/` — roots, file discovery, hash, hidden dirs, cleanup, KV store. Owns `Library{Roots}` type (the receiver for everything that previously took `roots []string`).
- `server/media/` — streaming + transcoding + cache: HLS, MP4 range serving, audio/video probe, conversion, subtitles, thumbnails, HLS cache eviction.
- `server/jobs/` — background work: aria2 RPC, whisper subprocess, ollama translation, shared `Pool` + `Tracker` primitives.

`main.go` is thin (~130 lines): creates `lib = &library.Library{Roots: ...}`, wires startup goroutines (`media.ProcessAll`, `jobs.Aria2Init`, `media.StartCacheCleanLoop`), and declares the route table pointing at handler functions in the three packages. Inline closures have been moved into `library/handlers.go` and `media/handlers.go`.

Package dependencies: `library` is standalone. `jobs` imports `library` (for `FindVid`). `media` imports both (`library` for file ops, `jobs` for `Aria2ActivePaths`). `jobs` uses a `jobs.OnDownloads func(*library.Library)` callback (wired in `main.go` to `media.ProcessAll`) to break the aria2→process cycle.

**Svelte 5 frontend** compiled by Vite into `public/assets/`. Entry: `src/main.ts` → `src/App.svelte`. The tree mirrors the backend:

- `src/core/` — shared primitives: `api.ts` (raw `GET/POST/DEL` + typed `api.*` namespace), `kv.ts` (typed KV helpers), `events.svelte.ts` (`PlayerState` class + timing constants), `idb.ts`, `clickOutside.ts`, `video.ts` utilities.
- `src/library/` — Home, Manage, and their components (FileRow, FolderRow, Header, Magnet, Downloads) plus `dl.ts` (BackgroundFetch + IndexedDB download manager).
- `src/player/` — Player, Subs, AudioPicker, Dropdown, all player subcomponents (bar, progress, volume, sync, whisper, related, Download), plus `subs.ts`, `ux.ts`, `avsync.ts`, `tracker.ts`, `view.svelte.ts` (`PlayerView` composing `PlayerState + SubsManager + AudioManager`).

Routing is hash-based in `App.svelte` (Home / Player / Manage).

## Key Patterns

**Video streaming** has two paths:
1. Direct MP4 → HTTP 206 range requests (`server/media/player.go`)
2. HLS adaptive → master playlist with quality levels 144p–2160p, 4s MPEG-TS segments generated on-demand by ffmpeg, cached to `./cache/` (`server/media/hls.go`)

**Background conversion** (`server/media/convert.go`): non-MP4 formats auto-convert on startup. Max 3 concurrent ffmpeg processes. Progress parsed from ffmpeg stderr via regex.

**Subtitle waterfall** (`server/media/subs.go` + `src/player/subs.ts`): tries local VTT → SRT → embedded extraction → OpenSubtitles API (hash, then title) → Whisper transcription. Each step is attempted only if prior steps fail.

**Whisper** (`server/jobs/whisper.go` + `tools/stream_whisper.py`): async transcription via faster-whisper Python subprocess. SSE streams cues to the frontend in real-time. Python runs in conda env `global`.

**Reactive state**: `PlayerState` in `src/core/events.svelte.ts` is the single source of truth for player chrome (current time, volume, UI visibility). `PlayerView` in `src/player/view.svelte.ts` composes `PlayerState + SubsManager + AudioManager` for the full player surface.

## External Dependencies

- **ffmpeg / ffprobe** — required for HLS segments, conversion, thumbnails, subtitle extraction
- **aria2c** — optional, torrent/magnet downloads via JSON-RPC on localhost:6800
- **faster-whisper** — optional, Python package in conda `global` env for AI subtitle generation

## Environment Variables (`.env`)

- `WHISPER_MODEL` — path to whisper model file
- `OPENSUBTITLES_API_KEY`, `OPENSUBTITLES_USER`, `OPENSUBTITLES_PASS` — OpenSubtitles API credentials
