export const UI_HIDE_MS = 3000;
export const HINT_SHOW_MS = 1500;
export const SAVE_INTERVAL_MS = 2000;
export const RESUME_THRESHOLD_S = 60;
export const END_CUTOFF_S = 5 * 60;
export const SW_POLL_MS = 60_000;
export const JOBS_POLL_MS = 2000;
export const DL_POLL_MS = 2000;
export const PWA_POLL_MS = 1000;

export function fmtTime(s: number): string {
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = Math.floor(s % 60);
  if (h > 0)
    return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

export class PlayerState {
  paused    = $state(true);
  hideUI    = $state(false);
  currentTime = $state(0);
  duration    = $state(0);
  volLevel   = $state(1);
  volVisible = $state(false);

  speed   = $state(1);
  hasSubs = $state(false);

  wMsg = $state("");

  syncMs      = $state(0);
  syncVisible = $state(false);

  rows     = $state<[string, any[]][]>([]);
  nextURL  = $state<string | null>(null);
  videoKey = $state("");

  get pct() {
    return this.duration > 0
      ? Math.round((this.currentTime / this.duration) * 100)
      : 0;
  }

  #uiTimer:   ReturnType<typeof setTimeout> | undefined;
  #volTimer:  ReturnType<typeof setTimeout> | undefined;
  #syncTimer: ReturnType<typeof setTimeout> | undefined;

  showSync(ms: number) {
    this.syncMs      = ms;
    this.syncVisible = true;
    clearTimeout(this.#syncTimer);
    this.#syncTimer = setTimeout(() => { this.syncVisible = false; }, HINT_SHOW_MS);
  }

  showUI(paused: boolean) {
    this.hideUI = false;
    clearTimeout(this.#uiTimer);
    if (!paused)
      this.#uiTimer = setTimeout(() => { this.hideUI = true; }, UI_HIDE_MS);
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
      this.#volTimer  = setTimeout(() => { this.volVisible = false; }, HINT_SHOW_MS);
    });

    player.on("timeupdate", () => {
      this.currentTime = player.currentTime() ?? 0;
    });

    player.on("durationchange", () => {
      const d = player.duration();
      if (d && isFinite(d)) this.duration = d;
    });

    player.on("ratechange", () => {
      this.speed = player.playbackRate() ?? 1;
    });

    const checkSubs = () => {
      const tracks = player.textTracks();
      let active = false;
      for (let i = 0; i < tracks.length; i++) {
        if (tracks[i].mode === "showing") { active = true; break; }
      }
      this.hasSubs = active;
    };

    player.on("texttrackchange", checkSubs);
    player.textTracks()?.addEventListener?.("change", checkSubs);
  }

  destroy() {
    clearTimeout(this.#uiTimer);
    clearTimeout(this.#volTimer);
    clearTimeout(this.#syncTimer);
  }
}
