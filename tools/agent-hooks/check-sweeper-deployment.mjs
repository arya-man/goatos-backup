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
  const service = extractBlock(
    cloudRunWorker,
    /resource\s+"google_cloud_run_v2_service"\s+"kernel_worker"\s*\{/,
  );
  if (!service) {
    findings.push("staging obligation_sweeper block is missing");
    return findings;
  }

  // Verify the kernel-worker service wires GOATOS_SWEEPER_ACTOR_ID correctly.
  // In HCL, the env variable is structured as:
  //   env {
  //     name  = "GOATOS_SWEEPER_ACTOR_ID"
  //     value = var.stg_sweeper_actor_id
  //   }
  // Bound the check to one env block. A file-wide regex can combine a fake actor
  // value from one env block with the expected variable from a later block and
  // incorrectly report green.
  const actorWired = findBlocks(service, /\benv\s*\{/).some((env) =>
    /\bname\s*=\s*"GOATOS_SWEEPER_ACTOR_ID"/.test(env) &&
    /\bvalue\s*=\s*var\.stg_sweeper_actor_id\b/.test(env),
  );
  if (!actorWired) {
    findings.push("staging obligation_sweeper must wire GOATOS_SWEEPER_ACTOR_ID from stg_sweeper_actor_id");
  }

  // Verify the variable definition is present and properly validated
  const variable = extractBlock(variables, /variable\s+"stg_sweeper_actor_id"\s*\{/) ?? "";
  if (!variable) findings.push("stg_sweeper_actor_id variable is missing");
  if (/\bdefault\s*=/.test(variable)) findings.push("stg_sweeper_actor_id must not have a guessed/default identity");
  if (!/validation\s*\{/.test(variable) || !/version-4 UUID/.test(variable)) {
    findings.push("stg_sweeper_actor_id must have UUID validation");
  }
  return findings;
}

// Terraform/HCL blocks can contain nested blocks. This small balanced-brace
// reader is deliberately limited to the guard's structural checks and ignores
// braces inside quoted strings and comments so matching cannot cross siblings.
function extractBlock(source, header) {
  const match = header.exec(source);
  if (!match) return null;
  const open = source.indexOf("{", match.index + match[0].length - 1);
  const close = matchingBrace(source, open);
  return close < 0 ? null : source.slice(open + 1, close);
}

function findBlocks(source, header) {
  const blocks = [];
  const re = new RegExp(header.source, header.flags.includes("g") ? header.flags : `${header.flags}g`);
  for (let match; (match = re.exec(source)); ) {
    const open = source.indexOf("{", match.index + match[0].length - 1);
    const close = matchingBrace(source, open);
    if (close < 0) break;
    blocks.push(source.slice(open + 1, close));
    re.lastIndex = close + 1;
  }
  return blocks;
}

function matchingBrace(source, open) {
  if (open < 0) return -1;
  let depth = 0;
  let quote = false;
  let escape = false;
  let lineComment = false;
  let blockComment = false;
  for (let i = open; i < source.length; i += 1) {
    const ch = source[i];
    const next = source[i + 1];
    if (lineComment) {
      if (ch === "\n") lineComment = false;
      continue;
    }
    if (blockComment) {
      if (ch === "*" && next === "/") {
        blockComment = false;
        i += 1;
      }
      continue;
    }
    if (quote) {
      if (escape) escape = false;
      else if (ch === "\\") escape = true;
      else if (ch === '"') quote = false;
      continue;
    }
    if (ch === '"') {
      quote = true;
      continue;
    }
    if (ch === "#" || (ch === "/" && next === "/")) {
      lineComment = true;
      if (ch === "/") i += 1;
      continue;
    }
    if (ch === "/" && next === "*") {
      blockComment = true;
      i += 1;
      continue;
    }
    if (ch === "{") depth += 1;
    else if (ch === "}" && --depth === 0) return i;
  }
  return -1;
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

  // Bad fixture 4: a fake actor value in one env block must not combine with
  // the expected variable from an unrelated later block.
  const crossBlock = `resource "google_cloud_run_v2_service" "kernel_worker" {
  template {
    containers {
      env {
        name  = "GOATOS_SWEEPER_ACTOR_ID"
        value = "fake-hardcoded-id"
      }
      env {
        name  = "UNRELATED_SETTING"
        value = var.stg_sweeper_actor_id
      }
    }
  }
}`;
  const bad4 = findingsForSources(crossBlock, goodVars);
  if (!bad4.some(f => f.includes("must wire GOATOS_SWEEPER_ACTOR_ID"))) {
    throw new Error(`self-test missed cross-env-block bypass: ${bad4.join("; ")}`);
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
