export const $ = (s) => document.querySelector(s);
export const search = new URLSearchParams(window.location.search);

const get = (url) =>
    fetch(url)
        .then((r) => r.json())
        .catch((e) => console.log(`[GET error]: ${url}`, e));

const del = (url) =>
    fetch(url, { method: "DELETE" })
        .then((r) => r.json())
        .catch((e) => console.log(`[DEL error]: ${url}`, e));

// return if get is 200
const exists = (url) => fetch(url).then((res) => res.status === 200);

export class Tracker {
    constructor(key = "lastPlayed") {
        this.key = key;
        this.data = JSON.parse(localStorage.getItem(this.key) || "{}");
    }

    get(video) {
        return this.data[video] ?? 0;
    }

    set(video, time) {
        this.data[video] = time;
        localStorage.setItem(this.key, JSON.stringify(this.data));
    }
}

export const net = {
    get,
    del,
    exists,
};
