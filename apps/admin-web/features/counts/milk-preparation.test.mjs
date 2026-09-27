import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./milk-preparation.tsx", import.meta.url), "utf8");

test("milk preparation renders the backend-owned worklist and whole-scope summary", () => {
  assert.match(source, /getMilkPreparation\(/);
  assert.match(source, /summary\.total_required_ml/);
  assert.match(source, /summary\.citric_acid_grams/);
	assert.match(source, /row\.verification_status/);
	// FARM count, not park: 000096_milk_actions_farm_grain.sql moved milk to farm grain and the
	// generated client exposes *_farm_count. This assertion previously named the park field, so it
	// matched a page that could not type-check against the real contract -- a source-regex test
	// pins spelling, never the contract, so it agreed with the bug instead of catching it.
	assert.match(source, /summary\.pending_verification_farm_count/);
  assert.doesNotMatch(source, /rows\.reduce/);
  assert.doesNotMatch(source, /getCountsBreakdown\(/);
});

test("milk preparation is composed on the template invoice-list anatomy", () => {
  assert.match(source, /WorklistFilters/);
  assert.match(source, /WorklistPager/);
  // KPI row = template CourseWidgetSummary via the KpiWidget adapter, farm states = InvoiceAnalytic strip, list = Scrollbar +
  // TableHeadCustom with soft Labels; the legacy .tag/.feed-table markup and Tag primitive are gone.
  assert.match(source, /<KpiWidget/);
  assert.match(source, /<InvoiceAnalytic/);
  assert.match(source, /<TableHeadCustom/);
  assert.doesNotMatch(source, /className="tag |<Tag |className="feed-table"/);
});
