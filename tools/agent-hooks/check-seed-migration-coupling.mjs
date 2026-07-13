#!/usr/bin/env node

// check-seed-migration-coupling.mjs
//
// A migration that changes initial-seed-owned tables must update the seed path
// in the same patch: seed command, seed test/E2E, or seed runbook. This catches
// schema/read-model drift where canonical rows seed correctly but the app fails
// because a new required projection/config table is empty or unhandled.
//
// Modes:
//   (default)   diff-scoped scan vs $SEED_MIGRATION_GUARD_BASE or origin/main,
//               plus staged/unstaged working-tree changes.
//   --self-test run the built-in fixtures and exit.
//
// Exception, only for a reviewed no-seed-impact case:
//   seed-migration-guard:ignore owner=<name> issue=<url|id> reason=<text> expiry=<YYYY-MM-DD>

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const SEED_SENSITIVE_TABLES = new Map([
  ["tenants", "tenant bootstrap"],
  ["locations", "park/shed source seed"],
  ["location_profiles", "park/shed source seed"],
  ["location_operational_attributes", "park/shed source seed"],
  ["animal_stage_lookup", "goat identity source seed"],
  ["goats", "goat identity source seed"],
  ["goat_identifiers", "goat identity source seed"],
  ["goat_identity_events", "goat identity/source event seed"],

  ["hr_departments", "HRMS setup seed"],
  ["workforce_members", "HRMS roster seed"],
  ["workforce_positions", "HRMS position/ownership seed"],
  ["workforce_absences", "attendance/leave seed"],
  ["workforce_member_devices", "notification/device setup seed"],
  ["position_module_duties", "position duty seed"],

  ["user_scope_grants", "founder/admin grant seed"],
  ["auth_pending_email_grants", "founder/admin grant seed"],
  ["org_tiers", "role catalog seed"],
  ["org_verticals", "role catalog seed"],
  ["org_role_catalog", "role catalog seed"],

  ["protocol_definitions", "protocol/config seed"],
  ["protocol_versions", "protocol/config seed"],
  ["protocol_rules", "protocol/config seed"],
  ["protocol_rule_dimensions", "protocol/config seed"],
  ["vaccination_capacity_config", "vaccination config seed"],

  ["sop_definitions", "SOP/config seed"],
  ["sop_versions", "SOP/config seed"],
  ["sop_tasks", "SOP execution seed boundary"],
  ["sop_submissions", "SOP execution seed boundary"],
  ["sop_submission_items", "SOP execution seed boundary"],
  ["proof_artifacts", "proof/verification setup seed boundary"],

  ["obligation_instances", "obligation kernel seed/generation"],
  ["obligation_batches", "obligation kernel seed/generation"],
  ["obligation_status_events", "obligation kernel seed/generation"],
  ["vaccination_completions", "vaccination history seed"],
  ["vaccination_eligibility_rollups", "vaccination rollup closeout"],

  ["process_integrity_projection_rows", "process-integrity projection closeout"],
  ["process_integrity_projection_state", "process-integrity projection closeout"],
  ["process_integrity_projection_summaries", "process-integrity projection closeout"],
  ["vaccination_shed_projection_rows", "vaccination shed projection closeout"],
  ["vaccination_shed_projection_state", "vaccination shed projection closeout"],
  ["vaccination_execution_projection_rows", "vaccination execution projection closeout"],
  ["vaccination_execution_projection_state", "vaccination execution projection closeout"],
  ["vaccination_operations_projection_rows", "vaccination operations projection closeout"],
  ["vaccination_operations_projection_state", "vaccination operations projection closeout"],
  ["calendar_event_projections", "calendar projection closeout"],
  ["calendar_projection_state", "calendar projection closeout"],
  ["calendar_history_projection_rows", "calendar history projection closeout"],
  ["calendar_history_projection_state", "calendar history projection closeout"],
  ["calendar_history_date_markers", "calendar history projection closeout"],
  ["vaccination_projection_dirty_scopes", "incremental projection closeout"],
  ["vaccination_shed_shard_state", "incremental projection closeout"],

  ["notification_requests", "notification setup/outbox seed boundary"],
  ["vaccination_reminder_cadence_fires", "notification cadence seed boundary"],
  ["notification_delivery_attempts", "notification delivery ledger seed boundary"],
  ["verification_items", "verification module setup seed boundary"],
]);

const SEED_SENSITIVE_PATTERNS = [
  {
    re: /^(?:vaccination|process_integrity|calendar|counts)_[a-z0-9_]*(?:projection|rollup|date_marker|dirty_scope|shard_state)[a-z0-9_]*$/,
    reason: "derived read-model/projection closeout",
  },
  {
    re: /^calendar_history_[a-z0-9_]+$/,
    reason: "calendar history projection closeout",
  },
];

const SEED_COMPANION_PATTERNS = [
  /^backend\/cmd\/seed-[^/]+\/.+/,
  /^backend\/cmd\/[^/]*seed[^/]*\/.+/,
  /^backend\/cmd\/[^/]*projection[^/]*recompute\/.+/,
  /^backend\/cmd\/vaccination-projection-worker\/.+/,
  /^tools\/dev\/seed-closeout\.sh$/,
  /^backend\/internal\/.*(?:seed|projection|recompute).*_test\.go$/,
  /^backend\/tests\/e2e(?:-[^/]+)?\/.+/,
  /^docs\/runbooks\/(?:.*seed.*|.*clean-slate.*|vaccination-seed-source-date-contract\.md)$/,
  /^context\/execution\/.*seed.*\.md$/,
];

const MIGRATION_RE = /^backend\/migrations\/postgres\/\d+_[^/]+\.sql$/;
const IGNORE_RE =
  /seed-migration-guard:ignore\s+owner=\S+\s+issue=\S+\s+reason=\S+.*\s+expiry=\d{4}-\d{2}-\d{2}/;

const SQL_OP_RE =
  /\b(CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?|ALTER\s+TABLE(?:\s+IF\s+EXISTS)?|DROP\s+TABLE(?:\s+IF\s+EXISTS)?|TRUNCATE(?:\s+TABLE)?|INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(?:ONLY\s+)?(?:(?:"?[a-zA-Z_][\w]*"?|\*)\.)?"?([a-zA-Z_][\w]*)"?/gi;

function git(args, options = {}) {
  return execFileSync("git", args, {
    cwd: repo,
    encoding: "utf8",
    stdio: ["ignore", "pipe", options.stderr ?? "ignore"],
  });
}

function gitMaybe(args) {
  try {
    return git(args);
  } catch {
    return "";
  }
}

function splitLines(out) {
  return out
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
}

function diffRange() {
  const base = process.env.SEED_MIGRATION_GUARD_BASE || "origin/main";
  for (const range of [`${base}...HEAD`, "HEAD~1...HEAD"]) {
    const left = range.split("...")[0];
    if (gitMaybe(["rev-parse", "--verify", "--quiet", `${left}^{commit}`]).trim()) {
      return range;
    }
  }
  return null;
}

function changedFiles() {
  const files = new Set();
  const range = diffRange();
  if (range) {
    splitLines(gitMaybe(["diff", "--name-only", "--diff-filter=ACMR", range])).forEach((f) =>
      files.add(f)
    );
  }
  splitLines(gitMaybe(["diff", "--name-only", "--diff-filter=ACMR"])).forEach((f) =>
    files.add(f)
  );
  splitLines(gitMaybe(["diff", "--cached", "--name-only", "--diff-filter=ACMR"])).forEach((f) =>
    files.add(f)
  );
  return [...files].sort();
}

function readChangedFile(rel) {
  const abs = resolve(repo, rel);
  if (existsSync(abs)) return readFileSync(abs, "utf8");
  for (const ref of ["HEAD", "origin/main"]) {
    const content = gitMaybe(["show", `${ref}:${rel}`]);
    if (content) return content;
  }
  return "";
}

function stripBlockComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, (match) =>
    match
      .split("\n")
      .map(() => "")
      .join("\n")
  );
}

function stripInlineSqlComment(line) {
  const idx = line.indexOf("--");
  return idx >= 0 ? line.slice(0, idx) : line;
}

function lineOfIndex(source, index) {
  return source.slice(0, index).split("\n").length;
}

function identifier(name) {
  return name.replace(/"/g, "").toLowerCase();
}

function sensitivityFor(table) {
  const t = identifier(table);
  if (SEED_SENSITIVE_TABLES.has(t)) return SEED_SENSITIVE_TABLES.get(t);
  for (const { re, reason } of SEED_SENSITIVE_PATTERNS) {
    if (re.test(t)) return reason;
  }
  return "";
}

function isIgnored(source, index) {
  const lines = source.split("\n");
  const line = lineOfIndex(source, index);
  const window = [
    lines[line - 3] ?? "",
    lines[line - 2] ?? "",
    lines[line - 1] ?? "",
    lines[line] ?? "",
  ].join("\n");
  return IGNORE_RE.test(window);
}

export function affectedSeedObjectsForSQL(source) {
  const withoutBlocks = stripBlockComments(source);
  const lines = withoutBlocks.split("\n");
  const searchable = lines.map(stripInlineSqlComment).join("\n");
  const findings = [];
  SQL_OP_RE.lastIndex = 0;
  let match;
  while ((match = SQL_OP_RE.exec(searchable)) !== null) {
    if (isIgnored(withoutBlocks, match.index)) continue;
    const op = match[1].replace(/\s+/g, " ").toUpperCase();
    const table = identifier(match[2]);
    const reason = sensitivityFor(table);
    if (!reason) continue;
    findings.push({
      op,
      table,
      reason,
      line: lineOfIndex(searchable, match.index),
    });
  }
  return findings;
}

function hasSeedCompanion(files) {
  return files.filter((rel) => SEED_COMPANION_PATTERNS.some((re) => re.test(rel)));
}

function runSelfTest() {
  const cases = [
    {
      name: "seed table without companion fails",
      sql: "ALTER TABLE goats ADD COLUMN source_batch text;",
      companions: [],
      wantAffected: ["goats"],
      wantPass: false,
    },
    {
      name: "seed table with seed command companion passes",
      sql: "ALTER TABLE goats ADD COLUMN source_batch text;",
      companions: ["backend/cmd/seed-vaccination-real/main.go"],
      wantAffected: ["goats"],
      wantPass: true,
    },
    {
      name: "derived table with closeout registry companion passes",
      sql: "CREATE TABLE vaccination_new_projection_rows (tenant_id uuid);",
      companions: ["tools/dev/seed-closeout.sh"],
      wantAffected: ["vaccination_new_projection_rows"],
      wantPass: true,
    },
    {
      name: "projection table creation is seed-closeout sensitive",
      sql: "CREATE TABLE calendar_history_projection_rows (tenant_id uuid);",
      companions: [],
      wantAffected: ["calendar_history_projection_rows"],
      wantPass: false,
    },
    {
      name: "seed config data migration is sensitive",
      sql: "UPDATE sop_versions SET row_version = row_version + 1;",
      companions: [],
      wantAffected: ["sop_versions"],
      wantPass: false,
    },
    {
      name: "index-only migration is not seed-coupling sensitive",
      sql: "CREATE INDEX goats_tag_idx ON goats (tag1);",
      companions: [],
      wantAffected: [],
      wantPass: true,
    },
    {
      name: "unrelated table passes",
      sql: "CREATE TABLE audit_debug_notes (id uuid PRIMARY KEY);",
      companions: [],
      wantAffected: [],
      wantPass: true,
    },
    {
      name: "complete reviewed ignore passes",
      sql: "-- seed-migration-guard:ignore owner=ravi issue=GOAT-123 reason=no-seed-path expiry=2026-12-31\nALTER TABLE goats ADD COLUMN scratch text;",
      companions: [],
      wantAffected: [],
      wantPass: true,
    },
  ];

  for (const tc of cases) {
    const affected = affectedSeedObjectsForSQL(tc.sql);
    const tables = affected.map((f) => f.table).sort();
    const wantTables = tc.wantAffected.slice().sort();
    if (JSON.stringify(tables) !== JSON.stringify(wantTables)) {
      throw new Error(
        `self-test ${tc.name}: affected=${JSON.stringify(tables)}, want=${JSON.stringify(wantTables)}`
      );
    }
    const pass = affected.length === 0 || hasSeedCompanion(tc.companions).length > 0;
    if (pass !== tc.wantPass) {
      throw new Error(`self-test ${tc.name}: pass=${pass}, want=${tc.wantPass}`);
    }
  }
  console.log(`seed-migration-coupling: all ${cases.length} self-tests passed`);
}

function runScan() {
  const files = changedFiles();
  const migrations = files.filter((rel) => MIGRATION_RE.test(rel));
  if (migrations.length === 0) {
    console.log("seed-migration-coupling: ok (no migration SQL changed)");
    return;
  }

  const affected = [];
  for (const rel of migrations) {
    const source = readChangedFile(rel);
    for (const finding of affectedSeedObjectsForSQL(source)) {
      affected.push({ file: rel, ...finding });
    }
  }

  if (affected.length === 0) {
    console.log(
      `seed-migration-coupling: ok (${migrations.length} migration file(s); no seed-sensitive setup tables changed)`
    );
    return;
  }

  const companions = hasSeedCompanion(files);
  if (companions.length > 0) {
    console.log(
      `seed-migration-coupling: ok (${affected.length} seed-sensitive migration touch(es); seed companion updated: ${companions.join(", ")})`
    );
    return;
  }

  console.error("seed-migration-coupling: seed-sensitive migration changed without seed-path update");
  console.error("");
  console.error("Affected migration operations:");
  for (const f of affected) {
    console.error(`- ${f.file}:${f.line}: ${f.op} ${f.table} (${f.reason})`);
  }
  console.error("");
  console.error("Update at least one seed companion in the same patch:");
  console.error("- backend/cmd/seed-* or a seed/projection recompute command");
  console.error("- backend/tests/e2e* or a focused seed/projection regression test");
  console.error("- docs/runbooks/*seed* / *clean-slate* with the new seed closeout step");
  console.error("");
  console.error(
    "For a reviewed no-seed-impact migration only, add: seed-migration-guard:ignore owner=<name> issue=<id> reason=<text> expiry=<YYYY-MM-DD>"
  );
  process.exit(1);
}

if (process.argv.includes("--self-test")) {
  runSelfTest();
} else {
  runScan();
}
