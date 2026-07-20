import { test } from "node:test";
import assert from "node:assert";

import { isLifecycleEmpty } from "./feed-lifecycle-state.ts";

// The maintainer's decision (2026-07-20): a day with no issued sheet now GENERATES a `preview` with
// rows instead of showing the empty "No sheet issued" wall. So `preview` must NEVER be treated as a
// lifecycle-empty wall — the table/KPIs must render whenever the preview carries rows.

const lc = (state) => ({ state, amendment_count: 0, workflows: [] });

test("preview with rows is NOT lifecycle-empty (table/KPIs render)", () => {
  assert.strictEqual(isLifecycleEmpty(lc("preview"), 12), false);
  assert.strictEqual(isLifecycleEmpty(lc("preview"), 1), false);
});

test("preview with ZERO rows is still NOT a lifecycle wall (genuinely-empty park → normal empty state)", () => {
  assert.strictEqual(isLifecycleEmpty(lc("preview"), 0), false);
});

test("a frozen sheet with rows is not lifecycle-empty", () => {
  for (const state of ["issued", "amended", "locked", "draft"]) {
    assert.strictEqual(isLifecycleEmpty(lc(state), 5), false);
    assert.strictEqual(isLifecycleEmpty(lc(state), 0), false);
  }
});

test("only pending/not_issued with zero rows collapse the table into the banner", () => {
  assert.strictEqual(isLifecycleEmpty(lc("pending"), 0), true);
  assert.strictEqual(isLifecycleEmpty(lc("not_issued"), 0), true);
  // With rows present, even those states render the table.
  assert.strictEqual(isLifecycleEmpty(lc("pending"), 3), false);
  assert.strictEqual(isLifecycleEmpty(lc("not_issued"), 3), false);
});

// beyond_horizon (day outside [today, tomorrow] with no issued sheet) always has zero rows and is
// explained by the banner, exactly like the nothing-issued states — the backend REFUSED to generate
// rather than fabricate a sheet.
test("beyond_horizon collapses the table into the banner (always zero rows)", () => {
  assert.strictEqual(isLifecycleEmpty(lc("beyond_horizon"), 0), true);
});
