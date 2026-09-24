import assert from "node:assert/strict";
import test from "node:test";
import { ShortReadCache } from "./short-read-cache.ts";

test("short read cache with zero ttl only coalesces in-flight reads", async () => {
  let calls = 0;
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const cache = new ShortReadCache(0, () => 1000);
  const read = () =>
    cache.read("weights:same-authority-same-query", async () => {
      calls += 1;
      await gate;
      return { ok: true, data: { version: calls } };
    });

  const first = read();
  const second = read();
  release();

  assert.deepEqual(await first, { ok: true, data: { version: 1 } });
  assert.deepEqual(await second, { ok: true, data: { version: 1 } });
  assert.equal(calls, 1);

  assert.deepEqual(await read(), { ok: true, data: { version: 2 } });
  assert.equal(calls, 2);
});

test("short read cache evicts failures and permission revocation responses immediately", async () => {
  let calls = 0;
  const cache = new ShortReadCache(120_000, () => 1000);

  const first = await cache.read("weights:same-authority-same-query", async () => {
    calls += 1;
    return { ok: false, error: { kind: "backend_down" } };
  });
  const second = await cache.read("weights:same-authority-same-query", async () => {
    calls += 1;
    return { ok: false, error: { kind: "permission_denied", status: 403 } };
  });
  const third = await cache.read("weights:same-authority-same-query", async () => {
    calls += 1;
    return { ok: true, data: { recovered: true } };
  });

  assert.equal(first.ok, false);
  assert.equal(second.ok, false);
  assert.deepEqual(third, { ok: true, data: { recovered: true } });
  assert.equal(calls, 3);
});

test("short read cache serves repeat reads inside the ttl and clear() drops them", async () => {
  let now = 1000;
  let calls = 0;
  const cache = new ShortReadCache(30_000, () => now);
  const read = () =>
    cache.read("weights:user-a", async () => {
      calls += 1;
      return { ok: true, data: { version: calls } };
    });

  assert.deepEqual(await read(), { ok: true, data: { version: 1 } });
  now += 29_000;
  assert.deepEqual(await read(), { ok: true, data: { version: 1 } });
  assert.equal(calls, 1);

  cache.clear();
  assert.deepEqual(await read(), { ok: true, data: { version: 2 } });
  now += 31_000;
  assert.deepEqual(await read(), { ok: true, data: { version: 3 } });
  assert.equal(calls, 3);
});
