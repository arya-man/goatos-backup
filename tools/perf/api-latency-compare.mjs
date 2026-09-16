#!/usr/bin/env node
import { actorEvidenceFailures } from "./api-latency-actor.mjs";
import { readFileSync } from "node:fs";

const DEFAULT_ALLOWED_REGRESSION_MS = 25;

export function compareApiLatencyEvidence(before, after, options = {}) {
  const failures = [];
  const allowedRegressionMs = Number.isFinite(options.allowedRegressionMs)
    ? options.allowedRegressionMs
    : DEFAULT_ALLOWED_REGRESSION_MS;
  if (!before || typeof before !== "object") return ["before latency report must be a JSON object"];
  if (!after || typeof after !== "object") return ["after latency report must be a JSON object"];
  failures.push(...actorEvidenceFailures(before).map((f) => `before ${f}`), ...actorEvidenceFailures(after).map((f) => `after ${f}`));
  if (before.actor?.user_id !== after.actor?.user_id || before.actor?.tenant_id !== after.actor?.tenant_id) failures.push("authenticated actor differs between reports");
  if (!before.git_sha || !after.git_sha) failures.push("both reports must carry git_sha");
  if (before.git_sha && after.git_sha && before.git_sha === after.git_sha) {
    failures.push("before and after git_sha must be different");
  }
  for (const key of ["tenant_id", "manifest_sha256", "iterations", "warmup", "concurrency"]) {
    if (before[key] !== after[key]) failures.push(`${key} differs between reports`);
  }
  for (const key of ["label", "certification_boundary"]) {
    if (before.dataset?.[key] !== after.dataset?.[key]) failures.push(`dataset.${key} differs between reports`);
  }
  if (before.scope?.evidence_profile !== after.scope?.evidence_profile) {
    failures.push("scope.evidence_profile differs between reports");
  }
  if (JSON.stringify(before.scope?.included ?? []) !== JSON.stringify(after.scope?.included ?? [])) {
    failures.push("scope.included differs between reports");
  }
  if (after.passed !== true) failures.push("after report did not pass its own latency gate");

  if (after.scope?.evidence_profile === "pr264_performance" && (!before.weighing_policy || !after.weighing_policy
    || JSON.stringify(before.weighing_policy) !== JSON.stringify(after.weighing_policy))) failures.push("Weights page policy differs or is missing between reports");
  const beforeByName = byName(before.results);
  const afterByName = byName(after.results);
  for (const name of beforeByName.keys()) {
    if (!afterByName.has(name)) failures.push(`after report missing endpoint ${name}`);
  }
  for (const name of afterByName.keys()) {
    if (!beforeByName.has(name)) failures.push(`before report missing endpoint ${name}`);
  }
  for (const [name, beforeResult] of beforeByName.entries()) {
    const afterResult = afterByName.get(name);
    if (!afterResult) continue;
    if (before.scope?.evidence_profile === "pr264_performance") {
      const prior = beforeResult.response_observations;
      const current = afterResult.response_observations;
      if (!Array.isArray(prior) || !prior.length || !Array.isArray(current) || !current.length) {
        failures.push(`${name} before/after response observations are missing`);
      } else {
        const bounds = (items) => {
          const counts = {};
          for (const item of items) {
            for (const [key, value] of Object.entries({ assertion_value: item.assertion_value, ...item.row_counts })) {
              counts[key] ??= [];
              counts[key].push(value);
            }
          }
          return Object.fromEntries(Object.entries(counts).map(([key, values]) => [key, [Math.min(...values), Math.max(...values)]]));
        };
        const oldCounts = bounds(prior), newCounts = bounds(current);
        for (const [key, range] of Object.entries(oldCounts)) {
          if (!newCounts[key] || newCounts[key][0] < range[0] || newCounts[key][1] < range[1]) failures.push(`${name} ${key} cardinality decreased or disappeared`);
        }
      }
    }
    if (beforeResult.path !== afterResult.path) failures.push(`${name} path differs between reports`);
    if (beforeResult.response_bytes_threshold !== afterResult.response_bytes_threshold) {
      failures.push(`${name} response byte threshold differs between reports`);
    }
    for (const percentile of ["p90_ms", "p95_ms", "p99_ms"]) {
      const beforeValue = Number(beforeResult[percentile]);
      const afterValue = Number(afterResult[percentile]);
      if (!Number.isFinite(beforeValue) || !Number.isFinite(afterValue)) {
        failures.push(`${name} ${percentile} is missing in before/after report`);
        continue;
      }
      if (afterValue > beforeValue + allowedRegressionMs) {
        failures.push(`${name} ${percentile} regressed from ${beforeValue}ms to ${afterValue}ms`);
      }
    }
    if (Number(afterResult.response_bytes_max) > Number(beforeResult.response_bytes_max) * 1.25
      && Number(afterResult.response_bytes_max) > Number(beforeResult.response_bytes_max) + 32768) {
      failures.push(`${name} response_bytes_max grew from ${beforeResult.response_bytes_max} to ${afterResult.response_bytes_max}`);
    }
  }
  return failures;
}

function byName(results) {
  return new Map((Array.isArray(results) ? results : []).map((result) => [result?.name, result]));
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

if (process.argv[1]?.endsWith("api-latency-compare.mjs")) {
  const args = parseArgs(process.argv.slice(2));
  if (!args.before || !args.after) {
    console.error("usage: api-latency-compare.mjs --before FILE --after FILE [--allowed-regression-ms N]");
    process.exit(2);
  }
  const before = JSON.parse(readFileSync(args.before, "utf8"));
  const after = JSON.parse(readFileSync(args.after, "utf8"));
  const failures = compareApiLatencyEvidence(before, after, {
    allowedRegressionMs: Number(args["allowed-regression-ms"]),
  });
  if (failures.length > 0) {
    for (const failure of failures) console.error(`API latency compare: ${failure}`);
    process.exit(1);
  }
  console.log(`API latency compare: PASS ${before.git_sha} -> ${after.git_sha}`);
}
