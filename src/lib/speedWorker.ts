const BASE = '/assets/notflix.css';

async function measure (): Promise<number> {
  const t0 = performance.now();

  const res = await fetch(
    `${BASE}?_=${Date.now()}`,
    { cache: 'no-store' }
  ).then((r) => r.arrayBuffer());

  const secs = (performance.now() - t0) / 1000;
  return (res.byteLength * 8) / (secs * 1_000_000); // Mbps
}

const respond = () => measure()
  .then((mbps) => postMessage(mbps))
  .catch(() => { });

self.addEventListener('message', (e: MessageEvent) => {
  if (e.data?.type === 'measure')
    respond();
});

respond();
setInterval(respond, 60_000);
