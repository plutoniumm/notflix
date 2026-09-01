import { toast } from "../core/toast.svelte";

// Everything the player paints over the picture. A gesture that lands on any
// of it belongs to that control, never to play/pause or seek.
const CHROME =
  ".bar, .dock, .rail, .sheet, .skip, .dropdown, .ghost, .synopsis, .error-overlay, .status-msg";

function safePlay(player: any) {
  const p = player?.play();
  if (p && typeof p.then === "function") {
    p.catch((err: any) => {
      if (err?.name === "NotAllowedError") {
        toast.info("Tap the video to start playback");
      } else if (err?.name !== "AbortError") {
        toast.err(`Playback blocked: ${err?.message ?? err}`);
      }
    });
  }
}

export function Touch(
  player: any,
  el: HTMLElement,
  onFullscreen: () => void,
): () => void {
  let [last, prev] = [0, 0];
  let tapTimer: ReturnType<typeof setTimeout> | null = null;

  const handler = (e: TouchEvent) => {
    if ((e.target as HTMLElement).closest(CHROME)) return;

    const now = Date.now();
    const touch = e.changedTouches[0];
    const x = touch.clientX;
    const w = el.clientWidth;

    if (now - last < 300 && Math.abs(x - prev) < 80) {
      if (tapTimer) {
        clearTimeout(tapTimer);
        tapTimer = null;
      }
      e.preventDefault();
      if (x < w / 3) {
        seek(player, -10);
        ripple(el, x, touch.clientY, "−10s");
      } else if (x > (2 * w) / 3) {
        seek(player, 10);
        ripple(el, x, touch.clientY, "+10s");
      } else {
        onFullscreen();
        ripple(el, x, touch.clientY, "⛶");
      }
      last = 0;
    } else {
      last = now;
      prev = x;
      // The timer must outlast the 300ms double-tap window, or a seek gesture
      // in the outer thirds also toggles play.
      if (tapTimer) clearTimeout(tapTimer);
      tapTimer = setTimeout(() => {
        tapTimer = null;
        player.paused() ? safePlay(player) : player.pause();
      }, 320);
    }
  };

  el.addEventListener("touchend", handler, { passive: false });
  return () => {
    el.removeEventListener("touchend", handler);
    if (tapTimer) clearTimeout(tapTimer);
  };
}

export function Click(
  player: any,
  el: HTMLElement,
  onFullscreen: () => void,
): () => void {
  const onPicture = (e: MouseEvent) =>
    e.button === 0 && !(e.target as HTMLElement).closest(CHROME);

  // pointerType, so touch stays with Touch() above and a tap doesn't toggle
  // twice; dblclick carries no pointerType at all, hence the two predicates.
  const tap = (e: PointerEvent) => {
    if (e.pointerType !== "mouse" || !onPicture(e)) return;
    player.paused() ? safePlay(player) : player.pause();
  };
  // Two pointerups already fired, so the play state is back where it started —
  // the same net effect every other player has on a double click.
  const full = (e: MouseEvent) => {
    if (!onPicture(e)) return;
    onFullscreen();
  };

  el.addEventListener("pointerup", tap);
  el.addEventListener("dblclick", full);

  return () => {
    el.removeEventListener("pointerup", tap);
    el.removeEventListener("dblclick", full);
  };
}

function ripple(el: HTMLElement, x: number, y: number, label: string) {
  const r = document.createElement("div");
  r.className = "tap-ripple";
  r.textContent = label;
  r.style.cssText = `left:${x}px;top:${y}px`;
  el.appendChild(r);

  setTimeout(() => r.remove(), 700);
}

export type Acts = {
  next: () => void;
  whisper: () => void;
  sync: (deltaMs: number) => void;
  skip: () => void;
  tune: () => void;
  rail: () => void;
  help: () => void;
  shot: () => void;
  esc: () => void;
};

// The one table: the help overlay renders it, README mirrors it, and
// tools/parity.test.ts fails when either drifts from the handler below.
export type Group = { g: string; keys: { k: string; a: string }[] };

export const KEYS: Group[] = [
  {
    g: "Playback",
    keys: [
      { k: "Space", a: "play / pause" },
      { k: "← / →", a: "seek 5s" },
      { k: "Shift ← / →", a: "seek 30s" },
      { k: "Alt ← / →", a: "nudge 0.1s" },
      { k: "0 – 9", a: "jump to 0% – 90%" },
      { k: "x", a: "skip intro / credits" },
      { k: "n", a: "next episode" },
      { k: "d / s", a: "speed +0.1 / −0.1" },
    ],
  },
  {
    g: "Sound",
    keys: [
      { k: "↑ / ↓", a: "volume ±10%" },
      { k: "m", a: "mute" },
      { k: "[ / ]", a: "audio delay −50ms / +50ms" },
    ],
  },
  {
    g: "Picture",
    keys: [
      { k: "f", a: "fullscreen" },
      { k: "p", a: "picture in picture" },
      { k: "c", a: "subtitles on / off" },
      { k: "w", a: "transcribe with Whisper" },
      { k: "g", a: "grab a frame" },
    ],
  },
  {
    g: "Clip",
    keys: [
      { k: "i", a: "mark clip in-point" },
      { k: "o", a: "mark clip out-point" },
      { k: "Enter", a: "save the clip" },
      { k: "Backspace", a: "clear the marks" },
    ],
  },
  {
    g: "Panels",
    keys: [
      { k: "v", a: "tune — picture, audio delay, speed" },
      { k: "t", a: "up next" },
      { k: "?", a: "this help" },
      { k: "Esc", a: "close whatever is open" },
      { k: "l", a: "back to the library" },
    ],
  },
  {
    g: "Library",
    keys: [
      { k: "/", a: "focus search" },
      { k: "Enter", a: "search torrents for what you typed" },
      { k: "Esc", a: "clear the search" },
      { k: "?", a: "this help" },
    ],
  },
  {
    g: "Pointer",
    keys: [
      { k: "Click", a: "play / pause" },
      { k: "Double-click", a: "fullscreen" },
      { k: "Tap", a: "play / pause" },
      { k: "Double-tap left third / right third", a: "seek −10s / +10s" },
      { k: "Double-tap centre", a: "fullscreen" },
      { k: "Scrub track", a: "drag to scrub; focus it for ← → Home End PageUp PageDown" },
    ],
  },
];

export function Hotkeys(
  player: any,
  onFullscreen: () => void,
  on: Acts,
): () => void {
  const handler = (e: KeyboardEvent) => {
    // Space/letters belong to whatever control has focus, and Cmd/Ctrl
    // chords belong to the browser. The scrub track stops its own arrows
    // from bubbling this far rather than opting out here.
    if ((e.target as HTMLElement).closest?.("input,button,a,select,textarea,[contenteditable]")) return;
    if (e.ctrlKey || e.metaKey) return;

    const key = e.key;
    const lkey = key.toLowerCase();

    if (key === "ArrowRight" && e.shiftKey) {
      seek(player, 30);
    } else if (key === "ArrowRight" && e.altKey) {
      seek(player, 0.1);
    } else if (key === "ArrowRight") {
      seek(player, 5);
    } else if (key === "ArrowLeft" && e.shiftKey) {
      seek(player, -30);
    } else if (key === "ArrowLeft" && e.altKey) {
      seek(player, -0.1);
    } else if (key === "ArrowLeft") {
      seek(player, -5);
    } else if (key === "ArrowUp") {
      e.preventDefault();
      player.volume(Math.min(1, Math.round((player.volume() + 0.1) * 10) / 10));
    } else if (key === "ArrowDown") {
      e.preventDefault();
      player.volume(Math.max(0, Math.round((player.volume() - 0.1) * 10) / 10));
    } else if (key === " ") {
      e.preventDefault();
      player.paused() ? safePlay(player) : player.pause();
    } else if (key === "?") {
      on.help();
    } else if (lkey === "g") {
      on.shot();
    } else if (key === "Escape") {
      on.esc();
    } else if (lkey === "f") {
      onFullscreen();
    } else if (lkey === "m") {
      player.muted(!player.muted());
    } else if (lkey === "d") {
      player.playbackRate(
        Math.min(4, Math.round((player.playbackRate() + 0.1) * 10) / 10),
      );
    } else if (lkey === "s") {
      player.playbackRate(
        Math.max(0.1, Math.round((player.playbackRate() - 0.1) * 10) / 10),
      );
    } else if (lkey === "w") {
      on.whisper();
    } else if (lkey === "l") {
      window.location.href = "/";
    } else if (lkey === "n") {
      on.next();
    } else if (lkey === "x") {
      on.skip();
    } else if (lkey === "v") {
      on.tune();
    } else if (lkey === "t") {
      on.rail();
    } else if (lkey === "p") {
      togglePip(player);
    } else if (lkey === "c") {
      const tracks = player.textTracks();
      for (let i = 0; i < tracks.length; i++) {
        const t = tracks[i];
        if (t.kind === "captions" || t.kind === "subtitles") {
          t.mode = t.mode === "showing" ? "hidden" : "showing";
        }
      }
    } else if (key >= "0" && key <= "9") {
      player.currentTime(player.duration() * parseInt(key) * 0.1);
    } else if (key === "]") {
      on.sync(50);
    } else if (key === "[") {
      on.sync(-50);
    }
  };

  document.addEventListener("keydown", handler);
  return () => document.removeEventListener("keydown", handler);
}

const AV1 = 'video/mp4; codecs="av01.0.05M.08"';

// Capability hint for /api/hls/master (no → h264 ladder). MSE is authoritative
// where it exists; iPhone Safari has none, so canPlayType is the probe there
// ("" means no). `??` not `||` — isTypeSupported legitimately returns false.
// Undeterminable answers NO: a wrong yes is a black screen.
export function av1Ok(): boolean {
  try {
    return (
      (window as any).MediaSource?.isTypeSupported?.(AV1) ??
      document.createElement("video").canPlayType(AV1) !== ""
    );
  } catch {
    return false;
  }
}

export function pipOk(player: any): boolean {
  const v = vidEl(player);
  if (!v) return false;

  return !!(
    (document.pictureInPictureEnabled && v.requestPictureInPicture) ||
    v.webkitSupportsPresentationMode?.("picture-in-picture")
  );
}

// WebKit has no requestPictureInPicture; Chrome rejects it outside a user
// gesture, so this must only ever run from a hotkey or a button press.
export function togglePip(player: any) {
  const v = vidEl(player);
  if (!v) return;

  if (v.webkitSupportsPresentationMode?.("picture-in-picture")) {
    const mode =
      v.webkitPresentationMode === "picture-in-picture"
        ? "inline"
        : "picture-in-picture";
    v.webkitSetPresentationMode(mode);
    return;
  }

  if (document.pictureInPictureElement) {
    document.exitPictureInPicture().catch(() => {});
    return;
  }

  v.requestPictureInPicture?.().catch((err: any) => {
    toast.err(`Picture-in-picture: ${err?.message ?? err}`);
  });
}

function vidEl(player: any): any {
  return player?.el()?.querySelector("video") ?? null;
}

function seek(player: any, n: number) {
  const t = player.currentTime() ?? 0;
  const d = player.duration() ?? 0;

  player.currentTime(Math.max(0, Math.min(d - 0.1, t + n)));
}
