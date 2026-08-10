import assert from "node:assert/strict";
import test from "node:test";

import { worklistFilterShownValue } from "./worklist-filter-value.ts";

// Regression cover for the Feed Config filter defect reproduced 2026-08-10: clearing a filter left
// the control showing its OLD value for the whole server round trip, which reads as "the filter is
// not applying". These assert the DECISION, not the spelling of the page that calls it — the
// source-regex tests around this component all passed while the bug shipped.

test("a pending clear renders empty instead of falling back to the stale server value", () => {
  // The exact defect: URL was ?fc_tag=Pregnant, operator picked All, parameter is now absent, and
  // the server has not answered yet. Before the fix this returned "Pregnant".
  assert.equal(worklistFilterShownValue(null, "Pregnant", true, true), "");
});

test("a pending Clear all renders every clearable control empty", () => {
  for (const [server, param] of [["Pregnant", "fc_tag"], ["Boer", "fc_group"], ["gt", "fc_grams_op"]]) {
    assert.equal(worklistFilterShownValue(null, server, true, true), "", `${param} should clear`);
  }
});

test("a pending SET renders the newly chosen value", () => {
  // Setting was never broken, because the parameter is present — pinned so a future fix to the clear
  // path cannot regress it.
  assert.equal(worklistFilterShownValue("Milking", "Pregnant", true, true), "Milking");
});

test("an un-clearable control keeps the server value when a sibling filter is pending", () => {
  // The Park picker (allowAll: false) is legitimately absent from the URL whenever the park came
  // from the top bar or the locations fallback. Blanking it because Shed tag changed would empty a
  // control nobody touched — the regression a naive "absent means cleared" rule would introduce.
  assert.equal(worklistFilterShownValue(null, "00000000-0000-4000-8000-000000003002", true, false),
    "00000000-0000-4000-8000-000000003002");
});

test("with no navigation pending an absent parameter falls back to the server value", () => {
  // Steady state: the server has answered and owns the truth again.
  assert.equal(worklistFilterShownValue(null, "Pregnant", false, true), "Pregnant");
});

test("an explicitly empty parameter is honoured, never treated as absent", () => {
  assert.equal(worklistFilterShownValue("", "Pregnant", true, true), "");
  assert.equal(worklistFilterShownValue("", "Pregnant", false, true), "");
});
