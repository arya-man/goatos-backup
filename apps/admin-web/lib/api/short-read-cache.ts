export type ShortReadCacheResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: unknown };

type CachedApiRead<T> = {
  expires: number;
  promise: Promise<ShortReadCacheResult<T>>;
};

export class ShortReadCache {
  private readonly entries = new Map<string, CachedApiRead<unknown>>();
  private readonly ttlMs: number;
  private readonly now: () => number;

  constructor(ttlMs: number, now: () => number = Date.now) {
    this.ttlMs = ttlMs;
    this.now = now;
  }

  /** Drops every cached read. Called after any backend write so no stale read survives it. */
  clear(): void {
    this.entries.clear();
  }

  read<T>(
    key: string,
    fn: () => Promise<ShortReadCacheResult<T>>,
  ): Promise<ShortReadCacheResult<T>> {
    const now = this.now();
    const cached = this.entries.get(key);
    if (cached && cached.expires >= now) {
      return cached.promise as Promise<ShortReadCacheResult<T>>;
    }
    if (this.entries.size > 256) {
      this.entries.clear();
    }
    const promise = fn()
      .then((result) => {
        if (!result.ok) {
          const current = this.entries.get(key);
          if (current?.promise === promise) {
            this.entries.delete(key);
          }
        }
        return result;
      })
      .finally(() => {
        const current = this.entries.get(key);
        if (current?.promise === promise && current.expires <= this.now()) {
          this.entries.delete(key);
        }
      });
    this.entries.set(key, { expires: now + this.ttlMs, promise });
    return promise;
  }
}
