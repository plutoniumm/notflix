export const $ = (s) => document.querySelector(s);
export const search = new URLSearchParams(window.location.search);

const get = (url) => fetch(url)
    .then((r) => r.json())
    .catch((e) => console.log(`[GET error]: ${url}`, e));

const text = (url) => fetch(url)
    .then((r) => r.text())
    .catch((e) => console.log(`[GET error]: ${url}`, e));

const del = (url) => fetch(url, { method: "DELETE" })
    .then((r) => r.json())
    .catch((e) => console.log(`[DEL error]: ${url}`, e));

export class Tracker {
    key: string;
    data: Record<string, number>;

    constructor(key = "lastPlayed") {
        this.key = key;
        this.data = JSON.parse(localStorage.getItem(this.key) || "{}");
    }

    get (video) {
        return this.data[video] ?? 0;
    }

    set (video, time) {
        this.data[video] = time;
        localStorage.setItem(this.key, JSON.stringify(this.data));
    }
}

export const net = {
    get,
    del,
    text,
};
