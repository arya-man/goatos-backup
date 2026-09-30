import assert from "node:assert/strict";
import test from "node:test";

import { rupeesLabel, todayKey } from "./format.ts";

test("rupees read the Indian way, like the backend's label", () => {
  assert.equal(rupeesLabel(0), "₹0");
  assert.equal(rupeesLabel(500), "₹500");
  assert.equal(rupeesLabel(1500), "₹1,500");
  assert.equal(rupeesLabel(100000), "₹1,00,000");
  assert.equal(rupeesLabel(12345678), "₹1,23,45,678");
});

test("today is the IST day, even late in the UTC evening", () => {
  assert.equal(todayKey(new Date("2026-09-30T20:00:00Z")), "2026-10-01");
  assert.equal(todayKey(new Date("2026-09-30T10:00:00Z")), "2026-09-30");
});
