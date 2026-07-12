import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  API_LATENCY_POLICY_MS,
  normalizeApiLatencyEndpoint,
  normalizeApiLatencyEndpoints,
} from "./api-latency-policy.mjs";

test("fills missing thresholds with the hard API latency policy", () => {
  assert.deepEqual(
    normalizeApiLatencyEndpoint({ name: "calendar", path: "/calendar" }),
    { name: "calendar", path: "/calendar", ...API_LATENCY_POLICY_MS },
  );
});

test("accepts exact and stricter percentile thresholds", () => {
  assert.deepEqual(
    normalizeApiLatencyEndpoints([
      { name: "exact", ...API_LATENCY_POLICY_MS },
      { name: "stricter", p90_ms: 200, p95_ms: 400, p99_ms: 800 },
    ]).map(({ p90_ms, p95_ms, p99_ms }) => ({ p90_ms, p95_ms, p99_ms })),
    [
      API_LATENCY_POLICY_MS,
      { p90_ms: 200, p95_ms: 400, p99_ms: 800 },
    ],
  );
});

test("the committed vaccination manifest satisfies the hard policy", () => {
  const manifest = JSON.parse(readFileSync(new URL("./hot-paths.vaccination.json", import.meta.url), "utf8"));
  assert.doesNotThrow(() => normalizeApiLatencyEndpoints(manifest.endpoints));
});

for (const [key, ceilingMs] of Object.entries(API_LATENCY_POLICY_MS)) {
  test(`rejects a manifest that relaxes ${key}`, () => {
    assert.throws(
      () => normalizeApiLatencyEndpoint({ name: "relaxed", [key]: ceilingMs + 1 }),
      new RegExp(`${key}=\\d+ms exceeds the hard ${ceilingMs}ms ceiling`),
    );
  });
}

test("rejects non-monotonic percentile thresholds", () => {
  assert.throws(
    () => normalizeApiLatencyEndpoint({ name: "invalid", p90_ms: 300, p95_ms: 250, p99_ms: 1000 }),
    /p90 <= p95 <= p99/,
  );
});
