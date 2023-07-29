import { writable } from 'svelte/store';
import type { Writable } from 'svelte/store';

const addMessage = (message: Message) => {
  messages.update((msgs) => [...msgs, message]);
};

export const messages: Writable<Message[]> = writable([
  {
    id: 12345,
    text: 'Hello, world!',
    user: {
      name: 'Manav',
      image: "https://api.dicebear.com/6.x/bottts/svg?seed=Manav"
    }
  },
  {
    id: 12346,
    text: 'Hello, world!',
    user: {
      name: 'Demo',
      image: "https://api.dicebear.com/6.x/bottts/svg?seed=Demo"
    }
  },
  {
    id: 12347,
    text: 'Hello, world!',
    user: {
      name: 'Demo',
      image: "https://api.dicebear.com/6.x/bottts/svg?seed=Demo"
    }
  },
]);

export const messageService = {
  addMessage,
};