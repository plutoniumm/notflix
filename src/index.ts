import { addHotkeys, VideoList, move } from "./player";
import { $, net, search, Tracker } from "./utils";
import * as videojs from "video.js";
import { Lolomo } from "./ui";
import { Video } from "./video";

let videoList: VideoList;
const video = new Video(search.get("video"));
const autoplay = search.get("autoplay") === "1";

if (video) {
    const name = video.name;
    document.title = `${name} | Notflix`;
    $("source").src = `/video/${video.raw}`;
    $(".title").innerText = `${video.dir}/${name}`;
    console.log(`[INFO] Loaded video: ${video.dir}/${name}`);
};

net.get("/list/video").then((data) => {
    if (!data) return;
    videoList = new VideoList(data);
    player.next = () => {
        window.location.href = videoList.getNext(video);
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

if (window.location.pathname === "/embed") {
    setInterval(() => {
        fetch("/cmd").then(res => res.text())
            .then(cmd => {
                if (!cmd.length) return;

                if (cmd === "tog") {
                    if (player.paused())
                        player.play();
                    else player.pause();

                    move(player, -2);
                } else if (cmd.startsWith("+")) {
                    const n = parseFloat(cmd.slice(1));
                    move(player, n);
                } else if (cmd.startsWith("-")) {
                    const n = parseFloat(cmd.slice(1));
                    move(player, -n);
                };
            });
    }, 1000);
}


const tracker = new Tracker();
function ready () {
    console.log(`[INFO] Player up!\nAutoplay: ${autoplay}`);
    if (autoplay) player.play();
    player.move = (n) => move(player, n);
    player.currentTime(tracker.get(video));

    let lastTime = 0;
    setInterval(() => {
        let current = player.currentTime()!;
        let dur = player.duration()!;

        tracker.set(video, current);
        if (dur - current > 5 * 60)
            tracker.del(video);

        if (player.paused()) return;
        if (current === lastTime) {
            player.currentTime(current);
        }

        lastTime = player.currentTime()!;
    }, 2000);

    addHotkeys(player);
}