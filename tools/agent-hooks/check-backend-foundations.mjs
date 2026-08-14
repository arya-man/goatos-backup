#!/usr/bin/env node
// Backend toolchain/security/verification recurrence guard.

import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { spawnSync } from "node:child_process";

const root = process.cwd();
const minimumGo = "1.25.13";
const minimumPgx = "5.9.2";
const pinnedVulncheck = "v1.6.0";

function versionParts(version) {
  return version.split(".").map((part) => Number.parseInt(part, 10) || 0);
}

function versionAtLeast(actual, minimum) {
  const left = versionParts(actual);
  const right = versionParts(minimum);
  for (let index = 0; index < Math.max(left.length, right.length); index += 1) {
    if ((left[index] ?? 0) !== (right[index] ?? 0)) return (left[index] ?? 0) > (right[index] ?? 0);
  }
  return true;
}

function validate({ goMod, docker, migrateDocker, localCi }) {
  const findings = [];
  const goVersion = goMod.match(/^go\s+(\d+\.\d+(?:\.\d+)?)\s*$/m)?.[1];
  if (!goVersion) findings.push("backend/go.mod must declare an exact Go patch version");
  else if (!versionAtLeast(goVersion, minimumGo)) findings.push(`Go ${goVersion} is below security floor ${minimumGo}`);

  for (const [name, contents] of [["backend/Dockerfile", docker], ["backend/Dockerfile.migrate", migrateDocker]]) {
    const builder = contents.match(/^FROM\s+golang:(\d+\.\d+(?:\.\d+)?)-alpine\s+AS\s+build\s*$/m)?.[1];
    if (!builder) findings.push(`${name} must pin an exact golang patch builder`);
    else if (goVersion && builder !== goVersion) findings.push(`${name} Go ${builder} does not match go.mod ${goVersion}`);
  }

  const pgx = goMod.match(/^\s*github\.com\/jackc\/pgx\/v5\s+v(\d+\.\d+\.\d+)\s*$/m)?.[1];
  if (!pgx || !versionAtLeast(pgx, minimumPgx)) findings.push(`pgx must be at least v${minimumPgx}`);

  const requiredCi = [
    "go mod verify",
    "go vet ./...",
    `govulncheck@${pinnedVulncheck}`,
    "vet -f sqlc.yaml",
    "diff -f sqlc.yaml",
    "go test -race",
  ];
  for (const required of requiredCi) {
    if (!localCi.includes(required)) findings.push(`local backend CI must execute: ${required}`);
  }
  return findings;
}

function changedGoFormattingFindings() {
  let base = process.env.GOATOS_CI_BASE || "origin/main";
  if (spawnSync("git", ["rev-parse", "--verify", `${base}^{commit}`], { cwd: root }).status !== 0) base = "HEAD~1";
  const diff = spawnSync("git", ["diff", "--name-only", "--diff-filter=ACMR", base, "--", "backend"], {
    cwd: root,
    encoding: "utf8",
  });
  const untracked = spawnSync("git", ["ls-files", "--others", "--exclude-standard", "--", "backend"], {
    cwd: root,
    encoding: "utf8",
  });
  const files = [...new Set(`${diff.stdout || ""}\n${untracked.stdout || ""}`.split("\n"))]
    .filter((file) => file.endsWith(".go") && fs.existsSync(path.join(root, file)));
  if (files.length === 0) return [];
  const formatted = spawnSync("gofmt", ["-l", ...files], { cwd: root, encoding: "utf8" });
  if (formatted.status !== 0) return [`gofmt failed: ${formatted.stderr?.trim() || "unknown error"}`];
  return (formatted.stdout || "").trim().split("\n").filter(Boolean).map((file) => `${file} is not gofmt-clean`);
}

function runSelfTest() {
  const good = validate({
    goMod: "module example\n\ngo 1.25.13\n\nrequire (\n\tgithub.com/jackc/pgx/v5 v5.9.2\n)\n",
    docker: "FROM golang:1.25.13-alpine AS build\n",
    migrateDocker: "FROM golang:1.25.13-alpine AS build\n",
    localCi: "go mod verify; go vet ./...; govulncheck@v1.6.0; sqlc vet -f sqlc.yaml; sqlc diff -f sqlc.yaml; go test -race",
  });
  const bad = validate({
    goMod: "module example\n\ngo 1.23.0\n\nrequire (\n\tgithub.com/jackc/pgx/v5 v5.7.6\n)\n",
    docker: "FROM golang:1.23-alpine AS build\n",
    migrateDocker: "FROM golang:1.24.0-alpine AS build\n",
    localCi: "go test ./...",
  });
  if (good.length !== 0 || bad.length < 6) throw new Error(`self-test failed: good=${good} bad=${bad}`);
  console.log("backend-foundations guard self-test passed");
}

if (process.argv.includes("--self-test")) {
  runSelfTest();
  process.exit(0);
}

const read = (relative) => fs.readFileSync(path.join(root, relative), "utf8");
const findings = validate({
  goMod: read("backend/go.mod"),
  docker: read("backend/Dockerfile"),
  migrateDocker: read("backend/Dockerfile.migrate"),
  localCi: read("tools/ci/run-local-ci.sh"),
});
findings.push(...changedGoFormattingFindings());

if (findings.length > 0) {
  console.error("backend-foundations guard FAILED:");
  for (const finding of findings) console.error(`  ${finding}`);
  process.exit(1);
}
console.log("backend-foundations guard passed");
