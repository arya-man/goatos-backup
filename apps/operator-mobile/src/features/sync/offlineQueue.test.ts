import assert from "node:assert/strict";
import { test } from "node:test";

import { enqueueDraft, markFailed, markSynced, markSyncing, type QueueItem } from "./offlineQueue.js";

const item: Omit<QueueItem, "status" | "attempt_count"> = {
  queue_id: "queue-1",
  task_id: "00000000-0000-4000-8000-000000000301",
  body: {
    sop_version_id: "00000000-0000-4000-8000-000000000101",
    idempotency_key: "task-301-install-1",
    answers: { goat_id: "00000000-0000-4000-8000-000000000201" },
    proof_refs: [],
  },
};

test("enqueueDraft queues once by idempotency key", () => {
  const first = enqueueDraft([], item);
  const second = enqueueDraft(first, { ...item, queue_id: "queue-duplicate" });

  assert.equal(first.length, 1);
  assert.equal(second.length, 1);
  assert.equal(second[0]?.queue_id, "queue-1");
  assert.equal(second[0]?.status, "queued");
});

test("sync status helpers preserve payload and track attempts", () => {
  const queued = enqueueDraft([], item)[0];
  assert.ok(queued);

  const syncing = markSyncing(queued);
  const failed = markFailed(syncing, "offline");
  const retrying = markSyncing(failed);
  const synced = markSynced(retrying);

  assert.equal(syncing.attempt_count, 1);
  assert.equal(failed.status, "failed");
  assert.equal(failed.last_error, "offline");
  assert.equal(retrying.attempt_count, 2);
  assert.equal(retrying.last_error, undefined);
  assert.equal(synced.status, "synced");
  assert.equal(synced.body.idempotency_key, item.body.idempotency_key);
});
