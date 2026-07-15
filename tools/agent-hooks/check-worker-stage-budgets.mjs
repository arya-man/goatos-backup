#!/usr/bin/env node
// check-worker-stage-budgets.mjs — KERN-REV-05 parity guard.
//
// Consolidating the retired per-stage Cloud Run Jobs into the kernel-worker
// service must NOT silently shrink their processing budgets. Each retired job
// ran with an explicit batch limit; the consolidated stages fall back to the
// one-shot default of 50 unless the worker service Terraform sets the env
// explicitly. This guard asserts each env's cloud_run_worker.tf sets the batch
// limits to at least the retired job budget, so a future edit that drops the env
// (reverting to 50) fails CI instead of quietly halving throughput.
import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const ENVS = ["dev", "stg"];

// Retired job budgets that the consolidated stages must meet or exceed.
// Source: the pre-consolidation infra/envs/*/cloud_run_jobs.tf kernel_jobs map
// (outbox-relay -limit=500; notification-dispatcher -limit=100).
const REQUIRED_MIN = {
  GOATOS_OUTBOX_LIMIT: 500,
  GOATOS_NOTIFICATION_LIMIT: 100,
};

// Reads an env value from a Cloud Run v2 service `env { name=.. value=".." }`
// block. Returns null if the env is absent.
function envValue(tf, name) {
  const re = new RegExp(
    `name\\s*=\\s*"${name}"[\\s\\S]{0,80}?value\\s*=\\s*"([^"]*)"`,
  );
  const m = tf.match(re);
  return m ? m[1] : null;
}

const errors = [];
for (const env of ENVS) {
  const path = resolve(repo, "infra/envs", env, "cloud_run_worker.tf");
  if (!existsSync(path)) {
    errors.push(`infra/envs/${env}/cloud_run_worker.tf: missing (kernel-worker service not defined)`);
    continue;
  }
  const tf = readFileSync(path, "utf8");
  for (const [name, min] of Object.entries(REQUIRED_MIN)) {
    const raw = envValue(tf, name);
    if (raw === null) {
      errors.push(
        `infra/envs/${env}/cloud_run_worker.tf: ${name} is not set — the consolidated stage would fall back to the default 50, below the retired job budget of ${min}`,
      );
      continue;
    }
    const val = Number.parseInt(raw, 10);
    if (!Number.isFinite(val) || val < min) {
      errors.push(
        `infra/envs/${env}/cloud_run_worker.tf: ${name}=${raw} is below the retired job budget of ${min}`,
      );
    }
  }
}

if (errors.length > 0) {
  console.error("worker-stage-budgets guard failed:");
  for (const e of errors) console.error(`- ${e}`);
  process.exit(1);
}
console.error(
  `worker-stage-budgets guard: consolidated stage batch limits meet the retired job budgets (${ENVS.length} environments checked)`,
);
