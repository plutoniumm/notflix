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
  private wake = () => this.ctx?.resume().catch(() => {});

  init(videoEl: HTMLVideoElement): this {
    if (!usable()) return this;

    try {
      this.ctx = new AudioContext();
      const src = this.ctx.createMediaElementSource(videoEl);
      this.delay = this.ctx.createDelay(2.0);
      this.delay.delayTime.value = 0;
      src.connect(this.delay);
      this.delay.connect(this.ctx.destination);
      // Autoplay or an in-place episode swap can leave the context suspended
      // (silent) — resume on any playback start, not just on `play`.
      this.el = videoEl;
      videoEl.addEventListener("play", this.wake);
      videoEl.addEventListener("playing", this.wake);
    } catch (e) {
      console.warn("[avsync] init failed:", e);
      this.ctx = null;
      this.delay = null;
    }

    return this;
  }

  get on(): boolean {
    return !!this.delay;
  }

  set(ms: number): number {
    if (!this.delay) return 0;
    this.offset = Math.max(0, Math.min(2000, Math.round(ms)));
    this.delay.delayTime.value = this.offset / 1000;

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
  }
}
