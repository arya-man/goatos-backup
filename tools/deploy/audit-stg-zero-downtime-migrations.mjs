#!/usr/bin/env node

import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const MIGRATION_DIR = "backend/migrations/postgres";
const ALLOW_MARKER = /--\s*(stg-zero-downtime|zero-downtime):\s*(safe|allow|compatible)\b/i;
const DOWNTIME_MARKER = /--\s*(deploy-downtime-required|stg-downtime-required):\s*\S+/i;

const destructiveRules = [
  ["drop-table", /\bdrop\s+table\b/i],
  ["drop-column", /\bdrop\s+column\b/i],
  ["drop-constraint", /\bdrop\s+constraint\b/i],
  ["rename-object", /\brename\s+(column|to)\b/i],
  ["truncate", /\btruncate\b/i],
  ["delete-data", /\bdelete\s+from\b/i],
  ["set-not-null", /\balter\s+column\b[\s\S]{0,160}\bset\s+not\s+null\b/i],
  ["validate-breaking-constraint", /\badd\s+constraint\b(?![\s\S]{0,240}\bnot\s+valid\b)/i],
  ["unique-index", /\bcreate\s+unique\s+index\b/i],
];

function gooseUp(sql) {
  const lines = sql.split(/\r?\n/);
  const out = [];
  let inUp = false;
  for (const line of lines) {
    if (/^\s*--\s*\+goose\s+up\b/i.test(line)) {
      inUp = true;
      continue;
    }
    if (/^\s*--\s*\+goose\s+down\b/i.test(line)) break;
    if (inUp) out.push(line);
  }
  return out.join("\n");
}

function stripSqlComments(sql) {
  return sql
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .split(/\r?\n/)
    .map((line) => line.replace(/--.*$/, ""))
    .join("\n");
}

export function auditSql(filename, sql) {
  const up = gooseUp(sql);
  const body = stripSqlComments(up);
  const rules = destructiveRules.filter(([, re]) => re.test(body)).map(([name]) => name);
  if (rules.length === 0) return null;

  const markedSafe = ALLOW_MARKER.test(up);
  const markedDowntime = DOWNTIME_MARKER.test(up);
  return {
    file: filename,
    rules,
    markedSafe,
    markedDowntime,
    okForZeroDowntime: markedSafe && !markedDowntime,
  };
}

function changedMigrationFiles(base, cwd = repo) {
  const args = ["diff", "--name-status", base, "--", `${MIGRATION_DIR}/*.sql`];
  const out = execFileSync("git", args, { cwd, encoding: "utf8" });
  return out
    .split(/\r?\n/)
    .filter(Boolean)
    .map((line) => {
      const [status, file] = line.split(/\s+/, 2);
      return { status, file };
    })
    .filter(({ status }) => status !== "D")
    .map(({ file }) => file);
}

function listAllMigrationFiles() {
  const out = execFileSync("git", ["ls-files", `${MIGRATION_DIR}/*.sql`], { cwd: repo, encoding: "utf8" });
  const tracked = out.split(/\r?\n/).filter(Boolean);
  const untrackedOut = execFileSync("git", ["ls-files", "--others", "--exclude-standard", `${MIGRATION_DIR}/*.sql`], {
    cwd: repo,
    encoding: "utf8",
  });
  return [...new Set([...tracked, ...untrackedOut.split(/\r?\n/).filter(Boolean)])].sort();
}

function usage() {
  console.error("usage: audit-stg-zero-downtime-migrations.mjs [--base <ref>|--all|--files <file...>] [--enforce]");
}

function parseArgs(argv) {
  const opts = { base: "HEAD", all: false, enforce: false, files: null, selfTest: false };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--self-test") opts.selfTest = true;
    else if (arg === "--all") opts.all = true;
    else if (arg === "--enforce") opts.enforce = true;
    else if (arg === "--base") opts.base = argv[++i];
    else if (arg === "--files") {
      opts.files = [];
      while (argv[i + 1] && !argv[i + 1].startsWith("--")) {
        opts.files.push(argv[++i]);
      }
    } else {
      usage();
      process.exit(2);
    }
  }
  return opts;
}

function run() {
  const opts = parseArgs(process.argv.slice(2));
  if (opts.selfTest) return selfTest();

  const files = opts.files ? opts.files : opts.all ? listAllMigrationFiles() : changedMigrationFiles(opts.base);
  const migrationFiles = files.filter((file) => file.startsWith(MIGRATION_DIR) && file.endsWith(".sql"));
  if (migrationFiles.length === 0) {
    console.log("stg zero-downtime migration audit: ok (no touched postgres migrations)");
    return;
  }

  const findings = [];
  for (const file of migrationFiles) {
    const path = resolve(repo, file);
    if (!existsSync(path)) continue;
    const finding = auditSql(file, readFileSync(path, "utf8"));
    if (finding) findings.push(finding);
  }

  if (findings.length === 0) {
    console.log(`stg zero-downtime migration audit: ok (${migrationFiles.length} migration file(s), no destructive Up operations)`);
    return;
  }

  console.error("stg zero-downtime migration audit: destructive migration operation(s) found");
  for (const f of findings) {
    const status = f.okForZeroDowntime ? "annotated compatible" : f.markedDowntime ? "requires downtime" : "not annotated";
    console.error(`- ${f.file}: ${f.rules.join(", ")} (${status})`);
  }
  console.error("For zero downtime, use expand/contract: add new schema first, deploy compatible code, backfill, then remove old schema later.");

  if (opts.enforce && findings.some((f) => !f.okForZeroDowntime)) process.exit(1);
}

function selfTest() {
  const safeAdd = "-- +goose Up\nCREATE TABLE a (id uuid PRIMARY KEY);\n-- +goose Down\nDROP TABLE a;";
  const safeAdd2 = "-- +goose Up\nCREATE TABLE b (id uuid PRIMARY KEY);\n-- +goose Down\nDROP TABLE b;";
  const drop = "-- +goose Up\nALTER TABLE goats DROP COLUMN old_name;\n-- +goose Down\n";
  const annotated = "-- +goose Up\n-- stg-zero-downtime: safe because old code does not read this table\nDROP TABLE unused_shadow;\n-- +goose Down\n";
  const markedDowntime = "-- +goose Up\n-- deploy-downtime-required: rewrites a live table\nDELETE FROM goats;\n-- +goose Down\n";

  if (auditSql("safe.sql", safeAdd) !== null) throw new Error("self-test: additive migration flagged");
  if (!auditSql("drop.sql", drop)?.rules.includes("drop-column")) throw new Error("self-test: drop column missed");
  if (!auditSql("annotated.sql", annotated)?.okForZeroDowntime) throw new Error("self-test: safe annotation not honored");
  if (auditSql("downtime.sql", markedDowntime)?.okForZeroDowntime) throw new Error("self-test: downtime marker allowed");

  const tmp = mkdtempSync(join(tmpdir(), "stg-zdt-migrations-"));
  try {
    const repoDir = join(tmp, "repo");
    const init = spawnSync("git", ["init", repoDir], { encoding: "utf8" });
    if (init.status !== 0) throw new Error(`self-test: git init failed: ${init.stderr}`);
    const dir = join(repoDir, MIGRATION_DIR);
    const mkdir = spawnSync("mkdir", ["-p", dir], { encoding: "utf8" });
    if (mkdir.status !== 0) throw new Error(`self-test: mkdir failed: ${mkdir.stderr}`);
    writeFileSync(join(dir, "000001_safe.sql"), safeAdd);
    const add = spawnSync("git", ["add", "."], { cwd: repoDir, encoding: "utf8" });
    if (add.status !== 0) throw new Error(`self-test: git add failed: ${add.stderr}`);
    const commit = spawnSync("git", ["commit", "-m", "base"], {
      cwd: repoDir,
      env: { ...process.env, GIT_AUTHOR_NAME: "Test", GIT_AUTHOR_EMAIL: "test@example.com", GIT_COMMITTER_NAME: "Test", GIT_COMMITTER_EMAIL: "test@example.com" },
      encoding: "utf8",
    });
    if (commit.status !== 0) throw new Error(`self-test: git commit failed: ${commit.stderr}`);
    writeFileSync(join(dir, "000001_safe.sql"), drop);
    if (!changedMigrationFiles("HEAD", repoDir).includes(`${MIGRATION_DIR}/000001_safe.sql`)) {
      throw new Error("self-test: changed lower-number migration was skipped");
    }
    writeFileSync(join(dir, "000002_safe.sql"), safeAdd2);
    const result = spawnSync(process.execPath, [fileURLToPath(import.meta.url), "--files", `${MIGRATION_DIR}/000002_safe.sql`, "--enforce"], {
      cwd: repoDir,
      encoding: "utf8",
    });
    if (result.status !== 0) throw new Error(`self-test: clean exit failed: ${result.stderr}`);
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }

  console.log("stg zero-downtime migration audit: self-test passed");
}

run();
