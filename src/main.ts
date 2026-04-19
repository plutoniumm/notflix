import { mount } from 'svelte'
import App from './App.svelte'
import { SW_POLL_MS } from './core/events.svelte'
import '../public/assets/global.css'
import '../public/assets/atomic.css'

mount(App, { target: document.getElementById('app')! });

if ('serviceWorker' in navigator) {
  const hadController = !!navigator.serviceWorker.controller;
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (hadController)
      window.location.reload();
  });

  navigator.serviceWorker.register('/sw.js').then((reg) => {
    setInterval(() => {
      if (document.hidden) return;
      reg.update();
    }, SW_POLL_MS);

    const onUpdate = () => {
      window.dispatchEvent(new CustomEvent('sw-update', { detail: reg }));
    };

    if (reg.waiting) onUpdate();
    reg.addEventListener('updatefound', () => {
      const sw = reg.installing;
      sw?.addEventListener('statechange', () => {
        if (sw.state === 'installed' && navigator.serviceWorker.controller) {
          onUpdate();
        }
      });
    });
  });
}
