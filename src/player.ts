import { net } from "./utils.js";

export class VideoList {
    constructor(data) {
        this.videos = Object.entries(data).flatMap(([dir, files]) =>
            files.map((v) => [v, `?video=${encodeURIComponent(v)}`]),
        );
    }

    getNext(current, autoplay = false) {
        const idx = this.videos.findIndex(([v]) => v === current);
        if (idx === -1 || idx === this.videos.length - 1) return null;
        return this.videos[idx + 1][1] + (autoplay ? "&autoplay=1" : "");
    }
}

export function move(player, n) {
    const time = player.currentTime();
    const dur = player.duration();
    if (time + n > dur) {
        player.currentTime(dur - 2);
    } else if (time + n < 0) {
        player.currentTime(0);
    } else {
        player.currentTime(time + n);
    }
}

export async function trySubs(player, video) {
    if (!video) {
        console.warn("No video provided for subtitles.");
        return;
    }
    const subfile = video.replace(".mp4", ".vtt");
    if (!(await net.exists(`/subs/${subfile}`))) return;

    const existing = Array.from(player.remoteTextTracks()).find(
        (t) => t.label === "notflix-sub",
    );
    if (existing) return;
    console.log("Adding subtitles", subfile);

    player.addRemoteTextTrack({
        kind: "captions",
        src: `/subs/${subfile}`,
        srclang: "en",
        label: "notflix-sub",
        default: true,
    });

    for (let track of player.remoteTextTracks()) {
        track.mode = track.label === "notflix-sub" ? "showing" : "disabled";
    }
}

export function addHotkeys(player) {
    document.addEventListener("keydown", (event) => {
        const shift = event.shiftKey;
        const alt = event.altKey;
        const key = event.key;
        const lkey = key.toLowerCase();

        console.log(`[KEY] ${key}`, player.currentTime());

        // +TIME
        if (key === "ArrowRight" && shift) {
            player.move(30);
        } else if (key === "ArrowRight" && alt) {
            player.move(0.1);
        } else if (key === "ArrowRight") {
            player.move(5);
            // -TIME
        } else if (key === "ArrowLeft" && shift) {
            player.move(-30);
        } else if (key === "ArrowLeft" && alt) {
            player.move(-0.1);
        } else if (key === "ArrowLeft") {
            player.move(-5);
            // PLAYER
        } else if (key === " ") {
            event.preventDefault(); // prevent scrolling
            player.paused() ? player.play() : player.pause(); // play/pause
        } else if (lkey === "f") {
            player.isFullscreen()
                ? player.exitFullscreen()
                : player.requestFullscreen(); // fullscreen
        } else if (key >= "0" && key <= "9") {
            player.currentTime(player.duration() * parseInt(key) * 0.1); // jump to %

            // OTHERS
        } else if (lkey === "n") {
            // next
            if (next) window.location.href = next;
        } else if (lkey === "p") {
            // pip
            if (document.pictureInPictureElement) {
                document.exitPictureInPicture();
            } else {
                player.requestPictureInPicture();
            }
        } else if (lkey === "b") {
            // brightness
            const video = player.el().querySelector("video");
            if (video.classList.contains("bright")) {
                video.classList.remove("bright");
            } else {
                video.classList.add("bright");
            }
        } else if (lkey === "c") {
            const el: HTMLElement = document.querySelector(".video-js")!;
            if (el.classList.contains("hideSubs")) {
                el.classList.remove("hideSubs");
            } else {
                el.classList.add("hideSubs");
            }
        }
    });
}
