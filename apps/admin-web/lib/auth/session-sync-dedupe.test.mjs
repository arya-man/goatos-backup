import assert from "node:assert/strict";
import test from "node:test";
import { SessionSyncDeduper, memorySessionSyncStore } from "./session-sync-dedupe.ts";

const TOKEN_A = `hdr.payload.${"a".repeat(48)}`;
const TOKEN_B = `hdr.payload.${"b".repeat(48)}`;

function recorder(ok = true) {
  const events = [];
  return {
    events,
    post: (eventType) => {
      events.push(eventType);
      // Resolve on a later macrotask so concurrent callers genuinely overlap.
      return new Promise((resolve) => setImmediate(resolve)).then(() => ok);
    },
  };
}

test("incident replay: login + concurrent onIdTokenChanged + 5 page loads posts one auth.sign_in", async () => {
  const store = memorySessionSyncStore();
  const r = recorder();
  const loginTab = new SessionSyncDeduper(store);
  await Promise.all([loginTab.signIn("uid-1", TOKEN_A, r.post), loginTab.bridge("uid-1", TOKEN_A, r.post)]);
  // Each full page load mounts a fresh bridge (fresh module state, same sessionStorage).
  for (let i = 0; i < 5; i += 1) {
    await new SessionSyncDeduper(store).bridge("uid-1", TOKEN_A, r.post);
  }
  assert.deepEqual(r.events, ["auth.sign_in"]);
});

test("restored session without explicit login records sign_in once, then refresh on a new token", async () => {
  const store = memorySessionSyncStore();
  const r = recorder();
  await new SessionSyncDeduper(store).bridge("u", TOKEN_A, r.post);
  await new SessionSyncDeduper(store).bridge("u", TOKEN_A, r.post);
  await new SessionSyncDeduper(store).bridge("u", TOKEN_B, r.post);
  assert.deepEqual(r.events, ["auth.sign_in", "auth.session_refresh"]);
});

test("a failed sync is retried rather than remembered", async () => {
  const store = memorySessionSyncStore();
  const d = new SessionSyncDeduper(store);
  const failed = recorder(false);
  await d.bridge("u", TOKEN_A, failed.post);
  const ok = recorder(true);
  await d.bridge("u", TOKEN_A, ok.post);
  assert.deepEqual([...failed.events, ...ok.events], ["auth.sign_in", "auth.sign_in"]);
});

test("different users in one browser session each get their own sign_in", async () => {
  const store = memorySessionSyncStore();
  const r = recorder();
  await new SessionSyncDeduper(store).signIn("u1", TOKEN_A, r.post);
  await new SessionSyncDeduper(store).bridge("u2", TOKEN_B, r.post);
  assert.deepEqual(r.events, ["auth.sign_in", "auth.sign_in"]);
});

test("logout then login of the same user in the same tab records a fresh auth.sign_in", async () => {
  const store = memorySessionSyncStore();
  const r = recorder();
  const d = new SessionSyncDeduper(store);
  await d.signIn("u", TOKEN_A, r.post);
  d.forget("u");
  await new SessionSyncDeduper(store).bridge("u", TOKEN_B, r.post);
  assert.deepEqual(r.events, ["auth.sign_in", "auth.sign_in"]);
});

test("an explicit login never settles for a concurrent session_refresh of the same token", async () => {
  const store = memorySessionSyncStore();
  const r = recorder();
  const d = new SessionSyncDeduper(store);
  await d.signIn("u", TOKEN_A, r.post); // earlier sign-in this session
  // Bridge sees a new token first and starts a refresh; login for the same token races it.
  await Promise.all([d.bridge("u", TOKEN_B, r.post), d.signIn("u", TOKEN_B, r.post)]);
  assert.deepEqual(r.events, ["auth.sign_in", "auth.session_refresh", "auth.sign_in"]);
});

test("the 50-minute timer's forced refresh and the onIdTokenChanged it fires post once", async () => {
  const store = memorySessionSyncStore();
  const r = recorder();
  const d = new SessionSyncDeduper(store);
  await d.signIn("u", TOKEN_A, r.post);
  // Timer: getIdToken(true) mints TOKEN_B and Firebase fires onIdTokenChanged(TOKEN_B).
  await Promise.all([d.bridge("u", TOKEN_B, r.post), d.bridge("u", TOKEN_B, r.post)]);
  await d.bridge("u", TOKEN_B, r.post); // a late listener call after the timer's post landed
  assert.deepEqual(r.events, ["auth.sign_in", "auth.session_refresh"]);
});
