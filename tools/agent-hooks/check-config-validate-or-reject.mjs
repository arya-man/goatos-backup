#!/usr/bin/env node

// check-config-validate-or-reject.mjs — enforces the Goat OS rule: "authored
// config/business values are validate-or-reject, never silently-default."
//
// A field that is PRESENT but out of range (e.g., rule_dsl.capacity.max_per_day < 1,
// max_buffer_days < 0) must FAIL the publish/save with a clear error, not be
// rewritten to a default or clamped. Defaults apply ONLY to genuinely-absent fields.
// Frontends must keep a cleared field distinct from an explicit 0.
//
// This guard scans backend/internal/**/*.go for the SILENT-COERCION anti-pattern:
// a present numeric config field that is out-of-range gets reassigned to a
// default/clamped instead of returning a validation error — e.g.:
//   if cfg.MaxPerDay < 1 { cfg.MaxPerDay = <default> }
//   capPerDay := cfg.Field; if capPerDay < 1 { capPerDay = 1 }
//   allowedDays := cfg.Field + 1; if allowedDays < 1 { allowedDays = 1 }
//
// Modes:
//   (default)     scan the whole tree and fail only on NEW offenders vs baseline.
//   --self-test   run the built-in adversarial fixtures (incl. silent coercion patterns).
//
// Exception (must be COMPLETE — owner, issue, scope, and expiry all present):
//   config-validate-guard:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Anti-pattern matchers: silent coercion of authored config fields to defaults.
// Keep matchers NARROW to avoid false positives. These should match:
// - If a field is OUT OF RANGE, it gets REASSIGNED to a default/clamped value.
// - NOT legitimate bounds-checking that returns an error.
const MATCHERS = [
  // Pattern 1: x := cfg.Field [op N]; if x [cmp] threshold { x = value }
  // Matches: "capPerDay := cfg.MaxPerDay\nif capPerDay < 1 { capPerDay = 1 }"
  // Also: "allowedDays := cfg.MaxBufferDays + 1\nif allowedDays < 1 { allowedDays = 1 }"
  // This catches the most common pattern in capacity.go.
  {
    name: "local_var_silent_reassign",
    re: /(\w+)\s*:=\s*cfg\.(\w+)(?:\s*[\+\-\*\/]\s*\d+)?[^;]*(?:\n|;)\s*if\s+\1\s*<\s*\d+\s*\{\s*\1\s*=\s*\d+/gm,
  },
];

const IGNORE_RE =
  /config-validate-guard:ignore:\s*owner=\S+\s+issue=\S+\s+scope=\S+\s+expiry=\d{4}-\d{2}-\d{2}/;

function lineOfIndex(source, index) {
  return source.slice(0, index).split("\n").length; // 1-indexed
}

// A match is exempt if a COMPLETE ignore directive sits on the match's line,
// the line immediately after it, or the line immediately above it.
function isExempt(source, matchIndex) {
  const lines = source.split("\n");
  const ln = lineOfIndex(source, matchIndex); // 1-indexed
  // Check the matching line and surrounding lines for the ignore directive
  const window = [
    lines[ln - 2] ?? "",
    lines[ln - 1] ?? "",
    lines[ln] ?? "",
  ].join("\n");
  return IGNORE_RE.test(window);
}

// Returns array of findings: { line, matcher, snippet }.
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

const SCAN_GLOBS = ["*.go"];

function isTestOrExcluded(rel) {
  if (rel.endsWith("_test.go")) return true;
  if (/(^|\/)(__tests__|node_modules|build|dist)\//.test(rel)) return true;
  // the guard itself may carry test fixtures
  if (rel.endsWith("tools/agent-hooks/check-config-validate-or-reject.mjs"))
    return true;
  return false;
}

function trackedScanFiles() {
  const args = SCAN_GLOBS.map((g) => `'${g}'`).join(" ");
  const out = execSync(`git ls-files -- ${args}`, { cwd: repo, encoding: "utf8" });
  return out
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean)
    .filter((rel) => rel.startsWith("backend/internal"))
    .filter((rel) => !isTestOrExcluded(rel));
}

function readBaseline() {
  const baselineFile = resolve(
    repo,
    "tools/agent-hooks/config-validate-or-reject-baseline.txt"
  );
  try {
    const content = readFileSync(baselineFile, "utf8");
    const lines = content.split("\n");
    const offenders = new Set();
    for (const line of lines) {
      if (!line.startsWith("#") && line.trim()) {
        offenders.add(line.trim());
      }
    }
    return offenders;
  } catch {
    return new Set(); // no baseline yet
  }
}

function baselineKey(file, line, matcher) {
  return `${file}:${line}:${matcher}`;
}

function runSelfTest() {
  const cases = [
    // Good: explicit validation that returns error
    {
      name: "good_explicit_validation",
      pass: true,
      src: `if cfg.MaxPerDay < 1 {
  return fmt.Errorf("max_per_day must be >= 1")
}`,
    },
    // Good: absent field gets default
    {
      name: "good_absent_defaults",
      pass: true,
      src: `out := domain.PublishedCapacity{
  MaxPerDay: 100,
  MaxBufferDays: 7,
}`,
    },
    // Bad: direct field silent reassign
    {
      name: "bad_direct_field_reassign",
      pass: false,
      src: `capPerDay := cfg.MaxPerDay
if capPerDay < 1 {
  capPerDay = 1
}`,
    },
    // Bad: local var from cfg field, then silent clamp
    {
      name: "bad_local_var_silent_clamp",
      pass: false,
      src: `allowedDays := cfg.MaxBufferDays + 1
if allowedDays < 1 {
  allowedDays = 1
}`,
    },
    // Exception: complete ignore passes
    {
      name: "exception_complete_ignore",
      pass: true,
      src: `capPerDay := cfg.MaxPerDay
if capPerDay < 1 { capPerDay = 1 } // config-validate-guard:ignore: owner=ravi issue=GH-123 scope=legacy-capacity expiry=2026-12-31`,
    },
    // Exception: incomplete ignore still fails
    {
      name: "exception_incomplete_ignore",
      pass: false,
      src: `capPerDay := cfg.MaxPerDay
if capPerDay < 1 { capPerDay = 1 } // config-validate-guard:ignore: legacy`,
    },
  ];

  let failures = 0;
  for (const tc of cases) {
    const findings = findingsForSource(tc.src);
    const clean = findings.length === 0;
    if (clean !== tc.pass) {
      failures++;
      console.error(
        `config-validate-guard self-test ${tc.name}: got ${clean ? "pass" : "fail"}, want ${tc.pass ? "pass" : "fail"} (findings=${JSON.stringify(findings)})`
      );
    }
  }
  if (failures > 0) {
    console.error(`config-validate-guard: ${failures} self-test(s) failed`);
    process.exit(1);
  }
  console.log(`config-validate-guard: all ${cases.length} self-tests passed`);
}

function runScan() {
  const violations = [];
  const baseline = readBaseline();
  const newOffenders = [];

  for (const rel of trackedScanFiles()) {
    let src;
    try {
      src = readFileSync(resolve(repo, rel), "utf8");
    } catch {
      continue;
    }
    for (const f of findingsForSource(src)) {
      const key = baselineKey(rel, f.line, f.matcher);
      if (!baseline.has(key)) {
        newOffenders.push({ key, rel, line: f.line, matcher: f.matcher });
      }
      violations.push({
        file: rel,
        line: f.line,
        matcher: f.matcher,
        snippet: f.snippet,
        isNew: !baseline.has(key),
      });
    }
  }

  if (newOffenders.length > 0) {
    console.error(
      `config-validate-guard: ${newOffenders.length} NEW silent-coercion anti-pattern(s) found (see backend/internal/protocol/app/publish.go for the validate-or-reject pattern):\n`
    );
    for (const offender of newOffenders) {
      console.error(
        `  [NEW] ${offender.rel}:${offender.line} [${offender.matcher}]`
      );
    }
    console.error(
      "\nSee the validate-or-reject rule in AGENTS.md and the pattern in backend/internal/protocol/app/publish.go."
    );
    process.exit(1);
  }

  const baselinedCount = violations.filter((v) => !v.isNew).length;
  console.log(
    `config-validate-guard: ok (${baselinedCount} baselined offender(s); no new silent-coercion anti-patterns)`
  );
}

const mode = process.argv[2];
if (mode === "--self-test") {
  runSelfTest();
} else {
  runScan();
}
