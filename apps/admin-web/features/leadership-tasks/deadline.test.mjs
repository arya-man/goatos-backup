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
