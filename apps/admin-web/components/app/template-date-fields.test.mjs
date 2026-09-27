// Guard: template-date-fields (TR1-#22). Filter-bar dates are the template's MUI X DatePicker (label on
// the border, trailing calendar icon, DD/MM/YYYY): a single day in WorklistFilters, a start / end pair
// in DateRangeField. No custom calendar inside an outlined TextField (inputComponent) any more.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const worklist = readFileSync(new URL("../worklist-filters.tsx", import.meta.url), "utf8");
const range = readFileSync(new URL("./date-range-field.tsx", import.meta.url), "utf8");

test("template-date-fields: DatePicker, not a themed calendar in a TextField", () => {
  assert.match(worklist, /<DatePicker\b[\s\S]{0,200}format="DD\/MM\/YYYY"/);
  assert.doesNotMatch(worklist, /ThemedDateInput|inputComponent: ThemedDate/);
  assert.equal((range.match(/<DatePicker\b/g) ?? []).length, 2, "start + end pickers");
  assert.match(range, /format="DD\/MM\/YYYY"/);
  assert.doesNotMatch(range, /inputComponent|ThemedDatePicker/);
});
