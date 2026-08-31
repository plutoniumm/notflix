# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```sh
make run          # build (Go + frontend) and start server on :4242
make build        # build Go binary + frontend (no run)
pnpm run dev      # frontend watch mode (Vite)
go test ./...     # all Go tests
go test ./server/media/ -run TestLadderFor_NeverEmpty   # single test
go test ./server/media/ -short              # skip ffmpeg e2e tests
node --test tools/*.test.ts                 # frontend tests (node strips types; no runner dep)
```

`make run` is the canonical way to build and start. The Makefile cleans, builds the Go binary with a `-ldflags "-X main.buildTime=..."` injection, builds the Svelte frontend into `public/assets/`, then runs the binary. Don't invoke `go build` directly — you'll lose the buildTime stamp (surfaced at `/api/build` and compared against the frontend's `__BUILD_TIME__` for staleness).

Tests live beside their subjects in all three server packages. The ffmpeg-driven e2e tests (`server/media/dual_path_test.go`, `av1_grid_test.go`) skip cleanly under `testing.Short()` or when `libsvtav1` isn't in the local ffmpeg build.

Frontend tests run on bare `node` (it strips the TypeScript itself). `fmt.test.ts` pins the edge cases the old per-component formatters got wrong — hour tiers, sub-MB byte tiers, NaN, negatives — including a `legacy` block asserting the *old wrong* outputs so the fix can't silently regress. `mobile.test.ts` asserts against the **compiled** bundle for rules that can't be exercised without a device: every hover-revealed control also reveals under `@media (hover: none)` (an `opacity: 0` button still takes taps), 44px touch targets, 16px fields (iOS zooms below that and never back), safe-area insets, `100dvh`.

## Architecture

**Go backend (Gin)** serves the API and the static frontend from `public/`. Three sub-packages, each owning a domain:

- `server/library/` — roots, file discovery, hash, cleanup, KV store, corrupt scan, pipeline ledger, stats. Owns the `Library{Roots, Caps}` type (receiver for everything that used to take `roots []string`) and `FFRun`, the single ffmpeg invocation wrapper.
- `server/media/` — streaming + transcoding + cache: HLS, MP4 range serving, probe, conversion, repair, subtitles, thumbnails, trickplay sprites, skip detection, phase status.
- `server/jobs/` — background work: aria2 RPC, yt-dlp, torrent search, whisper subprocess, ollama translation, LLM organize, Tailscale identity, shared `Pool` + `Tracker` primitives.
- `server/x/` — leaf stdlib, imports nothing internal so even `library` can use it without a cycle. `x/atom` (atomic `WriteFile`/`SaveJSON`/`LoadJSON` — tmp + rename, and `LoadJSON` keeps "absent" distinct from "malformed"), `x/httpc` (one outbound client with an **enforced** timeout and a **mandatory** 2xx check; a zero timeout falls back to 30s, so "no deadline" is unreachable and "write an HTTP error page to disk" is unrepresentable).

`main.go` resolves the root set, wires cross-package callbacks, starts background goroutines, and declares the route table. Handlers live in `library/handlers.go`, `media/handlers.go`, and the domain files.

**Roots** are declared in `main.go` as `configuredRoots`. `~/…` entries are auto-created; bare absolute paths (`/Volumes/…`) must pre-exist — never auto-created, since a regular dir at a mountpoint silently shadows the volume. Every root failure is non-fatal: boot must survive an unmounted drive. `rootCaps` is a **UI-only** soft quota (cosmetic budgeting for roots sharing the boot disk); writes aren't enforced.

**Package dependencies**: `library` is standalone. `jobs` imports `library`. `media` imports both. Cycles are broken with function-pointer callbacks wired in `main.go`:
- `jobs.OnDownloads = media.ProcessAll` (aria2 → reprocess)
- `library.IsTorrentBusy = jobs.Aria2Owns` (busy gate — a single RLock'd lookup; `IsBusy` runs per file in every background pass, so the old whole-map copy was O(paths) per check)
- `library.IdentityFromIP` → `jobs.TailscaleIdentity`, with `library.Device` mirroring `jobs.Identity` field-for-field so `library` never imports `jobs`.

**Svelte 5 frontend** compiled by Vite into `public/assets/` (single unsplit bundle — chunk hashing would break the service-worker precache list). Entry `src/main.ts` → `src/App.svelte`. Routing is **path + query based**: `/manage` → Manage, `?video=<rel>` → Player, else Home. The tree mirrors the backend:

- `src/core/` — the frontend stdlib. **Reach for these before hand-rolling.** `fmt.ts` is *every* number→string (`dur`/`durWords`/`bytes`/`rate`/`ago`/`pct`) — five drifted copies once disagreed, rendering a 2-hour job as "122m 5s" and a 400 KB/s download as "0 MB/s"; never write a formatter in a component. `store.ts` (`local(key, fallback)`) — `localStorage` *throws* when site data is blocked, so never call it directly. `path.ts` (`join(dir, name)`, which knows `dir === "."` is the root). `poll.ts` (`usePoll` — hidden-tab-aware; use instead of `setInterval`). `api.ts` (raw `GET/POST/DEL/POSTForm/head` + typed `api.*`; errors auto-toast unless `silent`). Plus `kv.ts`, `events.svelte.ts` (`PlayerState` + timing constants; re-exports `fmtTime`/`fmtAgo` from `fmt.ts`), `toast.svelte.ts` + `ToastHost.svelte`, `idb.ts`, `video.ts` (`clean`, `vidURL`, `thumbFail`).
- `src/components/` — shared presentational bits: `MediaCard.svelte` (the one video tile; owns the thumbnail fallback), `ScrollRow.svelte` (arrow chrome around a caller-supplied strip), `ProgressBar`, `Badge`, `EmptyState`, `Icon`, `BackLink`.
- `src/library/` — Home, Manage, Organize, rows (File/Folder/Show/Continues), Magnet, Downloads, `homeData.svelte.ts` (row grouping), `torrentName.ts` (season parsing), `dl.ts` (BackgroundFetch + IndexedDB).
- `src/player/` — Player + subcomponents (bar, dock, progress, volume, sync, tune, whisper, skip, related, Download), `subs.ts`, `ux.ts`, `avsync.ts`, `watch.ts`, `sprite.svelte.ts`, `view.svelte.ts` (`PlayerView` = `PlayerState + SubsManager + AudioManager`).

## Key Patterns

**Every ffmpeg call goes through `library.FFRun`** (`server/library/ffmpeg.go`). It prepends `-nostdin -v error`, routes `-progress` to its own stdout pipe (keeping progress out of the error buffer), caps retained stderr at 8 KB, supports `Nice` (batch work must never starve live streaming), and derives from a package-level `shutdownCtx`. `library.Shutdown()` cancels every in-flight ffmpeg on SIGTERM/SIGINT — orphaned encoders from a past run kept writing files the next run then stomped. Same reason `jobs.Aria2Stop()` runs on shutdown: an orphaned aria2c squats :6800 and wedges every later spawn.

**`library.IsBusy(path)` is the single "hands off, come back later" gate** for all background work (convert, thumbs, sprite, corrupt scan, clean). It's true when aria2 owns the file, a control sidecar exists (`.aria2`, `.tmp`, `.audio.tmp`), or the file was modified in the last 30s. If you're adding a background pass, gate it on `IsBusy`.

**The KV store has exactly one write path** (`server/library/kv.go`). `update(fn)` holds a single `kvMu.Lock()` across read → mutate → write, because the store is one whole-file JSON blob: reading under `RLock` and writing under a separate `Lock` lost every concurrent set but the last (50 concurrent writers once kept 2 keys). Writes go through `x/atom` (tmp + rename), and a present-but-malformed store **refuses** the write rather than replacing thousands of keys with the one being set. Never add a second read-modify-write path.

**Pipeline ledger** (`server/library/pipeline.go` → `cache/pipeline.json`) records per-file, per-step state (`running`/`done`/`failed`). On boot `PipelineInit` resets surviving `running` entries to missing (interrupted → redo) and returns them so `main.go` can delete truncated `.tmp` output before the redo lands.

**`ProcessAll`** (`server/media/convert.go`) is the master background sweep, guarded by a CAS so it can't overlap. Order: `CleanAll` alone first (tree-mutating rename/move/flatten, so later phases walk a settled tree), then convert / scan+repair / thumbs / sprites / whisper fan out concurrently, then skip detection. Each phase has its own pool and skips busy files. `toMP4` also dispatches sprite/thumb/whisper per-file as each conversion lands, so a file gains its thumbnail while the next is still transcoding. Progress is a fixed-order phase state machine in `server/media/status.go`, polled by `ProcessStatus.svelte` via `/api/process/status`.

**Video streaming** has two paths:
1. Direct MP4 → HTTP 206 range requests (`server/media/player.go`)
2. HLS → `server/media/hls.go`. Codec is per-video via KV (`codec.go`), default h264:
   - **h264** (grandfathered library): adaptive ladder 144p–2160p, 4s MPEG-TS segments transcoded on demand (hardware encoder when `encoders.go` finds one), or keyframe-aligned `-c:v copy` passthrough when the source already matches a rung.
   - **av1** (new conversions): **demuxed CMAF** — hls.js can't demux fMP4, so video and audio ship as two *separate single-track* renditions. Video: one source-passthrough rung (`q=src`) `-c:v copy`'d into fMP4 (.m4s) + av01-only init. Audio: a separate AAC fMP4 rendition (`q=audio`, `-vn -c:a aac`) + mp4a-only init, joined via `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud"` / `AUDIO="aud"`. Both cut on the same keyframe-aligned bounds (CMAF sync). **`muxerBounds` replicates ffmpeg's hls muxer rule exactly** — split on the first keyframe where `pts - start_pts >= segDur * segmentsWritten`, which is an ABSOLUTE grid. The old rule was relative to the last cut; the two agree only while every GOP is >= `hlsSegDur`, and a 3s-GOP source produced a 6-segment playlist over 9 on-disk segments (wrong frames mid-file, playlist ending early). Don't "simplify" it back to a relative rule, and don't derive the playlist from ffmpeg's own `video.m3u8` — that needs the pass to finish before the playlist can be served, which reintroduces the startup stall the continuous-pass design exists to avoid. Each rendition is ONE continuous ffmpeg pass — per-segment `-ss`+copy re-snapped to the previous GOP and drifted the audio. No AV1 transcode ladder (no HW AV1 encoder → live transcode rebuffers). Marked-but-not-copy-eligible av1 falls back to the h264 ladder. **Never reintroduce a muxed AV1 segment** — it serves HTTP 200 but plays frozen in hls.js.

**AV1 capability gate.** `HLSMaster` resolves the codec per client, because an AV1-only master with no h264 alternate is a black screen on any device without hardware AV1 decode (iPhone 14 and older, pre-M3 iPads). The player probes with `MediaSource.isTypeSupported` (falling back to `canPlayType`) and sends `&av1=1` or `&av1=0` **on the master request only** — the master hands out URLs for whichever codec it resolved, so the choice propagates. Only the literal `"1"`/`"0"` are honoured; anything else falls through to a conservative User-Agent heuristic (iOS → h264; Chrome/Firefox/Edge → AV1; unknown or no UA → h264, since h264 always plays and a wrong AV1 is a black screen). The master is the only per-client artifact — it's `no-cache` + `Vary: User-Agent`, and everything it points at stays content-addressed and shared.

**`MediaCodec` keys on the BASENAME hash.** Codec marks are stamped at conversion time with the then-current rel path, but `CleanAll`/Organize move files between dirs afterwards — a dir-qualified key silently orphans the mark and the file falls back to the h264 live-transcode ladder. Basenames survive moves.

**HLS cache invalidation**: `hlsURLVer` (currently `6`) cache-busts segment/init URLs, which are served immutable. Bump it on *any* change to segment or init production. Bumping only busts clients/CDN — for pipeline changes to take effect server-side you must also `rm -rf ./cache/[0-9]*/`.

**Background conversion** (`server/media/convert.go`): non-MP4 formats auto-convert, **stored as AV1** (libsvtav1 preset 6; `-c:v copy` if already AV1), then marked `codec=av1` so HLS serves them by copy-passthrough. Bounded concurrency, `Nice` priority, writes to `<dst>.tmp` then atomic-renames. Source media capability (SAR/rotation/bit depth/color/channels) is carried in `srcInfo` and preserved — deanamorphize, rotation, 10-bit HDR copy, surround, portrait ladder. See `feedback_media_upgrade_principle`: preserve the best the source offers, downconvert only when a target can't.

**`CleanAll` deletes on a junk DENYLIST, never an allowlist miss** (`server/library/clean.go`). It used to delete anything whose extension wasn't in a 12-entry video allowlist — which omitted `.m2ts`, `.mts`, `.vob` — and `jobs.OnDownloads` runs `ProcessAll` after every completed download, so a Blu-ray remux was destroyed about an hour after it landed. For the same reason nothing ever `RemoveAll`s a non-empty directory: `pruneEmpty` removes only genuinely empty ones.

**Never use `filepath.Glob` on a user filename.** Go has no glob escaper, and `[` is a character class: `filepath.Glob("Movie [2020] 1080p.*")` returns `[]` with **`err == nil`** — a silent miss. About 7% of this library is `-[YTS.MX]`-style releases, and this bug independently shipped in four places (sidecar moves in organize, local-subtitle discovery, and both the delete and rename sidecar sweeps), stranding or failing to clean `.srt`/`.vtt` siblings. Use `os.ReadDir` on the parent plus a `strings.HasPrefix`/`HasSuffix` filter.

**Corrupt scan + repair**: `library.ScanCorrupt` builds a corrupt set; `media.RepairAll` tries progressively harder strategies, replacing in place and preserving the original as `<file>.corrupt.bak` (protected from the junk sweep by `keepSuffixes`).

**Trickplay sprites** (`server/media/sprite.go`): one frame per 10s, 160px wide, 10 columns → `images/{key}.sprite.jpg` + `.sprite.json` manifest served by `/api/sprite/:key`. Key = `library.Hash(basename)`, same as thumbnails. Idempotent.

**Skip detection** (`server/media/skip.go`): episodes in a folder share intro/outro audio, so head/tail PCM fingerprints are cross-matched against a sibling. Needs ≥2 episodes; matches must run ≥15s and ≤300s. Persisted as `skip:<hash>` in KV, keyed on the **basename** like `codec:` (a dir-qualified key orphans the moment `CleanAll`/Organize moves the file, which silently re-ran PCM fingerprinting on already-scanned episodes); presence = scanned. The player turns these into skip buttons and does a seamless in-place next-episode swap (keeps Chrome's autoplay allowance). The thresholds are empirically tuned — don't "simplify" them.

**Subtitle waterfall** (`server/media/subs.go` + `src/player/subs.ts`): local VTT → SRT → embedded extraction → OpenSubtitles (hash, then title) → Subdl (title) → Whisper. `SubResult.Provider` distinguishes "os" from "subdl"; `GetSubs` branches on it. `subnormalize.go` defensively strips BOM/leading junk at every parse site — Windows-saved SRTs otherwise fail with `strconv.Atoi: parsing "<BOM>1"`.

**Whisper** (`server/jobs/whisper.go` + `tools/stream_whisper.py`): async transcription via a faster-whisper Python subprocess, SSE-streamed to the frontend. Python is a hardcoded conda `global` env path. `whisper_auto.go` backfills after conversion, throttled to ONE concurrent run, skipped when any English sidecar (or a prior `.whisper.vtt`) exists. Non-English audio is post-translated through ollama (`aya-expanse:8b` default).

**Torrents & downloads**: `/api/search` fans out to TPB/Knaben/torrents-csv/Nyaa (`search.go` + `providers.go`), dedupes by infohash, sorts by seeders. aria2 needs persistent DHT at `./cache/aria2.dht.dat` (not root-owned `~/.aria2/`), fixed listen ports, and global tracker fallback — "stuck at metadatafetch" is almost always zero peer connections. Flag changes silently don't apply if a stale aria2c is still holding :6800; `pkill aria2c`. `ytdlp.go` covers non-torrent URLs.

**LLM organize** (`server/jobs/organize.go`): the model only ever sees an indexed list of loose files + existing folder names and answers `{index → folder}` — filenames never round-trip, so a hallucination can't corrupt a move. Plan is a background job (POST `/plan`, poll `/status`, `/warm`) running several classifications merged by per-file vote, with an early exit when the first runs converge. Nothing touches disk until the user posts an approved subset to `/apply`.

**Stats** (`server/library/stats.go`): `cache/stats.jsonl` is appended by `statsTee`, called from `KVSet` for any payload that smells like a watch record — no separate frontend POST. Watch pings are attributed to a device via Tailscale identity (`jobs/tailscale.go`, resolution ladder deliberately excludes tags: a tag is a permission category shared by many devices). `StatsSummaryHandler` aggregates it, but is currently **not registered in main.go** — the /stats frontend page is gone.

**Reactive state**: `PlayerState` in `src/core/events.svelte.ts` is the single source of truth for player chrome. Runes need a `.svelte.ts` extension — `$state` in a plain `.ts` builds fine but ReferenceErrors at runtime.

## External Dependencies

- **ffmpeg / ffprobe** — required. `encoders.go` probes `ffmpeg -encoders` once and picks hardware paths (VideoToolbox / NVENC / QSV / AMF) when present. No zscale in this build, so there's no correct HDR tonemap; SVT-AV1 drops `color_trc`/`primaries` and only the bt2020 matrix survives.
- **aria2c** — optional, JSON-RPC on localhost:6800
- **yt-dlp** — optional, non-torrent URL downloads
- **faster-whisper** — optional, Python package in conda `global` env
- **ollama** — optional, subtitle translation
- **tailscale** — optional, `tailscale status --json` for stats attribution

## Environment Variables (`.env`)

- `OPENSUBTITLES_API_KEY`, `OPENSUBTITLES_USER`, `OPENSUBTITLES_PASS`
- `SUBDL_API_KEY` — optional fallback; unset skips that waterfall step silently
- `OLLAMA_HOST`, `OLLAMA_MODEL` — defaults `http://100.117.92.56:11434`, `aya-expanse:8b`

`WHISPER_MODEL` still appears in `.env` but is no longer read — the whisper model comes from the `?model=` query param (default `base`).
