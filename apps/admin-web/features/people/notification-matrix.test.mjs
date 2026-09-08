// The Notifications matrix (maintainer decision 2026-09-08) -- three properties this pins:
//
// 1. Rows group under the BACKEND's module order and never lose an alert whose module the
//    backend did not list; the draft/dirty arithmetic is order-insensitive.
// 2. The write is capability-gated through the compiled `edit_notifications` control -- never a
//    role-string conditional -- and every visible word is a backend copy key or matrix payload.
// 3. The save goes through a Server Action with the row_version the screen loaded, and the tab
//    is mounted by the People shell under the backend-owned `notifications` tab key.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import test from "node:test";

const { groupAlertsByModule, toggleDesignation, sameDesignations, isDirty } = await import(
  "./notification-matrix-rows.ts"
);

const alert = (key, module, designations, extra = {}) => ({
  key,
  module,
  module_label: module,
  label: key,
  blurb: "",
  default_designations: designations,
  designations,
  customised: false,
  row_version: 0,
  ...extra,
});

test("alerts group under the backend module order and keep an unlisted module", () => {
  const groups = groupAlertsByModule(
    [
      { key: "feed", label: "Feed" },
      { key: "weighing", label: "Weighing" },
      { key: "empty", label: "Nothing here" },
    ],
    [
      alert("weighing.submitted", "weighing", ["growth_director"]),
      alert("feed.low_stock", "feed", ["ceo_internal"]),
      alert("procurement.load_overdue", "procurement", ["ceo_internal"], { module_label: "Procurement" }),
    ],
  );
  assert.deepEqual(
    groups.map((g) => [g.label, g.alerts.map((a) => a.key)]),
    [
      ["Feed", ["feed.low_stock"]],
      ["Weighing", ["weighing.submitted"]],
      ["Procurement", ["procurement.load_overdue"]],
    ],
  );
});

test("toggle and dirty arithmetic are order-insensitive", () => {
  const row = alert("feed.low_stock", "feed", ["feed_director", "ceo_internal"]);
  assert.equal(isDirty(row, ["ceo_internal", "feed_director"]), false);
  const next = toggleDesignation(["ceo_internal", "feed_director"], "park_head");
  assert.deepEqual(next, ["ceo_internal", "feed_director", "park_head"]);
  assert.equal(isDirty(row, next), true);
  assert.deepEqual(toggleDesignation(next, "park_head"), ["ceo_internal", "feed_director"]);
  assert.equal(sameDesignations([], []), true);
  assert.equal(sameDesignations(["a"], []), false);
});

const source = readFileSync(fileURLToPath(new URL("./notification-matrix.tsx", import.meta.url)), "utf8");
const shell = readFileSync(fileURLToPath(new URL("./people-page.tsx", import.meta.url)), "utf8");
const actions = readFileSync(fileURLToPath(new URL("./notification-actions.ts", import.meta.url)), "utf8");

const stripComments = (text) => text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");

test("the write is gated on the compiled edit_notifications control, never a role string", () => {
  assert.match(source, /controlEnabled\(pageContract, "edit_notifications", false\)/);
  assert.doesNotMatch(stripComments(source), /ceo_internal|pc_director|growth_director|"role"/);
});

test("every visible word is a backend copy key or the matrix payload", () => {
  for (const key of [
    "notifications.intro",
    "notifications.always_told",
    "notifications.column.alert",
    "notifications.chip.default",
    "notifications.chip.custom",
    "notifications.chip.nobody",
    "notifications.action.save",
    "notifications.action.saving",
    "notifications.action.use_default",
    "notifications.saved",
    "notifications.empty",
  ]) {
    assert.ok(source.includes(`"${key}"`), `matrix must read copy key ${key}`);
  }
  // No internal words in visible JSX text (copy firewall); comments are stripped first.
  assert.doesNotMatch(stripComments(source), />[^<{]*\b(backend|API|payload|debug)\b[^<{]*</);
});

test("save carries the loaded row_version through the Server Action and the shell mounts the tab", () => {
  assert.match(source, /row_version:\s*row\.alert\.row_version/);
  assert.match(actions, /^"use server";/);
  assert.match(actions, /saveNotificationAudience\(alertKey, body\)/);
  assert.match(shell, /active === "notifications"/);
});
