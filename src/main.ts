import { mount } from 'svelte'
import App from './App.svelte'

mount(App, { target: document.getElementById('app')! });

if ('serviceWorker' in navigator) {
  const hadController = !!navigator.serviceWorker.controller;
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (hadController)
      window.location.reload();
  });

  navigator.serviceWorker.register('/sw.js');
}
