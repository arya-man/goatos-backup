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
});

export function validateApiLatencyEvidence(report, expectedSha) {
  const failures = [];
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return ["latency evidence must be a JSON object"];
  }
  if (!expectedSha) failures.push("expected SHA is required");
  if (report.git_sha !== expectedSha) failures.push(`report git_sha ${report.git_sha ?? "<missing>"} does not match ${expectedSha}`);
  if (report.expected_sha !== expectedSha) failures.push(`report expected_sha ${report.expected_sha ?? "<missing>"} does not match ${expectedSha}`);
  if (typeof report.worktree_dirty !== "boolean") failures.push("worktree_dirty evidence is missing");
  if (!report.worktree_diff_sha256) failures.push("worktree_diff_sha256 evidence is missing");
  if (!Array.isArray(report.worktree_status_short)) failures.push("worktree_status_short evidence is missing");
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
  }

  const requiredHotPaths = REQUIRED_HOT_PATH_PROFILES[evidenceProfile] ?? REQUIRED_HOT_PATHS;
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
    console.error("usage: api-latency-evidence.mjs --report FILE --expected-sha SHA");
    process.exit(2);
  }
  const report = JSON.parse(readFileSync(args.report, "utf8"));
  const failures = validateApiLatencyEvidence(report, args["expected-sha"]);
  if (failures.length > 0) {
    for (const failure of failures) console.error(`API latency evidence: ${failure}`);
    process.exit(1);
  }
  console.log(`API latency evidence: PASS @ ${args["expected-sha"]} (${REQUIRED_HOT_PATHS.length} hot paths)`);
}
