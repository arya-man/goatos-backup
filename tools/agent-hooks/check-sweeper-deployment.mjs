#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Kernel-worker convergence (see docs/decisions/operational-kernel-5k-50k-scale-envelope.md):
// The obligation sweeper now deploys as a stage INSIDE the kernel-worker Cloud Run SERVICE
// (infra/envs/stg/cloud_run_worker.tf), not as a standalone Cloud Run Job.
// The sweeper stage requires GOATOS_SWEEPER_ACTOR_ID to be wired from the audited
// stg_sweeper_actor_id variable. This guard verifies the service is deployed and
// the actor id is correctly wired from var.stg_sweeper_actor_id (not a hardcoded/fake value).

export function findingsForSources(cloudRunWorker, variables) {
  const findings = [];

  // Verify the kernel_worker service resource exists
  if (!/resource\s+"google_cloud_run_v2_service"\s+"kernel_worker"/.test(cloudRunWorker)) {
    findings.push("staging obligation_sweeper block is missing");
    return findings;
  }

  // Verify the kernel-worker service wires GOATOS_SWEEPER_ACTOR_ID correctly.
  // In HCL, the env variable is structured as:
  //   env {
  //     name  = "GOATOS_SWEEPER_ACTOR_ID"
  //     value = var.stg_sweeper_actor_id
  //   }
  // So we check for the name in an env block followed by the value assignment.
  if (!/GOATOS_SWEEPER_ACTOR_ID[\s\S]*?value\s*=\s*var\.stg_sweeper_actor_id/.test(cloudRunWorker)) {
    findings.push("staging obligation_sweeper must wire GOATOS_SWEEPER_ACTOR_ID from stg_sweeper_actor_id");
  }

  // Verify the variable definition is present and properly validated
  const variable = variables.match(/variable\s+"stg_sweeper_actor_id"\s*\{([\s\S]*?)\n\}/)?.[1] ?? "";
  if (!variable) findings.push("stg_sweeper_actor_id variable is missing");
  if (/\bdefault\s*=/.test(variable)) findings.push("stg_sweeper_actor_id must not have a guessed/default identity");
  if (!/validation\s*\{/.test(variable) || !/version-4 UUID/.test(variable)) {
    findings.push("stg_sweeper_actor_id must have UUID validation");
  }
  return findings;
}

function selfTest() {
  // Good fixture: kernel_worker service with GOATOS_SWEEPER_ACTOR_ID wired correctly
  const goodService = `resource "google_cloud_run_v2_service" "kernel_worker" {\n  name = "goatos-kernel-worker-stg"\n  template {\n    containers {\n      name = "kernel-worker"\n      env {\n        name  = "GOATOS_SWEEPER_ACTOR_ID"\n        value = var.stg_sweeper_actor_id\n      }\n    }\n  }\n}`;
  const goodVars = `variable "stg_sweeper_actor_id" {\n  type = string\n  validation { error_message = "version-4 UUID" }\n}`;

  if (findingsForSources(goodService, goodVars).length) {
    throw new Error("self-test rejected valid deployment wiring");
  }

  // Bad fixture 1: kernel_worker service is missing
  const noService = `resource "google_cloud_run_v2_job" "other" {\n  name = "some-job"\n}`;
  const bad1 = findingsForSources(noService, goodVars);
  if (!bad1.some(f => f.includes("missing"))) {
    throw new Error(`self-test missed missing service: ${bad1.join("; ")}`);
  }

  // Bad fixture 2: env wires a literal/fake value instead of var.stg_sweeper_actor_id
  const badEnv = `resource "google_cloud_run_v2_service" "kernel_worker" {\n  name = "goatos-kernel-worker-stg"\n  template {\n    containers {\n      name = "kernel-worker"\n      env {\n        name  = "GOATOS_SWEEPER_ACTOR_ID"\n        value = "fake-hardcoded-id"\n      }\n    }\n  }\n}`;
  const bad2 = findingsForSources(badEnv, goodVars);
  if (!bad2.some(f => f.includes("must wire GOATOS_SWEEPER_ACTOR_ID"))) {
    throw new Error(`self-test missed hardcoded actor id: ${bad2.join("; ")}`);
  }

  // Bad fixture 3: variable is missing validation
  const badVar = `variable "stg_sweeper_actor_id" {\n  type = string\n}`;
  const bad3 = findingsForSources(goodService, badVar);
  if (!bad3.some(f => f.includes("validation"))) {
    throw new Error(`self-test missed missing validation: ${bad3.join("; ")}`);
  }

  console.log("sweeper-deployment guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const cloudRunWorker = readFileSync(resolve(repo, "infra/envs/stg/cloud_run_worker.tf"), "utf8");
const variables = readFileSync(resolve(repo, "infra/envs/stg/variables.tf"), "utf8");
const findings = findingsForSources(cloudRunWorker, variables);
if (findings.length) {
  console.error("sweeper-deployment guard failed:");
  for (const finding of findings) console.error(`- ${finding}`);
  process.exit(1);
}
console.log("sweeper-deployment guard: staging actor wiring is fail-closed");
