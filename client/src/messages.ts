import { writable } from 'svelte/store';
import type { Writable } from 'svelte/store';

const addMessage = (message: Message) => {
  messages.update((msgs) => [...msgs, message]);
};

export const messages: Writable<Message[]> = writable([]);

export const messageService = {
  addMessage,
};