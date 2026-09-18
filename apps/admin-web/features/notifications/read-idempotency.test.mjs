import assert from "node:assert/strict";
import test from "node:test";

import {
  MAX_IDEMPOTENCY_KEY_LENGTH,
  MIN_IDEMPOTENCY_KEY_LENGTH,
  isUsableIdempotencyKey,
  notificationsReadIdempotencyKey,
} from "./read-idempotency.ts";
import { NOTIFICATION_PAGE_LIMIT, idsToMarkAllRead } from "./notification-model.ts";

const id = (n) => `11111111-2222-4333-8444-${String(n).padStart(12, "0")}`;

/**
 * n=20 is the case that shipped broken, and the numbers are the point.
 *
 * `idsToMarkAllRead` returns up to NOTIFICATION_PAGE_LIMIT = 20, and the retired derivation
 * (prefix + the sorted ids joined by ",") was 28 + 37n characters: 176 at n=4, 213 at n=5 --
 * already past the backend's 200-character ceiling -- and 768 at n=20. Everything from the fifth
 * notification onward was a 400 the optimistic state hid, so nothing was EVER marked read.
 */
test("a full page of 20 notifications produces a key the backend will accept", () => {
  const ids = Array.from({ length: NOTIFICATION_PAGE_LIMIT }, (_, index) => id(index));
  assert.equal(ids.length, 20);

  const key = notificationsReadIdempotencyKey(ids);
  assert.ok(isUsableIdempotencyKey(key), `key of ${key.length} chars is outside the accepted range`);
  assert.ok(key.length <= MAX_IDEMPOTENCY_KEY_LENGTH);
  assert.ok(key.length >= MIN_IDEMPOTENCY_KEY_LENGTH);
  // The prefix plus one sha256 in hex. Stated as a number so a change to the derivation has to
  // come back through this line.
  assert.equal(key.length, 93);

  // The defect, reproduced beside the fix so the two cannot drift apart.
  const retired = `admin-web-notifications-read:${[...ids].sort().join(",")}`;
  assert.equal(retired.length, 768);
  assert.ok(retired.length > MAX_IDEMPOTENCY_KEY_LENGTH);
});

test("the key length is constant for every page size the panel can send", () => {
  for (let n = 1; n <= NOTIFICATION_PAGE_LIMIT; n += 1) {
    const ids = Array.from({ length: n }, (_, index) => id(index));
    const key = notificationsReadIdempotencyKey(ids);
    assert.equal(key.length, 93, `n=${n}`);
    assert.ok(isUsableIdempotencyKey(key), `n=${n}`);
  }
});

/**
 * Idempotency is the whole reason the key exists, so bounding it must not have cost it. The same
 * SET is one write however it is ordered; a different set is a different write.
 */
test("the key is stable for the same id set and order-independent", () => {
  const ids = [id(3), id(1), id(2)];
  const key = notificationsReadIdempotencyKey(ids);
  assert.equal(notificationsReadIdempotencyKey([...ids].reverse()), key);
  assert.equal(notificationsReadIdempotencyKey([id(1), id(2), id(3)]), key);
});

test("a different id set gets its own key, including a strict subset and a superset", () => {
  const three = notificationsReadIdempotencyKey([id(1), id(2), id(3)]);
  assert.notEqual(notificationsReadIdempotencyKey([id(1), id(2)]), three);
  assert.notEqual(notificationsReadIdempotencyKey([id(1), id(2), id(3), id(4)]), three);
  // A truncated concatenation would have collided these two, which is why the fix is a hash:
  // the second, genuinely different write would have been swallowed as a replay.
  const longA = Array.from({ length: 20 }, (_, index) => id(index));
  const longB = [...longA.slice(0, 19), id(99)];
  assert.notEqual(notificationsReadIdempotencyKey(longA), notificationsReadIdempotencyKey(longB));
});

test("the key derived from a real mark-all-read click is within limits", () => {
  const feed = {
    items: Array.from({ length: NOTIFICATION_PAGE_LIMIT }, (_, index) => ({
      notification_request_id: id(index),
      notification_type: "leadership_task_mention",
      title: "Mentioned",
      body: "",
      status: "sent",
      requested_at: "2026-09-18T06:00:00Z",
      read_at: null,
      context: { group_key: "leadership_tasks" },
    })),
    unread_count: NOTIFICATION_PAGE_LIMIT,
    next_cursor: null,
  };
  const ids = idsToMarkAllRead(feed);
  assert.equal(ids.length, 20);
  assert.ok(isUsableIdempotencyKey(notificationsReadIdempotencyKey(ids)));
});

test("the bound mirrors the backend gate on both ends", () => {
  assert.equal(MAX_IDEMPOTENCY_KEY_LENGTH, 200);
  assert.equal(MIN_IDEMPOTENCY_KEY_LENGTH, 8);
  assert.equal(isUsableIdempotencyKey("a".repeat(201)), false);
  assert.equal(isUsableIdempotencyKey("a".repeat(7)), false);
  assert.equal(isUsableIdempotencyKey("a".repeat(200)), true);
});
