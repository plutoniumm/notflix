export const GET = (url: string) => fetch(url)
  .then(r => r.json())
  .catch(() => null);

export const POST = (url: string, data: any) => fetch(url, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: typeof data === 'string' ? data : JSON.stringify(data),
})
  .then(r => r.json())
  .catch(() => null);

export const DEL = (url: string) => fetch(url, { method: 'DELETE' })
  .then(r => r.json())
  .catch(() => null);

const E = encodeURIComponent;

export const api = {
  videoList: () => GET('/list/video'),

  video: {
    info: (file: string) => GET(`/api/video/info?file=${E(file)}`),
  },

  audio: {
    info: (file: string) => GET(`/api/audio/info?file=${E(file)}`),
  },

  hls: {
    avoffset: (file: string) => GET(`/api/hls/avoffset?file=${E(file)}`),
  },

  subs: {
    info: (file: string) => GET(`/api/subs/info?file=${E(file)}`),
    search: (file: string) => GET(`/api/subs/search?file=${E(file)}`),
    download: (fileId: number, file: string) => POST('/api/subs/download', { file_id: fileId, file }),
    extract: (file: string, track: number, language: string) =>
      POST('/api/subs/extract', { file, track, language }),
    whisper: (file: string) => POST('/api/subs/whisper', { file }),
    whisperStatus: (file: string) => GET(`/api/subs/whisper/status?file=${E(file)}`),
  },

  manage: {
    list: () => GET('/api/manage/list'),
    diskInfo: () => GET('/api/manage/diskinfo'),
    dirSizes: () => GET('/api/manage/dirsizes'),
    hidden: () => GET('/api/manage/hidden'),
  },

  conversions: () => GET('/api/conversions'),
  process: () => POST('/api/process', {}),
  build: () => GET('/api/build'),
  rename: (path: string, name: string) => POST('/api/rename', { path, name }),
  deleteDir: (path: string) => DEL(`/api/dir?path=${E(path)}`),
  deleteVideo: (rel: string) => DEL(`/video/${rel}`),

  aria2: {
    list: () => GET('/api/aria2/list'),
    add: (magnet: string, dir: string) => POST('/api/aria2/add', { magnet, dir }),
    pause: (gid: string) => POST(`/api/aria2/pause?gid=${gid}`, {}),
    resume: (gid: string) => POST(`/api/aria2/resume?gid=${gid}`, {}),
    remove: (gid: string) => DEL(`/api/aria2/remove?gid=${gid}`),
  },
};
