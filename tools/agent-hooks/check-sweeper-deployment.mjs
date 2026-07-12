#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

export function findingsForSources(jobs, variables) {
  const findings = [];
  const block = jobs.match(/obligation_sweeper\s*=\s*\{([\s\S]*?)\n\s*\}\n\s*[a-z_]+\s*=\s*\{/i)?.[1] ?? "";
  if (!block) findings.push("staging obligation_sweeper block is missing");
  if (!/GOATOS_SWEEPER_ACTOR_ID\s*=\s*var\.stg_sweeper_actor_id/.test(block)) {
    findings.push("staging obligation_sweeper must wire GOATOS_SWEEPER_ACTOR_ID from stg_sweeper_actor_id");
  }
  const variable = variables.match(/variable\s+"stg_sweeper_actor_id"\s*\{([\s\S]*?)\n\}/)?.[1] ?? "";
  if (!variable) findings.push("stg_sweeper_actor_id variable is missing");
  if (/\bdefault\s*=/.test(variable)) findings.push("stg_sweeper_actor_id must not have a guessed/default identity");
  if (!/validation\s*\{/.test(variable) || !/version-4 UUID/.test(variable)) {
    findings.push("stg_sweeper_actor_id must have UUID validation");
  }
  return findings;
}

function selfTest() {
  const goodJobs = `obligation_sweeper = {\n env = {\n GOATOS_SWEEPER_ACTOR_ID = var.stg_sweeper_actor_id\n }\n }\n next_job = {`;
  const goodVars = `variable "stg_sweeper_actor_id" {\n type = string\n validation { error_message = "version-4 UUID" }\n}`;
  if (findingsForSources(goodJobs, goodVars).length) throw new Error("self-test rejected valid deployment wiring");
  const bad = findingsForSources(`obligation_sweeper = {\n env = {}\n }\n next_job = {`, `variable "stg_sweeper_actor_id" {\n default = "fake"\n}`);
  if (bad.length < 3) throw new Error(`self-test missed deployment bypasses: ${bad.join("; ")}`);
  console.log("sweeper-deployment guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const jobs = readFileSync(resolve(repo, "infra/envs/stg/cloud_run_jobs.tf"), "utf8");
const variables = readFileSync(resolve(repo, "infra/envs/stg/variables.tf"), "utf8");
const findings = findingsForSources(jobs, variables);
if (findings.length) {
  console.error("sweeper-deployment guard failed:");
  for (const finding of findings) console.error(`- ${finding}`);
  process.exit(1);
}
console.log("sweeper-deployment guard: staging actor wiring is fail-closed");
