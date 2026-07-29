import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./milk-preparation.tsx", import.meta.url), "utf8");

test("milk preparation renders the backend-owned worklist and whole-scope summary", () => {
  assert.match(source, /getMilkPreparation\(/);
  assert.match(source, /summary\.total_required_ml/);
  assert.match(source, /summary\.citric_acid_grams/);
	assert.match(source, /row\.verification_status/);
	assert.match(source, /summary\.pending_verification_park_count/);
  assert.doesNotMatch(source, /rows\.reduce/);
  assert.doesNotMatch(source, /getCountsBreakdown\(/);
});

test("milk preparation reuses the shared Feed Packing worklist anatomy", () => {
  assert.match(source, /WorklistFilters/);
  assert.match(source, /WorklistPager/);
  assert.match(source, /className="feed-table"/);
  assert.match(source, /className="grid g4"/);
});
