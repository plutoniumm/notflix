// The room: an ambient field behind every page, driven by 1/f noise rather
// than a looped keyframe. A sine loop is identified as a loop within two
// cycles and then stops being environment and becomes decoration; natural
// light, wind and water all carry 1/f amplitude spectra, and mid-band fractal
// fields are the ones people rate as natural (research.md).
//
// It also carries machine state. When the library is working the field breathes
// on a respiration curve, so the pipeline is legible from any page without a
// progress bar on it — peripheral, pre-attentive, no reading required.

import { api } from "./api";
import { Pink, breath, lerp } from "./noise";

const TICK = 110;

export const room = $state({ busy: false, alarm: false });

export function mountRoom(poll = true) {
  const el = document.documentElement;
  const px = new Pink();
  const py = new Pink();
  const pk = new Pink();

  let x = 0;
  let y = 0;
  let k = 0;
  let t = 0;

  // The field tracks whatever artwork the pointer is over: the room takes its
  // colour from the thing you are reaching for, which is a consequence of your
  // own movement rather than an animation playing at you.
  const onOver = (e: Event) => {
    const hit = (e.target as HTMLElement | null)?.closest?.(
      "[style*='--art-1']",
    ) as HTMLElement | null;
    if (!hit || hit === el) return;

    const a = hit.style.getPropertyValue("--art-1");
    const b = hit.style.getPropertyValue("--art-2");
    if (a) el.style.setProperty("--art-1", a);
    if (b) el.style.setProperty("--art-2", b);
  };

  document.addEventListener("pointerover", onOver, { passive: true });

  const tick = () => {
    if (document.hidden) return;

    t += TICK / 1000;
    // Low-pass the walk: the top octave flips every tick and would read as
    // jitter, which is the one motion the eye refuses to ignore.
    x = lerp(x, px.next(), 0.05);
    y = lerp(y, py.next(), 0.05);
    k = lerp(k, pk.next() * 0.5 + 0.5, 0.05);

    // Travel through the library shifts the field with you (optic flow), so
    // scrolling feels like moving rather than like contents sliding past.
    const flow = Math.min(1, window.scrollY / 2600);
    const live = room.busy ? breath(t) : 0;

    el.style.setProperty("--room-x", (x * 9 + flow * 5).toFixed(3));
    el.style.setProperty("--room-y", (y * 7 - flow * 9).toFixed(3));
    el.style.setProperty("--room-k", (k * 0.35 + live * 0.65).toFixed(3));
    el.style.setProperty("--room-live", live.toFixed(3));
    el.style.setProperty("--room-bad", room.alarm ? "1" : "0");
  };

  tick();
  const timer = setInterval(tick, TICK);

  // One slow probe is the whole cost of knowing the machine's state everywhere.
  let probe: ReturnType<typeof setInterval> | null = null;
  if (poll) {
    const look = async () => {
      if (document.hidden) return;
      const s = await api.processStatus({ silent: true }).catch(() => null);
      room.busy = !!s?.running;
    };

    look();
    probe = setInterval(look, 15000);
  }

  return () => {
    clearInterval(timer);
    if (probe) clearInterval(probe);
    document.removeEventListener("pointerover", onOver);
  };
}
