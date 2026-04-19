export class AVSync {
  private ctx: AudioContext | null = null;
  private delay: DelayNode | null = null;
  private offset = 0;

  init(videoEl: HTMLVideoElement): this {
    try {
      this.ctx = new AudioContext();
      const src = this.ctx.createMediaElementSource(videoEl);
      this.delay = this.ctx.createDelay(2.0);
      this.delay.delayTime.value = 0;
      src.connect(this.delay);
      this.delay.connect(this.ctx.destination);
      videoEl.addEventListener("play", () => this.ctx?.resume()); // AudioContext requires user gesture to resume
    } catch (e) {
      console.warn("[avsync] init failed:", e);
    }
    return this;
  }

  set(ms: number): number {
    this.offset = Math.max(0, Math.min(2000, Math.round(ms)));
    if (this.delay) this.delay.delayTime.value = this.offset / 1000;
    return this.offset;
  }

  adjust(deltaMs: number): number {
    return this.set(this.offset + deltaMs);
  }

  get ms(): number {
    return this.offset;
  }

  destroy() {
    this.ctx?.close();
    this.ctx = null;
    this.delay = null;
  }
}
