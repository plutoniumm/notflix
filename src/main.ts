import { mount } from "svelte";
import App from "./App.svelte";
import { SW_POLL_MS } from "./core/events.svelte";
import { usePoll } from "./core/poll";
import { toast } from "./core/toast.svelte";
import "../public/assets/global.css";
import "../public/assets/atomic.css";

mount(App, { target: document.getElementById("app")! });

window.addEventListener("unhandledrejection", (e) => {
  const reason: any = e.reason;
  const msg = reason?.message || (typeof reason === "string" ? reason : "unhandled rejection");
  if (reason?.name === "AbortError") return;
  console.error("[unhandledrejection]", reason);
  toast.err(msg);
});

window.addEventListener("error", (e) => {
  if (!e.message) return;
  console.error("[error]", e.error || e.message);
  toast.err(e.message);
});

if ("serviceWorker" in navigator) {
  // Reload only when asked: a worker taking control on its own yanks the video out mid-playback.
  let swapping = false;
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (swapping) window.location.reload();
  });

  navigator.serviceWorker
    .register("/sw.js")
    .then((reg) => {
      usePoll(() => reg.update(), SW_POLL_MS);

      const apply = () => {
        const waiting = reg.waiting;
        if (!waiting) {
          window.location.reload();
          return;
        }

        swapping = true;
        waiting.postMessage({ type: "SKIP_WAITING" });
      };

      const offer = () => {
        window.dispatchEvent(new CustomEvent("sw-update", { detail: apply }));
      };

      // A waiting worker never activates on a plain reload — the page is
      // still controlled by the old one — so the prompt survived the very
      // action users take to dismiss it. Outside the player, adopt it on
      // load; the sentinel stops a failed swap from reloading forever.
      const adopt = () => {
        if (reg.waiting && !location.search.includes("video=")) {
          const url = reg.waiting.scriptURL;
          try {
            if (sessionStorage.getItem("swTried") === url) return false;
            sessionStorage.setItem("swTried", url);
          } catch {}
          apply();

          return true;
        }

        return false;
      };

      if (reg.waiting && !adopt()) offer();
      reg.addEventListener("updatefound", () => {
        const sw = reg.installing;
        sw?.addEventListener("statechange", () => {
          if (sw.state === "installed" && navigator.serviceWorker.controller) {
            offer();
          }
        });
      });
    })
    .catch((err) => {
      console.error("[sw.register]", err);
      toast.err(`Service worker failed: ${err?.message ?? err}`);
    });
}
