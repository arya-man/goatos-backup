#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const accountsPath = resolve(repo, "backend/cmd/seed-stg-login-grants/accounts.go");
const mainPath = resolve(repo, "backend/cmd/seed-stg-login-grants/main.go");
const perPersonPath = resolve(repo, "backend/cmd/seed-stg-login-grants/approvers.go");

function accountBlocks(source) {
  return topLevelBlocks(source, "var stgLoginAccounts = []Account{");
}

// perPersonGrants (backend/cmd/seed-stg-login-grants/approvers.go) grants extra roles to NAMED
// individuals at tenant scope. It was invisible to this guard until 2026-08-07, which meant a
// tenant-scoped RoleOperator grant could be added there and bypass the operator-scope invariant
// entirely -- the guard would still print "passed". Tenant scope is allowed here (these are
// authority grants layered on directors, not park staff accounts) but only when the source says
// why, so the exception is recorded rather than silent.
function perPersonBlocks(source) {
  return topLevelBlocks(source, "var perPersonGrants = []personGrant{");
}

function topLevelBlocks(source, marker) {
  const start = source.indexOf(marker);
  if (start < 0) return [];
  const bodyStart = source.indexOf("{", start);
  let depth = 0;
  let blockStart = -1;
  const blocks = [];
  for (let i = bodyStart + 1; i < source.length; i++) {
    const ch = source[i];
    if (ch === "{") {
      if (depth === 0) blockStart = i;
      depth++;
    } else if (ch === "}") {
      depth--;
      if (depth === 0 && blockStart >= 0) {
        blocks.push(source.slice(blockStart, i + 1));
        blockStart = -1;
      } else if (depth < 0) {
        break;
      }
    }
  }
  return blocks;
}

function displayName(block) {
  return block.match(/DisplayName:\s*"([^"]+)"/)?.[1] ?? "(unknown)";
}

function personEmail(block) {
  return block.match(/email:\s*"([^"]+)"/)?.[1] ?? "(unknown)";
}

function checkSources(accountsSource, mainSource, perPersonSource = "") {
  const problems = [];
  for (const block of accountBlocks(accountsSource)) {
    const requiresPark = /Role:\s*permissions\.RoleOperator\b/.test(block);
    if (requiresPark && !/ParkCode:\s*"[^"]+"/.test(block)) {
      problems.push(`${displayName(block)} is a park staff account but has no ParkCode`);
    }
  }

  // Per-person grants are always tenant-scoped by construction (seedPerPersonGrants passes
  // "tenant"), so an operator role here is a deliberate exception to the operator-scope invariant
  // and must carry its justification IN THE SAME BLOCK. A repo-wide string search would pass on one
  // annotation copied anywhere in the file, which is why this is block-scoped.
  for (const block of perPersonBlocks(perPersonSource)) {
    if (!/permissions\.RoleOperator\b/.test(block)) continue;
    if (!/stg-operator-scope:\s*tenant approved/.test(block)) {
      problems.push(
        `${personEmail(block)} is granted a tenant-scoped operator role with no "stg-operator-scope: tenant approved" justification in its own block`,
      );
    }
  }

  if (!/func\s+resolveGrantScope\b/.test(mainSource)) {
    problems.push("seed-stg-login-grants must resolve park staff scope explicitly");
  }
  if (!/func\s+materializeScopeGrant\b/.test(mainSource)) {
    problems.push("seed-stg-login-grants must materialize scoped grants, not tenant-only grants");
  }
  if (/func\s+materializeTenantGrant\b/.test(mainSource)) {
    problems.push("tenant-only materializeTenantGrant must not exist in STG login seeder");
  }
  if (!/scopeType\s*!=\s*"tenant"[\s\S]*status\s*=\s*'revoked'[\s\S]*scope_type\s*=\s*'tenant'/.test(mainSource)) {
    problems.push("park-scoped seeding must revoke stale active tenant grants");
  }

  return problems;
}

function selfTest() {
  const goodAccounts = `
var stgLoginAccounts = []Account{
  {DisplayName: "Ravi", Role: permissions.RoleCEOInternal},
  {DisplayName: "Amit", Role: permissions.RoleOperator, ParkCode: "CPT"},
  {DisplayName: "Chandrakant", Role: permissions.RolePCDirector},
}`;
  const goodMain = `
func resolveGrantScope() {}
func materializeScopeGrant() {
  if scopeType != "tenant" {
    UPDATE user_scope_grants SET status = 'revoked' WHERE scope_type = 'tenant'
  }
}`;
  const badAccounts = `
var stgLoginAccounts = []Account{
  {DisplayName: "Amit", Role: permissions.RoleOperator},
}`;
  // Per-person grants: tenant-scoped operator is allowed only with an in-block justification.
  const goodPerPerson = `
var perPersonGrants = []personGrant{
  {
    email: "director@example.com",
    roles: []string{
      permissions.RoleCountsApprover,
      // stg-operator-scope: tenant approved — layered on a director, not park staff.
      permissions.RoleOperator,
    },
  },
}`;
  const badPerPerson = `
var perPersonGrants = []personGrant{
  {
    email: "sneaky@example.com",
    roles: []string{permissions.RoleCountsApprover, permissions.RoleOperator},
  },
}`;
  // Adversarial: the justification exists in the FILE but in another person's block. A naive
  // whole-file string search passes this; a block-scoped check must not.
  const annotationInWrongBlock = `
var perPersonGrants = []personGrant{
  {
    email: "okdirector@example.com",
    roles: []string{
      // stg-operator-scope: tenant approved — this one is fine.
      permissions.RoleOperator,
    },
  },
  {
    email: "unjustified@example.com",
    roles: []string{permissions.RoleOperator},
  },
}`;

  if (checkSources(goodAccounts, goodMain).length !== 0) {
    throw new Error("self-test: valid park-scoped operator fixture failed");
  }
  if (!checkSources(badAccounts, goodMain).some((p) => p.includes("Amit"))) {
    throw new Error("self-test: missing operator ParkCode was not detected");
  }
  if (!checkSources(goodAccounts, "func materializeTenantGrant() {}").some((p) => p.includes("tenant-only"))) {
    throw new Error("self-test: tenant-only materializer was not detected");
  }
  if (checkSources(goodAccounts, goodMain, goodPerPerson).length !== 0) {
    throw new Error("self-test: justified tenant-scoped per-person operator grant failed");
  }
  if (!checkSources(goodAccounts, goodMain, badPerPerson).some((p) => p.includes("sneaky@example.com"))) {
    throw new Error("self-test: unjustified per-person operator grant was not detected");
  }
  const wrongBlock = checkSources(goodAccounts, goodMain, annotationInWrongBlock);
  if (!wrongBlock.some((p) => p.includes("unjustified@example.com"))) {
    throw new Error("self-test: justification in another person's block was accepted");
  }
  if (wrongBlock.some((p) => p.includes("okdirector@example.com"))) {
    throw new Error("self-test: correctly justified block was flagged");
  }
}

if (process.argv.includes("--self-test")) {
  selfTest();
  console.log("stg-operator-scope guard self-test passed");
  process.exit(0);
}

const problems = checkSources(
  readFileSync(accountsPath, "utf8"),
  readFileSync(mainPath, "utf8"),
  readFileSync(perPersonPath, "utf8"),
);
if (problems.length > 0) {
  console.error("STG operator scope guard failed:");
  for (const problem of problems) console.error(`- ${problem}`);
  console.error("Real operators must be park-scoped. Director/leadership visibility may be tenant-scoped.");
  process.exit(1);
}

console.log("STG operator scope guard passed");
