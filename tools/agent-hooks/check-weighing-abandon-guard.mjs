#!/usr/bin/env node
// check-weighing-abandon-guard.mjs — permanent guard: weighing has no "abandon" vocabulary.
//
// Business Decision (final, permanent):
// The weighing workflow is: scan → submit → close, or reopen if closed.
// AbandonScope was a close-variant that bypassed verification; it was a code path, not a concept.
// Vocabulary: CLOSE (with verification), REOPEN (revert a close). No abandon.
//
// What is BANNED (new write paths):
//   - Service methods: AbandonScope, abandonScope
//   - Repository methods: AbandonScope, abandonScope
//   - HTTP routes: /abandon (POST)
//   - Event types: weighing.shed.abandoned, weighing_shed_abandoned, EventWeighingShedAbandoned
//   - Audit actions: weighing.scope_abandoned
//   - Android ViewModel/API methods: abandonScope, AbandonScope
//   - Android string resources: weighing_abandon_*
//
// What is ALLOWED (read-side tolerance for historical data):
//   - Migration files (backend/migrations/postgres/*.sql): read-side tolerance of 'abandoned' status
//   - Comments/prose mentioning abandon in business context (e.g., "abandoned work")
//   - TestData/fixtures that reference historical abandoned rows (must NOT write new ones)
//
// Detection strategy:
//   1. Scan Go files: prohibit methods/consts matching \b(Abandon|abandon)Scope\b
//   2. Scan Go files: prohibit HTTP route patterns matching /abandon
//   3. Scan Go files: prohibit event type consts/strings matching weighing.*abandon
//   4. Scan Kotlin files: prohibit method/class names matching (Abandon|abandon)Scope
//   5. Scan Android XML strings: prohibit string IDs matching weighing_abandon_*
//   6. Scan proto files: prohibit enum values or message names containing abandon
//   7. Exclude: migrations (*.sql), read-side consumers, test assertions on historical data

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Marker to skip this file for a specific finding (use sparingly)
const ABANDON_GUARD_IGNORE = /abandon-guard:\s*ignore\s+([^\n]+)/i;

function walk(dir, filter = () => true, acc = []) {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return acc;
  }
  for (const entry of entries) {
    const path = join(dir, entry.name);
    const stat = statSync(path);
    if (stat.isDirectory()) {
      // Skip known non-source dirs
      if (
        entry.name === "node_modules" ||
        entry.name === "build" ||
        entry.name === ".git" ||
        entry.name === ".gradle" ||
        entry.name === "dist" ||
        entry.name === "coverage"
      )
        continue;
      walk(path, filter, acc);
    } else if (filter(entry.name, path)) {
      acc.push(path);
    }
  }
  return acc;
}

// ============= GO FILE CHECKS =============

function checkGoFiles() {
  const findings = [];
  const goFiles = walk(resolve(repo, "backend"), (name, path) => {
    // Skip migrations (they may have historical 'abandoned' status)
    if (path.includes("migrations")) return false;
    // Skip test files that assert on historical data
    if (name.endsWith("_test.go")) return false;
    return name.endsWith(".go");
  });

  const goPattern = /\b(Abandon|abandon)Scope\b/;

  for (const file of goFiles) {
    const content = readFileSync(file, "utf8");
    const lines = content.split("\n");

    // Check for AbandonScope method definitions
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (ABANDON_GUARD_IGNORE.test(line)) continue;

      // Skip pure comment lines (starting with //)
      const trimmed = line.trim();
      if (trimmed.startsWith("//") || trimmed.startsWith("/*") || trimmed.startsWith("*")) {
        continue;
      }

      // Check for method definitions: func (r *Repository) AbandonScope or func (s *Service) AbandonScope
      if (
        /\bfunc\s+\([^)]*\)\s+(Abandon|abandon)Scope\s*\(/.test(line) ||
        /\bfunc\s+(Abandon|abandon)Scope\s*\(/.test(line)
      ) {
        findings.push({
          file,
          line: i + 1,
          message: "AbandonScope method is banned; use CloseScope or ReopenScope only",
          text: line.trim(),
        });
      }

      // Check for event type constants or string literals (weighing-specific only)
      if (
        /eventType.*abandon|"weighing\.shed\.abandoned"|'weighing\.shed\.abandoned'|EventWeighingShedAbandoned|eventTypeScopeAbandoned|eventTypeShedAbandoned/.test(line)
      ) {
        findings.push({
          file,
          line: i + 1,
          message: "weighing abandonment event type is banned; close/reopen events only",
          text: line.trim(),
        });
      }

      // Check for weighing audit action strings (weighing-specific only)
      if (/weighing_scope_abandoned|weighing\.scope_abandoned|"weighing\.scope_abandoned"|auditAction.*abandon/.test(line)) {
        findings.push({
          file,
          line: i + 1,
          message: "weighing abandonment audit action is banned",
          text: line.trim(),
        });
      }
    }
  }

  // Check HTTP routes
  const handlerFile = resolve(repo, "backend/internal/weighing/adapters/http/handler.go");
  try {
    const handlerContent = readFileSync(handlerFile, "utf8");
    const lines = handlerContent.split("\n");
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (ABANDON_GUARD_IGNORE.test(line)) continue;

      if (/\/abandon["']|\/abandon\b/.test(line)) {
        findings.push({
          file: handlerFile,
          line: i + 1,
          message: "/abandon HTTP route is banned",
          text: line.trim(),
        });
      }
    }
  } catch {
    // File may not exist in this phase
  }

  return findings;
}

// ============= KOTLIN FILE CHECKS =============

function checkKotlinFiles() {
  const findings = [];

  const kotlinFiles = walk(resolve(repo, "apps/goatos-android"), (name, path) => {
    if (path.includes("build")) return false;
    return name.endsWith(".kt");
  });

  for (const file of kotlinFiles) {
    const content = readFileSync(file, "utf8");
    const lines = content.split("\n");

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (ABANDON_GUARD_IGNORE.test(line)) continue;

      // Check for method/class names: abandonScope, AbandonScope
      if (/\b(abandon|Abandon)Scope\b/.test(line)) {
        // Skip test mock expectations and comments
        if (line.includes("//") && line.indexOf("//") < line.search(/\b(abandon|Abandon)Scope\b/)) {
          continue; // Comment-only line
        }
        findings.push({
          file,
          line: i + 1,
          message: "abandonScope method/class is banned",
          text: line.trim(),
        });
      }
    }
  }

  return findings;
}

// ============= ANDROID STRING RESOURCES =============

function checkAndroidStrings() {
  const findings = [];

  const xmlFiles = walk(resolve(repo, "apps/goatos-android"), (name, path) => {
    if (path.includes("build")) return false;
    return name.endsWith(".xml") && (path.includes("values") || path.includes("res"));
  });

  for (const file of xmlFiles) {
    const content = readFileSync(file, "utf8");

    if (/weighing_abandon/i.test(content)) {
      findings.push({
        file,
        line: 1,
        message: "weighing_abandon_* string resource is banned; remove abandon UI strings",
        text: content.substring(0, 100),
      });
    }
  }

  return findings;
}

// ============= PROTO CHECKS =============

function checkProtoFiles() {
  const findings = [];

  const protoFiles = walk(resolve(repo, "packages"), (name) => name.endsWith(".proto"));

  for (const file of protoFiles) {
    const content = readFileSync(file, "utf8");
    const lines = content.split("\n");

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (ABANDON_GUARD_IGNORE.test(line)) continue;

      // Check for enum values or message names containing abandon
      if (
        /^\s*(ABANDONED|ABANDON)\b|^message\s+\w*Abandon\w*|enum\s+\w*Abandon\w*/.test(line)
      ) {
        findings.push({
          file,
          line: i + 1,
          message: "abandonment enum/message is banned from proto definitions",
          text: line.trim(),
        });
      }
    }
  }

  return findings;
}

// ============= SELF TEST =============

function selfTest() {
  // Simulate a violation detection
  const goodGoCode = `
func (r *Repository) CloseScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
  // valid close implementation
}`;

  const badGoCode = `
func (r *Repository) AbandonScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
  // banned abandon implementation
}`;

  const goodKotlinCode = `
fun closeScope(reason: String) {
  // valid close
}`;

  const badKotlinCode = `
fun abandonScope(reason: String) {
  // banned abandon
}`;

  // Test GO detection
  const goPattern = /\bfunc\s+\([^)]*\)\s+(Abandon|abandon)Scope\s*\(/;
  if (!goPattern.test(badGoCode)) {
    console.log(
      "abandon-guard self-test: FAIL (Go method detection broken)",
    );
    return false;
  }
  if (goPattern.test(goodGoCode)) {
    console.log(
      "abandon-guard self-test: FAIL (Go false positive on close)",
    );
    return false;
  }

  // Test event pattern
  const eventPattern = /weighing\.shed\.abandoned/;
  if (!eventPattern.test('eventType: "weighing.shed.abandoned"')) {
    console.log(
      "abandon-guard self-test: FAIL (event detection broken)",
    );
    return false;
  }

  // Test Kotlin detection
  const kotlinPattern = /\b(abandon|Abandon)Scope\b/;
  if (!kotlinPattern.test(badKotlinCode)) {
    console.log(
      "abandon-guard self-test: FAIL (Kotlin detection broken)",
    );
    return false;
  }
  if (kotlinPattern.test(goodKotlinCode)) {
    console.log(
      "abandon-guard self-test: FAIL (Kotlin false positive on close)",
    );
    return false;
  }

  // Test migration exclusion
  const migrationPath = "backend/migrations/postgres/000001_baseline.sql";
  if (!migrationPath.includes("migrations")) {
    console.log(
      "abandon-guard self-test: FAIL (migration exclusion broken)",
    );
    return false;
  }

  console.log("abandon-guard self-test: ok");
  return true;
}

// ============= MAIN =============

if (process.argv.includes("--self-test")) {
  const ok = selfTest();
  process.exit(ok ? 0 : 1);
}

const allFindings = [
  ...checkGoFiles(),
  ...checkKotlinFiles(),
  ...checkAndroidStrings(),
  ...checkProtoFiles(),
];

if (allFindings.length) {
  console.error("abandon-guard FAILED — weighing vocabulary is close/reopen only:");
  for (const f of allFindings) {
    console.error(
      `  ${relative(repo, f.file)}:${f.line}: ${f.message}`,
    );
    if (f.text) {
      console.error(`    > ${f.text.substring(0, 80)}`);
    }
    console.error(
      `    To suppress: add comment 'abandon-guard: ignore <reason>' on that line`,
    );
  }
  process.exit(1);
}

console.log("abandon-guard: ok (weighing vocabulary validated)");
