const LEVELS = [
  { q: "2160p", label: "4K", h: 2160, mbps: 15 },
  { q: "1080p", label: "1080p", h: 1080, mbps: 6 },
  { q: "720p", label: "720p", h: 720, mbps: 3 },
  { q: "480p", label: "480p", h: 480, mbps: 1.2 },
  { q: "360p", label: "360p", h: 360, mbps: 0.8 },
  { q: "240p", label: "240p", h: 240, mbps: 0.5 },
  { q: "144p", label: "144p", h: 144, mbps: 0 },
] as const;

export default {
  key: "notflix-quality",
  levels: LEVELS,
  type: (_q: string) => "video/mp4",
  src: (videoParam: string, q: string, seek = 0) => {
    if (q === "original")
      return `/video/${videoParam}`;
    const s = seek > 0 ? `&seek=${seek.toFixed(3)}` : "";

    return `/video/${videoParam}?q=${q}${s}`;
  },
  auto: (mbps: number, maxH: number) => {
    for (const lvl of LEVELS) {
      // allow for error
      if (lvl.h <= maxH && mbps >= lvl.mbps * 1.2)
        return lvl.q;
    };

    return "144p";
  }
} as const;
