import assert from "node:assert/strict";
import test from "node:test";

import { API_LATENCY_POLICY_MS } from "./api-latency-policy.mjs";
import { REQUIRED_HOT_PATHS, validateApiLatencyEvidence } from "./api-latency-evidence.mjs";

const sha = "0123456789abcdef";

function passingReport() {
  return {
    git_sha: sha,
    api_build_sha: sha,
    api_build_sha_end: sha,
    api_build_identity_source: "/version",
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
      response_observations: Array.from({ length: 20 }, () => ({ assertion_value: 1, row_counts: { rows: 1 }, degraded: [] })),
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

function pr264BrowserEvidence({ includeSignals = true } = {}) {
  const signals = new Map([
    ["/work-board?scope_mode=company", { lane_counts: { todo: 0, in_progress: 0, in_review: 0, done: 0 }, healthy_empty_state: true }],
    ["/work-board?scope_mode=company&date=2026-08-10", { lane_counts: { todo: 1, in_progress: 0, in_review: 0, done: 0 }, work_cards: 1, degraded: false }],
    ["/weighing/weights?scope_mode=company", { has_losing_weight_table: true }],
    ["/weighing/analytics?scope_mode=company&tab=general", { has_weighing_kpis: true, tab: "general" }],
    ["/weighing/analytics?scope_mode=company&tab=breed", { has_breed_breakdown: true, tab: "breed" }],
    ["/weighing/analytics?scope_mode=company&tab=breed&wt_from=${dynamic}&wt_to=${dynamic}", { has_breed_breakdown: true, tab: "breed", selected_window: true }],
    ["/weighing/analytics?scope_mode=company&tab=birth", { has_origin_breakdown: true, tab: "birth" }],
    ["/weighing/analytics?scope_mode=company&tab=shed", { has_shed_type_breakdown: true, tab: "shed" }],
    ["/weighing/analytics?scope_mode=company&tab=weight", { has_weight_band_breakdown: true, tab: "weight" }],
    ["/weighing/analytics?scope_mode=company&tab=time", { has_weekly_growth: true, tab: "time" }],
    ["/weighing/analytics?scope_mode=company&tab=load", { has_load_breakdown: true, tab: "load" }],
  ]);
  return {
    same_api_build: true,
    api_build_sha: sha,
    api_build_sha_end: sha,
    routes: [
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
    ].map((route) => ({
      route,
      loaded: true,
      viewports: [
        { name: "desktop", loaded: true, ...(includeSignals ? { route_signals: signals.get(route) } : {}) },
        { name: "mobile", loaded: true, ...(includeSignals ? { route_signals: signals.get(route) } : {}) },
      ],
      forbidden_strings_absent: [
        "backend_down",
        "Admin-web contract unavailable",
        "The board could not be loaded",
        "Weights could not be loaded",
      ],
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

test("accepts PR264 local OCI performance evidence only when every required route is present", () => {
  const report = passingReport();
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
  ];
  report.dataset = {
    label: "pr264_oci_staging_refresh",
    animal_equivalent_cardinality: 0,
    canonical_rows: 0,
    certification_boundary: "local_oci_latency_only",
  };
  report.scope.evidence_profile = "pr264_performance";
  report.scope.included = required;
  report.scope.required_hot_paths = required;
  report.scope.evidence_boundaries = {
    local_oci_pr264_route_reads: required,
    pr264_browser_render_routes: [
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
    ],
  };
  report.results = required.map((name) => ({ ...report.results[0], name }));
  report.browser_evidence = pr264BrowserEvidence();
  assert.deepEqual(validateApiLatencyEvidence(report, sha), []);

  for (const endSHA of [undefined, "changed-build"]) {
    report.browser_evidence.api_build_sha_end = endSHA;
    assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("browser final API build")));
  }
  report.browser_evidence.api_build_sha_end = sha;
  const historical = report.browser_evidence.routes.find((item) => item.route === "/work-board?scope_mode=company&date=2026-08-10");
  for (const invalid of [{work_cards: 0, degraded: false}, {work_cards: 1, degraded: true}]) {
    const original = historical.viewports[1].route_signals;
    historical.viewports[1].route_signals = invalid;
    assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("2026-08-10")));
    historical.viewports[1].route_signals = original;
  }

  report.results = report.results.filter((result) => result.name !== "pr264_app_vaccination_execution_with_card_summaries");
  assert.ok(
    validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("pr264_app_vaccination_execution_with_card_summaries is missing")),
  );
});

test("rejects PR264 performance evidence without browser proof for the observed failure screens", () => {
  const report = passingReport();
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
  ];
  report.dataset = {
    label: "pr264_oci_staging_refresh",
    animal_equivalent_cardinality: 0,
    canonical_rows: 0,
    certification_boundary: "local_oci_latency_only",
  };
  report.scope.evidence_profile = "pr264_performance";
  report.scope.included = required;
  report.scope.required_hot_paths = required;
  report.scope.evidence_boundaries = {
    local_oci_pr264_route_reads: required,
    pr264_browser_render_routes: [
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
    ],
  };
  report.results = required.map((name) => ({ ...report.results[0], name }));
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("PR264 browser evidence is missing")));

  report.browser_evidence = pr264BrowserEvidence();
  report.browser_evidence.routes[0].forbidden_strings_absent = ["backend_down"];
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("The board could not be loaded")));
});

test("rejects PR264 browser proof without route-specific product signals", () => {
  const report = passingReport();
  report.dataset.certification_boundary = "local_oci_latency_only";
  report.scope.evidence_profile = "pr264_performance";
  report.scope.included = REQUIRED_HOT_PATHS.filter((name) => name.startsWith("pr264_"));
  report.scope.evidence_boundaries = { local_oci_latency_only: report.scope.included };
  report.results = report.results.filter((result) => result.name.startsWith("pr264_"));
  report.browser_evidence = pr264BrowserEvidence({ includeSignals: false });

  const failures = validateApiLatencyEvidence(report, sha);
  assert.ok(failures.some((failure) => failure.includes("product signal")));
});

test("accepts PR264 browser evidence produced as one flat record per viewport", () => {
  const report = passingReport();
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
  ];
  report.dataset = {
    label: "pr264_oci_staging_refresh",
    animal_equivalent_cardinality: 0,
    canonical_rows: 0,
    certification_boundary: "local_oci_latency_only",
  };
  report.scope.evidence_profile = "pr264_performance";
  report.scope.included = required;
  report.scope.required_hot_paths = required;
  report.scope.evidence_boundaries = {
    local_oci_pr264_route_reads: required,
    pr264_browser_render_routes: [
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
    ],
  };
  report.results = required.map((name) => ({ ...report.results[0], name }));
  const forbidden = [
    "backend_down",
    "Admin-web contract unavailable",
    "The board could not be loaded",
    "Weights could not be loaded",
  ];
  const signals = new Map([
    ["/work-board?scope_mode=company", { lane_counts: { todo: 0, in_progress: 0, in_review: 0, done: 0 }, healthy_empty_state: true }],
    ["/work-board?scope_mode=company&date=2026-08-10", { lane_counts: { todo: 1, in_progress: 0, in_review: 0, done: 0 }, work_cards: 1, degraded: false }],
    ["/weighing/weights?scope_mode=company", { has_losing_weight_table: true }],
    ["/weighing/analytics?scope_mode=company&tab=general", { has_weighing_kpis: true, tab: "general" }],
    ["/weighing/analytics?scope_mode=company&tab=breed", { has_breed_breakdown: true, tab: "breed" }],
    ["/weighing/analytics?scope_mode=company&tab=breed&wt_from=${dynamic}&wt_to=${dynamic}", { has_breed_breakdown: true, tab: "breed", selected_window: true }],
    ["/weighing/analytics?scope_mode=company&tab=birth", { has_origin_breakdown: true, tab: "birth" }],
    ["/weighing/analytics?scope_mode=company&tab=shed", { has_shed_type_breakdown: true, tab: "shed" }],
    ["/weighing/analytics?scope_mode=company&tab=weight", { has_weight_band_breakdown: true, tab: "weight" }],
    ["/weighing/analytics?scope_mode=company&tab=time", { has_weekly_growth: true, tab: "time" }],
    ["/weighing/analytics?scope_mode=company&tab=load", { has_load_breakdown: true, tab: "load" }],
  ]);
  report.browser_evidence = {
    same_api_build: true,
    api_build_sha: sha,
    api_build_sha_end: sha,
    routes: report.scope.evidence_boundaries.pr264_browser_render_routes.flatMap((route) => [
      { route, viewport: "laptop", loaded: true, forbidden_strings_absent: forbidden, route_signals: signals.get(route) },
      { route, viewport: "mobile", loaded: true, forbidden_strings_absent: forbidden, route_signals: signals.get(route) },
    ]),
  };
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

test("rejects browser evidence from a different or undeclared API build", () => {
  const report = passingReport();
  report.scope.evidence_profile = "pr264_performance";
  report.browser_evidence = pr264BrowserEvidence();
  for (const apiBuild of [undefined, "stale-build"]) {
    report.browser_evidence.api_build_sha = apiBuild;
    assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("api_build_sha")));
  }
});

test("observes response cardinalities and refuses partial Work Board success", async () => {
  const { observeApiPayload } = await import("./api-latency-evidence.mjs");
  const endpoint = { name: "board", assertion: { type: "array_min", path: "lanes.todo.rows", min: 1 } };
  const payload = { lanes: { todo: { rows: [{ id: "one" }], degraded: [] } } };
  assert.deepEqual(observeApiPayload(endpoint, payload), { assertion_value: 1, row_counts: { "lanes.todo.rows": 1 }, degraded: [] });
  payload.lanes.todo.degraded = ["weighing"];
  assert.throws(() => observeApiPayload(endpoint, payload), /degraded.*weighing/);
  assert.throws(() => observeApiPayload(endpoint, { ...payload, degraded: ["feed"] }), /degraded/);
});

test("rejects missing, partial, or degraded measured observations", () => {
  const report = passingReport();
  report.scope.evidence_profile = "pr264_performance";
  report.results[0].name = "pr264_weighing_dates";
  for (const observations of [undefined, [], [{ assertion_value: 1, row_counts: {}, degraded: ["weighing"] }]]) {
    report.results[0].response_observations = observations;
    assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("response observations")));
  }
});

test("does not treat null numeric payloads as zero-cardinality success", async () => {
  const { observeApiPayload } = await import("./api-latency-evidence.mjs");
  const endpoint = { name: "growth", assertion: { type: "number_min", path: "headline.animals", min: 0 } };
  assert.throws(() => observeApiPayload(endpoint, { headline: { animals: null } }), /no numeric observation/);
  assert.equal(observeApiPayload(endpoint, { headline: { animals: 0 } }).assertion_value, 0);
});


test("rejects checkout-only identity and API build changes during measurement", () => {
  for (const update of [
    { api_build_sha: undefined },
    { api_build_sha: "different-build" },
    { api_build_sha_end: "different-build" },
    { api_build_sha_end: undefined },
    { api_build_identity_source: "checkout" },
  ]) {
    assert.ok(validateApiLatencyEvidence({ ...passingReport(), ...update }, sha).some((failure) => /build/i.test(failure)));
  }
});

test("exact-SHA certification rejects dirty diagnostic evidence and inconsistent status", () => {
  assert.deepEqual(validateApiLatencyEvidence(passingReport(), sha), []);
  for (const change of [
    {worktree_dirty: true},
    {worktree_dirty: false, worktree_status_short: [' M backend/file.go']},
    {worktree_dirty: false, worktree_status_short: ['?? new-source.ts']},
  ]) {
    assert.ok(validateApiLatencyEvidence({...passingReport(), ...change}, sha).some((failure) => failure.includes('exact-SHA certification')));
  }
});
