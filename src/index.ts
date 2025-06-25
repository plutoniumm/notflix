import { addHotkeys, VideoList, move } from "./player";
import { $, net, search, Tracker } from "./utils";
import * as videojs from "video.js";
import { Lolomo } from "./ui";

let next = null;
let videoList = [];
const video = search.get("video");
const autoplay = search.get("autoplay") === "1";

if (video) {
    document.title = `${video} | Notflix`;
    $("source").src = `/video/${video}`;
}

function delFile(video) {
    net.del(`/video/${video}`).then((res) =>
        $(`li:has(a[href*="${video}"])`)?.remove(),
    );
}
window.delFile = delFile;

net.get("/list").then((data) => {
    if (!data) return;
    videoList = new VideoList(data);
    next = videoList.getNext(video, autoplay);

    const sect = document.querySelector("#series");
    if (!sect) return;
    sect.innerHTML = Lolomo(data);
});

const subfile = video.replace(".mp4", ".vtt");
let player = videojs.default("notflix", {
    tracks: [
        {
            kind: "captions",
            src: `/subs/${subfile}`,
            srclang: "en",
            label: "notflix-sub",
            default: true,
        },
    ],
});

player.ready(ready);

const tracker = new Tracker();
function ready() {
    console.log(`[INFO] Player up!\nAutoplay: ${autoplay}`);
    if (autoplay) player.play();
    player.move = (n) => move(player, n);

    player.currentTime(tracker.get(video));

    let lastTime = 0;
    setInterval(() => {
        tracker.set(video, player.currentTime());

        if (player.paused()) return;
        if (player.currentTime() === lastTime) {
            player.currentTime(player.currentTime());
        }

        lastTime = player.currentTime();
    }, 2000);

    addHotkeys(player);
}
