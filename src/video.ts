export function rename(text: string) {
    let removes =
        `nflx,amzn,` +
        `hevc,hdtv,hdrip,bluray,` +
        `web-dl,webrip,web,hevc-psa,` +
        `webdl,mkv,eac3,avi,hdr,mp4,` +
        `dvdrip,repack,split,scenes,rq,` +
        `aac,10bit,atmos,ddp5,dd5,ac3,2025,` +
        `x264,x265,h264,h265,720p,480p`;
    removes = removes.split(",").map((i) => i.trim().toLowerCase());

    let string = text;
    for (let i of removes) {
        if (string.toLowerCase().includes(i)) {
            string = string.replace(new RegExp(i, "gi"), "");
        }
    }

    string = string
        .replaceAll("-", " ")
        .replaceAll(".", " ")
        .replace(/\s+/g, " ")
        .trim();

    return string;
}

export class Video {
    dir: string = "";
    name: string = "";
    raw: string = "";
    sub: string = "";

    constructor(video) {
        if (!video) return;

        let [dir, name] = video.split("/");
        this.raw = video;
        this.sub = name.replace(".mp4", ".vtt");
        name = rename(name.replace(".mp4", ""));

        this.dir = dir;
        this.name = name;
    }
}
