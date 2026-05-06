import Tracker from "./tracker";
import { kv } from "../core/kv";
import { RESUME_THRESHOLD_S, END_CUTOFF_S } from "../core/events.svelte";

// WatchProgress unifies the two stores that track playback position:
// - Tracker (localStorage) for instant resume on the same device
// - KV (server) for cross-device "Continue Watching" rows
//
// Rule: if we're within END_CUTOFF_S of the end, treat as finished and clear
// both stores. Below RESUME_THRESHOLD_S, ignore (not worth saving early).
export default class WatchProgress {
  private tracker = new Tracker();

  // Save current position to both stores. Async (KV roundtrip).
  save(videoParam: string, t: number, d: number) {
    this.tracker.set(videoParam, t);

    if (d > 0 && d - t < END_CUTOFF_S) {
      this.tracker.del(videoParam);
      kv.set(`watched:${videoParam}`, null);
    } else if (t > RESUME_THRESHOLD_S) {
      kv.set(`watched:${videoParam}`, { t, at: Date.now() });
    }
  }

  // Same as save but synchronous over the wire (sendBeacon). For unload.
  flushOnLeave(videoParam: string, t: number, d: number) {
    if (d > 0 && d - t < END_CUTOFF_S) {
      this.tracker.del(videoParam);
      kv.beacon(`watched:${videoParam}`, null);
    } else if (t > RESUME_THRESHOLD_S) {
      this.tracker.set(videoParam, t);
      kv.beacon(`watched:${videoParam}`, { t, at: Date.now() });
    }
    this.tracker.flush();
  }

  // Local-only resume position (synchronous read).
  localResume(videoParam: string): number {
    return this.tracker.get(videoParam);
  }

  // Server-side resume time, if any (async). Returns 0 if not present or
  // below the threshold.
  async serverResume(videoParam: string): Promise<number> {
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
    this.tracker.del(videoParam);
    kv.beacon(`watched:${videoParam}`, null);
  }
}
