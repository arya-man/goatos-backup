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
