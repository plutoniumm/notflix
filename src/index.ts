import { addHotkeys, VideoList, move } from "./player";
import { $, net, search, Tracker } from "./utils";
import * as videojs from "video.js";
import { Lolomo } from "./ui";
import { Video } from "./video";

let videoList = [];
const video = new Video(search.get("video"));
const autoplay = search.get("autoplay") === "1";

if (video) {
    const name = video.name;
    document.title = `${name} | Notflix`;
    $("source").src = `/video/${video.raw}`;
    $(".title").innerText = `${video.dir}/${name}`;
}

net.get("/list").then((data) => {
    if (!data) return;
    videoList = new VideoList(data);
    player.next = () => {
        window.location.href = videoList.getNext(video, autoplay);
    };

    const sect = document.querySelector("#series");
    if (!sect) return;
    sect.innerHTML = Lolomo(data, video);
});

let player = videojs.default("notflix", {
    tracks: [
        {
            kind: "captions",
            src: `/subs/${video.sub}`,
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
