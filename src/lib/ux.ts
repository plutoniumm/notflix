export function Touch (player: any, el: HTMLElement) {
  let [last, prev] = [0, 0];

  el.addEventListener('touchend', (e) => {
    const now = Date.now();
    const touch = e.changedTouches[0];
    const x = touch.clientX;
    const w = el.clientWidth;

    if (
      now - last < 300
      && Math.abs(x - prev) < 80
    ) {
      e.preventDefault();
      if (x < w / 3) {
        seek(player, -10);
        ripple(el, x, touch.clientY, '−10s');
      } else if (x > (2 * w) / 3) {
        seek(player, 10);
        ripple(el, x, touch.clientY, '+10s');
      } else {
        player.paused() ? player.play() : player.pause();
      }
      last = 0;
    } else {
      last = now;
      prev = x;
    }
  }, { passive: false });
}

function ripple (el: HTMLElement, x: number, y: number, label: string) {
  const r = document.createElement('div');
  r.className = 'tap-ripple';
  r.textContent = label;
  r.style.cssText = `left:${x}px;top:${y}px`;
  el.appendChild(r);

  setTimeout(() => r.remove(), 700);
}

export function Hotkeys (
  player: any,
  container: HTMLElement,
  onNext: () => void,
  onWhisper: () => void
) {
  document.addEventListener('keydown', (e) => {
    if ((e.target as HTMLElement).tagName === 'INPUT') return

    const key = e.key
    const lkey = key.toLowerCase()

    if (key === 'ArrowRight' && e.shiftKey) {
      seek(player, 30)
    } else if (key === 'ArrowRight' && e.altKey) {
      seek(player, 0.1)
    } else if (key === 'ArrowRight') {
      seek(player, 5)
    } else if (key === 'ArrowLeft' && e.shiftKey) {
      seek(player, -30)
    } else if (key === 'ArrowLeft' && e.altKey) {
      seek(player, -0.1)
    } else if (key === 'ArrowLeft') {
      seek(player, -5)
    } else if (key === 'ArrowUp') {
      e.preventDefault()
      player.volume(Math.min(1, Math.round((player.volume() + 0.1) * 10) / 10))
    } else if (key === 'ArrowDown') {
      e.preventDefault()
      player.volume(Math.max(0, Math.round((player.volume() - 0.1) * 10) / 10))
    } else if (key === ' ') {
      e.preventDefault()
      player.paused() ? player.play() : player.pause()
    } else if (lkey === 'f') {
      document.fullscreenElement ? document.exitFullscreen() : container.requestFullscreen()
    } else if (lkey === 'm') {
      player.muted(!player.muted())
    } else if (lkey === 'd') {
      player.playbackRate(Math.min(4, Math.round((player.playbackRate() + 0.1) * 10) / 10))
    } else if (lkey === 's') {
      player.playbackRate(Math.max(0.1, Math.round((player.playbackRate() - 0.1) * 10) / 10))
    } else if (lkey === 'w') {
      onWhisper()
    } else if (lkey === 'l') {
      window.location.href = '/'
    } else if (lkey === 'n') {
      onNext()
    } else if (lkey === 'p') {
      if (document.pictureInPictureElement) {
        document.exitPictureInPicture()
      } else {
        player.requestPictureInPicture?.()
      }
    } else if (lkey === 'b') {
      player.el()?.querySelector('video')?.classList.toggle('bright')
    } else if (lkey === 'c') {
      const tracks = player.textTracks()
      for (let i = 0; i < tracks.length; i++) {
        const t = tracks[i]
        if (t.kind === 'captions' || t.kind === 'subtitles') {
          t.mode = t.mode === 'showing' ? 'hidden' : 'showing'
        }
      }
    } else if (key >= '0' && key <= '9') {
      player.currentTime(player.duration() * parseInt(key) * 0.1)
    }
  })
}

function seek (player: any, n: number) {
  const t = player.currentTime() ?? 0
  const d = player.duration() ?? 0

  player.currentTime(Math.max(0, Math.min(d - 0.1, t + n)))
}