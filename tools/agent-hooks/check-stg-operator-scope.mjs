#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const accountsPath = resolve(repo, "backend/cmd/seed-stg-login-grants/accounts.go");
const mainPath = resolve(repo, "backend/cmd/seed-stg-login-grants/main.go");

function accountBlocks(source) {
  const marker = "var stgLoginAccounts = []Account{";
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

function checkSources(accountsSource, mainSource) {
  const problems = [];
  for (const block of accountBlocks(accountsSource)) {
    const requiresPark = /Role:\s*permissions\.RoleOperator\b/.test(block);
    if (requiresPark && !/ParkCode:\s*"[^"]+"/.test(block)) {
      problems.push(`${displayName(block)} is a park staff account but has no ParkCode`);
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
  if (checkSources(goodAccounts, goodMain).length !== 0) {
    throw new Error("self-test: valid park-scoped operator fixture failed");
  }
  if (!checkSources(badAccounts, goodMain).some((p) => p.includes("Amit"))) {
    throw new Error("self-test: missing operator ParkCode was not detected");
  }
  if (!checkSources(goodAccounts, "func materializeTenantGrant() {}").some((p) => p.includes("tenant-only"))) {
    throw new Error("self-test: tenant-only materializer was not detected");
  }
}

if (process.argv.includes("--self-test")) {
  selfTest();
  console.log("stg-operator-scope guard self-test passed");
  process.exit(0);
}

const problems = checkSources(readFileSync(accountsPath, "utf8"), readFileSync(mainPath, "utf8"));
if (problems.length > 0) {
  console.error("STG operator scope guard failed:");
  for (const problem of problems) console.error(`- ${problem}`);
  console.error("Real operators must be park-scoped. Director/leadership visibility may be tenant-scoped.");
  process.exit(1);
}

console.log("STG operator scope guard passed");
