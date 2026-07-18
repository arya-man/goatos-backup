import assert from "node:assert/strict";
import test from "node:test";

import { validateRequestPathUsage } from "./request-path-evidence.mjs";

function passingStats() {
  return {
    tables: {
      goats: { seq_scan: 0, idx_scan: 12 },
      obligation_instances: { seq_scan: 0, idx_scan: 18 },
      vaccination_completions: { seq_scan: 0, idx_scan: 4 },
      protocol_rules: { seq_scan: 0, idx_scan: 2 },
      locations: { seq_scan: 0, idx_scan: 8 },
    },
  };
}

test("accepts canonical request reads without deleted table reads", () => {
  assert.deepEqual(validateRequestPathUsage(passingStats()), []);
});

test("rejects a missing canonical table observation", () => {
  const stats = passingStats();
  delete stats.tables.obligation_instances;
  assert.ok(validateRequestPathUsage(stats).some((failure) => failure.includes("obligation_instances")));
});

test("rejects any read from a deleted table family", () => {
  const stats = passingStats();
  stats.tables.process_integrity_projection_rows = { seq_scan: 1, idx_scan: 0 };
  const failures = validateRequestPathUsage(stats);
  assert.ok(failures.some((failure) => failure.includes("process_integrity_projection_rows was read")));
});
