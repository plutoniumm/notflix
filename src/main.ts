import { mount } from 'svelte'
import App from './App.svelte'

mount(App, { target: document.getElementById('app')! });

const splash = document.getElementById('splash');
if (splash) {
  splash.style.opacity = '0';

  splash.addEventListener(
    'transitionend', () => splash.remove(),
    { once: true }
  );
}

if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js')
}
