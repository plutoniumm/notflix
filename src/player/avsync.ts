import videojs from "video.js";

// WebKit's MediaElementAudioSourceNode can't take HLS: routing the element
// through WebAudio silences it and kills AirPlay/PiP audio, so Safari and iOS
// get no delay graph — offset stays 0 and the sync UI hides.
const usable = () =>
  typeof AudioContext !== "undefined" &&
  !(videojs as any).browser?.IS_ANY_SAFARI &&
  !(videojs as any).browser?.IS_IOS;

export class AVSync {
  private ctx: AudioContext | null = null;
  private delay: DelayNode | null = null;
  private offset = 0;
  private el: HTMLVideoElement | null = null;
  private ok = false;
  private wake = () => this.ctx?.resume().catch(() => {});

  // init is deliberately cheap. The FIRST `new AudioContext()` in a page opens a
  // CoreAudio device and measured 2010ms on this machine — synchronously, from
  // the middle of the player's init block, so video.js could not even issue the
  // HLS master request until it returned. A/B with the constructor stubbed out
  // moved that request ~2.5s earlier. The second and later contexts cost 0.2ms,
  // so this is a one-time device open and nothing but a manual sync nudge ever
  // needs it. Build the graph then.
  init(videoEl: HTMLVideoElement): this {
    if (!usable()) return this;

    this.el = videoEl;
    this.ok = true;
    // Autoplay or an in-place episode swap can leave the context suspended
    // (silent) — resume on any playback start, not just on `play`. Harmless
    // before the graph exists; wake is a no-op while ctx is null.
    videoEl.addEventListener("play", this.wake);
    videoEl.addEventListener("playing", this.wake);

    return this;
  }

  private build(): boolean {
    if (this.delay) return true;
    if (!this.ok || !this.el) return false;

    try {
      this.ctx = new AudioContext();
      const src = this.ctx.createMediaElementSource(this.el);
      this.delay = this.ctx.createDelay(2.0);
      this.delay.delayTime.value = 0;
      src.connect(this.delay);
      this.delay.connect(this.ctx.destination);
      this.ctx.resume().catch(() => {});
    } catch (e) {
      console.warn("[avsync] init failed:", e);
      this.ctx = null;
      this.delay = null;
      this.ok = false;

      return false;
    }

    return true;
  }

  // Capability, not construction: the sync control shows or hides on this, and
  // it must not depend on whether the graph has been built yet.
  get on(): boolean {
    return this.ok;
  }

  set(ms: number): number {
    const want = Math.max(0, Math.min(2000, Math.round(ms)));
    // Zero is the default. Opening an audio device to apply no delay is exactly
    // the 2s this change exists to avoid.
    if (want === 0 && !this.delay) {
      this.offset = 0;

      return 0;
    }
    if (!this.build()) return 0;

    this.offset = want;
    this.delay!.delayTime.value = this.offset / 1000;

    return this.offset;
  }

  adjust(deltaMs: number): number {
    return this.set(this.offset + deltaMs);
  }

  get ms(): number {
    return this.offset;
  }

  destroy() {
    this.el?.removeEventListener("play", this.wake);
    this.el?.removeEventListener("playing", this.wake);
    this.el = null;
    this.ctx?.close();
    this.ctx = null;
    this.delay = null;
    this.ok = false;
  }
}
