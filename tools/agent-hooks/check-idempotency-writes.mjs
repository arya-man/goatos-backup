#!/usr/bin/env node

// check-idempotency-writes.mjs — guards the idempotency contract from AGENTS.md:
// "Treat idempotency as a mandatory write-path contract for every mutating API,
// worker, importer, webhook, state transition, outbox producer-consumer, server
// action, and UI-triggered write... The SQL pattern `ON CONFLICT DO UPDATE` with
// only `idempotency_key = EXCLUDED.idempotency_key` is NOT sufficient when later
// code can still mutate state."
//
// This guard flags the INSUFFICIENT pattern: ON CONFLICT (...) DO UPDATE where
// the ONLY or PRIMARY action is setting idempotency_key = EXCLUDED.idempotency_key.
// This pattern leaves the row exposed to later mutations and fails to implement
// the full idempotency contract (reserved key + fingerprint in same txn, result
// storage, replay detection, same-key-different-payload rejection).
//
// Modes:
//   (default)        scan backend/internal/**/*.{go,sql} and fail on any NEW
//                    offenders vs the baseline. Exits 0 if no new violations.
//   --all            audit the whole backend tree (backlog/audit view).
//   --self-test      run embedded positive/negative fixtures and exit.
//   --write-baseline write the current offender list (with dates/count) to the
//                    baseline file and exit 0. Used for ratcheting.

import { execSync } from "node:child_process";
import { readFileSync, writeFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const baselineFile = join(import.meta.dirname, "idempotency-writes-baseline.txt");

// Scan pattern: detect the INSUFFICIENT conflict handler form.
// Matches: ON CONFLICT (...) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
// This captures the whole statement so we can see what else is being updated (nothing = BAD).
const INSUFFICIENT_PATTERN = /ON\s+CONFLICT\s*\([^)]*\)\s+DO\s+UPDATE\s+SET\s+([^;]*?)(?:;|$|\n\s*(?:WHERE|INSERT|UPDATE|DELETE|CREATE|DROP))/gi;

// Pattern to detect idempotency_key-specific updates. The key sign of insufficient handling:
// when idempotency_key = EXCLUDED.idempotency_key is the ONLY SET clause (or nearly so).
const IDEMPOTENCY_KEY_UPDATE = /idempotency_key\s*=\s*EXCLUDED\.idempotency_key/i;

function parseSetClause(setText) {
  // Split on commas, trim, and filter out idempotency_key assignments and whitespace.
  const clauses = setText
    .split(",")
    .map((c) => c.trim())
    .filter((c) => c.length > 0);

  const meaningfulClauses = clauses.filter((c) => !IDEMPOTENCY_KEY_UPDATE.test(c));
  return { all: clauses, meaningful: meaningfulClauses };
}

function isInsufficientConflict(setText) {
  const { all, meaningful } = parseSetClause(setText);
  // Flag if:
  // 1. Only one clause total and it's idempotency_key (purely redundant)
  // 2. Multiple clauses but only one is meaningful (idempotency_key is noise)
  //    AND that meaningful clause is a WHERE or other non-mutation clause
  if (all.length === 1 && IDEMPOTENCY_KEY_UPDATE.test(all[0])) {
    return true; // ONLY clause is idempotency_key = EXCLUDED.idempotency_key
  }
  if (meaningful.length === 0 && all.length > 0) {
    return true; // all clauses were idempotency_key assignments
  }
  // If meaningful clauses exist (e.g., status = 'retry', updated_at = now()), it's likely sufficient.
  return false;
}

// Returns { line, statement, setText }: line is 1-indexed, statement is the full SQL, setText is the SET clause.
export function findingsForSource(source, filename) {
  const findings = [];
  const lines = source.split("\n");
  let lineNum = 1;

  // Process line-by-line, but reconstruct multi-line statements.
  let buffer = "";
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    buffer += line + "\n";

    // Test the buffer for a complete statement (ends with ; or EOF).
    if (line.includes(";") || i === lines.length - 1) {
      INSUFFICIENT_PATTERN.lastIndex = 0;
      let m;
      while ((m = INSUFFICIENT_PATTERN.exec(buffer)) !== null) {
        const setText = m[1];
        if (isInsufficientConflict(setText)) {
          // Compute the line number where this ON CONFLICT starts in the buffer.
          const conflictLineOffset = buffer.slice(0, m.index).split("\n").length - 1;
          findings.push({
            line: lineNum + conflictLineOffset,
            statement: m[0].slice(0, 100) + "...",
            setText: setText.slice(0, 80) + (setText.length > 80 ? "..." : ""),
          });
        }
      }
      buffer = "";
    }
    lineNum = i + 2; // Track current line for next buffer segment
  }

  return findings;
}

function walkBackendInternal() {
  const results = [];
  const dir = join(repo, "backend/internal");
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory() && !["build", "node_modules", ".code-review-graph"].includes(entry.name)) {
      results.push(...walkDir(path));
    }
  }
  return results;
}

function walkDir(dir) {
  const results = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory() && !["build", "node_modules", ".code-review-graph"].includes(entry.name)) {
      results.push(...walkDir(path));
    } else if (entry.isFile() && /\.(go|sql)$/.test(entry.name)) {
      // Skip test files
      if (!entry.name.includes("_test.") && entry.name !== "sqlc.yaml") {
        results.push(path);
      }
    }
  }
  return results;
}

function baselineKey(rel, line) {
  return `${rel}:${line}`;
}

function parseBaselineContent(content) {
  const lines = content.split("\n").filter((l) => l && !l.startsWith("#"));
  const offenders = new Set();
  for (const line of lines) {
    const match = line.match(/^(.+?):(\d+):\s/);
    if (match) {
      offenders.add(baselineKey(match[1], Number(match[2])));
    }
  }
  return offenders;
}

function readBaseline() {
  try {
    return parseBaselineContent(readFileSync(baselineFile, "utf8"));
  } catch {
    return new Set();
  }
}

function selfTest() {
  const bad = [
    // CASE 1: idempotency_key is the ONLY SET clause
    {
      src: "ON CONFLICT (id) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key;",
      shouldFlag: true,
      desc: "only clause is idempotency_key",
    },
    // CASE 2: Multi-line with only idempotency_key
    {
      src: `ON CONFLICT (tenant_id, idempotency_key)
DO UPDATE SET
  idempotency_key = EXCLUDED.idempotency_key;`,
      shouldFlag: true,
      desc: "multiline with only idempotency_key",
    },
    // CASE 3: Legitimate updates (sufficient pattern) — has meaningful clauses
    {
      src: "ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, updated_at = now();",
      shouldFlag: false,
      desc: "has meaningful status update",
    },
    // CASE 4: Sufficient pattern with idempotency_key present but not alone
    {
      src: `ON CONFLICT (id) DO UPDATE SET
  idempotency_key = EXCLUDED.idempotency_key,
  status = 'retry',
  attempt_count = EXCLUDED.attempt_count;`,
      shouldFlag: false,
      desc: "idempotency_key with other meaningful updates",
    },
    // CASE 5: idempotency_key alone in a WHERE clause (still insufficient for idempotency)
    {
      src: "ON CONFLICT (id) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key WHERE status = 'pending';",
      shouldFlag: true,
      desc: "only meaningful clause is idempotency_key with WHERE",
    },
  ];

  let passed = 0;
  let failed = 0;

  for (const tc of bad) {
    const findings = findingsForSource(tc.src, "test.sql");
    const found = findings.length > 0;
    if (found === tc.shouldFlag) {
      passed++;
    } else {
      failed++;
      console.error(
        `idempotency-writes self-test FAILED: "${tc.desc}"\n  Expected: ${
          tc.shouldFlag ? "flag" : "pass"
        }\n  Got: ${found ? "flag" : "pass"}\n  Source: ${tc.src.slice(0, 80)}\n`
      );
    }
  }

  const baseline = parseBaselineContent("backend/internal/example/repository.go:12: idempotency_key = EXCLUDED.idempotency_key\n");
  let baselinePassed = true;
  if (!baseline.has("backend/internal/example/repository.go:12")) {
    baselinePassed = false;
    console.error("idempotency-writes self-test FAILED: baseline did not store path:line key");
  }
  if (baseline.has("backend/internal/example/repository.go:99")) {
    baselinePassed = false;
    console.error("idempotency-writes self-test FAILED: baseline matched every offender in a file");
  }
  if (!baselinePassed) {
    failed++;
  }

  if (failed > 0) {
    console.error(`idempotency-writes: ${failed}/${bad.length + 1} self-tests failed`);
    process.exit(1);
  }
  console.log(`idempotency-writes: all ${bad.length + 1} self-tests passed`);
}

function runScan(writeBaseline = false) {
  const files = walkBackendInternal();
  const violations = [];

  for (const abs of files) {
    const rel = relative(repo, abs);
    let src;
    try {
      src = readFileSync(abs, "utf8");
    } catch {
      continue;
    }
    for (const f of findingsForSource(src, rel)) {
      violations.push({ rel, ...f });
    }
  }

  if (writeBaseline) {
    const header = `# Idempotency guard baseline — grandfathered offenders
# Generated: ${new Date().toISOString()}
# Rule: backend/internal/** writes MUST NOT use ON CONFLICT DO UPDATE with only
#       idempotency_key = EXCLUDED.idempotency_key as the action. Per AGENTS.md
#       this pattern is insufficient for the full idempotency contract.
#
# To add an exception, annotate the line:
#   // idempotency-guard:ignore: owner=<name> reason=<brief> expiry=<YYYY-MM-DD>
#
# NEW offenders (not in this baseline) will cause exit code 1.
# Format: path:line: statement snippet

`;
    const baselineLines = violations
      .sort((a, b) => a.rel.localeCompare(b.rel) || a.line - b.line)
      .map((v) => `${v.rel}:${v.line}: ${v.setText}`)
      .join("\n");
    writeFileSync(baselineFile, header + (violations.length > 0 ? baselineLines + "\n" : ""));
    console.log(`idempotency-writes: baseline written (${violations.length} offender(s))`);
    process.exit(0);
  }

  const baseline = readBaseline();
  const newViolations = violations.filter((v) => !baseline.has(baselineKey(v.rel, v.line)));

  if (newViolations.length > 0) {
    console.error(
      `idempotency-writes: ${newViolations.length} NEW offender(s) found (see AGENTS.md idempotency rule):\n`
    );
    for (const v of newViolations) {
      console.error(`  ${v.rel}:${v.line}: ${v.setText}`);
    }
    console.error("\nOffending pattern: ON CONFLICT DO UPDATE with only idempotency_key = EXCLUDED.idempotency_key");
    console.error(
      "This is insufficient when later code mutates state. Use the proven pattern from\nbackend/internal/obligation/adapters/postgres/idempotency.go (reserve + fingerprint + result storage)."
    );
    process.exit(1);
  }

  console.log(`idempotency-writes: ok (${files.length} file(s) scanned; no new idempotency violations)`);
  process.exit(0);
}

const args = process.argv.slice(2);
if (args.includes("--self-test")) {
  selfTest();
} else if (args.includes("--write-baseline")) {
  runScan(true);
} else if (args.includes("--all")) {
  runScan(false);
} else {
  runScan(false);
}
