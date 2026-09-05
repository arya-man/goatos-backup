import assert from "node:assert/strict";
import test from "node:test";

import { API_LATENCY_POLICY_MS } from "./api-latency-policy.mjs";
import { REQUIRED_HOT_PATHS, validateApiLatencyEvidence } from "./api-latency-evidence.mjs";

const sha = "0123456789abcdef";

function passingReport() {
  return {
    git_sha: sha,
    worktree_dirty: false,
    worktree_diff_sha256: "clean-worktree-hash",
    worktree_status_short: [],
    expected_sha: sha,
    manifest_sha256: "manifest-hash",
    started_at: "2026-07-12T00:00:00Z",
    finished_at: "2026-07-12T00:01:00Z",
    dataset: {
      label: "ci_canonical_5k_50k",
      animal_equivalent_cardinality: 50_000,
      canonical_rows: 50_000,
      certification_boundary: "canonical_5k_50k_serving_read_evidence",
    },
    scope: {
      included: REQUIRED_HOT_PATHS,
      excluded: { bootstrap: "separate bootstrap gate" },
      evidence_boundaries: {
        canonical_5k_50k_serving_reads: REQUIRED_HOT_PATHS.slice(0, 7),
        local_nonempty_latency_only: REQUIRED_HOT_PATHS.slice(7),
      },
    },
    passed: true,
    results: REQUIRED_HOT_PATHS.map((name) => ({
      name,
      samples: 20,
      failures: 0,
      p90_ms: API_LATENCY_POLICY_MS.p90_ms,
      p95_ms: API_LATENCY_POLICY_MS.p95_ms,
      p99_ms: API_LATENCY_POLICY_MS.p99_ms,
      p90_threshold_ms: API_LATENCY_POLICY_MS.p90_ms,
      p95_threshold_ms: API_LATENCY_POLICY_MS.p95_ms,
      p99_threshold_ms: API_LATENCY_POLICY_MS.p99_ms,
      response_bytes_max: 64_000,
      response_bytes_threshold: 524_288,
      assertion: { type: "array_min", path: "rows", min: 1 },
      passed: true,
    })),
  };
}

test("accepts complete current-SHA live evidence", () => {
  assert.deepEqual(validateApiLatencyEvidence(passingReport(), sha), []);
});

test("rejects a policy-only or stale-SHA false green", () => {
  const report = passingReport();
  report.git_sha = "stale";
  report.results = report.results.slice(1);
  const failures = validateApiLatencyEvidence(report, sha);
  assert.ok(failures.some((failure) => failure.includes("git_sha")));
  assert.ok(failures.some((failure) => failure.includes("control_tower is missing")));
});

test("requires worktree provenance", () => {
  const report = passingReport();
  delete report.worktree_dirty;
  delete report.worktree_diff_sha256;
  delete report.worktree_status_short;
  const failures = validateApiLatencyEvidence(report, sha);
  assert.ok(failures.some((failure) => failure.includes("worktree_dirty")));
  assert.ok(failures.some((failure) => failure.includes("worktree_diff_sha256")));
  assert.ok(failures.some((failure) => failure.includes("worktree_status_short")));
});

test("requires the mobile calendar hot paths that caught the seconds-class month click", () => {
  assert.ok(REQUIRED_HOT_PATHS.includes("calendar_mobile_month_filter_options"));
  const report = passingReport();
  report.results = report.results.filter((result) => !result.name.startsWith("calendar_mobile_"));
  const failures = validateApiLatencyEvidence(report, sha);
  assert.ok(failures.some((failure) => failure.includes("calendar_mobile_month_page_20 is missing")));
  assert.ok(failures.some((failure) => failure.includes("calendar_mobile_month_filter_options is missing")));
});

test("rejects percentile breaches even when a forged passed flag says true", () => {
  const report = passingReport();
  report.results[0].p90_ms = 301;
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("p90_ms=301")));
});

test("rejects undersized fixtures and overclaimed certification", () => {
  const report = passingReport();
  report.dataset.animal_equivalent_cardinality = 1_300;
  report.dataset.certification_boundary = "full_chain_50k_certified";
  const failures = validateApiLatencyEvidence(report, sha);
  assert.ok(failures.some((failure) => failure.includes("5k animal-equivalent")));
  assert.ok(failures.some((failure) => failure.includes("overclaims")));
});

test("accepts local OCI analytics evidence without canonical scale certification", () => {
  const report = passingReport();
  report.dataset = {
    label: "analytics_local_oci_feed_synced",
    animal_equivalent_cardinality: 0,
    canonical_rows: 0,
    certification_boundary: "local_oci_latency_only",
  };
  report.scope.evidence_profile = "analytics_local_oci";
  report.scope.included = [
    "vaccination_live_tracker",
    "calendar_today_7d",
    "calendar_month_page_20",
    "weighing_growth_adg",
    "weighing_weight_demographics",
    "weighing_shed_weights",
    "feed_directed_analytics",
    "feed_execution_overview_days",
    "feed_execution_analytics",
    "feed_execution_packing_variance",
    "feed_experiment_analytics",
    "feed_stock_analytics",
    "feed_shed_feed_analytics",
  ];
  report.scope.required_hot_paths = report.scope.included;
  report.scope.evidence_boundaries = { local_oci_ceo_route_switch_reads: report.scope.included };
  report.results = report.scope.included.map((name) => ({ ...report.results[0], name }));
  assert.deepEqual(validateApiLatencyEvidence(report, sha), []);
});

test("rejects oversized or unmeasured response payloads", () => {
  const report = passingReport();
  report.results[0].response_bytes_max = 524_289;
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("response_bytes_max")));
  report.results[0].response_bytes_max = 0;
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("no measured response payload")));
});

test("rejects self-declared required hot paths that do not match the verifier profile", () => {
  const report = passingReport();
  report.scope.required_hot_paths = ["control_tower"];
  const failures = validateApiLatencyEvidence(report, sha);
  assert.ok(failures.some((failure) => failure.includes("required_hot_paths must match the verifier-owned profile")));
});

test("rejects empty array assertions unless the endpoint declares that empty is valid", () => {
  const report = passingReport();
  report.results[0].assertion = { type: "array_min", path: "rows", min: 0 };
  const failures = validateApiLatencyEvidence(report, sha);
  assert.ok(failures.some((failure) => failure.includes("array_min assertion must require at least one row")));

  report.results[0].assertion.allow_empty = true;
  assert.deepEqual(validateApiLatencyEvidence(report, sha), []);
});
