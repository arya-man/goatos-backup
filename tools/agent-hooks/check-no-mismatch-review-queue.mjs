#!/usr/bin/env node

// check-no-mismatch-review-queue.mjs — enforces the Goat OS rule:
// "No runtime review/repair queue for a state that clean ingestion makes impossible."
//
// A data-integrity contradiction (stage='kid' but age>=182, species='goat' but
// vaccination='sheep', location_a but shed_b) is impossible if ingestion validates
// correctly. Building a runtime "review/reconcile/repair" screen to cope with such
// dirty data is a banned anti-pattern — it treats a seeding bug as a product feature.
//
// This guard scans for:
// - New table creation with names like <entity>_review_item, *_review, *_reconcile
//   (when tied to validation-at-ingestion contradictions).
// - New handler/endpoint for review/reconcile operations (POST /admin/reviews/{id}/resolve, etc.)
// - New sweeper/worker that marks rows for "review" based on a contradiction.
//
// Exception (must be COMPLETE — owner, issue, scope, and expiry all present):
//   no-mismatch-review-queue:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>
//
// Modes:
//   (default)     scan the diff vs origin/main and fail only on NEW violations.
//   --self-test   run the built-in adversarial fixtures (incl. review-queue creation patterns).

import { execSync } from "node:child_process";
import { readFileSync, existsSync } from "node:fs";
import { resolve, join } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Patterns that indicate a review/reconcile queue for ingestion-validation contradictions.
// These patterns match:
// - SQL CREATE TABLE with _review_item, _review, _reconcile in the name
// - Handler registration for /review, /reconcile endpoints
// - Sweeper/worker that marks rows for review based on a validation check
const MATCHERS = [
  // Pattern 1: CREATE TABLE <name>_review_item / *_review / *_reconcile
  {
    name: "review_table_creation",
    re: /CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\w+\.)?(\w*(?:_review_item|_review|_reconcile)\w*)/gim,
  },
  // Pattern 2: r.POST/PUT(..."/review...) or r.POST(..."/reconcile...)
  {
    name: "review_endpoint_registration",
    re: /r\.(POST|PUT|PATCH)\s*\(\s*["`'](?:\/)?[^`"']*(?:review|reconcile)[^`"']*["`']/gim,
  },
  // Pattern 3: Function/type name like RecordStageReviewItem, RecordReviewItem
  {
    name: "review_record_type",
    re: /(?:type|func|const)\s+\w*(?:Record|Create|Mark)\w*Review\w+(?:\s|\(|struct)/gim,
  },
];

const IGNORE_RE =
  /no-mismatch-review-queue:ignore:\s*owner=\S+\s+issue=\S+\s+scope=\S+\s+expiry=\d{4}-\d{2}-\d{2}/;

function lineOfIndex(source, index) {
  return source.slice(0, index).split("\n").length; // 1-indexed
}

function isExempt(source, matchIndex) {
  const lines = source.split("\n");
  const ln = lineOfIndex(source, matchIndex); // 1-indexed
  const window = [
    lines[ln - 2] ?? "",
    lines[ln - 1] ?? "",
    lines[ln] ?? "",
  ].join("\n");
  return IGNORE_RE.test(window);
}

export function findingsForSource(source) {
  const findings = [];
  for (const { name, re } of MATCHERS) {
    re.lastIndex = 0;
    let m;
    while ((m = re.exec(source)) !== null) {
      if (isExempt(source, m.index)) continue;
      const line = lineOfIndex(source, m.index);
      const snippet = source.slice(m.index, Math.min(m.index + 120, source.length));
      findings.push({ line, matcher: name, snippet });
    }
  }
  return findings;
}

function getDiffStat() {
  try {
    return execSync("git diff --name-only origin/main...HEAD 2>/dev/null || true", {
      encoding: "utf-8",
    })
      .split("\n")
      .filter(Boolean);
  } catch {
    return [];
  }
}

function getDiffContent(file) {
  try {
    return execSync(`git diff origin/main...HEAD -- "${file}" 2>/dev/null || true`, {
      encoding: "utf-8",
    });
  } catch {
    return "";
  }
}

function selfTest() {
  const fixtures = {
    review_table_creation: `
-- Migration: add stage review queue (ANTI-PATTERN)
CREATE TABLE stage_review_item (
  id uuid PRIMARY KEY,
  stage_expected varchar,
  stage_actual varchar
);
`,
    review_endpoint_registration: `
// HTTP handler registration
router.POST("/api/v1/admin/reviews/:review_id/resolve", handleResolveStageReview)
r.PUT("/reviews/{id}", updateReviewRecord)
`,
    review_record_type: `
// Struct definition (ANTI-PATTERN)
type RecordStageReviewItem struct {
  GoatID uuid.UUID
  StageMismatch bool
}

func MarkAnimalForReview(ctx context.Context, animalID uuid.UUID) error {
  // ...
}
`,
  };

  const allFindings = [];
  for (const [name, source] of Object.entries(fixtures)) {
    const findings = findingsForSource(source);
    if (findings.length === 0) {
      console.error(`✗ FAIL: Fixture '${name}' did not match expected patterns`);
      process.exit(1);
    }
    allFindings.push(...findings);
  }

  console.log("✓ Self-test passed. Found expected anti-patterns in fixtures:");
  for (const finding of allFindings) {
    console.log(`  Line ${finding.line}: ${finding.matcher} — ${finding.snippet.slice(0, 50)}...`);
  }
}

function main() {
  const args = process.argv.slice(2);

  if (args.includes("--self-test")) {
    selfTest();
    return;
  }

  const diffFiles = getDiffStat();
  if (diffFiles.length === 0) {
    console.log("ℹ No changes detected. Skipping check.");
    return;
  }

  const allFindings = [];

  for (const file of diffFiles) {
    // Only check Go, SQL, and TypeScript files
    if (!/\.(go|sql|ts|tsx)$/.test(file)) continue;

    if (!existsSync(join(repo, file))) continue;

    const diff = getDiffContent(file);
    // Scan only ADDED lines (new code). Removing the banned pattern (a purge/cleanup) must
    // PASS the guard, so strip context and '-' removed lines and scan only '+' additions
    // (excluding the '+++' file header). Otherwise deleting a stage-review table/test would
    // false-flag the very cleanup that satisfies this rule.
    const addedSource = diff
      .split("\n")
      .filter((l) => l.startsWith("+") && !l.startsWith("+++"))
      .map((l) => l.slice(1))
      .join("\n");
    const findings = findingsForSource(addedSource);

    if (findings.length > 0) {
      allFindings.push({ file, findings });
    }
  }

  if (allFindings.length === 0) {
    console.log("✓ No review/reconcile queues detected for impossible-state contradictions.");
    return;
  }

  console.error(
    "✗ FAIL: Ingestion-validation review/reconcile queue anti-pattern detected.\n"
  );
  console.error(
    "  Data-integrity contradictions (e.g., stage vs age) must be rejected at\n"
  );
  console.error(
    "  the INGESTION boundary, not persisted and then 'repaired' by a runtime queue.\n"
  );
  console.error(
    "  See: docs/decisions/ingestion-validation-not-runtime-review.md\n"
  );

  for (const { file, findings } of allFindings) {
    console.error(`\n${file}:`);
    for (const { line, matcher, snippet } of findings) {
      console.error(`  Line ${line} (${matcher}): ${snippet.slice(0, 60)}...`);
    }
  }

  console.error("\n  If this is a legitimate operator-facing reconciliation service");
  console.error("  (not a queue created to handle dirty ingested data), add a");
  console.error("  complete ignore directive:\n");
  console.error("    // no-mismatch-review-queue:ignore: owner=NAME issue=URL scope=REASON expiry=YYYY-MM-DD");

  process.exit(1);
}

main();
