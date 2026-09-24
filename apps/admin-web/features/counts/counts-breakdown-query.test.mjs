import assert from "node:assert/strict";
import test from "node:test";

import { breakdownFilterQuery, PARK_PARAM } from "./counts-breakdown-query.ts";
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

test("a park change drops pens of the old park; the same park keeps them", () => {
  const pen = `shed-1|Part 1`;
  const changed = new URLSearchParams(
    breakdownFilterQuery(`scope_mode=park&park=${CBE}&bd_shed=${encodeURIComponent(pen)}`, PARAMS, { park: [CPT], bd_shed: [pen] }),
  );
  assert.equal(changed.getAll("bd_shed").length, 0);
  const same = new URLSearchParams(
    breakdownFilterQuery(`scope_mode=park&park=${CBE}`, PARAMS, { park: [CBE], bd_shed: [pen] }),
  );
  assert.deepEqual(same.getAll("bd_shed"), [pen]);
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
