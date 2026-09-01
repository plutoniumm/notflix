import { kv } from "../core/kv";
import { GET } from "../core/api";
import { RESUME_THRESHOLD_S, END_CUTOFF_S } from "../core/events.svelte";

// Persists position to the server KV as `watched:<videoParam> = { t, dur, at }`.
// Within END_CUTOFF_S of the end → clear (finished); below RESUME_THRESHOLD_S →
// don't save. Server-only: a local mirror made cross-device resume go stale.
export default class WatchProgress {
  constructor() {
    // One-shot cleanup of the abandoned local store from older clients.
    try {
      localStorage.removeItem("lastPlayed");
    } catch {}
  }

  // A persisted t past the media end made fresh starts jump to ~2h.
  private sane(t: number, d: number): boolean {
    return Number.isFinite(t) && t >= 0 && (!(d > 0) || t <= d + 1);
  }

  // A flat END_CUTOFF_S swallows media shorter than it (a 4-min clip could
  // never resume), so short media get a relative cutoff.
  private done(t: number, d: number): boolean {
    return d > 0 && d - t < Math.min(END_CUTOFF_S, d * 0.1);
  }

  save(videoParam: string, t: number, d: number) {
    if (!this.sane(t, d)) return;
    if (this.done(t, d)) {
      kv.set(`watched:${videoParam}`, null);
    } else if (t > RESUME_THRESHOLD_S) {
      // dur is carried so the stats tee can compute completion without re-probing.
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

  // 0 when absent or below the threshold, else backed off for context.
  async resume(videoParam: string): Promise<number> {
    try {
      // Raw GET, not kv.get: silent, so a flaky network can't red-toast over
      // the video. Swap back once kv.get takes ReqOpts.
      const key = encodeURIComponent("watched:" + videoParam);
      const res: any = await GET(`/kv/get?key=${key}`, { silent: true });
      const t = res?.value?.t;
      return t > RESUME_THRESHOLD_S ? Math.max(0, t - RESUME_THRESHOLD_S) : 0;
    } catch (err) {
      console.warn("[resume]", err);
      return 0;
    }
  }

  clear(videoParam: string) {
    kv.beacon(`watched:${videoParam}`, null);
  }
}
