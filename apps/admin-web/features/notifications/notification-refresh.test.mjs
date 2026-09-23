import assert from "node:assert/strict";
import test from "node:test";
import { createNotificationRefreshGate } from "./notification-refresh.ts";

const deferred = () => {
  let resolve;
  const promise = new Promise((yes) => { resolve = yes; });
  return { promise, resolve };
};

test("twenty rapid route visits make one request, with revalidation at 60 seconds", async () => {
  let clock = 0;
  let calls = 0;
  const gate = createNotificationRefreshGate(async () => ++calls, () => clock);
  assert.equal(await gate.load(), 1);
  for (let route = 1; route <= 20; route += 1) {
    clock = route * 2_000;
    assert.equal(await gate.load(), undefined);
  }
  clock = 59_999;
  await gate.load();
  assert.equal(calls, 1);
  clock = 60_000;
  assert.equal(await gate.load(), 2);
  assert.equal(calls, 2);
});

test("slow navigation, open and repeated Refresh share one request and its result", async () => {
  const read = deferred();
  let calls = 0;
  const gate = createNotificationRefreshGate(() => { calls += 1; return read.promise; });
  const requests = [gate.load(), gate.load(), gate.load(true), gate.load(true)];
  await Promise.resolve();
  assert.equal(calls, 1);
  read.resolve({ ok: true, unread: 12 });
  assert.deepEqual(await Promise.all(requests), Array(4).fill({ ok: true, unread: 12 }));
  assert.equal(calls, 1);
});

test("explicit open and Refresh bypass completed navigation freshness", async () => {
  let calls = 0;
  const gate = createNotificationRefreshGate(async () => ++calls, () => 0);
  await gate.load();
  assert.equal(await gate.load(true), 2);
  assert.equal(await gate.load(true), 3);
  assert.equal(await gate.load(), undefined);
  assert.equal(calls, 3);
});

test("rejected reads back off route retries but explicit open retries immediately", async () => {
  let clock = 0;
  let calls = 0;
  const gate = createNotificationRefreshGate(async () => {
    calls += 1;
    if (calls === 1) throw new Error("backend unavailable");
    return { ok: true };
  }, () => clock);
  await assert.rejects(gate.load(), /backend unavailable/);
  for (let i = 0; i < 20; i += 1) await gate.load();
  assert.equal(calls, 1);
  assert.deepEqual(await gate.load(true), { ok: true });
  clock = 60_000;
  await gate.load();
  assert.equal(calls, 3);
});

test("resolved API errors also back off and do not prevent manual recovery", async () => {
  let calls = 0;
  const gate = createNotificationRefreshGate(async () => ({ ok: ++calls > 1 }), () => 0);
  assert.deepEqual(await gate.load(), { ok: false });
  await gate.load();
  assert.equal(calls, 1);
  assert.deepEqual(await gate.load(true), { ok: true });
});

test("separate mounted bells never share pending reads or freshness", async () => {
  const a = deferred();
  let aCalls = 0;
  let bCalls = 0;
  const first = createNotificationRefreshGate(() => { aCalls += 1; return a.promise; });
  const second = createNotificationRefreshGate(async () => { bCalls += 1; return "second member"; });
  const firstRead = first.load();
  assert.equal(await second.load(), "second member");
  a.resolve("first member");
  assert.equal(await firstRead, "first member");
  assert.equal(aCalls, 1);
  assert.equal(bCalls, 1);
});
