import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const viewSource = readFileSync(new URL("./calendar-full-view.tsx", import.meta.url), "utf8");
const baseline = readFileSync(new URL("../../theme/app-baseline.tsx", import.meta.url), "utf8");

// pr294 L-N2 / L-N3: AppBaseline holds every page-content table at a 540px floor below 861px
// (`${CONTENT} table: { minWidth: 540 }`). FullCalendar renders its month grid and agenda list as
// tables with no horizontal scroll owner, so at 390 the month grid was 540px wide (Sat / Sun cut
// off) and the history list scrolled the page body sideways. The calendar host must release the
// floor for its own tables.
test("the shell's phone table floor exists (the thing the calendar must release)", () => {
  assert.match(baseline, /table`\]: \{ minWidth: 540 \}/);
});

test("the calendar host releases the 540px table floor for FullCalendar's tables", () => {
  assert.match(viewSource, /"& \.fc table": \{ minWidth: 0 \}/);
});
