export class IDB {
  readonly #db: Promise<IDBDatabase>;

  constructor(
    name: string,
    version: number,
    upgrade: (db: IDBDatabase) => void,
  ) {
    this.#db = new Promise((resolve, reject) => {
      const req = indexedDB.open(name, version);

      req.onupgradeneeded = () => upgrade(req.result);
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  }

  async get<T> (store: string, key: IDBValidKey): Promise<T | undefined> {
    const db = await this.#db;

    return new Promise((resolve, reject) => {
      const req = db.transaction(store, 'readonly')
        .objectStore(store)
        .get(key);

      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  }

  async set (store: string, value: unknown): Promise<void> {
    const db = await this.#db;

    return new Promise((resolve, reject) => {
      const req = db.transaction(store, 'readwrite')
        .objectStore(store)
        .put(value as any);

      req.onsuccess = () => resolve();
      req.onerror = () => reject(req.error);
    });
  }

  async del (store: string, key: IDBValidKey): Promise<void> {
    const db = await this.#db;

    return new Promise((resolve, reject) => {
      const req = db.transaction(store, 'readwrite')
        .objectStore(store)
        .delete(key);

      req.onsuccess = () => resolve();
      req.onerror = () => reject(req.error);
    });
  }

  async has (store: string, key: IDBValidKey): Promise<boolean> {
    return (await this.get(store, key)) !== undefined;
  }

  async all<T> (store: string): Promise<T[]> {
    const db = await this.#db;

    return new Promise((resolve, reject) => {
      const req = db.transaction(store, 'readonly')
        .objectStore(store)
        .getAll();

      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  }

  async tx (
    stores: string[],
    mode: IDBTransactionMode,
    fn: (tx: IDBTransaction) => void,
  ): Promise<void> {
    const db = await this.#db;

    return new Promise((resolve, reject) => {
      const tx = db.transaction(stores, mode);

      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);

      fn(tx);
    });
  }
}
