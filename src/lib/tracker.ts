export class Tracker {
  private key: string
  private data: Record<string, number>

  constructor(key = 'lastPlayed') {
    this.key = key
    this.data = JSON.parse(localStorage.getItem(key) || '{}')
  }

  get(raw: string): number {
    return this.data[raw] ?? 0
  }

  set(raw: string, time: number) {
    this.data[raw] = time
    localStorage.setItem(this.key, JSON.stringify(this.data))
  }

  del(raw: string) {
    delete this.data[raw]
    localStorage.setItem(this.key, JSON.stringify(this.data))
  }
}
