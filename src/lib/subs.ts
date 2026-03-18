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

  whisperStream (raw: string, player: any, onMsg: (s: string) => void, onDone: () => void) {
    onMsg('Generating subtitles with Whisper…');

    const tracks = player.textTracks();
    for (let i = tracks.length - 1; i >= 0; i--) {
      if (tracks[i].label === 'Whisper')
        player.removeRemoteTextTrack(tracks[i]);
    }

    player.addRemoteTextTrack({
      kind: 'captions',
      src: '',
      srclang: 'en',
      label: 'Whisper',
      default: true
    }, false);
    let track: TextTrack | null = null;

    setTimeout(() => {
      const all = player.textTracks();
      for (let i = 0; i < all.length; i++) {
        if (all[i].label === 'Whisper') { track = all[i]; track!.mode = 'showing'; }
      }
    }, 100);

    let count = 0;
    const es = new EventSource(`/api/subs/whisper/stream?file=${E(raw)}`);
    es.onmessage = (e) => {
      const d = JSON.parse(e.data);
      if (d.done) {
        es.close();
        onDone();
        return;
      }
      if (d.error) {
        es.close();
        onMsg('Whisper error: ' + d.error);
        return;
      }
      if (track)
        track.addCue(new VTTCue(d.start, d.end, d.text));

      onMsg(`Whisper: ${++count} segments…`);
    };
    es.onerror = () => {
      es.close();
      onMsg('Whisper stream error');
    };
  },
} as const;
