// Web Audio API-based A/V sync control.
// Routes the video element's audio through a DelayNode so we can offset
// audio relative to video at millisecond precision.
// Positive delay = audio plays later (corrects audio-ahead-of-video).
export class AVSync {
  private ctx: AudioContext | null = null;
  private delay: DelayNode | null = null;
  private _ms = 0;

  init(videoEl: HTMLVideoElement): this {
    try {
      this.ctx = new AudioContext();
      const src = this.ctx.createMediaElementSource(videoEl);
      this.delay = this.ctx.createDelay(2.0);
      this.delay.delayTime.value = 0;
      src.connect(this.delay);
      this.delay.connect(this.ctx.destination);
      // AudioContext requires user gesture to resume
      videoEl.addEventListener('play', () => this.ctx?.resume());
    } catch (e) {
      console.warn('[avsync] init failed:', e);
    }
    return this;
  }

  set(ms: number): number {
    this._ms = Math.max(0, Math.min(2000, Math.round(ms)));
    if (this.delay) this.delay.delayTime.value = this._ms / 1000;
    return this._ms;
  }

  adjust(deltaMs: number): number {
    return this.set(this._ms + deltaMs);
  }

  get ms(): number {
    return this._ms;
  }

  destroy() {
    this.ctx?.close();
    this.ctx = null;
    this.delay = null;
  }
}
