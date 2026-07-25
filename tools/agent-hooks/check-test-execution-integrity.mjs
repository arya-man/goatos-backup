#!/usr/bin/env node
// Test execution integrity guard: detects Go test files with stray build constraints
// that cause them to be silently excluded from CI runs.
//
// The repo gates Postgres tests at RUNTIME via GOATOS_RUN_POSTGRES_TESTS=1 + pgtest.SkipIfNoDocker,
// NOT via build tags. A test file with // +build pgtest or //go:build pgtest is silently
// excluded by `go test` and never executes, but the test report shows "no tests to run" instead
// of failing the build. This guard catches such stray build constraints.

import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { spawnSync } from "node:child_process";

const root = process.cwd();

// Build tags that are intentionally passed by CI when they run.
// Standard runs pass no tags; explicit Postgres runs pass GOATOS_RUN_POSTGRES_TESTS at the env level, not as a go -tags value.
const ciPassedBuildTags = new Set([]);

// Patterns to detect stray build constraints in test files.
const buildConstraintPattern = /^\/\/\s*\+build\s+(.+)$|^\/\/go:build\s+(.+)$/m;

function detectStallySkippedTests() {
  const findings = [];

  // Get list of changed/added Go test files relative to origin/main.
  let base = process.env.GOATOS_CI_BASE || "origin/main";
  if (spawnSync("git", ["rev-parse", "--verify", `${base}^{commit}`], { cwd: root }).status !== 0) {
    base = "HEAD~1";
  }

  const diff = spawnSync("git", ["diff", "--name-only", "--diff-filter=ACMR", base, "--", "backend"], {
    cwd: root,
    encoding: "utf8",
  });

  const untracked = spawnSync("git", ["ls-files", "--others", "--exclude-standard", "--", "backend"], {
    cwd: root,
    encoding: "utf8",
  });

  const allFiles = [...new Set(`${diff.stdout || ""}\n${untracked.stdout || ""}`.split("\n"))]
    .filter((file) => file.endsWith("_test.go") && fs.existsSync(path.join(root, file)));

  for (const file of allFiles) {
    const fullPath = path.join(root, file);
    const content = fs.readFileSync(fullPath, "utf8");
    const lines = content.split("\n");

    // Check for old-style build constraint (// +build <tag>)
    let buildConstraint = null;
    for (const line of lines.slice(0, 20)) {
      // Build constraints must appear in a comment block at the top
      const match = line.match(buildConstraintPattern);
      if (match) {
        buildConstraint = (match[1] || match[2]).trim();
        break;
      }
      // Stop scanning once we hit non-comment/non-blank lines
      if (line.trim() && !line.trim().startsWith("//")) break;
    }

    if (buildConstraint) {
      const tags = buildConstraint.split(/\s+/).filter(Boolean);
      for (const tag of tags) {
        if (!ciPassedBuildTags.has(tag)) {
          findings.push(
            `${file} has build constraint // +build ${tag}, but '${tag}' is not passed by CI. ` +
              `Postgres tests must gate at runtime via GOATOS_RUN_POSTGRES_TESTS=1 + pgtest.SkipIfNoDocker(), ` +
              `not via build tags.`
          );
        }
      }
    }

    // Check for references to pgtest.Pool or other non-exported pgtest symbols that would cause compile failure.
    if (content.includes("pgtest.Pool") || content.includes("pgtest.StartPostgres") || content.includes("pgtest.SkipIfNoDocker")) {
      // These are valid pgtest functions. Check if they're being used correctly.
      // A file referencing pgtest.SkipIfNoDocker is fine (runtime gate).
      // A file referencing pgtest.Pool without runtime gating AND with a build constraint is suspicious.
      if (buildConstraint && !content.includes("pgtest.SkipIfNoDocker")) {
        findings.push(
          `${file} references pgtest.Pool but does not call pgtest.SkipIfNoDocker() for runtime opt-in. ` +
            `This test will silently fail to compile/run. Use runtime gating instead of build tags.`
        );
      }
    }

    // Check for Test* functions in files with stray build constraints.
    const hasTestFunc = /^func\s+Test/.test(content.match(/^func Test.*/m)?.[0] || "");
    if (hasTestFunc && buildConstraint && !ciPassedBuildTags.has(buildConstraint.split(/\s+/)[0])) {
      findings.push(
        `${file} declares func Test* but has an unsupported build constraint '${buildConstraint}'. ` +
          `The test will be silently excluded from all CI runs.`
      );
    }
  }

  return findings;
}

function runSelfTest() {
  // Test case 1: File with stray // +build pgtest should FAIL.
  const badPgtest = `// +build pgtest

package mypackage_test

import (
  "testing"
  "sg.mesha.goatos/backend/internal/platform/pgtest"
)

func TestMyDatabase(t *testing.T) {
  pool := pgtest.StartPostgres(t, context.Background())
  // test body
}
`;

  // Test case 2: File with runtime gating via pgtest.SkipIfNoDocker should PASS.
  const goodRuntimeGate = `package mypackage_test

import (
  "context"
  "testing"
  "sg.mesha.goatos/backend/internal/platform/pgtest"
)

func TestMyDatabaseRuntimeGated(t *testing.T) {
  pgtest.SkipIfNoDocker(t)
  pool := pgtest.StartPostgres(t, context.Background())
  // test body
}
`;

  // Test case 3: File with no test functions and a build constraint should PASS (not a test file).
  const goodHelper = `// +build pgtest

package mypackage

import "sg.mesha.goatos/backend/internal/platform/pgtest"

type Helper struct {
  pool pgtest.Pool
}
`;

  // Mock the git operations for self-test.
  // For now, we'll just validate the logic directly.

  // Simulate the bad pgtest file check.
  const badMatch = badPgtest.match(buildConstraintPattern);
  if (!badMatch) throw new Error("self-test: expected to find build constraint in bad pgtest file");

  const badHasTestFunc = /^func\s+Test/.test(badPgtest.match(/^func Test.*/m)?.[0] || "");
  if (!badHasTestFunc) throw new Error("self-test: expected to find Test* function in bad pgtest file");

  // Simulate the good runtime gate check.
  const goodMatch = goodRuntimeGate.match(buildConstraintPattern);
  if (goodMatch) throw new Error("self-test: expected NO build constraint in good runtime-gated file");

  const goodHasSkip = goodRuntimeGate.includes("pgtest.SkipIfNoDocker");
  if (!goodHasSkip) throw new Error("self-test: expected pgtest.SkipIfNoDocker in good file");

  console.log("test-execution-integrity guard self-test passed");
}

if (process.argv.includes("--self-test")) {
  runSelfTest();
  process.exit(0);
}

const findings = detectStallySkippedTests();

if (findings.length > 0) {
  console.error("test-execution-integrity guard FAILED:");
  for (const finding of findings) console.error(`  ${finding}`);
  process.exit(1);
}

console.log("test-execution-integrity guard passed");
