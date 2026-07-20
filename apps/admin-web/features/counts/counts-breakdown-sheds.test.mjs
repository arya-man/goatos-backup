import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { buildShedFilterOptions } from "./counts-breakdown-sheds.ts";

// Two sheds named "Castro 1", one in each park — the real-data case the backend facet exists
// for: 66 of 154 shed names exist in both parks. Sourcing from the locations master collapsed
// same-named sheds and never cascaded by park; this asserts the fix does neither.
const PARK_A = "11111111-1111-1111-1111-111111111111";
const PARK_B = "22222222-2222-2222-2222-222222222222";
const SHED_A = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa";
const SHED_B = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb";

const facetSheds = [
  { key: SHED_A, label: "Castro 1", park_id: PARK_A },
  { key: SHED_B, label: "Castro 1", park_id: PARK_B },
  // The blank-key "no shed assigned" bucket must never become a dropdown option: its
  // <option value=""> collides with the "All" sentinel.
  { key: "", label: "No shed", park_id: PARK_A },
];

test("same-named sheds in different parks stay distinct options", () => {
  const options = buildShedFilterOptions(facetSheds, "");

  // Both "Castro 1" sheds survive — not collapsed to one entry.
  assert.equal(options.length, 2);
  assert.deepEqual(
    options.map((o) => o.value).sort(),
    [SHED_A, SHED_B].sort(),
  );

  // Each option carries a composite park_id + shed_id React key, so two same-named sheds never
  // share a key, while `value` stays the bare shed UUID for the backend round-trip.
  const keys = options.map((o) => o.key);
  assert.equal(new Set(keys).size, 2, "composite keys must be distinct");
  assert.ok(keys.includes(`${PARK_A}|${SHED_A}`));
  assert.ok(keys.includes(`${PARK_B}|${SHED_B}`));

  // The blank-key bucket is dropped.
  assert.ok(!options.some((o) => o.value === ""));
});

test("selecting a park narrows the shed list to that park's sheds", () => {
  const options = buildShedFilterOptions(facetSheds, PARK_A);

  assert.equal(options.length, 1);
  assert.equal(options[0].value, SHED_A);
  assert.equal(options[0].key, `${PARK_A}|${SHED_A}`);
  // The other park's same-named shed is gone.
  assert.ok(!options.some((o) => o.value === SHED_B));
});

test("shed filter is sourced from the backend facet, not the locations master", () => {
  const source = readFileSync(new URL("./counts-breakdown.tsx", import.meta.url), "utf8");
  // Sheds come from the response facet + the shared cascade helper...
  assert.match(source, /buildShedFilterOptions\(breakdown\?\.facets\.sheds, selectedParkId\)/);
  // ...and the locations-master shed source is gone entirely.
  assert.doesNotMatch(source, /locations\.sheds/);
  assert.doesNotMatch(source, /getCensusLocations/);
});
