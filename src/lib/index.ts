export const GET = (url: string) => fetch(url)
  .then(r => r.json())
  .catch(() => null);

export const POST = (url: string, data: any) => fetch(url, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: typeof data === 'string' ? data : JSON.stringify(data),
})
  .then(r => r.json())
  .catch(() => null);

export const DEL = (url: string) => fetch(url, { method: 'DELETE' })
  .then(r => r.json())
  .catch(() => null);

export { default as Subs } from "./subs";
export { default as Tracker } from "./tracker";
