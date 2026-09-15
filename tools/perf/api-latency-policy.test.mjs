import { createServer } from "node:http";
import { execFile, execFileSync } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { WEIGHING_WORKLOADS, WEIGHING_DATES_NAME, WEIGHING_WINDOW_FROM, WEIGHING_LOOKBACK_DAYS, validateWeighingManifest, weighingWindow, weighingWindowFromResult, expandWeighingPath, weighingEvidenceFailures } from "./weighing-workload.mjs";
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

test("PR264 performance manifest measures rendered route shapes, not broad shortcuts", () => {
  const manifest = readHotPathManifest("hot-paths.pr264.json");
  const byName = new Map(manifest.endpoints.map((endpoint) => [endpoint.name, endpoint]));
  const required = [
    "pr264_weighing_dates",
    "pr264_weighing_shed_weights",
    "pr264_weighing_weight_demographics",
    "pr264_weighing_weight_demographics_dimensions_section",
    "pr264_weighing_weight_demographics_origin_section",
    "pr264_weighing_weight_demographics_shed_type_section",
    "pr264_weighing_weight_demographics_weight_bands_section",
    "pr264_weighing_weight_demographics_weekly_gain_section",
    "pr264_weighing_growth_weights_sections",
    "pr264_weighing_growth_general_sections",
    "pr264_weighing_growth_time_sections",
    "pr264_growth_director_weights_sections",
    "pr264_work_board_page_cbe",
    "pr264_work_board_page_cpt",
    "pr264_app_vaccination_execution_with_card_summaries",
    "pr264_app_vaccination_execution_fresh_as_of",
  ];
  assert.deepEqual(manifest.scope.required_hot_paths, required);
  assert.deepEqual(manifest.scope.included, required);
  assert.deepEqual(manifest.scope.evidence_boundaries.local_oci_pr264_route_reads, required);
  assert.deepEqual(manifest.scope.evidence_boundaries.pr264_browser_render_routes, [
    "/procurement/animal-purchases?scope_mode=company",
    "/work-board?scope_mode=company",
    "/work-board?scope_mode=company&date=2026-08-10",
    "/weighing/weights?scope_mode=company",
    "/weighing/analytics?scope_mode=company&tab=general",
    "/weighing/analytics?scope_mode=company&tab=breed",
    "/weighing/analytics?scope_mode=company&tab=breed&wt_from=${dynamic}&wt_to=${dynamic}",
    "/weighing/analytics?scope_mode=company&tab=birth",
    "/weighing/analytics?scope_mode=company&tab=shed",
    "/weighing/analytics?scope_mode=company&tab=weight",
    "/weighing/analytics?scope_mode=company&tab=time",
    "/weighing/analytics?scope_mode=company&tab=load",
  ]);
  assert.match(
    byName.get("pr264_weighing_weight_demographics")?.path ?? "",
    /[?&]sections=composition,dimensions,gain_thresholds(?:&|$)/,
  );
  assert.match(byName.get("pr264_weighing_weight_demographics_dimensions_section")?.path ?? "", /[?&]sections=dimensions(?:&|$)/);
  assert.match(byName.get("pr264_weighing_weight_demographics_origin_section")?.path ?? "", /[?&]sections=origin(?:&|$)/);
  assert.match(byName.get("pr264_weighing_weight_demographics_shed_type_section")?.path ?? "", /[?&]sections=shed_type(?:&|$)/);
  assert.match(byName.get("pr264_weighing_weight_demographics_weight_bands_section")?.path ?? "", /[?&]sections=weight_bands(?:&|$)/);
  assert.match(byName.get("pr264_weighing_weight_demographics_weekly_gain_section")?.path ?? "", /[?&]sections=weekly_gain(?:&|$)/);
  assert.match(byName.get("pr264_weighing_growth_weights_sections")?.path ?? "", /[?&]sections=headline,shed_leaderboard,losing_animals(?:&|$)/);
  assert.match(byName.get("pr264_weighing_growth_general_sections")?.path ?? "", /[?&]sections=headline,shed_leaderboard,by_park(?:&|$)/);
  assert.match(byName.get("pr264_weighing_growth_time_sections")?.path ?? "", /[?&]sections=weekly_gain(?:&|$)/);
  assert.match(byName.get("pr264_growth_director_weights_sections")?.path ?? "", /[?&]sections=road_to_sale,fair_fight(?:&|$)/);
  assert.match(byName.get("pr264_app_vaccination_execution_with_card_summaries")?.path ?? "", /[?&]include_card_summaries=true(?:&|$)/);
  assert.match(byName.get("pr264_work_board_page_cbe")?.path ?? "", /^\/work-board\/page\?/);
  assert.match(byName.get("pr264_work_board_page_cpt")?.path ?? "", /^\/work-board\/page\?/);
  assert.equal(byName.get("pr264_work_board_page_cbe")?.assertion?.min, 1);
  assert.equal(byName.get("pr264_app_vaccination_execution_with_card_summaries")?.assertion?.min, 1);
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

test("fresh-as-of workload preserves the default endpoint and requires bounded unique request evidence", async () => {
  const { VACCINATION_PATH, createRequestPlanner, freshAsOfEvidenceFailures } = await import("./api-latency-request.mjs");
  const endpoint = { method: "GET", path: VACCINATION_PATH, request_strategy: "fresh_as_of", assertion: { type: "array_min", path: "rows", min: 1 } };
  const plan = createRequestPlanner();
  const samples = [plan(endpoint, 1800000000000), plan(endpoint, 1800000000000)];
  const valid = { ...endpoint, response_observations: samples };
  assert.deepEqual(freshAsOfEvidenceFailures(valid), []);
  assert.equal(plan({ path: VACCINATION_PATH }, 1800000000000).request_path, VACCINATION_PATH);
  for (const mutate of [
    (x) => { x.response_observations[1] = { ...x.response_observations[0] }; },
    (x) => { x.response_observations[0].request_path = VACCINATION_PATH; },
    (x) => { x.response_observations[0].request_path = x.response_observations[0].request_path.replace("limit=20", "limit=1"); },
    (x) => { x.response_observations[0].request_started_at = "2020-01-01T00:00:00Z"; },
    (x) => { x.request_strategy = null; },
    (x) => { x.assertion.min = 0; },
  ]) {
    const bad = structuredClone(valid); mutate(bad); assert.ok(freshAsOfEvidenceFailures(bad).length > 0);
  }
  for (const bad of [{ ...endpoint, path: "/work-board/page" }, { ...endpoint, request_strategy: "cachebuster" }, { ...endpoint, path: VACCINATION_PATH + "&as_of=2020-01-01" }]) {
    assert.throws(() => normalizeApiLatencyEndpoint(bad), /fresh_as_of/);
  }
});

test("PR264 warmup guard refuses discarded or slow initial cache misses", async () => {
  const { warmupEvidenceFailures } = await import("./api-latency-request.mjs");
  const valid = {
    assertion: { type: "array_min", path: "rows", min: 1 },
    warmup_samples_ms: [500, 60], warmup_max_ms: 500,
    warmup_response_bytes: [64000, 64000], response_bytes_threshold: 524288,
    warmup_response_observations: Array.from({ length: 2 }, () => ({ assertion_value: 1, row_counts: { rows: 1 }, degraded: [] })),
  };
  assert.deepEqual(warmupEvidenceFailures(valid, 2), []);
  for (const mutate of [
    (x) => { delete x.warmup_samples_ms; },
    (x) => { delete x.warmup_response_bytes; },
    (x) => { x.warmup_response_bytes[0] = 524289; },
    (x) => { x.warmup_response_bytes[0] = 1048577; x.response_bytes_threshold = 2097152; },
    (x) => { x.warmup_samples_ms[0] = 1607; x.warmup_max_ms = 1607; },
    (x) => { x.warmup_max_ms = 60; },
    (x) => { x.warmup_samples_ms.pop(); },
    (x) => { x.warmup_response_observations.pop(); },
    (x) => { x.warmup_response_observations[0].degraded = ["vaccination"]; },
    (x) => { x.warmup_response_observations[0].assertion_value = 0; },
  ]) { const bad = structuredClone(valid); mutate(bad); assert.ok(warmupEvidenceFailures(bad, 2).length > 0); }
  assert.ok(warmupEvidenceFailures(valid, 0).length > 0);
});


test("PR273 Weights workloads keep the real landing filters and window", () => {
  const manifest = readHotPathManifest("hot-paths.pr264.json");
  for (const endpoint of manifest.endpoints.filter(({name}) => name.startsWith("pr264_weighing_") || name === "pr264_growth_director_weights_sections")) {
    const url = new URL(endpoint.path, "http://local.invalid");
    assert.equal(url.searchParams.get("sex"), "male", endpoint.name);
    assert.equal(url.searchParams.has("weighing_category"), false, endpoint.name);
    assert.equal(url.searchParams.get("from"), endpoint.name === "pr264_weighing_dates" ? "{weighing_lookback_from}" : "{weighing_from}", endpoint.name);
    assert.equal(url.searchParams.get("to"), endpoint.name === "pr264_weighing_dates" ? "{weighing_today}" : "{weighing_to}", endpoint.name);
  }
});


test("Weights benchmark defaults remain tied to both actual pages", () => {
  const constants = readFileSync(new URL("../../apps/admin-web/features/weighing/landing-window-constants.ts", import.meta.url), "utf8");
  assert.match(constants, new RegExp(`DEFAULT_WINDOW_FROM = "${WEIGHING_WINDOW_FROM}"`));
  assert.match(constants, new RegExp(`LATEST_LUMP_LOOKBACK_DAYS = ${WEIGHING_LOOKBACK_DAYS}`));
  for (const file of ["weights.tsx", "weights-analytics.tsx"]) {
    const source = readFileSync(new URL(`../../apps/admin-web/features/weighing/${file}`, import.meta.url), "utf8");
    assert.match(source, /rawSex === "female" \? "female" : rawSex === "all" \? "" : "male"/);
    assert.match(source, /return raw === "individual_animal" \|\| raw === "per_shed_partition" \? raw : "all"/);
    assert.match(source, /sex: sexFilter \|\| undefined/);
  }
  validateWeighingManifest(readHotPathManifest("hot-paths.pr264.json").endpoints);
});

test("Weights manifest rejects the old shortcut, short date range, and reordered date lookup", () => {
  const endpoints = readHotPathManifest("hot-paths.pr264.json").endpoints;
  const index = endpoints.findIndex(({name}) => name === "pr264_weighing_weight_demographics_dimensions_section");
  for (const mutate of [
    (path) => path.replace("&sex=male", ""),
    (path) => `${path}&weighing_category=per_shed_partition`,
    (path) => path.replace("{weighing_from}", "2026-09-01"),
    (path) => path.replace("sections=dimensions", "sections=origin"),
  ]) {
    const bad = structuredClone(endpoints);
    bad[index].path = mutate(bad[index].path);
    assert.throws(() => validateWeighingManifest(bad), /default Male/);
  }
  assert.throws(() => validateWeighingManifest([...endpoints.slice(1), endpoints[0]]), /before resolving/);
});

test("Weights window uses measured latest date, 400-day lookback and IST midnight", () => {
  const stamp = "2026-09-15T20:00:00Z"; // Already September 16 in India.
  const window = weighingWindow(stamp, "2026-09-09");
  assert.equal(window.weighing_today, "2026-09-16");
  assert.equal(window.weighing_from, "2026-08-03");
  assert.equal(window.weighing_to, "2026-09-09");
  assert.equal((Date.parse(window.weighing_today) - Date.parse(window.weighing_lookback_from)) / 86400000, 399);
  assert.equal(weighingWindow(stamp).weighing_to, "2026-09-16");
  assert.equal(weighingWindow(stamp, "2026-10-01").weighing_to, "2026-09-16");
  assert.throws(() => expandWeighingPath("?to={weighing_to}", {}), /Missing measured/);
  const result = { response_observations: [{ latest_weighing_date: "2026-09-09" }] };
  assert.deepEqual(weighingWindowFromResult(stamp, result), window);
  result.warmup_response_observations = [{ latest_weighing_date: "2026-09-08" }];
  assert.throws(() => weighingWindowFromResult(stamp, result), /changed during measurement/);
  assert.throws(() => weighingWindowFromResult(stamp, { response_observations: [{}] }), /must include/);
});

test("Weights evidence rejects altered sample URLs even when declared paths are correct", () => {
  const started_at = "2026-09-16T00:00:00Z";
  const window = weighingWindow(started_at, "2026-09-15");
  const results = Object.entries(WEIGHING_WORKLOADS).map(([name, template]) => {
    const path = expandWeighingPath(template, window);
    return { name, method: "GET", path, response_observations: [{ request_path: path,
      ...(name === WEIGHING_DATES_NAME ? { latest_weighing_date: "2026-09-15" } : {}),
    }] };
  });
  assert.deepEqual(weighingEvidenceFailures({ started_at, results }), []);
  results[1].response_observations[0].request_path += "&weighing_category=per_shed_partition";
  assert.match(weighingEvidenceFailures({ started_at, results }).join(), /sample request differs/);
});


test("executable latency gate measures the landing date lookup before default page requests", async () => {
  const root = fileURLToPath(new URL("../../", import.meta.url));
  const sha = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
  const requests = [];
  const latest = "2026-09-09";
  const server = createServer((request, response) => {
    requests.push(request.url);
    response.setHeader("Content-Type", "application/json");
    response.end(JSON.stringify(request.url === "/version" ? { build_sha: sha } : {
      latest_weighing_date: latest, lump_weighing_dates: [latest], rows: [{ id: "one" }],
      by_breed: [{}], gain_by_breed_origin: [{}], gain_by_breed_shed_type: [{}],
      by_weight_band: [{}], gain_by_breed_week: [{}], by_park: [{}], weekly_gain: [{}],
      headline: { headline_animals: 1 }, road_to_sale: { total_animals: 1 },
      lanes: { todo: { rows: [{}] } },
    }));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    const {stdout} = await promisify(execFile)(process.execPath, [
      "tools/perf/api-latency-gate.mjs", "--base-url", `http://127.0.0.1:${server.address().port}`,
      "--bearer-token", "synthetic-test-only", "--manifest", "tools/perf/hot-paths.pr264.json",
      "--iterations", "1", "--warmup", "1", "--timeout-ms", "5000",
    ], { cwd: root, maxBuffer: 2 * 1024 * 1024, timeout: 20000 });
    const report = JSON.parse(stdout);
    assert.equal(report.results.length, 16);
    assert.deepEqual(weighingEvidenceFailures(report), []);
    assert.match(requests[1], /^\/weighing\/weighing-dates\?/);
    for (const result of report.results.filter(({name}) => WEIGHING_WORKLOADS[name] && name !== WEIGHING_DATES_NAME)) {
      assert.equal(new URL(result.path, "http://local.invalid").searchParams.get("to"), latest);
      assert.ok(requests.includes(result.path));
    }
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
});
