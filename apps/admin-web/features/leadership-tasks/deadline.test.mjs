import assert from "node:assert/strict";
import { test } from "node:test";

import { farmDeadlineToRFC3339 } from "./deadline.ts";

test("a datetime-local deadline is stamped as an IST instant for the wire", () => {
  assert.equal(farmDeadlineToRFC3339("2026-09-15T17:00"), "2026-09-15T17:00:00+05:30");
  assert.equal(farmDeadlineToRFC3339(" 2026-09-15T17:00:30 "), "2026-09-15T17:00:30+05:30");
});

test("anything that is not a datetime-local value reads as no deadline", () => {
  for (const bad of ["", "2026-09-15", "15/09/2026 17:00", "2026-09-15T17", "tomorrow"]) {
    assert.equal(farmDeadlineToRFC3339(bad), "", bad);
  }
});

test("a stored deadline comes back as the farm clock's datetime-local value", async () => {
  const { rfc3339ToFarmDeadlineLocal } = await import("./deadline.ts");
  // Read as IST whatever offset the row carries, so the raiser edits the time they set.
  assert.equal(rfc3339ToFarmDeadlineLocal("2026-09-15T17:00:00+05:30"), "2026-09-15T17:00");
  assert.equal(rfc3339ToFarmDeadlineLocal("2026-09-15T11:30:00Z"), "2026-09-15T17:00");
  // Midnight IST must not render as hour 24.
  assert.equal(rfc3339ToFarmDeadlineLocal("2026-09-14T18:30:00Z"), "2026-09-15T00:00");
  // No deadline, or not an instant, leaves the field empty -- which the edit form sends as
  // "keep the stored deadline".
  for (const bad of ["", null, undefined, "   ", "tomorrow"]) {
    assert.equal(rfc3339ToFarmDeadlineLocal(bad), "", String(bad));
  }
});

test("the calendar day and the two time selects join into the datetime-local shape", async () => {
  const { composeFarmDeadlineLocal, splitFarmDeadlineLocal, farmDeadlineLocalFromForm } =
    await import("./deadline.ts");
  assert.equal(composeFarmDeadlineLocal("2026-09-20", "17", "30"), "2026-09-20T17:30");
  assert.equal(composeFarmDeadlineLocal(" 2026-09-20 ", "00", "05"), "2026-09-20T00:05");
  // A day with no time, or a time with no day, is NOT a deadline -- never midnight by default.
  assert.equal(composeFarmDeadlineLocal("2026-09-20", "", ""), "");
  assert.equal(composeFarmDeadlineLocal("", "17", "30"), "");
  assert.equal(composeFarmDeadlineLocal("20/09/2026", "17", "30"), "");
  assert.equal(composeFarmDeadlineLocal("2026-09-20", "24", "00"), "");
  assert.equal(composeFarmDeadlineLocal("2026-09-20", "7", "00"), "");
  // The round trip the edit modal makes.
  assert.deepEqual(splitFarmDeadlineLocal("2026-09-15T17:00"), { date: "2026-09-15", hour: "17", minute: "00" });
  assert.deepEqual(splitFarmDeadlineLocal(""), { date: "", hour: "", minute: "" });
  // The Server Action reads the older single field first, then the three parts.
  const old = new FormData();
  old.set("deadline_at", "2026-09-15T17:00");
  assert.equal(farmDeadlineLocalFromForm(old), "2026-09-15T17:00");
  const split = new FormData();
  split.set("deadline_date", "2026-09-15");
  split.set("deadline_hour", "17");
  split.set("deadline_minute", "00");
  assert.equal(farmDeadlineLocalFromForm(split), "2026-09-15T17:00");
  assert.equal(farmDeadlineLocalFromForm(new FormData()), "");
});
