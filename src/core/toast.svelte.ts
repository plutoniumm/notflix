export type ToastKind = "err" | "ok" | "info";

export type ToastItem = {
  id: number;
  kind: ToastKind;
  msg: string;
  t: number;
};

const TTL_MS = 5000;
const MAX = 4;

let nextId = 1;

class ToastStore {
  items = $state<ToastItem[]>([]);

  #push(kind: ToastKind, msg: string) {
    if (!msg) return;
    const last = this.items[this.items.length - 1];
    if (last && last.msg === msg && last.kind === kind && Date.now() - last.t < 2000) return;

    const id = nextId++;
    const t = Date.now();
    this.items = [...this.items, { id, kind, msg, t }].slice(-MAX);
    setTimeout(() => this.dismiss(id), TTL_MS);
  }

  err(msg: string) {
    console.warn("[toast.err]", msg);
    this.#push("err", msg);
  }

  ok(msg: string) {
    this.#push("ok", msg);
  }

  info(msg: string) {
    this.#push("info", msg);
  }

  dismiss(id: number) {
    this.items = this.items.filter((i) => i.id !== id);
  }
}

export const toast = new ToastStore();
