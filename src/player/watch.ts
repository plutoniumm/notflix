import { kv } from "../core/kv";
import { RESUME_THRESHOLD_S, END_CUTOFF_S } from "../core/events.svelte";

// WatchProgress persists playback position to the server KV store as
// `watched:<videoParam> = { t, at }`. `t` is the seek-back point, `at` is
// the wall-clock timestamp (ms) of the last save — surfaced as "x mins ago"
// on the Continue Watching row.
//
// Rule: within END_CUTOFF_S of the end → clear (finished). Below
// RESUME_THRESHOLD_S → ignore (not worth saving early).
//
// Earlier revisions mirrored every save into a `lastPlayed` localStorage
// blob and preferred it on resume; the local copy was per-device, so cross-
// device resume silently used a stale position. The KV write happens every
// SAVE_INTERVAL_MS, which is enough.
export default class WatchProgress {
  constructor() {
    // One-shot cleanup of the abandoned local store from older clients.
    try {
      localStorage.removeItem("lastPlayed");
    } catch {}
  }

  // Reject insane positions so a momentary broken timeline can't poison
  // resume (a persisted t past the media end made fresh starts jump to ~2h).
  private sane(t: number, d: number): boolean {
    return Number.isFinite(t) && t >= 0 && (!(d > 0) || t <= d + 1);
  }

  // "Close enough to the end to count as finished". A flat END_CUTOFF_S would
  // swallow every video shorter than it — a 4-minute clip was cleared at every
  // position and could never resume — so short media get a relative cutoff.
  private done(t: number, d: number): boolean {
    return d > 0 && d - t < Math.min(END_CUTOFF_S, d * 0.1);
  }

  save(videoParam: string, t: number, d: number) {
    if (!this.sane(t, d)) return;
    if (this.done(t, d)) {
      kv.set(`watched:${videoParam}`, null);
    } else if (t > RESUME_THRESHOLD_S) {
      // dur is carried so the server-side stats tee can compute completion
      // ratio later without re-probing the file.
      kv.set(`watched:${videoParam}`, { t, dur: d, at: Date.now() });
    }
  }

  // Same as save but synchronous over the wire (sendBeacon). For unload.
  flushOnLeave(videoParam: string, t: number, d: number) {
    if (!this.sane(t, d)) return;
    if (this.done(t, d)) {
      kv.beacon(`watched:${videoParam}`, null);
    } else if (t > RESUME_THRESHOLD_S) {
      kv.beacon(`watched:${videoParam}`, { t, dur: d, at: Date.now() });
    }
  }

  // Server-side resume time, if any. Returns 0 if not present or below the
  // threshold. Caller seeks back RESUME_THRESHOLD_S for context.
  async resume(videoParam: string): Promise<number> {
    try {
      const res: any = await kv.get("watched:" + videoParam);
      const t = res?.value?.t;
      return t > RESUME_THRESHOLD_S ? Math.max(0, t - RESUME_THRESHOLD_S) : 0;
    } catch (err) {
      console.warn("[resume]", err);
      return 0;
    }
  }

  // Mark a video as fully watched (e.g. when user advances to next).
  clear(videoParam: string) {
    kv.beacon(`watched:${videoParam}`, null);
  }
}
