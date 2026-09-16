import "./api-latency-actor.test.mjs";
import { WEIGHING_WORKLOADS, WEIGHING_DATES_NAME, weighingWindow, expandWeighingPath } from "./weighing-workload.mjs";
import { FRESH_AS_OF_NAME, VACCINATION_PATH, createRequestPlanner } from "./api-latency-request.mjs";
import assert from "node:assert/strict";
import test from "node:test";

import { API_LATENCY_POLICY_MS } from "./api-latency-policy.mjs";
import { REQUIRED_HOT_PATHS, validateApiLatencyEvidence } from "./api-latency-evidence.mjs";

const sha = "0123456789abcdef";

function passingReport() {
  return {
    tenant_id: "00000000-0000-4000-8000-000000000001",
    actor: { user_id: "00000000-0000-4000-8000-000000000002", tenant_id: "00000000-0000-4000-8000-000000000001" },
    actor_identity_source: "/app/me",
    warmup: 5,
    git_sha: sha,
    api_build_sha: sha,
    api_build_sha_end: sha,
    api_build_identity_source: "/version",
    worktree_dirty: false,
    worktree_diff_sha256: "clean-worktree-hash",
    worktree_status_short: [],
    expected_sha: sha,
    manifest_sha256: "manifest-hash",
    started_at: "2026-09-16T00:00:00Z",
    weighing_policy: {source: "/admin-web/bootstrap", copy: {}},
    weighing_policy_end: {source: "/admin-web/bootstrap", copy: {}},
    finished_at: "2026-09-16T00:01:00Z",
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
      warmup_samples_ms: [100, 100, 100, 100, 100],
      warmup_response_bytes: [64000, 64000, 64000, 64000, 64000],
      warmup_max_ms: 100,
      warmup_response_observations: Array.from({ length: 5 }, () => ({ assertion_value: 1, row_counts: { rows: 1 }, degraded: [] })),
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
    ["/procurement/animal-purchases?scope_mode=company", { has_animal_purchase_review: true }],
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
    ...browserProvenance(),
    same_api_build: true,
    api_build_sha: sha,
    api_build_sha_end: sha,
    routes: [
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
    ].map((route) => ({
      route,
      loaded: true,
      viewports: [
        { name: "desktop", loaded: true, actual_pathname: route.split("?")[0], ...(includeSignals ? { route_signals: signals.get(route) } : {}) },
        { name: "mobile", loaded: true, actual_pathname: route.split("?")[0], ...(includeSignals ? { route_signals: signals.get(route) } : {}) },
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
    "pr264_app_vaccination_execution_fresh_as_of",
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
    ],
  };
  report.results = required.map((name) => pr264Result(report.results[0], name));
  report.browser_evidence = pr264BrowserEvidence();
  assert.deepEqual(validateApiLatencyEvidence(report, sha), []);
  const workload = report.results.find(({name}) => name === "pr264_weighing_weight_demographics_dimensions_section");
  const expectedPath = workload.path;
  workload.path = expectedPath.replace("&sex=male", "");
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("default Weights landing request")));
  workload.path = expectedPath;

  const procurement = report.browser_evidence.routes.find((item) => item.route.startsWith("/procurement/animal-purchases"));
  const procurementSignals = procurement.viewports[0].route_signals;
  procurement.viewports[0].route_signals = {};
  assert.ok(validateApiLatencyEvidence(report, sha).some((failure) => failure.includes("has_animal_purchase_review")));
  procurement.viewports[0].route_signals = procurementSignals;
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
    "pr264_app_vaccination_execution_fresh_as_of",
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
    ],
  };
  report.results = required.map((name) => pr264Result(report.results[0], name));
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
    "pr264_app_vaccination_execution_fresh_as_of",
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
    ],
  };
  report.results = required.map((name) => pr264Result(report.results[0], name));
  const forbidden = [
    "backend_down",
    "Admin-web contract unavailable",
    "The board could not be loaded",
    "Weights could not be loaded",
  ];
  const signals = new Map([
    ["/procurement/animal-purchases?scope_mode=company", { has_animal_purchase_review: true }],
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
    ...browserProvenance(),
    same_api_build: true,
    api_build_sha: sha,
    api_build_sha_end: sha,
    routes: report.scope.evidence_boundaries.pr264_browser_render_routes.flatMap((route) => [
      { route, viewport: "laptop", loaded: true, actual_pathname: route.split("?")[0], forbidden_strings_absent: forbidden, route_signals: signals.get(route) },
      { route, viewport: "mobile", loaded: true, actual_pathname: route.split("?")[0], forbidden_strings_absent: forbidden, route_signals: signals.get(route) },
    ]),
  };
  assert.deepEqual(validateApiLatencyEvidence(report, sha), []);
  for (const markers of [undefined, [], ["backend_down"]]) {
    const bad = structuredClone(report);
    bad.browser_evidence.routes[0].forbidden_strings_absent = markers;
    assert.ok(validateApiLatencyEvidence(bad, sha).some((failure) => failure.includes("forbidden-string proof")));
  }
  for (const mutate of [
    (b) => { b.actor.user_id = "wrong-actor"; },
    (b) => { b.local_stack_launch_receipt.git_sha = "old-build"; },
    (b) => { b.local_stack_launch_receipt.build_provenance.clean_source = false; },
    (b) => { b.local_stack_launch_receipt.mode = "dev"; },
    (b) => { b.routes[0].actual_pathname = "/approvals"; },
  ]) {
    const bad = structuredClone(report); mutate(bad.browser_evidence);
    assert.ok(validateApiLatencyEvidence(bad, sha).length > 0);
  }
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

function pr264Result(template, name) {
  const result = { ...template, name };
  if (WEIGHING_WORKLOADS[name]) {
    result.method = "GET";
    result.path = expandWeighingPath(WEIGHING_WORKLOADS[name], weighingWindow("2026-09-16T00:00:00Z", "2026-09-15"));
    for (const key of ["warmup_response_observations", "response_observations"]) {
      result[key] = result[key].map((sample) => ({ ...sample, request_path: result.path,
        ...(name === WEIGHING_DATES_NAME ? { latest_weighing_date: "2026-09-15" } : {}),
      }));
    }
  }
  if (name === FRESH_AS_OF_NAME) {
    const plan = createRequestPlanner();
    Object.assign(result, {
      method: "GET", path: VACCINATION_PATH, request_strategy: "fresh_as_of",
      assertion: { type: "array_min", path: "rows", min: 1 },
    });
    result.warmup_response_observations = result.warmup_response_observations.map((sample) => ({
      ...sample, ...plan(result, 1800000000000),
    }));
    result.response_observations = result.response_observations.map((sample) => ({
      ...sample, ...plan(result, 1800000000000),
    }));
  }
  return result;
}

function browserProvenance() {
  const api = "http://127.0.0.1:18174";
  const web = "http://127.0.0.1:13473";
  const user = "00000000-0000-4000-8000-000000000002";
  const tenant = "00000000-0000-4000-8000-000000000001";
  return {
    api_build_identity_source: "/version", api_base_url: api, admin_web_base_url: web,
    actor: { user_id: user, tenant_id: tenant },
    weighing_policy: {source: "/admin-web/bootstrap", copy: {}},
    weighing_policy_end: {source: "/admin-web/bootstrap", copy: {}},
    local_stack_launch_receipt: {
      git_sha: sha, mode: "start", api_base_url: api, admin_web_base_url: web,
      local_user_id: user, tenant_id: tenant,
      build_provenance: { git_sha: sha, build_id: "actual-next-build", clean_source: true, source_status_start: [], source_status_end: [] },
    },
  };
}

test("rejects missing API actor and browser/API user mismatch", () => {
  const missing = passingReport();
  delete missing.actor;
  assert.ok(validateApiLatencyEvidence(missing, sha).some((f) => /actor/.test(f)));
  const report = passingReport();
  report.scope.evidence_profile = "pr264_performance";
  report.browser_evidence = pr264BrowserEvidence();
  report.actor.user_id = "different-user";
  assert.ok(validateApiLatencyEvidence(report, sha).some((f) => /actor/.test(f)));
});

test("browser policy must match measured authored window", () => {
  const report = passingReport();
  report.scope.evidence_profile = "pr264_performance";
  report.browser_evidence = {weighing_policy:{source:"/admin-web/bootstrap",copy:{changed:"yes"}}};
  assert.ok(validateApiLatencyEvidence(report, report.git_sha).some(f => /browser Weights policy/.test(f)));
});
