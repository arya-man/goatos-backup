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

// STG admin-web died of `JavaScript heap out of memory` roughly daily (2026-09-14 .. 09-18): the
// contract is ~1 MB of JSON per entry, the key is the bearer token, and Firebase rotates that token
// every hour — so each user-hour left a dead entry on the heap until the 128-entry cap, well past
// the ~256 MB heap a 512Mi container gives Node. An entry nobody has touched for longer than the
// longest possible TTL can never be revalidated again and must be dropped.
test("drops entries idle past the revalidation window instead of holding them to the entry cap", async () => {
  let now = 0;
  const cache = new AdminBootstrapCache(() => now);
  const fetcher = async () => ({
    data: { cache_policy: { etag: 'W/"x"', in_process_ttl_sec: 60 } },
    status: 200,
  });
  const base = { baseUrl: "http://api", tenantId: "tenant" };
  for (let hour = 0; hour < 24; hour++) {
    now = hour * 3_600_000;
    await cache.get({ ...base, bearerToken: `token-hour-${hour}` }, fetcher);
  }
  assert.equal(cache.size, 1);
});

test("keeps a recently used entry past its TTL so it can still revalidate by ETag", async () => {
  let now = 0;
  let calls = 0;
  const cache = new AdminBootstrapCache(() => now);
  const authority = { baseUrl: "http://api", tenantId: "tenant", bearerToken: "token-a" };
  const fetcher = async (etag) => {
    calls++;
    if (etag === 'W/"one"') return { data: null, status: 304, etag };
    return { data: { cache_policy: { etag: 'W/"one"', in_process_ttl_sec: 60 } }, status: 200, etag: 'W/"one"' };
  };
  await cache.get(authority, fetcher);
  now = 5 * 60_000;
  await cache.get(authority, fetcher);
  assert.equal(calls, 2);
  assert.equal(cache.size, 1);
});

// A Configuration save (a new species, gender, breed ...) must reach every other screen's pickers
// on the next page load, not after the TTL (47 s observed 2026-09-26). expireAll forces that one
// revalidation, still by ETag, so an unchanged contract costs only a 304.
test("expireAll makes the next read revalidate within the TTL and pick up a changed contract", async () => {
  let now = 0;
  let version = "one";
  const seenEtags = [];
  const cache = new AdminBootstrapCache(() => now);
  const authority = { baseUrl: "http://api", tenantId: "tenant", bearerToken: "token-a" };
  const fetcher = async (etag) => {
    seenEtags.push(etag);
    if (etag === `W/"${version}"`) return { data: null, status: 304, etag };
    return { data: { value: version, cache_policy: { etag: `W/"${version}"`, in_process_ttl_sec: 60 } }, status: 200, etag: `W/"${version}"` };
  };
  assert.equal((await cache.get(authority, fetcher)).value, "one");
  version = "two"; // a species was added; the backend's revision moved
  now = 5_000; // well inside the TTL
  assert.equal((await cache.get(authority, fetcher)).value, "one", "without expireAll the old list is served");
  cache.expireAll();
  assert.equal((await cache.get(authority, fetcher)).value, "two", "after expireAll the new list is served at once");
  assert.deepEqual(seenEtags, [undefined, 'W/"one"']);
});
