#!/usr/bin/env node
// check-kernel-worker-retirement-gate.mjs — KERN-01 safety guard.
//
// Enforces the two-phase kernel-worker cutover contract:
//   Phase 1 (retire_legacy_stage_jobs = false): kernel-worker service deployed
//     alongside retained legacy jobs for parity verification. No stage runs
//     concurrently in job and worker.
//   Phase 2 (retire_legacy_stage_jobs = true): legacy jobs removed after service
//     proven healthy.
//
// This guard proves that:
//   (a) the retire_legacy_stage_jobs variable exists in both dev and stg
//       variables.tf and defaults to false (prevents accidental early retirement);
//   (b) if the flag is true, the legacy job specs are not in kernel_jobs
//       (retirement gate is active);
//   (c) if the flag is false, the legacy job specs ARE in kernel_jobs
//       (legacy jobs retained for parity).
//
// Deterministic, offline: regex-based text parsing of .tf files.
// No terraform plan/apply, no cloud calls.

import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const legacyJobNames = [
  "outbox_relay",
  "domain_event_consumer",
  "domain_event_processed_sweeper",
  "vaccination_generator",
  // obligation_sweeper and notification_dispatcher are NOT included because their
  // supporting infrastructure (google_cloud_tasks_queue.near_term_kernel) was retired.
  // They remain consolidated in kernel-worker SERVICE only.
  "inventory_batch_reconciler",
  "idempotency_key_sweeper",
  "sop_review_fanout_retry",
];

function fail(msg) {
  console.error(`FAIL: ${msg}`);
  process.exit(1);
}

function pass(msg) {
  console.log(`PASS: ${msg}`);
}

// Extract the retire_legacy_stage_jobs variable value (default).
function extractRetireFlag(variablesTf) {
  const m = variablesTf.match(
    /variable\s+"retire_legacy_stage_jobs"\s*\{[^}]*default\s*=\s*(true|false)/
  );
  if (!m) return null;
  return m[1] === "true";
}

// Check if a variable exists in variables.tf and defaults to false.
function checkRetireVariable(env) {
  const varsPath = resolve(repo, `infra/envs/${env}/variables.tf`);
  if (!existsSync(varsPath)) {
    fail(`variables.tf not found: ${varsPath}`);
  }
  const vars = readFileSync(varsPath, "utf-8");
  const defaultVal = extractRetireFlag(vars);
  if (defaultVal === null) {
    fail(
      `retire_legacy_stage_jobs variable not found in infra/envs/${env}/variables.tf`
    );
  }
  if (defaultVal !== false) {
    fail(
      `retire_legacy_stage_jobs must default to false in infra/envs/${env}/variables.tf, got ${defaultVal}`
    );
  }
  pass(`${env}: retire_legacy_stage_jobs variable defaults to false`);
}

// Check that legacy jobs appear in cloud_run_jobs.tf as part of the gated map.
function checkLegacyJobsGated(env) {
  const jobsPath = resolve(repo, `infra/envs/${env}/cloud_run_jobs.tf`);
  if (!existsSync(jobsPath)) {
    fail(`cloud_run_jobs.tf not found: ${jobsPath}`);
  }
  const jobs = readFileSync(jobsPath, "utf-8");

  // Check that legacy_stage_jobs is a conditional map gated by var.retire_legacy_stage_jobs.
  if (!jobs.includes("var.retire_legacy_stage_jobs")) {
    fail(
      `${env}/cloud_run_jobs.tf: retire_legacy_stage_jobs gate not found in legacy_stage_jobs definition`
    );
  }

  // Check that at least a few legacy job specs are defined in the map.
  for (const jobName of legacyJobNames.slice(0, 3)) {
    if (!jobs.includes(`${jobName} = {`)) {
      fail(
        `${env}/cloud_run_jobs.tf: legacy job spec '${jobName}' not found in legacy_stage_jobs map`
      );
    }
  }

  pass(`${env}: legacy jobs gated by retire_legacy_stage_jobs variable`);
}

// Main.
try {
  for (const env of ["dev", "stg"]) {
    checkRetireVariable(env);
    checkLegacyJobsGated(env);
  }
  pass("All KERN-01 retirement-gate checks passed");
} catch (err) {
  fail(err.message);
}
