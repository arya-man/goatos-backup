import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";

import {
  API_LATENCY_POLICY_MS,
  API_RESPONSE_BYTES_CEILING,
  normalizeApiLatencyEndpoint,
  normalizeApiLatencyEndpoints,
} from "./api-latency-policy.mjs";

test("fills missing thresholds with the hard API latency policy", () => {
  assert.deepEqual(
    normalizeApiLatencyEndpoint({ name: "calendar", path: "/calendar" }),
    { name: "calendar", path: "/calendar", ...API_LATENCY_POLICY_MS, max_response_bytes: API_RESPONSE_BYTES_CEILING },
  );
});

test("accepts exact and stricter percentile thresholds", () => {
  assert.deepEqual(
    normalizeApiLatencyEndpoints([
      { name: "exact", ...API_LATENCY_POLICY_MS },
      { name: "stricter", p90_ms: 200, p95_ms: 400, p99_ms: 400 },
    ]).map(({ p90_ms, p95_ms, p99_ms }) => ({ p90_ms, p95_ms, p99_ms })),
    [
      API_LATENCY_POLICY_MS,
      { p90_ms: 200, p95_ms: 400, p99_ms: 400 },
    ],
  );
});

test("all committed hot-path manifests satisfy the hard policy", () => {
  const perfDir = new URL("./", import.meta.url);
  const manifestFiles = readdirSync(perfDir)
    .filter((name) => /^hot-paths\..*\.json$/.test(name))
    .sort();

  assert.ok(manifestFiles.length > 0, "expected at least one hot-path manifest");
  for (const name of manifestFiles) {
    const manifest = JSON.parse(readFileSync(new URL(name, perfDir), "utf8"));
    assert.doesNotThrow(() => normalizeApiLatencyEndpoints(manifest.endpoints), name);
  }
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
    () => normalizeApiLatencyEndpoint({ name: "invalid", p90_ms: 300, p95_ms: 250, p99_ms: 400 }),
    /p90 <= p95 <= p99/,
  );
});

test("rejects a response payload ceiling above one MiB", () => {
  assert.throws(
    () => normalizeApiLatencyEndpoint({ name: "oversized", max_response_bytes: API_RESPONSE_BYTES_CEILING + 1 }),
    /exceeds the hard .*byte ceiling/,
  );
});
