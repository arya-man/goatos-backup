import assert from "node:assert/strict";
import test from "node:test";
import { AdminBootstrapCache } from "./admin-bootstrap-cache.ts";

test("deduplicates within TTL and conditionally revalidates after expiry", async () => {
  let now = 0;
  let calls = 0;
  const seenEtags = [];
  const cache = new AdminBootstrapCache(() => now);
  const authority = { baseUrl: "http://api", tenantId: "tenant", bearerToken: "token-a" };
  const fetcher = async (etag) => {
    calls++;
    seenEtags.push(etag);
    if (etag === 'W/"one"') return { data: null, status: 304, etag };
    return { data: { value: "one", cache_policy: { etag: 'W/"one"', in_process_ttl_sec: 60 } }, status: 200, etag: 'W/"one"' };
  };

  assert.equal((await cache.get(authority, fetcher)).value, "one");
  now = 30_000;
  assert.equal((await cache.get(authority, fetcher)).value, "one");
  now = 61_000;
  assert.equal((await cache.get(authority, fetcher)).value, "one");
  assert.equal(calls, 2);
  assert.deepEqual(seenEtags, [undefined, 'W/"one"']);
});

test("never shares contracts across credentials", async () => {
  let calls = 0;
  const cache = new AdminBootstrapCache(() => 0);
  const fetcher = async () => {
    calls++;
    return { data: { value: String(calls), cache_policy: { etag: `W/"${calls}"`, in_process_ttl_sec: 60 } }, status: 200 };
  };
  const base = { baseUrl: "http://api", tenantId: "tenant" };
  assert.equal((await cache.get({ ...base, bearerToken: "token-a" }, fetcher)).value, "1");
  assert.equal((await cache.get({ ...base, bearerToken: "token-b" }, fetcher)).value, "2");
  assert.equal(calls, 2);
});
