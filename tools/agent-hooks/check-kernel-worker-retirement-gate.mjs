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
// This guard proves, per env (dev, stg):
//   (a) the retire_legacy_stage_jobs variable exists and defaults to false
//       (prevents accidental early retirement);
//   (b) the legacy_stage_jobs map is gated by var.retire_legacy_stage_jobs;
//   (c) EVERY legacy stage job spec is present in the gated map (flag-polarity
//       contract: the jobs exist and are enabled/disabled by the flag, not deleted);
//   (d) the consolidated kernel-worker SERVICE is declared (the flag retires jobs
//       INTO this service, so it must exist for retirement to be safe).
// Cross-env: the flag default must be identical (false) in both envs so a cutover
// is deliberate and not accidentally half-applied.
//
// Deterministic, offline: regex/text parsing of .tf files. No terraform plan/apply,
// no cloud calls.
//
// NOTE (follow-up): deep service-account + IAM-binding polarity (that a retired job's
// runtime SA / secret accessors are also removed or re-pointed at kernel-worker) is
// partially covered today by `make secret-accessors-guard`. Extending THIS guard to
// assert per-job SA/IAM removal on Phase 2 is tracked in the guardrail manifest owner
// doc; the checks below cover job specs + flag polarity + service presence.

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

// ── Pure evaluators (all inputs injected so the self-test can feed fixtures) ──

export function extractRetireFlag(variablesTf) {
  const m = variablesTf.match(
    /variable\s+"retire_legacy_stage_jobs"\s*\{[^}]*default\s*=\s*(true|false)/
  );
  return m ? m[1] === "true" : null;
}

// Returns { flag, problems } for one env's tf pair.
export function evaluateEnv({ env, variablesTf, jobsTf, jobNames = legacyJobNames }) {
  const problems = [];

  const flag = extractRetireFlag(variablesTf);
  if (flag === null) {
    problems.push(`${env}: retire_legacy_stage_jobs variable not found in variables.tf`);
  } else if (flag !== false) {
    problems.push(`${env}: retire_legacy_stage_jobs must default to false, got ${flag}`);
  }

  if (!jobsTf.includes("var.retire_legacy_stage_jobs")) {
    problems.push(`${env}: retire_legacy_stage_jobs gate not found in legacy_stage_jobs definition`);
  }

  // (c) EVERY legacy job spec must be present in the gated map — not just a sample.
  for (const jobName of jobNames) {
    if (!jobsTf.includes(`${jobName} = {`)) {
      problems.push(`${env}: legacy job spec '${jobName}' missing from legacy_stage_jobs map`);
    }
  }

  // (d) the consolidated kernel-worker service must be declared.
  if (!/kernel[_-]worker/.test(jobsTf)) {
    problems.push(`${env}: kernel-worker service reference not found — retirement target must exist`);
  }

  return { flag, problems };
}

// Cross-env consistency: both envs must carry the SAME (false) default.
export function evaluateCrossEnv(perEnv) {
  const problems = [];
  const flags = perEnv.map((e) => e.flag);
  const distinct = new Set(flags.filter((f) => f !== null));
  if (distinct.size > 1) {
    problems.push(`retire_legacy_stage_jobs default differs across envs (${JSON.stringify(flags)}); a cutover must be applied deliberately to all envs`);
  }
  return problems;
}

function selfTest() {
  const goodVars = 'variable "retire_legacy_stage_jobs" {\n  default = false\n}';
  const goodJobs =
    "var.retire_legacy_stage_jobs\nresource kernel_worker {}\n" +
    legacyJobNames.map((n) => `${n} = {`).join("\n");

  const clean = evaluateEnv({ env: "dev", variablesTf: goodVars, jobsTf: goodJobs });
  if (clean.problems.length !== 0) throw new Error(`self-test: expected clean, got ${JSON.stringify(clean.problems)}`);

  // flag defaulting true -> detected.
  const trueFlag = evaluateEnv({ env: "dev", variablesTf: 'variable "retire_legacy_stage_jobs" {\n  default = true\n}', jobsTf: goodJobs });
  if (!trueFlag.problems.some((p) => p.includes("must default to false"))) throw new Error("self-test: true default not detected");

  // missing a legacy job spec -> detected.
  const missingJob = evaluateEnv({ env: "dev", variablesTf: goodVars, jobsTf: goodJobs.replace("outbox_relay = {", "") });
  if (!missingJob.problems.some((p) => p.includes("outbox_relay"))) throw new Error("self-test: missing job not detected");

  // missing gate var -> detected.
  const noGate = evaluateEnv({ env: "dev", variablesTf: goodVars, jobsTf: goodJobs.replace("var.retire_legacy_stage_jobs", "") });
  if (!noGate.problems.some((p) => p.includes("gate not found"))) throw new Error("self-test: missing gate not detected");

  // missing kernel-worker service -> detected.
  const noService = evaluateEnv({ env: "dev", variablesTf: goodVars, jobsTf: goodJobs.replace("resource kernel_worker {}\n", "") });
  if (!noService.problems.some((p) => p.includes("kernel-worker service reference not found"))) throw new Error("self-test: missing service not detected");

  // cross-env polarity mismatch -> detected.
  const mismatch = evaluateCrossEnv([{ flag: false }, { flag: true }]);
  if (!mismatch.some((p) => p.includes("differs across envs"))) throw new Error("self-test: cross-env mismatch not detected");

  console.log("kernel-worker-retirement-gate guard: self-test passed");
}

function run() {
  const problems = [];
  const perEnv = [];
  for (const env of ["dev", "stg"]) {
    const varsPath = resolve(repo, `infra/envs/${env}/variables.tf`);
    const jobsPath = resolve(repo, `infra/envs/${env}/cloud_run_jobs.tf`);
    if (!existsSync(varsPath)) { problems.push(`${env}: variables.tf not found (${varsPath})`); continue; }
    if (!existsSync(jobsPath)) { problems.push(`${env}: cloud_run_jobs.tf not found (${jobsPath})`); continue; }
    const res = evaluateEnv({
      env,
      variablesTf: readFileSync(varsPath, "utf-8"),
      jobsTf: readFileSync(jobsPath, "utf-8"),
    });
    perEnv.push(res);
    problems.push(...res.problems);
  }
  problems.push(...evaluateCrossEnv(perEnv));

  if (problems.length > 0) {
    console.error("kernel-worker-retirement-gate guard: FAIL");
    for (const p of problems) console.error(`  - ${p}`);
    process.exit(1);
  }
  console.log("kernel-worker-retirement-gate guard: all KERN-01 checks passed (dev, stg)");
}

if (process.argv.includes("--self-test")) selfTest();
else run();
