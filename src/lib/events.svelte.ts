export function fmtTime(s: number): string {
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = Math.floor(s % 60);
  if (h > 0)
    return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

export class PlayerState {
  // playback
  paused    = $state(true);
  hideUI    = $state(false);
  currentTime = $state(0);
  duration    = $state(0);
  // volume pill
  volLevel   = $state(1);
  volVisible = $state(false);

  // whisper message
  wMsg = $state("");

  // video list / meta
  rows     = $state<[string, any[]][]>([]);
  nextURL  = $state<string | null>(null);
  videoKey = $state("");

  get pct() {
    return this.duration > 0
      ? Math.round((this.currentTime / this.duration) * 100)
      : 0;
  }

  #uiTimer:  ReturnType<typeof setTimeout> | undefined;
  #volTimer: ReturnType<typeof setTimeout> | undefined;

  showUI(paused: boolean) {
    this.hideUI = false;
    clearTimeout(this.#uiTimer);
    if (!paused)
      this.#uiTimer = setTimeout(() => { this.hideUI = true; }, 3000);
  }

  bind(player: any) {
    player.on("play", () => {
      this.paused = false;
      this.showUI(false);
    });

    player.on("pause", () => {
      this.paused = true;
      this.hideUI = false;
      clearTimeout(this.#uiTimer);
    });

    player.on("volumechange", () => {
      this.volLevel   = player.muted() ? 0 : player.volume();
      this.volVisible = true;
      clearTimeout(this.#volTimer);
      this.#volTimer  = setTimeout(() => { this.volVisible = false; }, 1500);
    });

    player.on("timeupdate", () => {
      this.currentTime = player.currentTime() ?? 0;
    });

    player.on("durationchange", () => {
      const d = player.duration();
      if (d && isFinite(d)) this.duration = d;
    });
  }

  destroy() {
    clearTimeout(this.#uiTimer);
    clearTimeout(this.#volTimer);
  }
}
