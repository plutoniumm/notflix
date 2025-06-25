import { addHotkeys, VideoList, move, trySubs } from "./player.js";
import { $, net, search, Tracker } from "./utils.js";
import * as videojs from "video.js";

let listings = $("#listing");
let videoList = [];
const video = search.get("video");
const autoplay = search.get("autoplay") === "1";
console.log(video, autoplay);

if (video) {
    document.title = `${video} | Notflix`;
    $("source").src = `/video/${video}`;
} else {
    listings.classList.remove("hidden");
}

function delFile(video) {
    net.del(`/video/${video}`).then((res) =>
        $(`li:has(a[href*="${video}"])`)?.remove(),
    );
}
window.delFile = delFile;

net.get("/list").then((data) => {
    if (!data?.length) return;
    videoList = new VideoList(data);
    next = videoList.getNext(video, autoplay);
    data.sort((a, b) => a.localeCompare(b, "en", { numeric: true })).forEach(
        (v) => {
            let href = `?video=${encodeURIComponent(v)}`;
            const div = document.createElement("li");
            div.classList.add("li", "f", "j-bw");

            div.innerHTML = `
                <a class="d-b" href="${href}">${v}</a>
                <span class="closer o-0 ptr" onclick="delFile('${v}')">✕</span>
            `;
            listings.appendChild(div);
        },
    );
});

let player = videojs.default("notflix");
player.ready(ready);
player = player.player_;

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

    trySubs(player, video);
    addHotkeys(player);
}
