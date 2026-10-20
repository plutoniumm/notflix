// Client for the command bus (server/tv/bus.go). Shared by both pages: the
// screen subscribes and reports, the remote sends and watches. Rune-free on
// purpose so it stays a plain .ts — the reactive state lives in the components.

import { POST } from "../core/api";

export type Cmd = {
  cmd:
    | "play"
    | "pause"
    | "resume"
    | "seek"
    | "seekBy"
    | "stop"
    | "volume"
    | "unmute"
    | "fullscreen"
    | "sub";
  file?: string;
  t?: number;
  d?: number;
  v?: number;
  // Index into State.subs, or -1 for off.
  sub?: number;
};

export type State = {
  file: string;
  title: string;
  pos: number;
  dur: number;
  paused: boolean;
  volume: number;
  screens: number;
  muted?: boolean;
  full?: boolean;
  // The tracks this title has and which is showing (-1 = off). The screen is
  // the only thing that knows what actually loaded, so it reports them.
  subs?: string[];
  sub?: number;
};

export const EMPTY: State = {
  file: "",
  title: "",
  pos: 0,
  dur: 0,
  paused: true,
  muted: false,
  full: false,
  subs: [],
  sub: -1,
  volume: 1,
  screens: 0,
};

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// Returns how many screens got it. Zero is the answer the remote needs to
// show ("no screen listening"), not an error to swallow.
export async function send(cmd: Cmd): Promise<number> {
  const res = await POST("/bus/cmd", cmd, { tag: "tv" });

  return res?.screens ?? 0;
}

// Fire-and-forget, deliberately not through core/api: this runs about once a
// second and api.ts would log or toast on every failure, so a TV that lost the
// network for a minute would bury the page in noise. keepalive lets the last
// report survive the page going away.
export function report(state: State) {
  try {
    fetch("/bus/state", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(state),
      keepalive: true,
    }).catch(() => {});
  } catch {}
}

// onStatus reports whether the stream is currently up, which is all the screen
// needs for its "connected" hint.
export function subscribe(
  onCmd: (c: Cmd) => void,
  onStatus?: (up: boolean) => void,
): () => void {
  let es: EventSource | null = null;
  let timer: any = 0;
  let stopped = false;

  const open = () => {
    es = new EventSource("/bus/events");

    es.onopen = () => onStatus?.(true);

    es.onmessage = (e) => {
      try {
        onCmd(JSON.parse(e.data) as Cmd);
      } catch (err) {
        console.warn("[tv] bad command", e.data, err);
      }
    };

    es.onerror = () => {
      onStatus?.(false);
      // An EventSource retries a dropped connection itself (the server even
      // sends `retry: 2000`), but a non-2xx closes it for good. A TV that woke
      // while the server was restarting would then never come back, so the
      // CLOSED case is reopened by hand.
      if (stopped || !es || es.readyState !== EventSource.CLOSED) return;
      es.close();
      clearTimeout(timer);
      timer = setTimeout(open, 3000);
    };
  };

  open();

  return () => {
    stopped = true;
    clearTimeout(timer);
    es?.close();
    onStatus?.(false);
  };
}

// Long-polls /bus/state. The first request carries no rev (take whatever is
// there); every later one passes the rev we have, and the server holds up to
// 25s for something newer. A same-rev answer is the server's timeout, not a
// change, so it re-asks without calling back.
export function watchState(onState: (s: State) => void): () => void {
  let stopped = false;
  let ctl: AbortController | null = null;

  (async () => {
    let rev: number | null = null;
    let backoff = 1000;

    while (!stopped) {
      ctl = new AbortController();
      try {
        const res = await fetch(
          rev === null ? "/bus/state" : `/bus/state?rev=${rev}`,
          { signal: ctl.signal, cache: "no-store" },
        );
        if (!res.ok) throw new Error(`HTTP ${res.status}`);

        const body = await res.json();
        backoff = 1000;

        if (body.rev !== rev) {
          rev = body.rev;
          onState({ ...EMPTY, ...body.state } as State);
        }
      } catch {
        if (stopped) return;
        await sleep(backoff);
        backoff = Math.min(backoff * 2, 15000);
      }
    }
  })();

  return () => {
    stopped = true;
    ctl?.abort();
  };
}
