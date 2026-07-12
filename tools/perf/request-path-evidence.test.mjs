import assert from "node:assert/strict";
import test from "node:test";

import { validateRequestPathUsage } from "./request-path-evidence.mjs";

function passingStats() {
  return {
    tables: {
      process_integrity_projection_rows: { seq_scan: 0, idx_scan: 4 },
      process_integrity_projection_summaries: { seq_scan: 0, idx_scan: 3 },
      calendar_event_projections: { seq_scan: 0, idx_scan: 1 },
      vaccination_shed_projection_rows: { seq_scan: 0, idx_scan: 1 },
      vaccination_execution_projection_rows: { seq_scan: 0, idx_scan: 1 },
      vaccination_operations_projection_rows: { seq_scan: 0, idx_scan: 2 },
      goats: { seq_scan: 0, idx_scan: 0 },
      obligation_instances: { seq_scan: 0, idx_scan: 0 },
      vaccination_completions: { seq_scan: 0, idx_scan: 0 },
    },
  };
}

test("accepts indexed projection reads without canonical source reads", () => {
  assert.deepEqual(validateRequestPathUsage(passingStats()), []);
});

test("rejects a projection table that merely exists off the request path", () => {
  const stats = passingStats();
  stats.tables.vaccination_shed_projection_rows.idx_scan = 0;
  assert.ok(validateRequestPathUsage(stats).some((failure) => failure.includes("vaccination_shed_projection_rows")));
});

test("allows a small-fixture projection scan but rejects canonical compute-on-read access", () => {
  const stats = passingStats();
  stats.tables.process_integrity_projection_rows.seq_scan = 1;
  stats.tables.goats.idx_scan = 20;
  const failures = validateRequestPathUsage(stats);
  assert.ok(!failures.some((failure) => failure.includes("process_integrity_projection_rows")));
  assert.ok(failures.some((failure) => failure.includes("goats was read")));
});
