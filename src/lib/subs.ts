import { GET, POST } from '.';

const E = encodeURIComponent;

export default {
  async start (player: any, raw: string, sub: string) {
    const info = await GET(`/api/subs/info?file=${E(raw)}`);
    if (!info || info.vtt) return;

    if (info.srt) {
      this.reload(player, `/subs/${sub}`, 'English');
      return;
    }

    if (!info.embedded?.length) return;
    const L = info.embedded;

    const eng = L.find((t: any) => ['en', 'eng'].includes(t.language)) ?? L[0];
    await POST('/api/subs/extract', {
      file: raw,
      track: eng.index
    });

    this.reload(player, `/subs/${sub}`, 'English');
  },

  async search (raw: string): Promise<any[] | null> {
    let res = await GET(`/api/subs/search?file=${E(raw)}`);

    return res?.results?.length ? res.results : null;
  },

  reload (player: any, src: string, label: string, show = label === 'English') {
    const tracks = player.remoteTextTracks();
    for (let i = tracks.length - 1; i >= 0; i--) {
      if (tracks[i].label === label)
        player.removeRemoteTextTrack(tracks[i]);
    }

    player.addRemoteTextTrack({
      kind: 'captions',
      src, srclang: 'en',
      label,
      default: show
    }, false);

    if (show) {
      setTimeout(() => {
        const all = player.textTracks();
        for (let i = 0; i < all.length; i++) {
          if (all[i].label === label)
            all[i].mode = 'showing';
        }
      }, 100);
    }
  },

  async whisper (raw: string, onMsg: (s: string) => void, onDone: () => void) {
    onMsg('Generating subtitles with Whisper…');
    await POST('/api/subs/whisper', { file: raw });

    const timer = setInterval(async () => {
      const s = await GET(`/api/subs/whisper/status?file=${E(raw)}`);

      if (!s) return;
      if (s.status === 'done') {
        clearInterval(timer);
        onDone();
      } else if (s.status === 'error') {
        clearInterval(timer);
        onMsg('Whisper failed: ' + (s.error || 'unknown'));
      }
    }, 3000);
  },
} as const;
