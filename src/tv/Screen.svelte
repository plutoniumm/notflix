<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import videojs from "video.js";
  import "video.js/dist/video-js.css";
  import { api } from "../core/api";
  import { dur } from "../core/fmt";
  import { clean } from "../core/video";
  import { report, subscribe, type Cmd } from "./bus";

  const E = encodeURIComponent;

  let videoEl: HTMLVideoElement;
  let player: any = null;

  let file = $state("");
  let title = $state("");
  let pos = $state(0);
  let len = $state(0);
  let paused = $state(true);
  let muted = $state(false);
  let live = $state(false);
  let needGesture = $state(false);
  let err = $state("");
  let flash = $state(false);

  // Never rendered, so plain lets: where a freshly-set src has to land, the
  // one-shot MP4 -> HLS fallback latch, and a sequence so a slow subtitle
  // lookup cannot attach its track to the file that replaced it.
  let seekTo = 0;
  let fellBack = false;
  let subSeq = 0;
  let flashTimer: any = 0;

  // The label is up while paused (someone is looking at a still frame) and for
  // a few seconds after anything changes. A TV in a dark room gets nothing
  // else.
  const show = $derived(!!err || (!!file && (flash || paused)));

  const fin = (n: number) => (Number.isFinite(n) ? n : 0);
  const clamp01 = (n: number) => Math.min(1, Math.max(0, n));

  function bump() {
    flash = true;
    clearTimeout(flashTimer);
    flashTimer = setTimeout(() => (flash = false), 4000);
  }

  function push() {
    report({
      file,
      title,
      pos: fin(player?.currentTime()),
      dur: fin(player?.duration()),
      paused: player ? player.paused() : true,
      volume: player ? clamp01(fin(player.volume())) : 1,
      subs,
      sub,
      muted: player ? !!player.muted() : false,
      full: isFull(),
      screens: 0,
    });
  }

  function safePlay() {
    const p = player?.play();

    p?.catch?.(() => {
      // Autoplay with sound needs a prior gesture and this page never had one.
      // Muted is the only way to get pixels up; any key or click (a TV remote's
      // OK arrives as one) unmutes, and so does the first volume command.
      try {
        player.muted(true);
        muted = true;
        needGesture = true;
        player.play()?.catch?.(() => (err = "Press OK to start"));

        // Try to undo it once pixels are actually moving. The policy gates
        // STARTING playback, not unmuting something already running, so on most
        // engines this just works and the room gets sound without anyone
        // touching the TV. Where it does not, the browser pauses us instead —
        // so check, and put the mute back rather than leaving a dead picture.
        setTimeout(() => {
          if (!player || player.paused() || !player.muted()) return;
          player.muted(false);
          const stillOk = !player.paused();
          if (stillOk) {
            muted = false;
            needGesture = false;
          } else {
            player.muted(true);
            player.play()?.catch?.(() => {});
          }
          push();
        }, 900);
      } catch {
        err = "Press OK to start";
      }
    });
  }

  // Before loadedmetadata a currentTime() write is dropped, so it is remembered
  // and replayed instead.
  function seekAbs(t: number) {
    const n = Math.max(0, t);
    if (!player || player.readyState() < 1) {
      seekTo = n;
      return;
    }
    player.currentTime(n);
  }

  function clearTracks() {
    const tracks = player?.remoteTextTracks();
    if (!tracks) return;
    for (let i = tracks.length - 1; i >= 0; i--)
      player.removeRemoteTextTrack(tracks[i]);
  }

  // The direct MP4 first: compat-mode titles are stored H.264 and a TV plays
  // those with no transcoding at all. The HLS fallback below covers everything
  // else.
  function start(rel: string, at = 0) {
    file = rel;
    title = clean(rel.slice(rel.lastIndexOf("/") + 1)) || rel;
    err = "";
    pos = at;
    len = 0;
    seekTo = at > 0 ? at : 0;
    fellBack = false;
    needGesture = false;
    subSeq++;
    clearTracks();
    bump();

    // Ask the server what this device should be given. Handing a stick a 4K
    // 10-bit AV1 file does not produce a media error it can fall back from —
    // the software decoder exhausts memory and takes the whole page with it,
    // which is what happened on Obsession. The server has the probe data, so
    // the decision belongs there.
    const seq = subSeq;
    api
      .tvPlan(rel, caps(), { silent: true })
      .then((plan: any) => {
        if (seq !== subSeq || !player) return;
        const src = plan?.src ?? `/video/${encodeURI(rel)}`;
        const type = plan?.type ?? "video/mp4";
        // A ladder plan IS the fallback, so do not fall back from it again.
        fellBack = !plan?.direct;
        player.src({ src, type });
        safePlay();
      })
      .catch(() => {
        if (seq !== subSeq || !player) return;
        player.src({ src: `/video/${encodeURI(rel)}`, type: "video/mp4" });
        safePlay();
      });

    loadSubs(rel, subSeq);
  }

  function stop() {
    subSeq++;
    clearTracks();
    try {
      player?.pause();
      player?.reset();
    } catch {}

    file = "";
    title = "";
    pos = 0;
    len = 0;
    err = "";
    seekTo = 0;
    needGesture = false;
    flash = false;
    clearTimeout(flashTimer);
  }

  function handle(c: Cmd) {
    if (!player) return;

    switch (c.cmd) {
      case "play":
        if (c.file) start(c.file, c.t ?? 0);
        else if (file) {
          if (c.t !== undefined) seekAbs(c.t);
          bump();
          safePlay();
        }
        break;

      case "resume":
        if (file) {
          bump();
          safePlay();
        }
        break;

      case "pause":
        player.pause();
        break;

      case "seek":
        if (file) {
          seekAbs(c.t ?? 0);
          bump();
        }
        break;

      case "seekBy":
        if (file) {
          const next = fin(player.currentTime()) + (c.d ?? 0);
          const end = fin(player.duration());
          seekAbs(end > 0 ? Math.min(next, end) : next);
          bump();
        }
        break;

      case "stop":
        stop();
        break;

      // A relayed tap is a deliberate act, but the browser cannot know that —
      // the autoplay policy only counts gestures in THIS document. Unmuting
      // something already playing is allowed where starting it was not, so this
      // usually works; where it does not, the engine pauses us instead, so put
      // the mute back rather than leaving a dead picture.
      case "sub":
        if (c.sub !== undefined && c.sub !== null) pickSub(c.sub);
        break;

      case "fullscreen":
        toggleFull();
        bump();
        break;

      case "unmute": {
        player.muted(false);
        muted = false;
        needGesture = false;
        if (player.paused()) {
          player.muted(true);
          muted = true;
          needGesture = true;
          player.play()?.catch?.(() => {});
        }
        bump();
        break;
      }

      case "volume": {
        const v = clamp01(c.v ?? 1);
        player.volume(v);
        if (v > 0 && player.muted()) {
          player.muted(false);
          muted = false;
          needGesture = false;
        }
        bump();
        break;
      }
    }

    push();
  }

  // Nice-to-have, not the point: first English local track, else the first
  // local, else the plain sidecar (asking for .vtt also converts a .srt server
  // side), else whisper.
  // Every track this title has, in the order the remote sees them.
  let subs = $state<string[]>([]);
  let sub = $state(-1);

  // Loads ALL available tracks rather than guessing one. A TV has no subtitle
  // menu to go and correct a wrong guess with, so the correction has to live on
  // the phone — and that means the phone needs the whole list.
  async function loadSubs(rel: string, seq: number) {
    subs = [];
    sub = -1;

    const info = await api.subs.info(rel, { silent: true });
    if (!info || seq !== subSeq || !player) return;

    const dir = rel.slice(0, rel.lastIndexOf("/") + 1);
    const base = rel.replace(/\.[^./]+$/, "");
    const locals: { file: string; language: string }[] = info.local ?? [];

    const found: { label: string; src: string; lang: string }[] = locals.map((t) => ({
      label: (t.language || "sub").toUpperCase(),
      src: `${dir}${t.file}`,
      lang: t.language || "und",
    }));

    if (!found.length && (info.vtt || info.srt)) {
      found.push({ label: "SUBS", src: `${base}.vtt`, lang: "und" });
    }
    if (!found.length && info.whisper) {
      found.push({ label: "WHISPER", src: `${base}.whisper.vtt`, lang: "und" });
    }
    if (!found.length) {
      push();

      return;
    }

    for (const f of found) {
      player.addRemoteTextTrack(
        { kind: "captions", src: `/subs/${encodeURI(f.src)}`, srclang: f.lang, label: f.label },
        false,
      );
    }

    subs = found.map((f) => f.label);
    // English first if it exists: it is the only sane default for a library
    // whose sidecars arrive in whatever order a release group felt like.
    const en = found.findIndex((f) => /^en/i.test(f.lang));
    pickSub(en >= 0 ? en : 0);
  }

  // `default` only MARKS a track as default — video.js attaches it with mode
  // "disabled", so the cues load and nothing renders. Setting mode is the only
  // thing that actually turns a track on, and some engines reset it when the
  // cues finish loading, hence the retries.
  function pickSub(i: number) {
    sub = i;

    const apply = () => {
      const tracks = player?.remoteTextTracks();
      if (!tracks) return;
      for (let n = 0; n < tracks.length; n++) {
        try {
          tracks[n].mode = n === i ? "showing" : "disabled";
        } catch {}
      }
    };

    apply();
    setTimeout(apply, 400);
    setTimeout(apply, 1500);
    push();
  }

  // Only ever rescues a blocked autoplay. A stray keypress must not resume
  // something the remote deliberately paused.
  // A TV remote's NUMBER keys never reach the page — the firmware eats them for
  // channel entry. What does get through is the d-pad, OK and Back. So the
  // controls are real buttons and you move between them, which is the only
  // interaction a television has ever had.
  //
  // Focus is tracked here rather than left to the DOM: TV browsers have erratic
  // focus models, and a highlight we draw ourselves cannot be lost to one.
  let ctrls = $state(false);
  // Paused means stopped-and-looking, which is exactly when the controls should
  // be on screen — and the pause usually arrives from the remote, which never
  // touched showCtrls(). Deriving it means no caller has to remember.
  const ctrlsOn = $derived(ctrls || (!!file && paused));
  let pick = $state(0);
  let ctrlTimer: ReturnType<typeof setTimeout>;

  function showCtrls() {
    ctrls = true;
    clearTimeout(ctrlTimer);
    ctrlTimer = setTimeout(() => (ctrls = false), 8000);
  }

  let stage: HTMLDivElement | undefined;

  // Some builds of Silk deliver only keyup for the d-pad. Taking both and
  // ignoring the repeat is cheaper than finding out which build this is.
  let lastKeyAt = 0;
  function onKeyUp(e: KeyboardEvent) {
    if (performance.now() - lastKeyAt < 400) return;
    onKey(e);
  }

  // Probed once and cached: isTypeSupported is cheap but this runs per title,
  // and the answer cannot change mid-session.
  let capCache: { av1: boolean; p10: boolean } | null = null;

  function supports(type: string): boolean {
    try {
      const ms = (window as any).MediaSource;
      if (ms?.isTypeSupported) return !!ms.isTypeSupported(type);

      return document.createElement("video").canPlayType(type) !== "";
    } catch {
      return false;
    }
  }

  // The server decides WHAT to send; this only reports what the panel claims it
  // can take. A TV browser's answer is optimistic — it tells you it knows the
  // codec, not that it can sustain the stream — so the server keeps its own
  // resolution cap regardless of what we say here.
  function caps() {
    capCache ??= {
      av1: supports('video/mp4; codecs="av01.0.08M.08"'),
      p10: supports('video/mp4; codecs="av01.0.08M.10"'),
    };

    return capCache;
  }

  function isFull(): boolean {
    const d = document as any;

    return !!(d.fullscreenElement || d.webkitFullscreenElement || d.webkitIsFullScreen);
  }

  function toggleFull() {
    try {
      const el = document.documentElement as any;
      if (document.fullscreenElement) {
        (document as any).exitFullscreen?.() ?? (document as any).webkitExitFullscreen?.();
      } else {
        el.requestFullscreen?.() ?? el.webkitRequestFullscreen?.();
      }
    } catch {}
  }

  function playPause() {
    if (!player) return;

    // A press on the TV IS the gesture the autoplay policy wanted, so this is
    // the one moment sound is certain to be allowed. Take it.
    if (player.muted()) {
      player.muted(false);
      muted = false;
      needGesture = false;
    }

    if (player.paused()) player.play()?.catch?.(() => {});
    else player.pause();
    push();
  }

  function nudge(by: number) {
    if (!player) return;
    player.currentTime(Math.max(0, fin(player.currentTime()) + by));
    push();
  }

  const ACTIONS = [
    { id: "back", label: "Back 10", run: () => nudge(-10) },
    { id: "play", label: "Play", run: playPause },
    { id: "fwd", label: "Forward 10", run: () => nudge(10) },
    { id: "full", label: "Full screen", run: toggleFull },
    { id: "stop", label: "Stop", run: () => stop() },
  ];

  // A Fire TV remote sends the d-pad, OK, Back and the transport keys. Silk
  // reports some of them with a non-standard `key`, so keyCode is checked too —
  // and it delivers none of them unless something in the page holds focus,
  // which is why the stage is focusable and takes focus on mount.
  const KEYCODE: Record<number, string> = {
    13: "Enter",
    27: "Escape",
    8: "Escape",
    37: "ArrowLeft",
    38: "ArrowUp",
    39: "ArrowRight",
    40: "ArrowDown",
    179: "PlayPause",
    227: "Rewind",
    228: "Forward",
    415: "Play",
    19: "Pause",
  };

  function keyOf(e: KeyboardEvent): string {
    const k = e.key;
    if (k && k !== "Unidentified") {
      if (k === "MediaPlayPause" || k === "MediaPlay" || k === "MediaPause") return "PlayPause";
      if (k === "MediaRewind") return "Rewind";
      if (k === "MediaFastForward") return "Forward";
      if (k === "Select" || k === "Accept") return "Enter";
      if (k === "Back" || k === "BrowserBack" || k === "GoBack") return "Escape";

      return k;
    }

    return KEYCODE[e.keyCode] ?? "";
  }

  function onKey(e: KeyboardEvent) {
    // Capture BEFORE wake clears it: when autoplay was blocked, wake() both
    // unmutes and resumes, so letting the key fall through would pause the very
    // thing the viewer just pressed to hear.
    lastKeyAt = performance.now();
    const rescued = needGesture;
    wake();

    // The first press only summons the controls. Pressing OK on a screen with
    // nothing on it should not fire whatever happens to be under the cursor.
    const wasHidden = !ctrls;
    showCtrls();
    if (rescued || wasHidden) return;

    switch (keyOf(e)) {
      // The stick's own transport keys do the obvious thing without anyone
      // having to find a button first.
      case "PlayPause":
      case "Play":
      case "Pause":
        playPause();
        return;
      case "Rewind":
        nudge(-10);
        return;
      case "Forward":
        nudge(10);
        return;

      case "ArrowLeft":
        pick = (pick + ACTIONS.length - 1) % ACTIONS.length;
        break;
      case "ArrowRight":
        pick = (pick + 1) % ACTIONS.length;
        break;
      case "Enter":
      case " ":
        ACTIONS[pick]?.run();
        break;
      case "Escape":
        if (isFull()) toggleFull();
        else ctrls = false;
        break;
    }
  }

  // Silk has a pointer, so a click must do what a key does: summon the
  // controls. Previously only onKey called showCtrls, so clicking anywhere —
  // including on a control that was not yet drawn — did nothing at all.
  function onClick() {
    const rescued = needGesture;
    wake();
    showCtrls();
    if (rescued) return;
  }

  function wake() {
    if (!needGesture || !player) return;

    needGesture = false;
    try {
      player.muted(false);
      muted = false;
    } catch {}
    err = "";
    safePlay();
  }

  onMount(() => {
    player = videojs(videoEl, {
      controls: false,
      controlBar: false,
      bigPlayButton: false,
      errorDisplay: false,
      textTrackSettings: false,
      preload: "auto",
      fill: true,
      playsinline: true,
      html5: { vhs: { limitRenditionByPlayerDimensions: false } },
    });

    player.on("loadedmetadata", () => {
      len = fin(player.duration());
      if (seekTo > 0) {
        player.currentTime(seekTo);
        seekTo = 0;
      }
      push();
    });

    player.on("timeupdate", () => (pos = fin(player.currentTime())));
    player.on("durationchange", () => (len = fin(player.duration())));
    player.on("play", () => {
      paused = false;
      bump();
      push();
    });
    // The rescue prompt asks for a gesture. Once pixels are moving the ask is
    // answered, whoever answered it — leaving it up turns a one-time nudge into
    // a permanent label across the picture.
    player.on("playing", () => {
      err = "";
      needGesture = false;
      push();
    });
    player.on("pause", () => {
      paused = true;
      push();
    });
    player.on("seeked", () => {
      pos = fin(player.currentTime());
      push();
    });
    player.on("ended", () => {
      paused = true;
      push();
    });
    player.on("volumechange", () => {
      muted = !!player.muted();
      push();
    });

    // One retry, onto the h264 ladder. `av1=0` is not optional: TVs routinely
    // have no AV1 decoder, and an AV1 master with no alternate is a black
    // screen — which is the whole reason this app transcodes at all.
    player.on("error", () => {
      const e = player.error();
      if (!e || !file) return;

      if (!fellBack) {
        fellBack = true;
        seekTo = Math.max(fin(player.currentTime()), seekTo, pos);
        player.src({
          src: `/api/hls/master?file=${E(file)}&av1=0`,
          type: "application/vnd.apple.mpegurl",
        });
        safePlay();
        return;
      }

      err = e.message || "Playback error";
      push();
    });

    const unsub = subscribe(handle, (up) => (live = up));
    const tick = setInterval(() => {
      if (player && !player.paused()) push();
    }, 1000);

    // Silk routes the d-pad to its own chrome unless something in the page
    // holds focus, which is why nothing reached onKey on the Fire TV. Claim it
    // on mount, and again whenever the page is shown.
    const grab = () => stage?.focus?.();
    grab();
    const onVis = () => { if (!document.hidden) grab(); };
    document.addEventListener("visibilitychange", onVis);

    // Say "here, idle" straight away: a screen that reloaded mid-session would
    // otherwise leave the remote scrubbing a title nothing is playing.
    push();

    return () => {
      unsub();
      clearInterval(tick);
      document.removeEventListener("visibilitychange", onVis);
    };
  });

  onDestroy(() => {
    clearTimeout(flashTimer);
    try {
      player?.dispose();
    } catch {}
  });
</script>

<!-- ONE listener, on window. The stage below is focusable because Silk will not
     route the d-pad to a page that holds no focus, but it must NOT also handle
     the key: the event bubbles from it to window, so a handler on both ran
     everything twice — fullscreen entered and instantly exited, play/pause
     toggled back to where it started, and the whole thing read as jank. -->
<svelte:window onkeydown={onKey} onkeyup={onKeyUp} onclick={onClick} />

<!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
<div
  class="scr"
  bind:this={stage}
  tabindex="-1"
  role="application"
>
  <div class="vid">
    <!-- svelte-ignore a11y_media_has_caption -->
    <video bind:this={videoEl} class="video-js" playsinline preload="auto"></video>
  </div>

  {#if show}
    <div class="ovl">
      <div class="ttl">{title}</div>
      <div class="sub">
        {#if err}
          {err}
        {:else}
          {dur(pos)} / {dur(len)}{#if muted} · muted{/if}
        {/if}
      </div>
    </div>
  {/if}

  {#if ctrlsOn}
    <!-- Real buttons, moved between with the d-pad and fired with OK. Big
         because a television is read from across a room, and highlighted by a
         fill rather than an outline so the selection survives a panel with
         crushed blacks. -->
    <div class="pad">
      {#if !file}
        <div class="idle">Nothing playing — pick something on your phone</div>
      {/if}

      <div class="keys">
        {#each ACTIONS as a, i}
          <button
            class="key"
            class:on={i === pick}
            disabled={!file && a.id !== "stop"}
            onclick={() => { pick = i; a.run(); showCtrls(); }}
          >
            <span class="g">
              {#if a.id === "play"}{paused ? "▶" : "❚❚"}
              {:else if a.id === "back"}⏪
              {:else if a.id === "fwd"}⏩
              {:else if a.id === "full"}⛶
              {:else}✕{/if}
            </span>
            <span class="lbl">{a.id === "play" ? (paused ? "Play" : "Pause") : a.label}</span>
          </button>
        {/each}
      </div>

      <div class="hint">Move with ◀ ▶ · press OK</div>
    </div>
  {/if}

  <i class="dot" class:live></i>
</div>

<style>
  /* Overscan: a TV clips roughly 3% of every edge, some more. 6% keeps this
     clear on the worst panel. */
  .pad {
    position: absolute;
    left: 6%;
    right: 6%;
    bottom: 6%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 3vh;
    padding: 4vh 3vw;
    background: rgba(0, 0, 0, 0.74);
    -webkit-backdrop-filter: blur(12px);
    backdrop-filter: blur(12px);
    border-radius: 1.5vw;
  }

  .idle {
    font-size: 2.4vw;
    color: rgba(255, 255, 255, 0.82);
    text-align: center;
  }

  .keys {
    display: flex;
    gap: 3vw;
  }

  .key {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1vh;
    min-width: 13vw;
    padding: 2.4vh 1vw;
    border: none;
    border-radius: 1.2vw;
    background: rgba(255, 255, 255, 0.1);
    color: rgba(255, 255, 255, 0.72);
    font: inherit;
    cursor: none;
  }

  /* A FILL, not an outline: a 2px ring disappears on a panel with crushed
     blacks or heavy sharpening, and then nothing on screen says where you are. */
  .key.on {
    background: #fff;
    color: #000;
    transform: scale(1.06);
  }

  .key:disabled {
    opacity: 0.3;
  }

  .key .g {
    font-size: 4.2vw;
    line-height: 1;
  }

  .key .lbl {
    font-size: 1.6vw;
    font-weight: 600;
  }

  .hint {
    font-size: 1.5vw;
    color: rgba(255, 255, 255, 0.5);
  }

  .scr {
    position: fixed;
    inset: 0;
    overflow: hidden;
    background: #000;
    color: #fff;
    cursor: none;
    font-family: system-ui, -apple-system, sans-serif;
    -webkit-font-smoothing: antialiased;
  }

  .vid,
  .vid :global(.video-js),
  .vid :global(.vjs-tech) {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    background: #000;
  }

  .vid :global(.vjs-tech) {
    object-fit: contain;
  }

  /* Panels clip roughly 3% per edge, so nothing that has to be read sits
     nearer than 4% — the label, the hint and the cue box all respect it. */
  .ovl {
    position: absolute;
    left: 4%;
    right: 4%;
    bottom: 4%;
    opacity: 0.74;
    text-shadow: 0 2px 22px #000;
    animation: in 0.45s var(--ease-out, ease) both;
  }

  .ttl {
    font-size: clamp(20px, 2.7vw, 64px);
    font-weight: 600;
    letter-spacing: -0.015em;
    line-height: 1.15;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub {
    margin-top: 0.3em;
    font-size: clamp(13px, 1.5vw, 34px);
    font-variant-numeric: tabular-nums;
    opacity: 0.66;
  }

  /* The only thing on an idle screen. Visible means the bus is connected; it
     does not pulse and it does not glow. */
  .dot {
    position: absolute;
    top: 4%;
    right: 4%;
    width: 0.5vw;
    height: 0.5vw;
    min-width: 6px;
    min-height: 6px;
    border-radius: 50%;
    background: #fff;
    opacity: 0.03;
    transition: opacity 1.4s ease;
  }

  .dot.live {
    opacity: 0.1;
  }

  .scr :global(.vjs-text-track-display) {
    inset: 0 4% 5% 4% !important;
  }

  /* video.js writes the cue styles inline, so only !important reaches them. */
  .scr :global(.vjs-text-track-cue > div) {
    font-size: clamp(16px, 2.1vw, 48px) !important;
    line-height: 1.3 !important;
    background: rgba(0, 0, 0, 0.44) !important;
    text-shadow: 0 2px 10px #000 !important;
  }

  @keyframes in {
    from {
      opacity: 0;
      translate: 0 0.4em;
    }
  }
</style>
