# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```sh
make run          # build (Go + frontend) and start server on :4242
make build        # build Go binary + frontend (no run)
pnpm run dev      # frontend watch mode (Vite)
pnpm run build    # frontend production build → public/assets/
```

`make run` is the canonical way to build and start the server. The Makefile cleans, builds the Go binary with a `-ldflags "-X main.buildTime=..."` injection, builds the Svelte frontend, then runs the binary. Don't invoke `go build` directly — you'll lose the buildTime stamp.

Tests live in `server/media/dual_path_test.go` and cover the codec selector, segPaths layout, master/playlist generation, and an ffmpeg-driven AV1 init/segment round-trip. Run with `go test ./server/media/`. The e2e tests skip cleanly if `libsvtav1` isn't in the local ffmpeg build (`testing.Short()` also skips them).

## Architecture

**Go backend (Gin)** serves both the API and the static frontend assets from `public/`. The backend lives in three sub-packages, each owning a domain:

- `server/library/` — roots, file discovery, hash, hidden dirs, cleanup, KV store. Owns `Library{Roots}` type (the receiver for everything that previously took `roots []string`).
- `server/media/` — streaming + transcoding + cache: HLS, MP4 range serving, audio/video probe, conversion, subtitles, thumbnails, HLS cache eviction.
- `server/jobs/` — background work: aria2 RPC, whisper subprocess, ollama translation, shared `Pool` + `Tracker` primitives.

`main.go` is thin (~130 lines): creates `lib = &library.Library{Roots: ...}`, wires startup goroutines (`media.ProcessAll`, `jobs.Aria2Init`, `media.StartCacheCleanLoop`), and declares the route table pointing at handler functions in the three packages. Inline closures have been moved into `library/handlers.go` and `media/handlers.go`.

Package dependencies: `library` is standalone. `jobs` imports `library` (for `FindVid`). `media` imports both (`library` for file ops, `jobs` for `Aria2ActivePaths`). `jobs` uses a `jobs.OnDownloads func(*library.Library)` callback (wired in `main.go` to `media.ProcessAll`) to break the aria2→process cycle.

**Svelte 5 frontend** compiled by Vite into `public/assets/`. Entry: `src/main.ts` → `src/App.svelte`. The tree mirrors the backend:

- `src/core/` — shared primitives: `api.ts` (raw `GET/POST/DEL` + typed `api.*` namespace), `kv.ts` (typed KV helpers), `events.svelte.ts` (`PlayerState` class + timing constants), `toast.svelte.ts` + `ToastHost.svelte` (app-wide toast bus, mounted once in `App.svelte`), `idb.ts`, `clickOutside.ts`, `video.ts` utilities.
- `src/library/` — Home, Manage, and their components (FileRow, FolderRow, Header, Magnet, Downloads) plus `dl.ts` (BackgroundFetch + IndexedDB download manager).
- `src/player/` — Player, Subs, AudioPicker, Dropdown, all player subcomponents (bar, progress, volume, sync, whisper, related, Download), plus `subs.ts`, `ux.ts`, `avsync.ts`, `tracker.ts`, `view.svelte.ts` (`PlayerView` composing `PlayerState + SubsManager + AudioManager`).

Routing is hash-based in `App.svelte` (Home / Player / Manage).

## Key Patterns

**Video streaming** has two paths:
1. Direct MP4 → HTTP 206 range requests (`server/media/player.go`)
2. HLS → `server/media/hls.go`. Codec is per-video via the KV store (`MediaCodec`/`codec.go`), default h264:
   - **h264** (grandfathered library): adaptive ladder 144p–2160p, 4s MPEG-TS segments transcoded on-demand (h264_videotoolbox if available), or keyframe-aligned `-c:v copy` passthrough when the source already matches a rung.
   - **av1** (new conversions, marked `codec:<hash>=av1`): **demuxed CMAF** — hls.js can't demux fMP4, so video and audio ship as two *separate single-track* renditions. Video: one source-passthrough rung (`q=src`, no audio param) `-c:v copy`'d into fMP4 (.m4s) + av01-only init. Audio: a separate AAC fMP4 rendition (`q=audio`, `-vn -c:a aac`) + mp4a-only init, joined via `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud"` / `AUDIO="aud"`. Both cut on the same keyframe-aligned `si.bounds` (CMAF sync). EXT-X-VERSION:6 + EXT-X-MAP per rendition. No transcode ladder (no AV1 HW encoder → live transcode rebuffers; copy is ~instant). Marked-but-not-copy-eligible av1 → `HLSMaster` falls back to the h264 ladder. **Never reintroduce a muxed AV1 segment** — it serves HTTP 200 but plays frozen in hls.js. Only the AV1 branch differs from h264; the h264 muxed-TS path is untouched.

**Background conversion** (`server/media/convert.go`): non-MP4 formats auto-convert on startup, **stored as AV1** (`av1File`, libsvtav1 preset 6, 8-bit yuv420p; `-c:v copy` if the source is already AV1), then marked `codec=av1` so HLS serves them via AV1 copy-passthrough. Max 3 concurrent ffmpeg processes. Progress parsed from ffmpeg stderr via regex.

**Subtitle waterfall** (`server/media/subs.go` + `src/player/subs.ts`): tries local VTT → SRT → embedded extraction → OpenSubtitles (hash, then title) → Subdl (title) → Whisper transcription. Each step is attempted only if prior steps fail. `SubResult.Provider` distinguishes "os" from "subdl"; `GetSubs` branches on it — OS uses the /download endpoint with a `file_id`, Subdl fetches a `.zip` (or `.srt`) directly from the URL and converts the first `.srt` to `.vtt`.

**Whisper** (`server/jobs/whisper.go` + `tools/stream_whisper.py`): async transcription via faster-whisper Python subprocess. SSE streams cues to the frontend in real-time. Python runs in conda env `global`.

**Reactive state**: `PlayerState` in `src/core/events.svelte.ts` is the single source of truth for player chrome (current time, volume, UI visibility). `PlayerView` in `src/player/view.svelte.ts` composes `PlayerState + SubsManager + AudioManager` for the full player surface.

## External Dependencies

- **ffmpeg / ffprobe** — required for HLS segments, conversion, thumbnails, subtitle extraction
- **aria2c** — optional, torrent/magnet downloads via JSON-RPC on localhost:6800
- **faster-whisper** — optional, Python package in conda `global` env for AI subtitle generation

## Environment Variables (`.env`)

- `WHISPER_MODEL` — path to whisper model file
- `OPENSUBTITLES_API_KEY`, `OPENSUBTITLES_USER`, `OPENSUBTITLES_PASS` — OpenSubtitles API credentials
- `SUBDL_API_KEY` — optional Subdl fallback. If unset, the Subdl step in the waterfall is skipped silently.
- `OLLAMA_HOST`, `OLLAMA_MODEL` — for Whisper post-translation of non-English audio. Defaults: `http://100.117.92.56:11434`, `qwen2.5:7b`.
