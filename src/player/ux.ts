import { toast } from "../core/toast.svelte";

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
    if (
      (e.target as HTMLElement).closest(
        ".vjs-control-bar, .bar, .dock, .rail, .tune, .skip",
      )
    )
      return;

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
      // A single tap anywhere toggles playback. Gating this on the centre
      // third left the outer two thirds inert, which reads as a dead player.
      // The timer outlasts the 300ms double-tap window so a seek gesture in
      // the outer thirds can never also toggle play.
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

function ripple(el: HTMLElement, x: number, y: number, label: string) {
  const r = document.createElement("div");
  r.className = "tap-ripple";
  r.textContent = label;
  r.style.cssText = `left:${x}px;top:${y}px`;
  el.appendChild(r);

  setTimeout(() => r.remove(), 700);
}

export function Hotkeys(
  player: any,
  onFullscreen: () => void,
  onNext: () => void,
  onWhisper: () => void,
  onSync: (deltaMs: number) => void,
  onSkip: () => void,
  onTune: () => void,
): () => void {
  const handler = (e: KeyboardEvent) => {
    // Space/letters belong to whatever control has focus, and Cmd/Ctrl
    // chords belong to the browser.
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
      onWhisper();
    } else if (lkey === "l") {
      window.location.href = "/";
    } else if (lkey === "n") {
      onNext();
    } else if (lkey === "x") {
      onSkip();
    } else if (lkey === "v") {
      onTune();
    } else if (lkey === "p") {
      togglePip(player);
    } else if (lkey === "b") {
      player.el()?.querySelector("video")?.classList.toggle("bright");
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
      onSync(50);
    } else if (key === "[") {
      onSync(-50);
    }
  };

  document.addEventListener("keydown", handler);
  return () => document.removeEventListener("keydown", handler);
}

const AV1 = 'video/mp4; codecs="av01.0.05M.08"';

// Capability hint for /api/hls/master: the server serves the h264 ladder
// instead of the AV1-only master when this says no. MediaSource is
// authoritative wherever MSE exists (desktop, Android Chrome); iPhone Safari
// typically has no MSE and plays HLS natively, so the element's own
// canPlayType is the right probe there — it answers "", "maybe" or "probably",
// and only "" means no. `??` not `||`, because isTypeSupported legitimately
// returns false. Anything undeterminable answers NO: h264 always plays, a
// wrong yes is a black screen.
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
