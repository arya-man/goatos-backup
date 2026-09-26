import assert from "node:assert/strict";
import test from "node:test";

import {
  notificationAction,
  notificationChips,
  notificationInitials,
  applyLocallyRead,
  idsToMarkAllRead,
  isNotificationRead,
  leadershipTaskNotificationHref,
  normalizeNotificationIds,
  notificationBadgeCount,
  notificationGroupKey,
  notificationHref,
  sortNotificationsNewestFirst,
  unreadNotifications,
} from "./notification-model.ts";

const TASK_ID = "11111111-2222-3333-4444-555555555555";

function row(over = {}) {
  return {
    notification_request_id: "n-1",
    notification_type: "leadership_task_mention",
    title: "Ravi mentioned you",
    body: "on GT-104",
    status: "sent",
    requested_at: "2026-09-18T04:00:00Z",
    context: { task_id: TASK_ID, group_key: "leadership_tasks" },
    ...over,
  };
}

test("a leadership-task mention links to team_progress, the one scope that can show it", () => {
  // assigned_to_me would be EMPTY for a mentioned bystander, so the link would look broken for
  // exactly the person the notification was for.
  assert.equal(
    leadershipTaskNotificationHref(TASK_ID),
    `/tasks?scope=team_progress&task=${TASK_ID}`,
  );
  assert.equal(notificationHref(row()), `/tasks?scope=team_progress&task=${TASK_ID}`);
});

test("the Android href is never used, and a bad task id gets NO link", () => {
  // context.href is "/leadership-tasks/<id>" -- the Android route, a 404 in the browser.
  assert.equal(notificationHref(row({ context: { href: "/leadership-tasks/x" } })), undefined);
  assert.equal(notificationHref(row({ context: { task_id: "not-a-uuid" } })), undefined);
  assert.equal(notificationHref(row({ context: undefined })), undefined);
  // An off-site destination cannot be smuggled in through the task id either.
  assert.equal(notificationHref(row({ context: { task_id: "//evil.example" } })), undefined);
});

test("an unknown notification type still renders and still links", () => {
  const future = row({ notification_type: "leadership_task_comment_added" });
  assert.equal(notificationHref(future), `/tasks?scope=team_progress&task=${TASK_ID}`);
  assert.equal(notificationGroupKey(future), "leadership_tasks");
  // No group_key yet? The type is the section, so nothing lands in a nameless bucket.
  assert.equal(notificationGroupKey(row({ context: { task_id: TASK_ID } })), "leadership_task_mention");
});

test("newest first, with a stable tiebreak on equal timestamps", () => {
  const items = [
    row({ notification_request_id: "b", requested_at: "2026-09-18T04:00:00Z" }),
    row({ notification_request_id: "c", requested_at: "2026-09-18T06:00:00Z" }),
    row({ notification_request_id: "a", requested_at: "2026-09-18T04:00:00Z" }),
  ];
  assert.deepEqual(
    sortNotificationsNewestFirst(items).map((item) => item.notification_request_id),
    ["c", "a", "b"],
  );
  // The input is not mutated: the panel re-sorts on every render.
  assert.equal(items[0].notification_request_id, "b");
});

test("read is either the status or a read_at stamp", () => {
  assert.equal(isNotificationRead(row()), false);
  assert.equal(isNotificationRead(row({ status: "read" })), true);
  assert.equal(isNotificationRead(row({ read_at: "2026-09-18T07:00:00Z" })), true);
  assert.equal(unreadNotifications([row(), row({ status: "read" })]).length, 1);
});

test("the badge keeps the SERVER total when it exceeds the page of rows served", () => {
  // The page is capped, so a badge that shrank on open would be a lie.
  const feed = { items: [row(), row({ notification_request_id: "n-2" })], unread_count: 12, available: true };
  assert.equal(notificationBadgeCount(feed), 12);
  // One click clears one, from both sides of the max.
  assert.equal(notificationBadgeCount(feed, ["n-1"]), 11);
  assert.equal(notificationBadgeCount({ ...feed, unread_count: 0 }, ["n-1"]), 1);
  assert.equal(notificationBadgeCount({ ...feed, unread_count: 0 }, ["n-1", "n-2"]), 0);
  // A nonsense total never renders a negative or NaN badge.
  assert.equal(notificationBadgeCount({ items: [], unread_count: Number.NaN, available: true }), 0);
  assert.equal(notificationBadgeCount({ items: [], unread_count: -4, available: true }), 0);
});

test("optimism lives in ONE place, so a refresh cannot un-read a row", () => {
  const feed = { items: [row(), row({ notification_request_id: "n-2" })], unread_count: 2, available: true };
  const shown = applyLocallyRead(feed, ["n-1"]);
  assert.equal(shown.items.find((item) => item.notification_request_id === "n-1").status, "read");
  assert.equal(isNotificationRead(shown.items.find((item) => item.notification_request_id === "n-2")), false);
  assert.equal(shown.unread_count, 1);
  // Idempotent: applying the same set twice changes nothing.
  assert.deepEqual(applyLocallyRead(shown, ["n-1"]).unread_count, 1);
});

test("mark-all sends the ids still unread, never a timestamp sweep", () => {
  // A sweep would silently swallow a row that arrived while the panel was open.
  const feed = {
    items: [row(), row({ notification_request_id: "n-2" }), row({ notification_request_id: "n-3", status: "read" })],
    unread_count: 2,
    available: true,
  };
  assert.deepEqual(idsToMarkAllRead(feed), ["n-1", "n-2"]);
  assert.deepEqual(idsToMarkAllRead(feed, ["n-1"]), ["n-2"]);
  assert.deepEqual(idsToMarkAllRead({ items: [], unread_count: 0, available: true }), []);
});

test("blank and duplicate ids are dropped before a write leaves", () => {
  assert.deepEqual(normalizeNotificationIds([" n-1 ", "n-1", "", "  ", "n-2"]), ["n-1", "n-2"]);
  assert.deepEqual(normalizeNotificationIds([]), []);
});

test("inline action only for a task id or a load id; chips from short context values; initials", () => {
  const base = { notification_request_id: "n", notification_type: "x", title: "t", body: "", status: "queued", requested_at: "2026-09-19T00:00:00Z" };
  const task = "3f1c2a7e-9b1d-4c0a-8e2f-5a6b7c8d9e0f";
  assert.equal(notificationAction({ ...base, context: { task_id: task } }).kind, "task");
  assert.equal(notificationAction({ ...base, context: { load_id: task, load_ref: "101" } }).href, `/sales/loads?load=${task}`);
  assert.equal(notificationAction({ ...base, context: { load_id: "not-a-uuid" } }), undefined);
  assert.equal(notificationAction({ ...base, context: { href: "/evil" } }), undefined);
  assert.deepEqual(notificationAction({ ...base, context: { screen: "sales_loads" } }), { kind: "load", href: "/sales/loads" });
  assert.equal(notificationAction({ ...base, context: { screen: "leadership_task" } }), undefined);
  assert.deepEqual(notificationChips({ ...base, context: { task_no: "T-214", priority: "high", status: "open", farm: "CPT" } }), ["T-214", "high", "open"]);
  assert.deepEqual(notificationChips({ ...base, context: { status: "x".repeat(30) } }), []);
  assert.deepEqual(notificationChips({ ...base, context: { priority: "normal", task_no: "T-1" } }), ["T-1"]);
  assert.equal(notificationInitials("Ramesh Reddy"), "RR");
  assert.equal(notificationInitials("Ravi"), "R");
  assert.equal(notificationInitials(undefined), "");
});
