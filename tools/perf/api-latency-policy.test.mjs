import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";

import {
  API_LATENCY_POLICY_MS,
  API_RESPONSE_BYTES_CEILING,
  normalizeApiLatencyEndpoint,
  normalizeApiLatencyEndpoints,
} from "./api-latency-policy.mjs";

const STG_SLOW_AND_ANALYTICS_HOT_PATHS = Object.freeze([
  "stg_control_tower_vaccination",
  "stg_calendar_vaccination_events_7d",
  "stg_calendar_vaccination_events_month_page",
  "stg_vaccination_command_board",
  "stg_vaccination_command_drives",
  "stg_vaccination_command_cohort_matrix",
  "stg_vaccination_command_shed_dose_matrix",
  "stg_vaccination_live_tracker",
  "stg_counts_breakdown",
  "stg_admin_locations",
  "stg_verification_queue",
  "stg_vaccination_adherence",
  "stg_weighing_dates",
  "stg_weighing_leadership_growth",
  "stg_weighing_weight_demographics",
  "stg_verification_oversight_analytics",
  "stg_feed_execution_analytics",
  "stg_feed_execution_overview_days",
  "stg_feed_execution_packing_variance",
  "stg_feed_experiment_analytics",
  "stg_weighing_shed_weights",
  "stg_feed_directed_analytics",
  "stg_feed_stock_analytics",
  "stg_feed_shed_feed_analytics",
]);

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

test("committed manifests cover every STG endpoint over 1s p95 plus analytics hot paths", () => {
  const manifests = readCommittedHotPathManifests();
  const names = new Set(manifests.flatMap(({ manifest }) => manifest.endpoints.map((endpoint) => endpoint.name)));

  for (const name of STG_SLOW_AND_ANALYTICS_HOT_PATHS) {
    assert.ok(names.has(name), `missing STG slow/API hot path from latency manifests: ${name}`);
  }
});

test("analytics hot-path contracts use bounded sectioned feed execution reads", () => {
  const analytics = readHotPathManifest("hot-paths.analytics.json");
  const byName = new Map(analytics.endpoints.map((endpoint) => [endpoint.name, endpoint]));

  assert.match(
    byName.get("vaccination_live_tracker")?.path ?? "",
    /[?&]business_date=2026-09-04(?:&|$)/,
    "live tracker must use business_date, not date",
  );
  assert.doesNotMatch(
    byName.get("vaccination_live_tracker")?.path ?? "",
    /[?&]date=/,
    "live tracker latency contract must not use the wrong date parameter",
  );
  assert.match(
    byName.get("feed_execution_overview_days")?.path ?? "",
    /[?&]sections=days(?:&|$)/,
    "feed overview must guard the KPI-only execution request it actually renders",
  );
  assert.doesNotMatch(
    byName.get("feed_execution_overview_days")?.path ?? "",
    /sections=days,consumption/,
    "feed overview must not hide behind the heavier execution-tab request",
  );
  assert.match(
    byName.get("feed_execution_analytics")?.path ?? "",
    /[?&]sections=days,consumption,distribution_completions(?:&|$)/,
    "feed execution rolling contract must ask only for rendered non-variance sections",
  );
  assert.match(
    byName.get("feed_execution_analytics")?.path ?? "",
    /[?&]completion_limit=25(?:&|$)/,
    "feed execution contract must keep completion pagination bounded",
  );
  assert.match(
    byName.get("feed_execution_packing_variance")?.path ?? "",
    /date_from=2026-09-05&date_to=2026-09-05/,
    "feed execution variance contract must use the page's default packing-day + 1 feed-day request",
  );
  assert.match(
    byName.get("feed_execution_packing_variance")?.path ?? "",
    /[?&]variance_limit=25(?:&|$)/,
    "feed execution variance contract must keep variance pagination bounded",
  );
  assert.equal(
    byName.get("feed_experiment_analytics")?.assertion?.path,
    "items",
    "feed experiment tab must have its own latency contract instead of hiding behind overview",
  );
  assert.doesNotMatch(
    byName.get("feed_shed_feed_analytics")?.path ?? "",
    /[?&](limit|offset)=/,
    "shed-feed analytics has no fake paging contract",
  );
});

test("STG slow manifest captures the observed over-1s endpoint groups", () => {
  const manifest = readHotPathManifest("hot-paths.stg-slow.json");
  assert.deepEqual(manifest.scope.included, manifest.endpoints.map((endpoint) => endpoint.name));
  assert.ok(
    manifest.scope.evidence_boundaries.stg_observed_over_1s_p95.length >= 15,
    "expected the broad STG slow list, not the narrow 10-endpoint analytics list",
  );
  const observed = new Set(manifest.scope.evidence_boundaries.stg_observed_over_1s_p95);
  for (const name of STG_SLOW_AND_ANALYTICS_HOT_PATHS) {
    assert.ok(observed.has(name), `STG slow evidence list is missing ${name}`);
  }

  const byName = new Map(manifest.endpoints.map((endpoint) => [endpoint.name, endpoint]));
  assert.match(byName.get("stg_vaccination_live_tracker")?.path ?? "", /[?&]business_date=2026-09-04(?:&|$)/);
  assert.equal(byName.get("stg_counts_breakdown")?.assertion?.path, "items");
  assert.match(byName.get("stg_feed_execution_overview_days")?.path ?? "", /[?&]sections=days(?:&|$)/);
  assert.match(byName.get("stg_feed_execution_analytics")?.path ?? "", /[?&]sections=days,consumption,distribution_completions(?:&|$)/);
  assert.match(byName.get("stg_feed_execution_analytics")?.path ?? "", /[?&]completion_limit=25(?:&|$)/);
  assert.match(byName.get("stg_feed_execution_packing_variance")?.path ?? "", /date_from=2026-09-05&date_to=2026-09-05/);
  assert.match(byName.get("stg_feed_execution_packing_variance")?.path ?? "", /[?&]sections=packing_variance(?:&|$)/);
  assert.match(byName.get("stg_feed_execution_packing_variance")?.path ?? "", /[?&]variance_limit=25(?:&|$)/);
  assert.equal(byName.get("stg_feed_experiment_analytics")?.assertion?.path, "items");
  assert.doesNotMatch(byName.get("stg_feed_shed_feed_analytics")?.path ?? "", /[?&](limit|offset)=/);
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

function readCommittedHotPathManifests() {
  const perfDir = new URL("./", import.meta.url);
  return readdirSync(perfDir)
    .filter((name) => /^hot-paths\..*\.json$/.test(name))
    .sort()
    .map((name) => ({ name, manifest: readHotPathManifest(name) }));
}

function readHotPathManifest(name) {
  const manifest = JSON.parse(readFileSync(new URL(name, import.meta.url), "utf8"));
  if (Array.isArray(manifest)) return { endpoints: manifest };
  return manifest;
}
