import assert from "node:assert/strict";
import test from "node:test";

import { API_LATENCY_POLICY_MS } from "./api-latency-policy.mjs";
import { REQUIRED_HOT_PATHS, validateApiLatencyEvidence } from "./api-latency-evidence.mjs";

const sha = "0123456789abcdef";

function passingReport() {
  return {
    git_sha: sha,
    expected_sha: sha,
    manifest_sha256: "manifest-hash",
    started_at: "2026-07-12T00:00:00Z",
    finished_at: "2026-07-12T00:01:00Z",
    dataset: {
      label: "ci_scale_shaped_projection",
      animal_equivalent_cardinality: 1_000_000,
      projection_rows: 2_000_000,
      certification_boundary: "scale_shaped_projection_smoke_not_full_chain_1m_certification",
    },
    scope: {
      included: REQUIRED_HOT_PATHS,
      excluded: { bootstrap: "separate bootstrap gate" },
      evidence_boundaries: {
        million_scale_projection_backed: REQUIRED_HOT_PATHS.slice(0, 6),
        canonical_1300_nonempty_latency_only: REQUIRED_HOT_PATHS.slice(6),
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

test("rejects percentile breaches even when a forged passed flag says true", () => {
  const report = passingReport();
  report.results[0].p90_ms = 301;
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("p90_ms=301")));
});

test("rejects small fixtures and 1M certification overclaims", () => {
  const report = passingReport();
  report.dataset.animal_equivalent_cardinality = 1_300;
  report.dataset.certification_boundary = "full_chain_1m_certified";
  const failures = validateApiLatencyEvidence(report, sha);
  assert.ok(failures.some((failure) => failure.includes("1M animal-equivalent")));
  assert.ok(failures.some((failure) => failure.includes("overclaims")));
});

test("rejects oversized or unmeasured response payloads", () => {
  const report = passingReport();
  report.results[0].response_bytes_max = 524_289;
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("response_bytes_max")));
  report.results[0].response_bytes_max = 0;
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("no measured response payload")));
});
