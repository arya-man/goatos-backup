import assert from "node:assert/strict";
import test from "node:test";
import { notificationKind, notificationTabCounts, relativeNotificationTime } from "./notification-kind.ts";

test("every backend notification type maps to a tone, icon and category; unknown falls back", () => {
  assert.deepEqual(notificationKind("leadership_task_mentioned"), { tone: "violet", icon: "at-sign", category: "Mention" });
  assert.equal(notificationKind("leadership_task_raised").category, "Tasks");
  assert.equal(notificationKind("leadership_task_done").tone, "success");
  assert.equal(notificationKind("procurement_load_overdue").icon, "clock");
  assert.equal(notificationKind("procurement_load_overdue").tone, "warning");
  assert.equal(notificationKind("weighing_due").icon, "clock");
  assert.equal(notificationKind("leave_request_decided").tone, "primary");
  assert.equal(notificationKind("leave_request_raised").tone, "warning");
  assert.equal(notificationKind("feed_low_stock").category, "Feed");
  assert.equal(notificationKind("escalation").tone, "error");
  assert.deepEqual(notificationKind("something_new"), { tone: "neutral", icon: "bell", category: "Update" });
  assert.deepEqual(notificationKind(undefined), notificationKind(""));
});

test("relative time is short, past-only, and empty beyond a week or for junk", () => {
  const now = Date.parse("2026-09-19T10:00:00Z");
  assert.equal(relativeNotificationTime("2026-09-19T09:59:40Z", now), "just now");
  assert.equal(relativeNotificationTime("2026-09-19T09:15:00Z", now), "45m ago");
  assert.equal(relativeNotificationTime("2026-09-19T04:00:00Z", now), "6h ago");
  assert.equal(relativeNotificationTime("2026-09-16T10:00:00Z", now), "3d ago");
  assert.equal(relativeNotificationTime("2026-09-01T10:00:00Z", now), "");
  assert.equal(relativeNotificationTime("2026-09-19T11:00:00Z", now), "");
  assert.equal(relativeNotificationTime("not a date", now), "");
});

test("tab counts: All shows a server total or nothing, never the page size; unread never undercounts", () => {
  const items = [
    { status: "queued" },
    { status: "read" },
    { status: "sent", read_at: "2026-09-19T00:00:00Z" },
  ];
  assert.deepEqual(notificationTabCounts(items, 0), { all: undefined, unread: 1, archived: undefined });
  assert.deepEqual(notificationTabCounts(items, 12), { all: undefined, unread: 12, archived: undefined });
  assert.deepEqual(notificationTabCounts(items, 12, 4500), { all: 4500, unread: 12, archived: 4488 });
  assert.deepEqual(notificationTabCounts([], Number.NaN), { all: undefined, unread: 0, archived: undefined });
});
