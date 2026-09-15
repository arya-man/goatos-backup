import assert from "node:assert/strict";
import test from "node:test";

import { compareApiLatencyEvidence } from "./api-latency-compare.mjs";

function report(overrides = {}) {
  const base = {
    git_sha: "before-sha",
    actor: { user_id: "actor-a", tenant_id: "11111111-1111-4111-8111-111111111111" },
    actor_identity_source: "/app/me",
    tenant_id: "11111111-1111-4111-8111-111111111111",
    manifest_sha256: "manifest-hash",
    iterations: 15,
    warmup: 3,
    concurrency: 1,
    dataset: {
      label: "pr264_oci_staging_refresh",
      certification_boundary: "local_oci_latency_only",
    },
    scope: {
      evidence_profile: "pr264_performance",
      included: ["weighing", "work_board", "vaccination"],
    },
    passed: true,
    results: [
      result("weighing", "/weighing/leadership/growth?sections=headline,shed_leaderboard,losing_animals"),
      result("work_board", "/work-board/page?park_id=cbe"),
      result("vaccination", "/app/vaccination/execution?include_card_summaries=true"),
    ],
  };
  return structuredClone({ ...base, ...overrides });
}

function result(name, path, overrides = {}) {
  return {
    name,
    path,
    response_observations: [{ assertion_value: 10, row_counts: { rows: 10 }, degraded: [] }],
    p90_ms: 120,
    p95_ms: 150,
    p99_ms: 220,
    response_bytes_max: 64000,
    response_bytes_threshold: 524288,
    ...overrides,
  };
}

test("accepts same-scenario before and after evidence when after is not slower", () => {
  const before = report();
  const after = report({
    git_sha: "after-sha",
    results: before.results.map((entry) => ({
      ...entry,
      p90_ms: entry.p90_ms - 20,
      p95_ms: entry.p95_ms - 20,
      p99_ms: entry.p99_ms - 20,
    })),
  });
  assert.deepEqual(compareApiLatencyEvidence(before, after), []);
});

test("rejects comparing the same commit or a different dataset shape", () => {
  assert.ok(compareApiLatencyEvidence(report(), report()).some((failure) => failure.includes("git_sha")));

  const before = report();
  const after = report({ git_sha: "after-sha", iterations: 3 });
  assert.ok(compareApiLatencyEvidence(before, after).some((failure) => failure.includes("iterations differs")));

  after.iterations = before.iterations;
  after.dataset.label = "tiny_fixture";
  assert.ok(compareApiLatencyEvidence(before, after).some((failure) => failure.includes("dataset.label differs")));
});

test("rejects missing endpoints or changed route shapes", () => {
  const before = report();
  const after = report({
    git_sha: "after-sha",
    results: before.results.filter((entry) => entry.name !== "vaccination"),
  });
  assert.ok(compareApiLatencyEvidence(before, after).some((failure) => failure.includes("missing endpoint vaccination")));

  after.results = before.results.map((entry) => entry.name === "weighing"
    ? { ...entry, path: "/weighing/leadership/growth" }
    : entry);
  assert.ok(compareApiLatencyEvidence(before, after).some((failure) => failure.includes("weighing path differs")));
});

test("rejects percentile regressions and material payload growth", () => {
  const before = report();
  const after = report({
    git_sha: "after-sha",
    results: before.results.map((entry) => entry.name === "work_board"
      ? { ...entry, p95_ms: 500, response_bytes_max: 160000 }
      : entry),
  });
  const failures = compareApiLatencyEvidence(before, after, { allowedRegressionMs: 25 });
  assert.ok(failures.some((failure) => failure.includes("work_board p95_ms regressed")));
  assert.ok(failures.some((failure) => failure.includes("work_board response_bytes_max grew")));
});

test("allows a failing before report but still requires after to pass", () => {
  const before = report({ passed: false });
  const after = report({ git_sha: "after-sha", passed: false });
  const failures = compareApiLatencyEvidence(before, after);
  assert.ok(!failures.some((failure) => failure.includes("before report did not pass")));
  assert.ok(failures.some((failure) => failure.includes("after report did not pass")));
});

test("rejects lost rows even when the latency improves", () => {
  const before = report();
  const after = report({git_sha: "after-sha"});
  after.results[0].response_observations[0].row_counts.rows = 0;
  assert.match(compareApiLatencyEvidence(before, after).join(), /cardinality decreased/);
});

test("rejects missing or different authenticated actors with otherwise equal workloads", () => {
  const before = report();
  for (const actor of [undefined, {}, { ...before.actor, user_id: "actor-b" }, { ...before.actor, tenant_id: "other" }]) {
    assert.ok(compareApiLatencyEvidence(before, report({ git_sha: "after-sha", actor })).some((f) => /actor/.test(f)));
  }
  assert.ok(compareApiLatencyEvidence(report({ actor: undefined }), report({ git_sha: "after-sha", actor: undefined })).some((f) => /actor/.test(f)));
});
