import { GET, POST } from '../core/api';

const E = encodeURIComponent;

export type LocalTrack = { file: string; language: string };

export type SubsInfo = {
  vtt: boolean;
  srt: boolean;
  embedded: Array<{ index: number; language: string }>;
  whisper: boolean;
  local: LocalTrack[];
};

export type WhisperCue = { start: number; end: number; text: string };

const langNames: Record<string, string> = {
  eng: 'English', en: 'English',
  spa: 'Spanish', es: 'Spanish',
  fre: 'French', fra: 'French', fr: 'French',
  ger: 'German', deu: 'German', de: 'German',
  ita: 'Italian', it: 'Italian',
  por: 'Portuguese', pt: 'Portuguese',
  rus: 'Russian', ru: 'Russian',
  jpn: 'Japanese', ja: 'Japanese',
  kor: 'Korean', ko: 'Korean',
  chi: 'Chinese', zho: 'Chinese', zh: 'Chinese',
  ara: 'Arabic', ar: 'Arabic',
  hin: 'Hindi', hi: 'Hindi',
  tur: 'Turkish', tr: 'Turkish',
  pol: 'Polish', pl: 'Polish',
  dut: 'Dutch', nld: 'Dutch', nl: 'Dutch',
  swe: 'Swedish', sv: 'Swedish',
  nor: 'Norwegian', no: 'Norwegian',
  dan: 'Danish', da: 'Danish',
  fin: 'Finnish', fi: 'Finnish',
  tha: 'Thai', th: 'Thai',
  vie: 'Vietnamese', vi: 'Vietnamese',
  ind: 'Indonesian', id: 'Indonesian',
  may: 'Malay', msa: 'Malay', ms: 'Malay',
  rum: 'Romanian', ron: 'Romanian', ro: 'Romanian',
  hun: 'Hungarian', hu: 'Hungarian',
  ces: 'Czech', cze: 'Czech', cs: 'Czech',
  heb: 'Hebrew', he: 'Hebrew',
  sdh: 'English (SDH)',
  und: 'Unknown',
  whisper: 'Whisper',
};

export function langLabel(code: string): string {
  if (!code) return 'Subtitles';
  const base = code.replace(/\d+$/, '');
  const name = langNames[base] ?? code.toUpperCase();
  const num = code.replace(base, '');
  return num ? `${name} (${num})` : name;
}

function subDir(videoParam: string): string {
  const i = videoParam.lastIndexOf('/');
  return i === -1 ? '' : videoParam.slice(0, i + 1);
}

export default {
  async start(player: any, raw: string, sub: string): Promise<SubsInfo | null> {
    const info: SubsInfo | null = await GET(`/api/subs/info?file=${E(raw)}`);
    if (!info) return null;

    const dir = subDir(raw);

    if (info.local?.length) {
      const engIdx = info.local.findIndex((t) =>
        ['eng', 'en', 'english', 'sdh'].includes(t.language.replace(/\d+$/, '')));
      info.local.forEach((t, i) => {
        const isDefault = engIdx >= 0 ? i === engIdx : i === 0;
        const label = langLabel(t.language);
        this.reload(player, `/subs/${dir}${t.file}`, label, isDefault);
      });
    } else if (info.vtt) {
      this.reload(player, `/subs/${sub}`, 'Subtitles', true);
    } else if (info.srt) {
      this.reload(player, `/subs/${sub}`, 'Subtitles', true);
    } else if (info.embedded?.length) {
      const L = info.embedded;
      const eng = L.find((t) => ['en', 'eng'].includes(t.language)) ?? L[0];
      const res = await POST('/api/subs/extract', { file: raw, track: eng.index, language: eng.language });
      if (res?.ok && res.file) {
        this.reload(player, `/subs/${dir}${res.file}`, langLabel(eng.language), true);
        const updated = await GET(`/api/subs/info?file=${E(raw)}`);
        if (updated) Object.assign(info, updated);
      }
    } else if (info.whisper) {
      const wsub = sub.replace('.vtt', '.whisper.vtt');
      this.reload(player, `/subs/${wsub}`, 'Whisper', true);
    }

    return info;
  },

  async extractEmbedded(player: any, raw: string, index: number, language: string): Promise<string | null> {
    const res = await POST('/api/subs/extract', { file: raw, track: index, language });
    if (!res?.ok || !res.file) return null;
    const dir = subDir(raw);
    const label = langLabel(language);
    this.reload(player, `/subs/${dir}${res.file}`, label, true);
    return res.file;
  },

  async search(raw: string): Promise<any[] | null> {
    let res = await GET(`/api/subs/search?file=${E(raw)}`);
    return res?.results?.length ? res.results : null;
  },

  async downloadOnline(player: any, raw: string, fileId: number): Promise<{ file: string } | { error: string }> {
    const res = await POST('/api/subs/download', { file_id: fileId, file: raw });
    if (!res?.ok || !res.file) return { error: res?.error ?? 'Download failed' };
    const dir = subDir(raw);
    this.reload(player, `/subs/${dir}${res.file}`, 'English', true);
    return { file: res.file };
  },

  reload(player: any, src: string, label: string, show = false) {
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
      const all = player.textTracks();
      for (let i = 0; i < all.length; i++) {
        if (all[i].label !== label && all[i].mode === 'showing')
          all[i].mode = 'hidden';
      }
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
  ): () => void {
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

    return () => es.close();
  },
} as const;
