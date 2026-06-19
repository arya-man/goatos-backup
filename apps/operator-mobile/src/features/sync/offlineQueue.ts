import type { SubmitTaskRequest } from "../../shared/api/client.js";

export type QueueItem = {
  queue_id: string;
  task_id: string;
  body: SubmitTaskRequest;
  status: "draft" | "queued" | "syncing" | "synced" | "failed";
  attempt_count: number;
  last_error?: string;
};

export function enqueueDraft(existing: QueueItem[], item: Omit<QueueItem, "status" | "attempt_count">): QueueItem[] {
  const duplicate = existing.find((entry) => entry.body.idempotency_key === item.body.idempotency_key);
  if (duplicate) return existing;
  return [...existing, { ...item, status: "queued", attempt_count: 0 }];
}

export function markSyncing(item: QueueItem): QueueItem {
  const { last_error: _lastError, ...rest } = item;
  return { ...rest, status: "syncing", attempt_count: item.attempt_count + 1 };
}

export function markSynced(item: QueueItem): QueueItem {
  const { last_error: _lastError, ...rest } = item;
  return { ...rest, status: "synced" };
}

export function markFailed(item: QueueItem, error: string): QueueItem {
  return { ...item, status: "failed", last_error: error };
}
