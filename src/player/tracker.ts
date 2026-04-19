const schedule: (cb: () => void) => number =
  typeof (globalThis as any).requestIdleCallback === 'function'
    ? (globalThis as any).requestIdleCallback.bind(globalThis)
    : (cb) => setTimeout(cb, 500) as unknown as number

export default class Tracker {
  private key: string
  private data: Record<string, number>
  private dirty = false
  private pending = false

  constructor(key = 'lastPlayed') {
    this.key = key
    this.data = JSON.parse(localStorage.getItem(key) || '{}')
  }

  get (raw: string): number {
    return this.data[raw] ?? 0
  }

  set (raw: string, time: number) {
    this.data[raw] = time
    this.scheduleFlush()
  }

  del (raw: string) {
    delete this.data[raw]
    this.scheduleFlush()
  }

  flush () {
    if (!this.dirty) return
    localStorage.setItem(this.key, JSON.stringify(this.data))
    this.dirty = false
  }

  private scheduleFlush () {
    this.dirty = true
    if (this.pending) return
    this.pending = true
    schedule(() => {
      this.pending = false
      this.flush()
    })
  }
}
