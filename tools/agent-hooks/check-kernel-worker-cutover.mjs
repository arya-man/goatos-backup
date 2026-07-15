#!/usr/bin/env node
// check-kernel-worker-cutover.mjs — KERN-01 worker-stage exclusion guard.
//
// Enforces atomic mutual exclusion of legacy jobs and kernel-worker stages:
//   Phase 1 (retire_legacy_stage_jobs = false):
//     - legacy_stage_jobs map populated (gated false -> full map)
//     - GOATOS_WORKER_STAGES_ENABLED = "false" (stages disabled/shadowed)
//   Phase 2 (retire_legacy_stage_jobs = true):
//     - legacy_stage_jobs map empty (gated true -> empty map)
//     - GOATOS_WORKER_STAGES_ENABLED = "true" (stages enabled)
//
// Prevents the two-owner anti-pattern (KERN-01 defect):
//   - Both active (legacy jobs running + worker stages enabled) = race/double-process
//   - Both inactive (legacy jobs removed + worker stages disabled) = zero owner/gap
//
// Deterministic, offline: regex-based text parsing of .tf files.
// No terraform plan/apply, no cloud calls.

import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";

const repo = process.argv.find(arg => arg.startsWith("--test-dir="))
  ? process.argv.find(arg => arg.startsWith("--test-dir=")).split("=")[1]
  : resolve(import.meta.dirname, "../..");

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

// Extract GOATOS_WORKER_STAGES_ENABLED env value from cloud_run_worker.tf.
// Returns the raw expression (e.g., "true", "false", 'var.retire_legacy_stage_jobs ? "true" : "false"').
// Handles multi-line env blocks by looking for the name first, then value on the next line.
function extractWorkerStagesEnabledValue(workerTf) {
  // Look for the env block with GOATOS_WORKER_STAGES_ENABLED name and capture the value.
  const m = workerTf.match(
    /name\s*=\s*"GOATOS_WORKER_STAGES_ENABLED"\s+value\s*=\s*([^\n]+)/
  );
  if (!m) return null;
  return m[1].trim();
}

// cutoverFindings is the PURE check: given the three .tf file contents for an env, it returns an
// array of finding strings (empty = the one-owner-per-stage invariant holds). No disk/process.exit,
// so the self-test can drive it with crafted adversarial inputs.
function cutoverFindings(env, { jobs, worker, vars }) {
  const findings = [];

  // retire_legacy_stage_jobs must exist and DEFAULT to false (never retire before parity).
  const retireDefault = extractRetireFlag(vars);
  if (retireDefault === null) {
    findings.push(`${env}/variables.tf: retire_legacy_stage_jobs variable not found`);
  } else if (retireDefault !== false) {
    findings.push(`${env}/variables.tf: retire_legacy_stage_jobs must default to false, got ${retireDefault}`);
  }

  // legacy_stage_jobs must be gated 'var.retire_legacy_stage_jobs ? {} : {…}' (present only when flag=false).
  if (!/legacy_stage_jobs\s*=\s*var\.retire_legacy_stage_jobs\s*\?\s*\{\s*\}\s*:/.test(jobs)) {
    findings.push(
      `${env}/cloud_run_jobs.tf: legacy_stage_jobs must be gated 'var.retire_legacy_stage_jobs ? {} : {…}' so the jobs exist ONLY when the flag is false`,
    );
  }

  // GOATOS_WORKER_STAGES_ENABLED must be the mirror-image ternary on the SAME flag, so worker stages
  // are active ONLY when the flag is true — mutually exclusive with the legacy jobs above.
  const stagesEnabledValue = extractWorkerStagesEnabledValue(worker);
  if (!stagesEnabledValue) {
    findings.push(`${env}/cloud_run_worker.tf: GOATOS_WORKER_STAGES_ENABLED env var not found — worker stages are unconditionally active (two owners in phase 1)`);
    return findings; // cannot evaluate polarity without the value
  }
  const normalized = stagesEnabledValue.replace(/\s+/g, " ");
  if (!normalized.includes("var.retire_legacy_stage_jobs")) {
    findings.push(
      `${env}/cloud_run_worker.tf: GOATOS_WORKER_STAGES_ENABLED must be bound to var.retire_legacy_stage_jobs (hardcoding it decouples worker stages from legacy-job retirement), got ${stagesEnabledValue}`,
    );
  } else if (normalized.includes('? "false" : "true"')) {
    findings.push(
      `${env}/cloud_run_worker.tf: GOATOS_WORKER_STAGES_ENABLED polarity reversed — must be 'var.retire_legacy_stage_jobs ? "true" : "false"' (flag true => stages on), got ${stagesEnabledValue}`,
    );
  } else if (!normalized.includes('? "true" : "false"')) {
    findings.push(
      `${env}/cloud_run_worker.tf: GOATOS_WORKER_STAGES_ENABLED must be 'var.retire_legacy_stage_jobs ? "true" : "false"', got ${stagesEnabledValue}`,
    );
  }
  return findings;
}

// Self-test: drive cutoverFindings with crafted inputs and assert each adversarial case is caught and
// the compliant case is clean. Throws (non-zero exit) on any miss.
function selfTest() {
  const goodVars = `variable "retire_legacy_stage_jobs" {\n  type    = bool\n  default = false\n}`;
  const goodJobs = `locals {\n  legacy_stage_jobs = var.retire_legacy_stage_jobs ? {} : {\n    outbox_relay = { name = "goatos-stg-outbox-relay" }\n  }\n}`;
  const goodWorker = `env {\n  name  = "GOATOS_WORKER_STAGES_ENABLED"\n  value = var.retire_legacy_stage_jobs ? "true" : "false"\n}`;

  const expectClean = (name, c) => {
    const f = cutoverFindings("test", c);
    if (f.length !== 0) throw new Error(`self-test (${name}): compliant config produced findings: ${JSON.stringify(f)}`);
  };
  const expectFail = (name, c, needle) => {
    const f = cutoverFindings("test", c);
    if (!f.some((x) => x.includes(needle))) {
      throw new Error(`self-test (${name}): expected a finding containing "${needle}", got ${JSON.stringify(f)}`);
    }
  };

  // 0) compliant config -> no findings.
  expectClean("compliant", { jobs: goodJobs, worker: goodWorker, vars: goodVars });

  // 1) both active (KERN-01 defect): worker stages hardcoded on while legacy jobs still present.
  expectFail("both-active", { jobs: goodJobs, worker: `env {\n  name  = "GOATOS_WORKER_STAGES_ENABLED"\n  value = "true"\n}`, vars: goodVars }, "must be bound to var.retire_legacy_stage_jobs");

  // 2) both inactive (zero-owner gap): worker stages hardcoded off.
  expectFail("both-inactive", { jobs: goodJobs, worker: `env {\n  name  = "GOATOS_WORKER_STAGES_ENABLED"\n  value = "false"\n}`, vars: goodVars }, "must be bound to var.retire_legacy_stage_jobs");

  // 3) polarity reversed: flag true would remove legacy jobs but DISABLE worker stages.
  expectFail("polarity-reversed", { jobs: goodJobs, worker: `env {\n  name  = "GOATOS_WORKER_STAGES_ENABLED"\n  value = var.retire_legacy_stage_jobs ? "false" : "true"\n}`, vars: goodVars }, "polarity reversed");

  // 4) worker stages env var missing entirely (stages unconditionally active).
  expectFail("missing-env", { jobs: goodJobs, worker: `env {\n  name  = "SOMETHING_ELSE"\n  value = "x"\n}`, vars: goodVars }, "GOATOS_WORKER_STAGES_ENABLED env var not found");

  // 5) retire flag defaults to true (retires legacy before parity).
  expectFail("default-true", { jobs: goodJobs, worker: goodWorker, vars: `variable "retire_legacy_stage_jobs" {\n  default = true\n}` }, "must default to false");

  // 6) legacy jobs not gated by the flag (always present -> two owners once stages turn on).
  expectFail("legacy-not-gated", { jobs: `locals {\n  legacy_stage_jobs = {\n    outbox_relay = {}\n  }\n}`, worker: goodWorker, vars: goodVars }, "must be gated");

  console.log("KERN-01 cutover-exclusion self-test: passed (7 cases: compliant + both-active + both-inactive + polarity-reversed + missing-env + default-true + legacy-not-gated)");
}

// checkCutoverExclusion reads the env's .tf files and returns findings via the pure checker.
function checkCutoverExclusion(env) {
  const jobsPath = resolve(repo, `infra/envs/${env}/cloud_run_jobs.tf`);
  const workerPath = resolve(repo, `infra/envs/${env}/cloud_run_worker.tf`);
  const varsPath = resolve(repo, `infra/envs/${env}/variables.tf`);
  for (const [label, p] of [["cloud_run_jobs.tf", jobsPath], ["cloud_run_worker.tf", workerPath], ["variables.tf", varsPath]]) {
    if (!existsSync(p)) return [`${env}/${label} not found: ${p}`];
  }
  return cutoverFindings(env, {
    jobs: readFileSync(jobsPath, "utf-8"),
    worker: readFileSync(workerPath, "utf-8"),
    vars: readFileSync(varsPath, "utf-8"),
  });
}

// Main.
function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }

  const findings = [];
  for (const env of ["dev", "stg"]) {
    findings.push(...checkCutoverExclusion(env));
  }
  if (findings.length > 0) {
    console.error("kernel-worker-cutover guard failed:");
    for (const f of findings) console.error(`- ${f}`);
    process.exit(1);
  }
  pass("All KERN-01 cutover-exclusion checks passed (dev + stg: exactly one active owner per stage in every phase)");
}

main();
