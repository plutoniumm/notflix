export class Video {
    constructor(dir, { name, key }) {
        this.name = name;
        this.id = key;
        this.dir = dir;

        name = `${dir}/${name}`;
        this.url = `?video=${encodeURIComponent(name)}`;
    }

    get uri() {
        const autoplay =
            new URLSearchParams(window.location.search).get("autoplay") === "1";

        return this.url + (autoplay ? "&autoplay=1" : "");
    }
}

export class VideoList {
    constructor(data) {
        this.videos = Object.entries(data).flatMap(([dir, files]) =>
            files.map((v) => new Video(dir, v)),
        );
    }

    getNext(currentName, autoplay = false) {
        const idx = this.videos.findIndex((v) => v.name === currentName);
        if (idx === -1 || idx === this.videos.length - 1) return null;
        return this.videos[idx + 1].uri;
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
