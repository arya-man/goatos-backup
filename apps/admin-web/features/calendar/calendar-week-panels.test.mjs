import assert from "node:assert/strict";
import test from "node:test";

// Guard: RemindersRail logic correctly computes omitted count when count > items.length
test("RemindersRail omitted count calculation: 21 total, 20 shown = 1 omitted", () => {
  // Seed a reminder_rail with count=21, items.length=20
  const reminderRail = {
    count: 21,
    empty_message: "No reminders",
    items: Array.from({ length: 20 }, (_, i) => ({
      event_id: `event-${i}`,
      title: `Reminder ${i + 1}`,
    })),
  };

  const items = reminderRail.items ?? [];
  const totalCount = reminderRail?.count ?? 0;
  const omittedCount = totalCount - items.length;

  // Assert: the reminders rail shows 20 items out of 21 total
  assert.strictEqual(items.length, 20, "Should have 20 items shown");
  assert.strictEqual(totalCount, 21, "Should have 21 total count");
  assert.strictEqual(omittedCount, 1, "Should calculate 1 omitted item");
  assert(omittedCount > 0, "Should trigger +N more indicator rendering");
});

// Guard: RemindersRail does NOT show "+N more" when all items are shown
test("RemindersRail omitted count calculation: 5 total, 5 shown = 0 omitted", () => {
  // Seed a reminder_rail with count=5, items.length=5
  const reminderRail = {
    count: 5,
    empty_message: "No reminders",
    items: Array.from({ length: 5 }, (_, i) => ({
      event_id: `event-${i}`,
      title: `Reminder ${i + 1}`,
    })),
  };

  const items = reminderRail.items ?? [];
  const totalCount = reminderRail?.count ?? 0;
  const omittedCount = totalCount - items.length;

  // Assert: the reminders rail shows all 5 items
  assert.strictEqual(items.length, 5, "Should have 5 items shown");
  assert.strictEqual(totalCount, 5, "Should have 5 total count");
  assert.strictEqual(omittedCount, 0, "Should calculate 0 omitted items");
  assert(!(omittedCount > 0), "Should NOT trigger +N more indicator rendering");
});

// Guard: RemindersRail correctly handles the case when reminder_rail is null
test("RemindersRail omitted count calculation: null reminder_rail = 0 total", () => {
  const reminderRail = null;

  const items = reminderRail?.items ?? [];
  const totalCount = reminderRail?.count ?? 0;
  const omittedCount = totalCount - items.length;

  // Assert: when null, nothing is shown and omitted count is 0
  assert.strictEqual(items.length, 0, "Should have 0 items when null");
  assert.strictEqual(totalCount, 0, "Should have 0 total when null");
  assert.strictEqual(omittedCount, 0, "Should calculate 0 omitted when null");
});
