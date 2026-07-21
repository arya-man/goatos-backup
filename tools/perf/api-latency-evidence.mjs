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
]);

export function validateApiLatencyEvidence(report, expectedSha) {
  const failures = [];
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return ["latency evidence must be a JSON object"];
  }
  if (!expectedSha) failures.push("expected SHA is required");
  if (report.git_sha !== expectedSha) failures.push(`report git_sha ${report.git_sha ?? "<missing>"} does not match ${expectedSha}`);
  if (report.expected_sha !== expectedSha) failures.push(`report expected_sha ${report.expected_sha ?? "<missing>"} does not match ${expectedSha}`);
  if (!report.manifest_sha256) failures.push("manifest_sha256 is missing");
  if (!report.started_at || !report.finished_at) failures.push("started_at/finished_at evidence is missing");
  if (!report.scope || !Array.isArray(report.scope.included) || typeof report.scope.excluded !== "object") {
    failures.push("benchmark scope and explicit exclusions are missing");
  } else if (!report.scope.evidence_boundaries
    || !Array.isArray(report.scope.evidence_boundaries.canonical_5k_50k_serving_reads)
    || !Array.isArray(report.scope.evidence_boundaries.local_nonempty_latency_only)) {
    failures.push("canonical 5k-50k and local-nonempty evidence boundaries are missing");
  }
  if (!report.dataset || typeof report.dataset !== "object") {
    failures.push("dataset evidence is missing");
  } else {
    if (report.dataset.animal_equivalent_cardinality < 5_000) {
      failures.push("dataset does not declare at least 5k animal-equivalent cardinality");
    }
    if (report.dataset.canonical_rows < 5_000) {
      failures.push("dataset has fewer than 5k canonical rows");
    }
    if (report.dataset.certification_boundary !== "canonical_5k_50k_serving_read_evidence") {
      failures.push("dataset certification boundary is missing or overclaims the 5k-50k canonical proof");
    }
  }

  const results = Array.isArray(report.results) ? report.results : [];
  const byName = new Map(results.map((result) => [result?.name, result]));
  for (const name of REQUIRED_HOT_PATHS) {
    const result = byName.get(name);
    if (!result) {
      failures.push(`required hot path ${name} is missing`);
      continue;
    }
    if (result.samples <= 0) failures.push(`${name} has no measured samples`);
    if (result.failures !== 0) failures.push(`${name} has ${result.failures} request failures`);
    if (!result.assertion) failures.push(`${name} has no non-empty response assertion`);
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
  if (results.some((result) => !REQUIRED_HOT_PATHS.includes(result?.name))) {
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
