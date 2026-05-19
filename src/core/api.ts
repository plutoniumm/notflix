import { toast } from "./toast.svelte";

export type ReqOpts = {
  silent?: boolean;
  tag?: string;
};

async function parseBody(r: Response) {
  const ct = r.headers.get("content-type") ?? "";
  const text = await r.text();
  if (!text) return null;
  if (ct.includes("application/json")) {
    try {
      return JSON.parse(text);
    } catch {
      return null;
    }
  }
  return text;
}

function report(tag: string, msg: string, silent?: boolean) {
  if (silent) {
    console.warn(`[api ${tag}]`, msg);
    return;
  }
  toast.err(`${tag}: ${msg}`);
}

async function request(
  method: string,
  url: string,
  body: any | undefined,
  opts: ReqOpts = {},
): Promise<any> {
  const tag = opts.tag ?? `${method} ${url.split("?")[0]}`;
  try {
    const init: RequestInit = { method };
    if (body !== undefined) {
      init.headers = { "Content-Type": "application/json" };
      init.body = typeof body === "string" ? body : JSON.stringify(body);
    }
    const r = await fetch(url, init);
    if (!r.ok) {
      const payload = await parseBody(r);
      const msg =
        (payload && typeof payload === "object" && (payload.error || payload.message)) ||
        (typeof payload === "string" && payload.slice(0, 160)) ||
        `HTTP ${r.status}`;
      report(tag, String(msg), opts.silent);
      return null;
    }
    return await parseBody(r);
  } catch (e: any) {
    report(tag, e?.message || "network error", opts.silent);
    return null;
  }
}

export const GET = (url: string, opts?: ReqOpts) => request("GET", url, undefined, opts);
export const POST = (url: string, data: any, opts?: ReqOpts) => request("POST", url, data ?? {}, opts);
export const DEL = (url: string, opts?: ReqOpts) => request("DELETE", url, undefined, opts);

export async function HEAD(url: string): Promise<boolean> {
  try {
    const r = await fetch(url, { method: "HEAD" });
    return r.ok;
  } catch {
    return false;
  }
}

const E = encodeURIComponent;

export const api = {
  videoList: (opts?: ReqOpts) => GET("/list/video", opts),

  search: (q: string, opts?: ReqOpts) => GET(`/api/search?q=${E(q)}`, opts),

  audio: {
    info: (file: string, opts?: ReqOpts) => GET(`/api/audio/info?file=${E(file)}`, opts),
  },

  hls: {
    avoffset: (file: string, opts?: ReqOpts) => GET(`/api/hls/avoffset?file=${E(file)}`, opts),
  },

  subs: {
    info: (file: string, opts?: ReqOpts) => GET(`/api/subs/info?file=${E(file)}`, opts),
    search: (file: string, opts?: ReqOpts) => GET(`/api/subs/search?file=${E(file)}`, opts),
    download: (
      pick: { provider?: string; file_id?: number; url?: string },
      file: string,
      opts?: ReqOpts,
    ) => POST("/api/subs/download", { ...pick, file }, opts),
    extract: (file: string, track: number, language: string, opts?: ReqOpts) =>
      POST("/api/subs/extract", { file, track, language }, opts),
    whisper: (file: string, opts?: ReqOpts) => POST("/api/subs/whisper", { file }, opts),
    whisperStatus: (file: string, opts?: ReqOpts) =>
      GET(`/api/subs/whisper/status?file=${E(file)}`, opts),
  },

  manage: {
    list: (opts?: ReqOpts) => GET("/api/manage/list", opts),
    diskInfo: (opts?: ReqOpts) => GET("/api/manage/diskinfo", opts),
    dirSizes: (opts?: ReqOpts) => GET("/api/manage/dirsizes", opts),
    hidden: (opts?: ReqOpts) => GET("/api/manage/hidden", opts),
  },

  conversions: (opts?: ReqOpts) => GET("/api/conversions", opts),
  process: (opts?: ReqOpts) => POST("/api/process", {}, opts),
  build: (opts?: ReqOpts) => GET("/api/build", opts),
  rename: (path: string, name: string, opts?: ReqOpts) =>
    POST("/api/rename", { path, name }, opts),
  deleteDir: (path: string, opts?: ReqOpts) => DEL(`/api/dir?path=${E(path)}`, opts),
  deleteVideo: (rel: string, opts?: ReqOpts) => DEL(`/video/${rel}`, opts),

  aria2: {
    list: (opts?: ReqOpts) => GET("/api/aria2/list", opts),
    add: (magnet: string, dir: string, opts?: ReqOpts) =>
      POST("/api/aria2/add", { magnet, dir }, opts),
    pause: (gid: string, opts?: ReqOpts) => POST(`/api/aria2/pause?gid=${gid}`, {}, opts),
    resume: (gid: string, opts?: ReqOpts) => POST(`/api/aria2/resume?gid=${gid}`, {}, opts),
    remove: (gid: string, opts?: ReqOpts) => DEL(`/api/aria2/remove?gid=${gid}`, opts),
  },
};
