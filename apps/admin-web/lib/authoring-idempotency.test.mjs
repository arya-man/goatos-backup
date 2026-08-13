import { test } from "node:test";
import assert from "node:assert";
import { afterSubmit, CLOSED_STATE, openIntent } from "./authoring-idempotency.ts";

// Regression coverage for the P2 idempotency-regeneration fix: `upsertFeedConfig*` used to default
// the Idempotency-Key header to a fresh `randomUUID()` minted INSIDE the server call, so a retry
// after a lost/ambiguous response sent a DIFFERENT key and the backend could apply a second write.
// These tests drive the extracted state machine directly (no React mount needed) through the
// exact sequence a lost-response-then-resubmit produces.

let seq = 0;
function mintKey() {
  seq += 1;
  return `key-${seq}`;
}

test.beforeEach(() => {
  seq = 0;
});

test("closed state has no key", () => {
  assert.strictEqual(CLOSED_STATE.open, false);
  assert.strictEqual(CLOSED_STATE.key, null);
});

test("opening mints exactly one key for the new intent", () => {
  const state = openIntent(mintKey);
  assert.strictEqual(state.open, true);
  assert.strictEqual(state.key, "key-1");
});

test("a lost-response-then-resubmit reuses the SAME key -- exactly one write effect", () => {
  // The form opens: one intent, one key.
  let state = openIntent(mintKey);
  const openedKey = state.key;

  // First submit's response is lost/ambiguous -- the caller cannot tell if the write landed.
  // afterSubmit(ok=false) models "no confirmed success", which is exactly what a lost response
  // looks like from the client's point of view (it never resolved to a definite success).
  state = afterSubmit(state, false, mintKey);
  assert.strictEqual(state.key, openedKey, "a lost/failed response must not rotate the key");

  // The operator resubmits the exact same open form. The hidden idempotency_key field still
  // holds openedKey, so this second attempt sends the SAME key the first attempt used -- the
  // backend recognizes the replay and returns the original result rather than writing twice.
  assert.strictEqual(state.key, openedKey);

  // This time the retry is CONFIRMED successful.
  state = afterSubmit(state, true, mintKey);
  assert.notStrictEqual(state.key, openedKey, "a confirmed success rotates the key for the NEXT intent");
  assert.strictEqual(state.open, true, "the form stays open to show the success message");
});

test("a rejected (validation) outcome keeps the same key for the fix-and-resubmit retry", () => {
  let state = openIntent(mintKey);
  const openedKey = state.key;

  // e.g. blank-is-not-zero rejection: no request was even sent, so there is certainly no side
  // effect to protect against replaying.
  state = afterSubmit(state, false, mintKey);
  assert.strictEqual(state.key, openedKey);

  // Operator fixes the field and resubmits -- still one intent, still the same key.
  state = afterSubmit(state, false, mintKey);
  assert.strictEqual(state.key, openedKey);
});

test("closing and reopening mints a genuinely new intent's key", () => {
  const first = openIntent(mintKey);
  const reopened = openIntent(mintKey);
  assert.notStrictEqual(first.key, reopened.key);
});

test("afterSubmit on a closed state is a no-op (nothing to rotate)", () => {
  const result = afterSubmit(CLOSED_STATE, true, mintKey);
  assert.deepStrictEqual(result, CLOSED_STATE);
});

test("two consecutive confirmed successes each mint a fresh key for the next intent", () => {
  let state = openIntent(mintKey);
  const key1 = state.key;
  state = afterSubmit(state, true, mintKey);
  const key2 = state.key;
  state = afterSubmit(state, true, mintKey);
  const key3 = state.key;
  assert.strictEqual(new Set([key1, key2, key3]).size, 3, "every confirmed success must rotate to a unique key");
});
