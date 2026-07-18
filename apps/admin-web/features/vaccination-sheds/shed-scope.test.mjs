import assert from "node:assert/strict";
import test from "node:test";

import { vaccinationCurrentViewScope } from "./shed-scope.ts";

test("vaccination current-view scope strips historical as_of instead of emptying the live board", () => {
  const scope = { asOf: "2026-07-01", domain: "pc.vaccination" };

  assert.deepEqual(vaccinationCurrentViewScope(scope), {});
});

test("vaccination current-view scope still preserves park_id", () => {
  const park = "30000000-0000-4000-8000-000000000001";
  const scope = { parkId: park, asOf: "2026-07-01" };

  assert.deepEqual(vaccinationCurrentViewScope(scope), { parkId: park });
});
