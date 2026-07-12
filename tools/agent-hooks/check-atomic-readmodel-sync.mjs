#!/usr/bin/env node

// check-atomic-readmodel-sync.mjs — enforces atomic state transitions with derived read-model sync.
//
// Rule: A state transition and the sync of any derived read model it OWNS must
// be ONE atomic transaction. A record must never reach published/committed state
// while its owned read-model upsert failed. Every such flow needs a rollback
// regression test.
//
// Canonical cases:
//   - PublishVersionWithCapacity / PublishVersionWithDerivedRules:
//     publish a vaccination protocol version atomically with capacity config
//     upsert; a parity mismatch (ports.ErrCapacityParityMismatch) rolls back
//     the publish. Tests: TestPublishVersionWithCapacityRollsBackOnSyncFailure,
//     TestPublishVersionWithDerivedRulesRollsBackOnCapacityFailure.
//
// This guard scans backend/internal/**/*.go for state-transition methods
// (Publish*/Finalize*) that upsert a derived read model, and asserts a
// corresponding rollback regression test exists (a *_test.go with
// RollsBack/ParityMismatch/SyncFailure referencing that method).
//
// Heuristic (low false positives):
//   - Method pattern: func (r *Repository) Publish\w+(...) / Finalize\w+(...) error
//   - Read-model upsert signals: INSERT, UPDATE, upsert, sync keywords
//   - Test pattern: Test.*RollsBack|Test.*SyncFailure|Test.*ParityMismatch
//   - Link: test must reference the method name
//
// Modes:
//   (default)     scan backend/ and diff vs baseline, exit 1 only on NEW offenders.
//   --self-test   run built-in fixtures and exit.
//
// Exception (must be COMPLETE):
//   atomic-readmodel-sync:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>

import { execSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Patterns to identify state-transition methods.
const STATE_TRANSITION_METHOD_PATTERN = /^\s*func\s*\(\w+\s+\*\w+\)\s+(Publish\w+|Finalize\w+)\s*\(/;

// Patterns to identify derived read-model upsert operations.
const UPSERT_PATTERNS = [
  /\bINSERT\b/i,
  /\bUPDATE\b/i,
  /\bupsert\b/i,
  /\bSyncVaccinationCapacityConfig\b/,
  /\bSyncVaccinationEligibilityRollups\b/,
  /\bUpsert.*Config\b/i,
  /\bInsert.*Derived\b/i,
];

// Patterns to identify rollback regression tests.
const ROLLBACK_TEST_PATTERN = /Test.*(?:RollsBack|SyncFailure|ParityMismatch)/;

const IGNORE_RE = /atomic-readmodel-sync:ignore:\s*owner=\S+\s+issue=\S+\s+scope=\S+\s+expiry=\d{4}-\d{2}-\d{2}/;

// Scan a Go file and extract state-transition method names.
export function findStateTransitionMethods(source, filename) {
  const findings = [];
  const lines = source.split("\n");

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (!STATE_TRANSITION_METHOD_PATTERN.test(line)) continue;

    const methodMatch = line.match(/(Publish\w+|Finalize\w+)\s*\(/);
    if (!methodMatch) continue;

    const methodName = methodMatch[1];
    const lineNum = i + 1;

    // Extract the method body (simple heuristic: until the next `func` at start of line or EOF).
    let bodyStart = i + 1;
    let bodyEnd = lines.length;
    for (let j = i + 1; j < lines.length; j++) {
      if (/^func\s+\(/.test(lines[j])) {
        bodyEnd = j;
        break;
      }
    }
    const body = lines.slice(bodyStart, Math.min(bodyEnd, bodyStart + 200)).join("\n");

    // Check if the body contains upsert/sync operations.
    const hasUpsert = UPSERT_PATTERNS.some((p) => p.test(body));
    if (!hasUpsert) continue;

    // Check for ignore directive on the method signature or the line before.
    const ignoreLine = lines[i];
    const ignorePrevLine = i > 0 ? lines[i - 1] : "";
    const isIgnored = IGNORE_RE.test(ignoreLine) || IGNORE_RE.test(ignorePrevLine);

    findings.push({ methodName, lineNum, hasUpsert, isIgnored, filename });
  }

  return findings;
}

// Scan all *_test.go files and extract rollback test names + the methods they reference.
export function findRollbackTests(source, filename) {
  const tests = [];
  const lines = source.split("\n");

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (!ROLLBACK_TEST_PATTERN.test(line)) continue;

    const testMatch = line.match(/func Test(\w+)/);
    if (!testMatch) continue;

    const testName = testMatch[1];

    // Extract the test body to find which method(s) it references.
    let bodyStart = i + 1;
    let bodyEnd = lines.length;
    for (let j = i + 1; j < lines.length; j++) {
      if (/^func\s+Test/.test(lines[j])) {
        bodyEnd = j;
        break;
      }
    }
    const body = lines.slice(bodyStart, Math.min(bodyEnd, bodyStart + 150)).join("\n");

    // Extract referenced method names (e.g., PublishVersionWithCapacity, PublishVersionWithDerivedRules).
    const refMatches = body.match(/(Publish\w+|Finalize\w+)/g);
    const methodRefs = refMatches ? [...new Set(refMatches)] : [];

    tests.push({ testName, methodRefs, filename });
  }

  return tests;
}

// Load the baseline file: NEW offenders only.
function loadBaseline() {
  const baselineFile = resolve(repo, "tools/agent-hooks/atomic-readmodel-sync-baseline.txt");
  try {
    const content = readFileSync(baselineFile, "utf8");
    const lines = content.split("\n").filter((l) => l.trim() && !l.startsWith("#"));
    // Parse lines as "backend/internal/<module>/<type>:<method>".
    return new Set(lines.map((l) => l.trim()));
  } catch {
    return new Set();
  }
}

function scanBackendDirectory() {
  const backendDir = resolve(repo, "backend/internal");
  const goFiles = [];
  const testFiles = [];

  function walk(dir) {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.isDirectory() && entry.name !== "node_modules" && entry.name !== ".git") {
        walk(join(dir, entry.name));
      } else if (entry.isFile() && entry.name.endsWith(".go")) {
        const path = join(dir, entry.name);
        const rel = relative(repo, path);
        if (entry.name.endsWith("_test.go")) {
          testFiles.push({ path, rel });
        } else {
          goFiles.push({ path, rel });
        }
      }
    }
  }

  walk(backendDir);
  return { goFiles, testFiles };
}

export function runScan() {
  const { goFiles, testFiles } = scanBackendDirectory();
  const baseline = loadBaseline();

  // Collect all state-transition methods.
  const methods = [];
  for (const { path, rel } of goFiles) {
    let src;
    try {
      src = readFileSync(path, "utf8");
    } catch {
      continue;
    }
    const findings = findStateTransitionMethods(src, rel);
    methods.push(...findings);
  }

  // Collect all rollback tests.
  const testsByMethod = new Map();
  for (const { path } of testFiles) {
    let src;
    try {
      src = readFileSync(path, "utf8");
    } catch {
      continue;
    }
    const tests = findRollbackTests(src, relative(repo, path));
    for (const test of tests) {
      for (const methodRef of test.methodRefs) {
        if (!testsByMethod.has(methodRef)) {
          testsByMethod.set(methodRef, []);
        }
        testsByMethod.get(methodRef).push(test.testName);
      }
    }
  }

  // Check each method: must have a rollback test.
  const violations = [];
  for (const method of methods) {
    if (method.isIgnored) continue;

    const baselineKey = `${method.filename}:${method.methodName}`;
    if (baseline.has(baselineKey)) continue; // grandfathered

    const tests = testsByMethod.get(method.methodName) || [];
    if (tests.length === 0) {
      violations.push({
        file: method.filename,
        line: method.lineNum,
        method: method.methodName,
        tests: tests,
      });
    }
  }

  if (violations.length > 0) {
    console.error(
      "atomic-readmodel-sync: state transitions without rollback regression tests found:\n",
    );
    for (const v of violations) {
      console.error(`  ${v.file}:${v.line}: ${v.method} — no rollback/parity/sync-failure test found`);
    }
    console.error(
      "\nSee AGENTS.md: 'A state transition and the sync of any derived read model it OWNS must be ONE atomic transaction.'\n",
    );
    console.error(
      "Canonical: PublishVersionWithCapacity / PublishVersionWithDerivedRules parity-checks rule_dsl.capacity in-txn and ErrCapacityParityMismatch rolls back.\n",
    );
    console.error(
      "Expected test pattern: Test*RollsBack | Test*SyncFailure | Test*ParityMismatch, must reference the method.\n",
    );
    console.error(
      "Exception (complete): atomic-readmodel-sync:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>\n",
    );
    process.exit(1);
  }

  console.log(`atomic-readmodel-sync: ok (${methods.length} state-transition method(s) scanned)`);
}

export function runSelfTest() {
  const cases = [
    {
      name: "publish_with_capacity_sync",
      pass: true, // should pass: has a matching RollsBack test
      methodFile: `
package postgres

// PublishVersionWithCapacity publishes atomically with capacity upsert.
func (r *Repository) PublishVersionWithCapacity(ctx context.Context, tenantID, versionID string, publishedBy *string, capacity domain.PublishedCapacity, idempotencyKey ...string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// publish logic
	if err := upsertVaccinationCapacityConfigTx(ctx, tx, tenantID, capacity); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
`,
      testFile: `
package postgres

// TestPublishVersionWithCapacityRollsBackOnSyncFailure proves atomicity.
func TestPublishVersionWithCapacityRollsBackOnSyncFailure(t *testing.T) {
	// ... test that calls PublishVersionWithCapacity
	repo.PublishVersionWithCapacity(ctx, tenantID, versionID, nil, capacity)
}
`,
    },
    {
      name: "finalize_without_test",
      pass: false, // should fail: no rollback test
      methodFile: `
package postgres

// FinalizeUploadWithReadModel finalizes an upload and syncs metadata.
func (r *Repository) FinalizeUploadWithReadModel(ctx context.Context, id string) error {
	// UPDATE queries
	return nil
}
`,
      testFile: `
package postgres

// This test does not have RollsBack/SyncFailure/ParityMismatch in its name
func TestFinalizeUpload(t *testing.T) {
	// ... does not call FinalizeUploadWithReadModel
}
`,
    },
    {
      name: "publish_ignored",
      pass: true, // should pass: has ignore directive
      methodFile: `
package postgres

// atomic-readmodel-sync:ignore: owner=ravi issue=GH-999 scope=legacy-compat expiry=2026-12-31
func (r *Repository) PublishLegacyFormat(ctx context.Context, id string) error {
	// UPDATE queries (but ignored)
	return nil
}
`,
      testFile: ``,
    },
    {
      name: "publish_no_upsert",
      pass: true, // should pass: no upsert/sync in body
      methodFile: `
package postgres

// PublishMetadata publishes metadata (no derived sync).
func (r *Repository) PublishMetadata(ctx context.Context, id string) error {
	// SELECT and other read-only logic
	return nil
}
`,
      testFile: ``,
    },
  ];

  let failures = 0;

  for (const tc of cases) {
    // Simulate a method file.
    const methodFindings = findStateTransitionMethods(tc.methodFile, "backend/internal/test/repo.go");
    const hasMethod = methodFindings.length > 0 && !methodFindings[0].isIgnored;

    // Simulate a test file.
    const testFindings = tc.testFile ? findRollbackTests(tc.testFile, "backend/internal/test/repo_test.go") : [];
    const hasTest = testFindings.length > 0;

    const shouldPass =
      !hasMethod || // no state-transition method found
      hasTest || // test found
      (methodFindings.length > 0 && methodFindings[0].isIgnored); // ignored

    if (shouldPass !== tc.pass) {
      failures++;
      console.error(
        `atomic-readmodel-sync self-test ${tc.name}: got ${shouldPass ? "pass" : "fail"}, want ${tc.pass ? "pass" : "fail"} (method=${hasMethod} test=${hasTest})`,
      );
    }
  }

  if (failures > 0) {
    console.error(`atomic-readmodel-sync: ${failures} self-test(s) failed`);
    process.exit(1);
  }

  console.log(`atomic-readmodel-sync: all ${cases.length} self-tests passed`);
}

const mode = process.argv[2];
if (mode === "--self-test") {
  runSelfTest();
} else {
  runScan();
}
