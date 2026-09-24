import assert from "node:assert/strict";
import test from "node:test";

import { breakdownFilterQuery, PARK_PARAM, withSelectedOptions } from "./counts-breakdown-query.ts";
import { parseScope } from "../../lib/scope.ts";

const CBE = "00000000-0000-4000-8000-000000003001";
const CPT = "00000000-0000-4000-8000-000000003002";
const PARAMS = [PARK_PARAM, "bd_stage", "bd_breed", "bd_shed", "bd_sex"];

test("Farm writes the shared park the other Counts pages read, not a page-private key", () => {
  const qs = breakdownFilterQuery("scope_mode=company", PARAMS, { park: [CBE] });
  const params = new URLSearchParams(qs);
  assert.equal(params.get("park"), CBE);
  assert.equal(params.get("scope_mode"), "park");
  assert.equal(params.has("bd_farm"), false);
  // The same parser Herd Analytics and Mortality use sees the park.
  assert.equal(parseScope(Object.fromEntries(params)).parkId, CBE);
});

test("clearing Farm returns every page to company-wide", () => {
  const qs = breakdownFilterQuery(`scope_mode=park&park=${CPT}`, PARAMS, { park: [] });
  const params = new URLSearchParams(qs);
  assert.equal(params.has("park"), false);
  assert.equal(params.get("scope_mode"), "company");
  assert.equal(parseScope(Object.fromEntries(params)).parkId, undefined);
});

test("an old bd_farm link is dropped on the next apply", () => {
  const qs = breakdownFilterQuery(`scope_mode=company&bd_farm=${CBE}`, PARAMS, { park: [CPT] });
  const params = new URLSearchParams(qs);
  assert.equal(params.has("bd_farm"), false);
  assert.equal(params.get("park"), CPT);
});

const CBE_PEN = "shed-cbe|Part 1";
const CPT_PEN = "shed-cpt|Part 2";
const PEN_PARKS = { [CBE_PEN]: CBE, [CPT_PEN]: CPT };

test("a park change drops the old park's pens", () => {
  const params = new URLSearchParams(
    breakdownFilterQuery(`scope_mode=park&park=${CBE}&bd_shed=${encodeURIComponent(CBE_PEN)}`, PARAMS, { park: [CPT], bd_shed: [CBE_PEN] }, PEN_PARKS),
  );
  assert.deepEqual(params.getAll("bd_shed"), []);
});

// PR #395 review: from All farms, picking CPT and a CPT pen in ONE Apply must keep the pen, or the
// page silently shows the whole farm.
test("picking a farm and one of its pens in the same Apply keeps the pen", () => {
  const params = new URLSearchParams(
    breakdownFilterQuery("scope_mode=company", PARAMS, { park: [CPT], bd_shed: [CPT_PEN, CBE_PEN] }, PEN_PARKS),
  );
  assert.equal(params.get("park"), CPT);
  assert.deepEqual(params.getAll("bd_shed"), [CPT_PEN]);
});

test("the same park, or All farms, keeps every selected pen", () => {
  const same = new URLSearchParams(
    breakdownFilterQuery(`scope_mode=park&park=${CBE}`, PARAMS, { park: [CBE], bd_shed: [CBE_PEN] }, PEN_PARKS),
  );
  assert.deepEqual(same.getAll("bd_shed"), [CBE_PEN]);
  const all = new URLSearchParams(
    breakdownFilterQuery(`scope_mode=park&park=${CBE}`, PARAMS, { park: [], bd_shed: [CBE_PEN, CPT_PEN] }, PEN_PARKS),
  );
  assert.deepEqual(all.getAll("bd_shed"), [CBE_PEN, CPT_PEN]);
});

test("a pen of unknown park is dropped on a park change rather than guessed", () => {
  const params = new URLSearchParams(
    breakdownFilterQuery("scope_mode=company", PARAMS, { park: [CPT], bd_shed: ["shed-gone|Part 9"] }, PEN_PARKS),
  );
  assert.deepEqual(params.getAll("bd_shed"), []);
});

// PR #395 review: a Stage/Breed picked under one farm and missing from the next farm's options
// must stay in the list, so it can be unticked instead of silently emptying the table.
test("a selected value the options no longer carry stays listed and removable", () => {
  const options = [{ value: "K1", label: "K1" }, { value: "Fattening", label: "Fattening" }];
  const got = withSelectedOptions(options, ["Mother", "K1"], (v) => (v === "Mother" ? "Mother (label)" : undefined));
  assert.deepEqual(got.map((o) => o.value), ["K1", "Fattening", "Mother"]);
  assert.equal(got.find((o) => o.value === "Mother")?.label, "Mother (label)");
  // Already present: never listed twice. Nothing selected: the options pass through unchanged.
  assert.equal(got.filter((o) => o.value === "K1").length, 1);
  assert.deepEqual(withSelectedOptions(options, []), options);
  // No label known: the value itself is shown rather than an empty row.
  assert.equal(withSelectedOptions(options, ["Malai"])[2].label, "Malai");
});

test("any apply resets paging and keeps unrelated params", () => {
  const params = new URLSearchParams(
    breakdownFilterQuery(`bd_page=4&bd_limit=25&as_of=2026-09-20`, PARAMS, { bd_stage: ["K3", "K2"] }),
  );
  assert.equal(params.has("bd_page"), false);
  assert.equal(params.get("bd_limit"), "25");
  assert.equal(params.get("as_of"), "2026-09-20");
  assert.deepEqual(params.getAll("bd_stage"), ["K3", "K2"]);
});
