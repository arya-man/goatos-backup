#!/usr/bin/env node
import { readFileSync } from "node:fs";

import { API_LATENCY_POLICY_MS, API_RESPONSE_BYTES_CEILING } from "./api-latency-policy.mjs";

export const REQUIRED_HOT_PATHS = Object.freeze([
  "control_tower",
  "action_center",
  "action_center_counts",
  "protocol_adherence",
  "calendar_vaccination",
  "calendar_vaccination_completed_history",
  "calendar_vaccination_date_markers",
  "calendar_mobile_month_page_20",
  "calendar_mobile_month_filter_options",
  "calendar_mobile_month_status_filter",
  "calendar_mobile_month_vaccine_filter",
  "calendar_mobile_history_page_20",
  "vaccination_schedule",
  "vaccination_execution",
  "vaccination_operations",
  "vaccination_shed_summary",
  // The command board and its lazy sections. These MUST be listed here as well as in the manifest:
  // validateApiLatencyEvidence rejects a report containing any name outside this list, so adding an
  // endpoint to hot-paths.vaccination.json without adding it here fails the live-api-latency job
  // with "latency evidence contains undeclared hot paths" rather than reporting its latency.
  "vaccination_command_board",
  "vaccination_live_tracker",
  "vaccination_command_drives",
  "vaccination_command_closed_without_dose",
  "vaccination_command_cohort_matrix",
  "vaccination_command_shed_dose_matrix",
  "app_vaccination_execution",
]);

const REQUIRED_HOT_PATH_PROFILES = Object.freeze({
  canonical_5k_50k: REQUIRED_HOT_PATHS,
  analytics_local_oci: Object.freeze([
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
  ]),
  pr264_performance: Object.freeze([
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
  ]),
});

// Keep observed cardinalities beside latency so missing work cannot look like a speedup.
export function observeApiPayload(endpoint, payload) {
  const rowCounts = {};
  const degraded = [];
  function visit(value, path = "") {
    if (!value || typeof value !== "object") return;
    for (const [key, child] of Object.entries(value)) {
      const childPath = path ? `${path}.${key}` : key;
      if (key === "degraded" && Array.isArray(child)) {
        degraded.push(...child.map((module) => `${childPath}:${String(module)}`));
      }
      if (Array.isArray(child)) rowCounts[childPath] = child.length;
      else visit(child, childPath);
    }
  }
  visit(payload);
  if (degraded.length) throw new Error(`${endpoint.name} degraded response: ${degraded.join(", ")}`);
  const value = String(endpoint.assertion?.path ?? "").split(".").filter(Boolean)
    .reduce((current, key) => current?.[key], payload);
  const assertionValue = endpoint.assertion?.type === "array_min" ? value?.length : value;
  // Missing/null numeric fields must not be converted into a successful zero.
  if (endpoint.assertion && (typeof assertionValue !== "number" || !Number.isFinite(assertionValue))) {
    throw new Error(`${endpoint.name} assertion ${endpoint.assertion.path} has no numeric observation`);
  }
  // Degraded arrays are health markers, not business rows.
  for (const path of Object.keys(rowCounts)) {
    if (path === "degraded" || path.endsWith(".degraded")) delete rowCounts[path];
  }
  return { assertion_value: assertionValue ?? null, row_counts: rowCounts, degraded };
}

export function validateApiLatencyEvidence(report, expectedSha) {
  const failures = [];
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return ["latency evidence must be a JSON object"];
  }
  if (!expectedSha) failures.push("expected SHA is required");
  if (!report.api_build_sha || report.api_build_sha !== expectedSha) failures.push("observed api_build_sha must match expected SHA");
  if (!report.api_build_sha_end || report.api_build_sha_end !== report.api_build_sha) failures.push("API build changed or final build identity is missing");
  if (report.api_build_identity_source !== "/version") failures.push("API build identity must come from /version");
  if (report.git_sha !== expectedSha) failures.push(`report git_sha ${report.git_sha ?? "<missing>"} does not match ${expectedSha}`);
  if (report.expected_sha !== expectedSha) failures.push(`report expected_sha ${report.expected_sha ?? "<missing>"} does not match ${expectedSha}`);
  if (report.worktree_dirty !== false) failures.push("worktree_dirty must be false for exact-SHA certification");
  if (!report.worktree_diff_sha256) failures.push("worktree_diff_sha256 evidence is missing");
  if (!Array.isArray(report.worktree_status_short) || report.worktree_status_short.length !== 0) failures.push("worktree_status_short must be an empty array for exact-SHA certification");
  if (!report.manifest_sha256) failures.push("manifest_sha256 is missing");
  if (!report.started_at || !report.finished_at) failures.push("started_at/finished_at evidence is missing");
  if (!report.scope || !Array.isArray(report.scope.included) || typeof report.scope.excluded !== "object") {
    failures.push("benchmark scope and explicit exclusions are missing");
  } else {
    const profile = report.scope.evidence_profile ?? "canonical_5k_50k";
    if (!Object.hasOwn(REQUIRED_HOT_PATH_PROFILES, profile)) {
      failures.push(`unknown latency evidence profile ${profile}`);
    }
    if (!report.scope.evidence_boundaries) {
      failures.push("latency evidence boundaries are missing");
    } else if (profile === "canonical_5k_50k"
      && (!Array.isArray(report.scope.evidence_boundaries.canonical_5k_50k_serving_reads)
        || !Array.isArray(report.scope.evidence_boundaries.local_nonempty_latency_only))) {
      failures.push("canonical 5k-50k and local-nonempty evidence boundaries are missing");
    } else if (profile === "analytics_local_oci"
      && !Array.isArray(report.scope.evidence_boundaries.local_oci_ceo_route_switch_reads)) {
      failures.push("analytics local OCI evidence boundary is missing");
    } else if (profile === "pr264_performance"
      && !Array.isArray(report.scope.evidence_boundaries.local_oci_pr264_route_reads)) {
      failures.push("PR264 local OCI route-read evidence boundary is missing");
    } else if (profile === "pr264_performance"
      && !Array.isArray(report.scope.evidence_boundaries.pr264_browser_render_routes)) {
      failures.push("PR264 browser render evidence boundary is missing");
    }
  }
  const evidenceProfile = report.scope?.evidence_profile ?? "canonical_5k_50k";
  if (!report.dataset || typeof report.dataset !== "object") {
    failures.push("dataset evidence is missing");
  } else if (evidenceProfile === "canonical_5k_50k") {
    if (report.dataset.animal_equivalent_cardinality < 5_000) {
      failures.push("dataset does not declare at least 5k animal-equivalent cardinality");
    }
    if (report.dataset.canonical_rows < 5_000) {
      failures.push("dataset has fewer than 5k canonical rows");
    }
    if (report.dataset.certification_boundary !== "canonical_5k_50k_serving_read_evidence") {
      failures.push("dataset certification boundary is missing or overclaims the 5k-50k canonical proof");
    }
  } else if (evidenceProfile === "analytics_local_oci") {
    if (!String(report.dataset.label ?? "").includes("oci")) {
      failures.push("analytics local OCI evidence must label the OCI dataset");
    }
    if (report.dataset.certification_boundary !== "local_oci_latency_only") {
      failures.push("analytics local OCI evidence must use local_oci_latency_only certification boundary");
    }
  } else if (evidenceProfile === "pr264_performance") {
    if (!String(report.dataset.label ?? "").includes("oci")) {
      failures.push("PR264 performance evidence must label the OCI/staging-equivalent dataset");
    }
    if (report.dataset.certification_boundary !== "local_oci_latency_only") {
      failures.push("PR264 performance evidence must use local_oci_latency_only certification boundary");
    }
  }

  const requiredHotPaths = REQUIRED_HOT_PATH_PROFILES[evidenceProfile] ?? REQUIRED_HOT_PATHS;
  if (evidenceProfile === "pr264_performance") {
    validatePr264BrowserEvidence(report, failures);
  }
  if (Array.isArray(report.scope?.required_hot_paths)
    && JSON.stringify(report.scope.required_hot_paths) !== JSON.stringify(requiredHotPaths)) {
    failures.push(`${evidenceProfile} required_hot_paths must match the verifier-owned profile`);
  }
  const results = Array.isArray(report.results) ? report.results : [];
  const byName = new Map(results.map((result) => [result?.name, result]));
  for (const name of requiredHotPaths) {
    const result = byName.get(name);
    if (!result) {
      failures.push(`required hot path ${name} is missing`);
      continue;
    }
    if (evidenceProfile === "pr264_performance") {
      const observations = result.response_observations;
      if (!Array.isArray(observations) || observations.length !== result.samples || observations.length === 0
        || observations.some((item) => !Number.isFinite(item?.assertion_value)
          || item.assertion_value < Number(result.assertion?.min ?? 1)
          || !item.row_counts || typeof item.row_counts !== "object"
          || Object.values(item.row_counts).some((count) => !Number.isInteger(count) || count < 0)
          || !Array.isArray(item.degraded) || item.degraded.length > 0)) {
        failures.push(`${name} response observations must cover every sample with valid counts and no degraded modules`);
      }
    }
    if (result.samples <= 0) failures.push(`${name} has no measured samples`);
    if (result.failures !== 0) failures.push(`${name} has ${result.failures} request failures`);
    if (!result.assertion) {
      failures.push(`${name} has no non-empty response assertion`);
    } else if (result.assertion.type === "array_min" && result.assertion.min < 1 && result.assertion.allow_empty !== true) {
      failures.push(`${name} array_min assertion must require at least one row or declare allow_empty=true`);
    }
    if (!Number.isFinite(result.response_bytes_max) || result.response_bytes_max <= 0) {
      failures.push(`${name} has no measured response payload size`);
    }
    if (!Number.isFinite(result.response_bytes_threshold) || result.response_bytes_threshold > API_RESPONSE_BYTES_CEILING) {
      failures.push(`${name} response byte threshold exceeds the hard ${API_RESPONSE_BYTES_CEILING}-byte ceiling`);
    } else if (result.response_bytes_max > result.response_bytes_threshold) {
      failures.push(`${name} response_bytes_max=${result.response_bytes_max} exceeds ${result.response_bytes_threshold}`);
    }
    for (const [percentile, ceiling] of Object.entries(API_LATENCY_POLICY_MS)) {
      const thresholdKey = `${percentile.replace("_ms", "")}_threshold_ms`;
      if (result[thresholdKey] !== ceiling && result[thresholdKey] > ceiling) {
        failures.push(`${name} ${thresholdKey}=${result[thresholdKey]} exceeds ${ceiling}`);
      }
      if (!Number.isFinite(result[percentile]) || result[percentile] > ceiling) {
        failures.push(`${name} ${percentile}=${result[percentile]} exceeds ${ceiling}`);
      }
    }
    if (result.passed !== true) failures.push(`${name} did not pass`);
  }
  if (results.some((result) => !requiredHotPaths.includes(result?.name))) {
    failures.push("latency evidence contains undeclared hot paths");
  }
  if (report.passed !== true) failures.push("top-level latency gate did not pass");
  return failures;
}

function validatePr264BrowserEvidence(report, failures) {
  const requiredRoutes = [
    { route: "/procurement/animal-purchases?scope_mode=company", viewports: ["desktop", "mobile"], signal: "has_animal_purchase_review" },
    { route: "/work-board?scope_mode=company", viewports: ["desktop", "mobile"], signal: "lane_counts" },
    { route: "/work-board?scope_mode=company&date=2026-08-10", viewports: ["desktop", "mobile"], signal: "populated_work_cards" },
    { route: "/weighing/weights?scope_mode=company", viewports: ["desktop", "mobile"], signal: "has_losing_weight_table" },
    { route: "/weighing/analytics?scope_mode=company&tab=general", viewports: ["desktop", "mobile"], signal: "has_weighing_kpis" },
    { route: "/weighing/analytics?scope_mode=company&tab=breed", viewports: ["desktop", "mobile"], signal: "has_breed_breakdown" },
    {
      route: "/weighing/analytics?scope_mode=company&tab=breed&wt_from=${dynamic}&wt_to=${dynamic}",
      pattern: /^\/weighing\/analytics\?scope_mode=company&tab=breed&wt_from=\d{4}-\d{2}-\d{2}&wt_to=\d{4}-\d{2}-\d{2}$/,
      viewports: ["desktop", "mobile"],
      signal: "has_breed_breakdown",
    },
    { route: "/weighing/analytics?scope_mode=company&tab=birth", viewports: ["desktop", "mobile"], signal: "has_origin_breakdown" },
    { route: "/weighing/analytics?scope_mode=company&tab=shed", viewports: ["desktop", "mobile"], signal: "has_shed_type_breakdown" },
    { route: "/weighing/analytics?scope_mode=company&tab=weight", viewports: ["desktop", "mobile"], signal: "has_weight_band_breakdown" },
    { route: "/weighing/analytics?scope_mode=company&tab=time", viewports: ["desktop", "mobile"], signal: "has_weekly_growth" },
    { route: "/weighing/analytics?scope_mode=company&tab=load", viewports: ["desktop", "mobile"], signal: "has_load_breakdown" },
  ];
  const requiredForbidden = [
    "backend_down",
    "Admin-web contract unavailable",
    "The board could not be loaded",
    "Weights could not be loaded",
  ];
  const browser = report.browser_evidence;
  if (!browser || typeof browser !== "object") {
    failures.push("PR264 browser evidence is missing");
    return;
  }
  if (!browser.api_build_sha || browser.api_build_sha !== report.git_sha) {
    failures.push("PR264 browser api_build_sha must match the latency report git_sha");
  }
  if (!browser.api_build_sha_end || browser.api_build_sha_end !== browser.api_build_sha) {
    failures.push("PR264 browser final API build identity is missing or changed");
  }
  if (browser.same_api_build !== true) {
    failures.push("PR264 browser evidence must use the same API build as the latency report");
  }
  const routes = Array.isArray(browser.routes) ? browser.routes : [];
  const routeEvidence = groupedBrowserRoutes(routes);
  for (const requirement of requiredRoutes) {
    const evidence = routeEvidence.get(requirement.route)
      ?? (requirement.pattern
        ? [...routeEvidence.values()].find((candidate) => requirement.pattern.test(candidate.route))
        : null);
    if (!evidence) {
      failures.push(`PR264 browser evidence missing route ${requirement.route}`);
      continue;
    }
    if (evidence.loaded !== true) {
      failures.push(`PR264 browser route ${requirement.route} did not load successfully`);
    }
    for (const viewport of requirement.viewports) {
      const viewports = Array.isArray(evidence.viewports) ? evidence.viewports : [];
      const viewportEvidence = viewports.find((item) => item?.name === viewport);
      if (!viewportEvidence || viewportEvidence.loaded !== true) {
        failures.push(`PR264 browser route ${requirement.route} missing loaded ${viewport} proof`);
      } else if (!viewportEvidence.route_signals || !hasRouteSignal(viewportEvidence.route_signals, requirement.signal)) {
        failures.push(`PR264 browser route ${requirement.route} missing ${viewport} product signal ${requirement.signal}`);
      }
    }
    for (const forbidden of requiredForbidden) {
      if (!Array.isArray(evidence.forbidden_strings_absent) || !evidence.forbidden_strings_absent.includes(forbidden)) {
        failures.push(`PR264 browser route ${requirement.route} missing forbidden-string proof for ${forbidden}`);
      }
    }
  }
}

function groupedBrowserRoutes(routes) {
  const grouped = new Map();
  for (const item of routes) {
    if (!item?.route) continue;
    const current = grouped.get(item.route) ?? {
      route: item.route,
      loaded: true,
      viewports: [],
      forbidden_strings_absent: [],
    };
    current.loaded = current.loaded && item.loaded === true;
    if (Array.isArray(item.viewports)) {
      for (const viewport of item.viewports) current.viewports.push(viewport);
    } else if (item.viewport) {
      current.viewports.push({
        name: normalizeBrowserViewport(item.viewport),
        loaded: item.loaded === true,
        route_signals: item.route_signals,
      });
    }
    if (Array.isArray(item.forbidden_strings_absent)) {
      if (current.forbidden_strings_absent.length === 0) {
        current.forbidden_strings_absent = [...item.forbidden_strings_absent];
      } else {
        current.forbidden_strings_absent = current.forbidden_strings_absent.filter((marker) => item.forbidden_strings_absent.includes(marker));
      }
    }
    grouped.set(item.route, current);
  }
  return grouped;
}

function hasRouteSignal(signals, signal) {
  if (!signals || typeof signals !== "object") return false;
  if (signal === "populated_work_cards") {
    return Number.isInteger(signals.work_cards) && signals.work_cards > 0 && signals.degraded === false;
  }
  if (signal === "lane_counts") {
    return signals.lane_counts && Object.values(signals.lane_counts).every((value) => Number.isFinite(value));
  }
  return signals[signal] === true;
}

function normalizeBrowserViewport(viewport) {
  if (viewport === "laptop") return "desktop";
  return viewport;
}

function parseArgs(argv) {
  const out = {};
  for (let index = 0; index < argv.length; index += 1) {
    if (!argv[index].startsWith("--")) continue;
    out[argv[index].slice(2)] = argv[index + 1];
    index += 1;
  }
  return out;
}

if (process.argv[1]?.endsWith("api-latency-evidence.mjs")) {
  const args = parseArgs(process.argv.slice(2));
  if (!args.report || !args["expected-sha"]) {
    console.error("usage: api-latency-evidence.mjs --report FILE --expected-sha SHA [--browser-evidence FILE]");
    process.exit(2);
  }
  const report = JSON.parse(readFileSync(args.report, "utf8"));
  if (args["browser-evidence"]) {
    report.browser_evidence = JSON.parse(readFileSync(args["browser-evidence"], "utf8"));
  }
  const failures = validateApiLatencyEvidence(report, args["expected-sha"]);
  if (failures.length > 0) {
    for (const failure of failures) console.error(`API latency evidence: ${failure}`);
    process.exit(1);
  }
  console.log(`API latency evidence: PASS @ ${args["expected-sha"]} (${REQUIRED_HOT_PATHS.length} hot paths)`);
}
