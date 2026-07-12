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
const DEFAULT_MAX_ENTRIES = 128;

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
    authority: { baseUrl: string; tenantId: string; bearerToken: string },
    fetcher: (etag?: string) => Promise<BootstrapFetchResult<T>>,
  ): Promise<T> {
    const now = this.now();
    const key = authorityKey(authority);
    const cached = this.entries.get(key);
    if (cached && cached.expiresAt > now) {
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

  private evictIfNeeded() {
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

function authorityKey(authority: { baseUrl: string; tenantId: string; bearerToken: string }) {
  return createHash("sha256")
    .update(authority.baseUrl)
    .update("\0")
    .update(authority.tenantId)
    .update("\0")
    .update(authority.bearerToken)
    .digest("base64url");
}

function ttlMs(seconds: number) {
  if (!Number.isFinite(seconds) || seconds <= 0) return DEFAULT_TTL_MS;
  return Math.min(Math.max(seconds * 1000, 1000), MAX_TTL_MS);
}
