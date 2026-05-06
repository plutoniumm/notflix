import { mount } from "svelte";
import App from "./App.svelte";
import { SW_POLL_MS } from "./core/events.svelte";
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
  const hadController = !!navigator.serviceWorker.controller;
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (hadController) window.location.reload();
  });

  navigator.serviceWorker
    .register("/sw.js")
    .then((reg) => {
      setInterval(() => {
        if (document.hidden) return;
        reg.update().catch((err) => {
          console.warn("[sw.update]", err);
        });
      }, SW_POLL_MS);

      const onUpdate = () => {
        window.dispatchEvent(new CustomEvent("sw-update", { detail: reg }));
      };

      if (reg.waiting) onUpdate();
      reg.addEventListener("updatefound", () => {
        const sw = reg.installing;
        sw?.addEventListener("statechange", () => {
          if (sw.state === "installed" && navigator.serviceWorker.controller) {
            onUpdate();
          }
        });
      });
    })
    .catch((err) => {
      console.error("[sw.register]", err);
      toast.err(`Service worker failed: ${err?.message ?? err}`);
    });
}
