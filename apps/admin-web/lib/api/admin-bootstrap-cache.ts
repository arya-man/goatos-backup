import { createHash } from "node:crypto";

export type BootstrapCachePolicy = {
  etag: string;
  in_process_ttl_sec: number;
};

type CacheEntry<T extends { cache_policy: BootstrapCachePolicy }> = {
  data: T;
  etag: string;
  expiresAt: number;
  lastUsedAt: number;
};

export type BootstrapFetchResult<T> = {
  data: T | null;
  status: number;
  etag?: string | null;
};

const DEFAULT_TTL_MS = 60_000;
const MAX_TTL_MS = 10 * 60_000;
const DEFAULT_MAX_ENTRIES = 32;
// An entry nobody has read for this long has outlived every TTL the backend can advertise, so its
// ETag can never be revalidated again: its bearer token has rotated (Firebase rotates hourly) and
// no request will ever look it up. Dropping it eagerly is what keeps the heap flat — the contract
// is ~1 MB of JSON per entry, and holding dead entries up to the entry cap put STG admin-web past
// Node's heap limit roughly daily (2026-09-14 .. 09-18).
const IDLE_EVICT_MS = 2 * MAX_TTL_MS;

// Process-local, authority-isolated cache for the backend-owned admin UI contract. The key includes
// the complete credential digest: responses from different users/roles/tokens can never share data.
// The advertised backend TTL bounds freshness; after it expires, ETag revalidation avoids retransmitting
// the large contract when its tenant/role/family revision is unchanged.
export class AdminBootstrapCache<T extends { cache_policy: BootstrapCachePolicy }> {
  private readonly entries = new Map<string, CacheEntry<T>>();
  private readonly now: () => number;
  private readonly maxEntries: number;

  constructor(now: () => number = Date.now, maxEntries = DEFAULT_MAX_ENTRIES) {
    this.now = now;
    this.maxEntries = maxEntries;
  }

  async get(
    authority: { baseUrl: string; tenantId: string; bearerToken: string; view?: string },
    fetcher: (etag?: string) => Promise<BootstrapFetchResult<T>>,
    options: { forceRevalidate?: boolean } = {},
  ): Promise<T> {
    const now = this.now();
    const key = authorityKey(authority);
    const cached = this.entries.get(key);
    if (!options.forceRevalidate && cached && cached.expiresAt > now) {
      cached.lastUsedAt = now;
      return cached.data;
    }

    const result = await fetcher(cached?.etag);
    if (result.status === 304) {
      if (!cached) throw new Error("admin bootstrap returned 304 without a cached contract");
      cached.expiresAt = now + ttlMs(cached.data.cache_policy.in_process_ttl_sec);
      cached.lastUsedAt = now;
      return cached.data;
    }
    if (result.status !== 200 || !result.data) {
      throw new Error(`admin bootstrap returned ${result.status} without a contract`);
    }

    const etag = result.etag || result.data.cache_policy.etag;
    this.entries.set(key, {
      data: result.data,
      etag,
      expiresAt: now + ttlMs(result.data.cache_policy.in_process_ttl_sec),
      lastUsedAt: now,
    });
    this.evictIfNeeded();
    return result.data;
  }

  /**
   * Marks every held contract stale, so the next read revalidates with its ETag. Called after a
   * write that changes a vocabulary the contract compiles (Configuration > Items & settings): the
   * backend bumps that family's revision, the ETag no longer matches, and the new contract comes
   * back at once instead of after the advertised TTL (up to a minute, seen at 47 s on 2026-09-26).
   * The ETag is kept, so an unchanged contract still costs only a 304.
   */
  expireAll(): void {
    for (const entry of this.entries.values()) entry.expiresAt = 0;
  }

  /** Number of contracts currently held; exposed so tests can pin the heap bound. */
  get size(): number {
    return this.entries.size;
  }

  private evictIfNeeded() {
    const idleBefore = this.now() - IDLE_EVICT_MS;
    for (const [key, entry] of this.entries) {
      if (entry.lastUsedAt < idleBefore) this.entries.delete(key);
    }
    const maxEntries = Math.max(1, this.maxEntries);
    while (this.entries.size > maxEntries) {
      let victim: string | undefined;
      let oldest = Number.POSITIVE_INFINITY;
      for (const [key, entry] of this.entries) {
        if (entry.lastUsedAt < oldest) {
          oldest = entry.lastUsedAt;
          victim = key;
        }
      }
      if (!victim) break;
      this.entries.delete(victim);
    }
  }
}

// `view` is the page projection the entry holds (summary / page:<route_id> / full): the API answers
// each view with its own ETag, so entries for different views of one authority never share a slot.
function authorityKey(authority: { baseUrl: string; tenantId: string; bearerToken: string; view?: string }) {
  return createHash("sha256")
    .update(authority.baseUrl)
    .update("\0")
    .update(authority.tenantId)
    .update("\0")
    .update(authority.bearerToken)
    .update("\0")
    .update(authority.view ?? "")
    .digest("base64url");
}

function ttlMs(seconds: number) {
  if (!Number.isFinite(seconds) || seconds <= 0) return DEFAULT_TTL_MS;
  return Math.min(Math.max(seconds * 1000, 1000), MAX_TTL_MS);
}
