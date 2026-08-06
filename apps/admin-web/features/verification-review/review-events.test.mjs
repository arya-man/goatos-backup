import assert from "node:assert/strict";
import test from "node:test";

// A window/document stand-in must exist BEFORE the module is imported: the buffer registers unload
// handlers in its constructor and skips them entirely when `window` is undefined, so a test that
// imported first would silently exercise the no-listener path.
const listeners = new Map();
globalThis.window = {
  addEventListener: (type, fn) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
  },
  removeEventListener: (type, fn) => {
    const fns = listeners.get(type) ?? [];
    const i = fns.indexOf(fn);
    if (i >= 0) fns.splice(i, 1);
  },
};
globalThis.document = { visibilityState: "visible" };

const { ReviewEventBuffer } = await import("./review-events.ts");

function fire(type) {
  for (const fn of [...(listeners.get(type) ?? [])]) fn();
}

/**
 * Let the buffer's fire-and-forget flush settle. The unload handlers call flush() without awaiting
 * it, so the assertion has to run after the pending microtasks resolve — a fixed sleep would be both
 * slower and flakier, and the repo's frontend-foundations guard rightly refuses one.
 */
async function drain() {
  await new Promise((resolve) => setImmediate(resolve));
}

/** Collects posted batches and can be told to fail, so retry behaviour is observable. */
function recorder() {
  const batches = [];
  let failWith = null;
  return {
    batches,
    failNextWith(error) {
      failWith = error;
    },
    postFn: async (events) => {
      if (failWith) {
        const err = failWith;
        failWith = null;
        throw err;
      }
      batches.push(events);
    },
  };
}

test("recordEvent stamps a stable session id and a unique client_event_id per event", async () => {
  const r = recorder();
  const buffer = new ReviewEventBuffer(r.postFn);
  buffer.recordEvent("item-1", "item_opened", {});
  buffer.recordEvent("item-1", "video_play", { video_position_ms: 0 }, "proof-1");
  await buffer.forceFlush();

  const [batch] = r.batches;
  assert.equal(batch.length, 2);
  assert.match(batch[0].session_id, /^[0-9a-f-]{36}$/, "a real uuid session id");
  assert.equal(batch[0].session_id, batch[1].session_id, "one session across the review");
  assert.notEqual(batch[0].client_event_id, batch[1].client_event_id);
  assert.equal(batch[1].proof_id, "proof-1");
  assert.equal(batch[1].item_id, "item-1");
  assert.ok(!Number.isNaN(Date.parse(batch[0].occurred_at)), "occurred_at is an ISO instant");
  await buffer.dispose();
});

test("queue_opened carries a null item_id rather than a placeholder", async () => {
  const r = recorder();
  const buffer = new ReviewEventBuffer(r.postFn);
  buffer.recordEvent(null, "queue_opened", { category: "weighing_proof" });
  await buffer.forceFlush();
  assert.equal(r.batches[0][0].item_id, null, "the backend rejects a placeholder id with 422");
  await buffer.dispose();
});

test("a transient failure re-queues and retries with the SAME client_event_id", async () => {
  const r = recorder();
  const buffer = new ReviewEventBuffer(r.postFn);
  buffer.recordEvent("item-1", "video_play", {});
  const idBefore = buffer.events?.[0]?.client_event_id;

  r.failNextWith(new Error("transient: backend_down"));
  await buffer.forceFlush();
  assert.equal(r.batches.length, 0, "nothing was accepted");

  await buffer.forceFlush(); // retry
  assert.equal(r.batches.length, 1, "the retry delivered it");
  if (idBefore) {
    assert.equal(r.batches[0][0].client_event_id, idBefore, "same id => server dedupes, never double-counts");
  }
  await buffer.dispose();
});

test("a permanent rejection is DROPPED, not retried forever", async () => {
  const r = recorder();
  const buffer = new ReviewEventBuffer(r.postFn);
  buffer.recordEvent("item-1", "video_play", {});

  // Telemetry ingest is verifier-only authority, so a read-only leadership principal gets a
  // forbidden no retry can fix. Re-queueing it grew the buffer forever.
  r.failNextWith(new Error("permanent: verification review events rejected: permission_denied 403"));
  await buffer.forceFlush();

  await buffer.forceFlush();
  assert.equal(r.batches.length, 0, "the batch must be discarded, not resent");
  await buffer.dispose();
});

test("recordVerdict posts the decision immediately, with everything buffered behind it", async () => {
  const r = recorder();
  const buffer = new ReviewEventBuffer(r.postFn);
  buffer.recordEvent("item-1", "video_play", {});
  buffer.recordEvent("item-1", "video_ended", {});
  await buffer.recordVerdict("item-1", "approved");

  assert.equal(r.batches.length, 1, "the verdict flushes on its own, not on the 10s timer");
  const types = r.batches[0].map((e) => e.event_type);
  assert.deepEqual(types, ["video_play", "video_ended", "verdict_recorded"]);
  assert.equal(r.batches[0].at(-1).payload.verdict, "approved");
  await buffer.dispose();
});

test("hiding the tab flushes what is buffered", async () => {
  const r = recorder();
  const buffer = new ReviewEventBuffer(r.postFn);
  buffer.recordEvent("item-1", "video_pause", { video_position_ms: 1200 });

  globalThis.document.visibilityState = "hidden";
  fire("visibilitychange");
  await drain();
  globalThis.document.visibilityState = "visible";

  assert.equal(r.batches.length, 1, "a tab switch must not silently discard the review so far");
  assert.equal(r.batches[0][0].payload.video_position_ms, 1200);
  await buffer.dispose();
});

test("flushing an empty buffer posts nothing", async () => {
  const r = recorder();
  const buffer = new ReviewEventBuffer(r.postFn);
  await buffer.forceFlush();
  assert.equal(r.batches.length, 0);
  await buffer.dispose();
});

test("dispose detaches the window listeners it registered", async () => {
  const before = (listeners.get("visibilitychange") ?? []).length;
  const r = recorder();
  const buffer = new ReviewEventBuffer(r.postFn);
  assert.equal(
    (listeners.get("visibilitychange") ?? []).length,
    before + 1,
    "the buffer registers one visibility listener",
  );
  await buffer.dispose();
  assert.equal(
    (listeners.get("visibilitychange") ?? []).length,
    before,
    "and releases it — otherwise every drawer open leaks a listener for the session",
  );
});

test("a verdict flush does not disable later auto-flushing", async () => {
  const r = recorder();
  const buffer = new ReviewEventBuffer(r.postFn);
  await buffer.recordVerdict("item-1", "rejected");
  // forceFlush used to clearInterval, so the periodic flush died with the first verdict and a second
  // item reviewed on the same buffer only ever delivered if something flushed explicitly.
  assert.notEqual(
    buffer.flushTimer,
    null,
    "the periodic flush timer must survive a verdict — clearing it here was the regression",
  );
  buffer.recordEvent("item-2", "item_opened", {});
  globalThis.document.visibilityState = "hidden";
  fire("visibilitychange");
  await drain();
  globalThis.document.visibilityState = "visible";
  assert.equal(r.batches.length, 2, "the second item's events still reach the server");
  await buffer.dispose();
});
