import { GET, POST } from '.';

const E = encodeURIComponent;

export type SubsInfo = {
  vtt: boolean;
  srt: boolean;
  embedded: Array<{ index: number; language: string }>;
  whisper: boolean;
};

export type WhisperCue = { start: number; end: number; text: string };

export default {
  async start(player: any, raw: string, sub: string): Promise<SubsInfo | null> {
    const info: SubsInfo | null = await GET(`/api/subs/info?file=${E(raw)}`);
    if (!info) return null;

    if (!info.vtt) {
      if (info.srt) {
        this.reload(player, `/subs/${sub}`, 'English');
      } else if (info.embedded?.length) {
        const L = info.embedded;
        const eng = L.find((t) => ['en', 'eng'].includes(t.language)) ?? L[0];
        await POST('/api/subs/extract', { file: raw, track: eng.index });
        this.reload(player, `/subs/${sub}`, 'English');
      } else if (info.whisper) {
        const wsub = sub.replace('.vtt', '.whisper.vtt');
        this.reload(player, `/subs/${wsub}`, 'Whisper', true);
      }
    }

    return info;
  },

  async extractEmbedded(player: any, raw: string, sub: string, index: number): Promise<void> {
    await POST('/api/subs/extract', { file: raw, track: index });
    this.reload(player, `/subs/${sub}`, 'English');
  },

  async search(raw: string): Promise<any[] | null> {
    let res = await GET(`/api/subs/search?file=${E(raw)}`);
    return res?.results?.length ? res.results : null;
  },

  reload(player: any, src: string, label: string, show = label === 'English') {
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

  whisperStream(
    raw: string,
    sub: string,
    onMsg: (s: string) => void,
    onCue: (cue: WhisperCue) => void,
    onDone: () => void,
  ) {
    onMsg('Generating subtitles with Whisper…');

    let count = 0;
    const url = `/api/subs/whisper/stream?file=${E(raw)}`;
    const es = new EventSource(url);

    es.onmessage = (e) => {
      let d: any;
      try { d = JSON.parse(e.data); } catch {
        return;
      }

      if (d.translating) {
        onMsg('Translating with Ollama…');
        return;
      }
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

      onCue({ start: d.start, end: d.end, text: d.text });
      onMsg(`Whisper: ${++count} segments…`);
    };

    es.onerror = () => {
      es.close();
      onMsg('Whisper stream error');
    };
  },
} as const;
