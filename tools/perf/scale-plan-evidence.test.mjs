import assert from "node:assert/strict";
import test from "node:test";

import { inspectPlan, validateCardinality, validateProcessIntegrityListSource } from "./scale-plan-evidence.mjs";

function plan(nodeType, execution = 20, relation = "process_integrity_projection_rows") {
  return JSON.stringify([{
    Plan: {
      "Node Type": "Limit",
      "Actual Rows": 51,
      Plans: [{
        "Node Type": nodeType,
        "Relation Name": relation,
        "Index Name": nodeType === "Seq Scan" ? undefined : "process_integrity_projection_rows_serving_hot_idx",
        "Actual Rows": 51,
      }],
    },
    "Execution Time": execution,
  }]);
}

test("accepts a bounded indexed million-row projection plan", () => {
  assert.equal(inspectPlan("process", plan("Index Scan")).passed, true);
});

test("rejects sequential scans and latency breaches", () => {
  assert.ok(inspectPlan("process", plan("Seq Scan")).failures.some((failure) => failure.includes("sequential")));
  assert.ok(inspectPlan("process", plan("Index Scan", 1001)).failures.some((failure) => failure.includes("1000ms")));
});

function metadata() {
  return {
    process_integrity: {
      projection_version: 9001,
      serving_projection_version: 9001,
      projection_age_seconds: 10,
      as_of_lag_seconds: 10,
      freshness_status: "green",
      serving_state: "fresh",
    },
    calendar: {
      projection_version: 9001,
      serving_projection_version: 9001,
      projection_age_seconds: 10,
      as_of_lag_seconds: 10,
      freshness_status: "fixture_fresh",
      serving_state: "direct_projection",
    },
    vaccination_shed: {
      projection_version: 9001,
      serving_projection_version: 9001,
      projection_age_seconds: 10,
      as_of_lag_seconds: 10,
      freshness_status: "green",
      serving_state: "fresh",
    },
    vaccination_execution: {
      projection_version: 9001,
      serving_projection_version: 9001,
      projection_age_seconds: 10,
      as_of_lag_seconds: 10,
      freshness_status: "green",
      serving_state: "fresh",
    },
    vaccination_operations: {
      projection_version: 9001,
      serving_projection_version: 9001,
      projection_age_seconds: 10,
      as_of_lag_seconds: 10,
      freshness_status: "green",
      serving_state: "fresh",
    },
  };
}

test("accepts the indexed vaccination-shed projection plan", () => {
  assert.equal(inspectPlan("shed", plan("Index Scan", 20, "vaccination_shed_projection_rows")).passed, true);
});

test("accepts a bounded summary scan and rejects an unbounded one", () => {
  assert.equal(inspectPlan("process_integrity_summary", plan("Seq Scan", 20, "process_integrity_projection_summaries")).passed, true);
  const unbounded = JSON.parse(plan("Seq Scan", 20, "process_integrity_projection_summaries"));
  unbounded[0].Plan.Plans[0]["Actual Rows"] = 10_001;
  assert.ok(inspectPlan("process_integrity_summary", JSON.stringify(unbounded)).failures.some((failure) => failure.includes("10,000")));
});

test("requires all hot projections to carry the scale-shaped cardinality", () => {
  assert.equal(validateCardinality({
    process_integrity_projection_rows: 1_000_000,
    process_integrity_unique_due_at: 1_000_000,
    process_integrity_projection_summaries: 4_000,
    process_integrity_summary_covered_rows: 1_000_000,
    process_integrity_summary_business_dates: 365,
    calendar_event_projections: 1_000_000,
    vaccination_shed_projection_rows: 1_000,
    vaccination_shed_projection_animals: 1_000_000,
    vaccination_execution_projection_rows: 1,
    vaccination_operations_projection_rows: 1,
    projection_metadata: metadata(),
  }).passed, true);
  assert.equal(validateCardinality({
    process_integrity_projection_rows: 1_000_000,
    process_integrity_unique_due_at: 1_000_000,
    process_integrity_projection_summaries: 4_000,
    process_integrity_summary_covered_rows: 1_000_000,
    process_integrity_summary_business_dates: 365,
    calendar_event_projections: 999_999,
    vaccination_shed_projection_rows: 1_000,
    vaccination_shed_projection_animals: 1_000_000,
    vaccination_execution_projection_rows: 1,
    vaccination_operations_projection_rows: 1,
    projection_metadata: metadata(),
  }).passed, false);
});

test("rejects stale or mismatched serving projection evidence", () => {
  const projectionMetadata = metadata();
  projectionMetadata.process_integrity.projection_age_seconds = 1801;
  projectionMetadata.vaccination_shed.serving_projection_version = 9000;
  const evidence = validateCardinality({
    process_integrity_projection_rows: 1_000_000,
    process_integrity_unique_due_at: 1_000_000,
    process_integrity_projection_summaries: 4_000,
    process_integrity_summary_covered_rows: 1_000_000,
    process_integrity_summary_business_dates: 365,
    calendar_event_projections: 1_000_000,
    vaccination_shed_projection_rows: 1_000,
    vaccination_shed_projection_animals: 1_000_000,
    vaccination_execution_projection_rows: 1,
    vaccination_operations_projection_rows: 1,
    projection_metadata: projectionMetadata,
  });
  assert.ok(evidence.failures.some((failure) => failure.includes("projection age")));
  assert.ok(evidence.failures.some((failure) => failure.includes("serving projection version")));
});

test("rejects COUNT(*) OVER in the process-integrity request list before LIMIT", () => {
  const safe = "const processIntegrityProjectionRowsSQL = `SELECT row_id FROM rows LIMIT 51`;\nconst processIntegrityProjectionCountsSQL = `SELECT 1`;";
  const unsafe = "const processIntegrityProjectionRowsSQL = `SELECT row_id, COUNT(*) OVER() FROM rows LIMIT 51`;\nconst processIntegrityProjectionCountsSQL = `SELECT 1`;";
  assert.deepEqual(validateProcessIntegrityListSource(safe), []);
  assert.ok(validateProcessIntegrityListSource(unsafe).some((failure) => failure.includes("COUNT(*) OVER")));
});
