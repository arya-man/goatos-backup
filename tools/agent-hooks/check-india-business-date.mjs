#!/usr/bin/env node

// check-india-business-date.mjs — enforces AGENTS.md rule:
// "Goat OS time semantics = India-business-calendar. UTC must never define a Goat OS
// business day — scheduling, due/missed buckets, reminder keys, reporting groups, audit
// display, UI labels must convert to Asia/Kolkata first."
//
// This is FIXCHK-002 bug class: checking that business-day derivation ALWAYS goes through
// the biztime helper (biztime.BusinessDayStart, biztime.BusinessDate, biztime.DefaultLocation)
// and never uses UTC for date/day decisions.
//
// Heuristics: scan for UTC-based date-truncation or date-formatting patterns in Go backend code:
// - .UTC().Truncate(24*time.Hour)    [truncating to day boundary in UTC]
// - .UTC().Format("2006-01-02")      [formatting as date string in UTC]
// - time.Now().UTC() ... Format/Truncate [UTC now followed by date ops]
// - business-key generation with UTC (e.g., reminder keys, due-date comparisons, buckets)
//
// False positives kept low: only flag when .UTC() is directly on or near a date-based op.
// Test files (_test.go) are INCLUDED — business-date semantics must be correct in tests too
// (unlike clinical defer guard which exempts tests for negative-case fixtures).
//
// Modes:
//   (default)     scan the whole backend/internal tree and compare against baseline;
//                 exit 1 only if NEW offenders found (baseline-ratchet).
//   --self-test   run the built-in fixtures and exit.
//
// Exception (complete):
//   india-date-guard:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve, relative } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Patterns that flag UTC-based business-day derivation WITHOUT going through biztime.
// Line-scan only (not multiline, to keep false positives low).
// Each pattern captures context around the .UTC() call.
const PATTERNS = [
  {
    name: "utc_truncate_day",
    re: /\.UTC\(\)\.Truncate\s*\(\s*24\s*\*\s*time\.Hour\s*\)/,
    reason: "truncating to day boundary in UTC; use biztime.BusinessDayStart() instead",
  },
  {
    name: "utc_format_date",
    re: /\.UTC\(\)\.Format\s*\(\s*["\']2006[- ]?01[- ]?02["\']\s*\)/,
    reason: "formatting date string in UTC; use biztime.BusinessDate() or .In(biztime.DefaultLocation()) first",
  },
  {
    name: "now_utc_then_format_date",
    re: /time\.Now\(\)\.UTC\(\).*\.Format\s*\(\s*["\']2006[- ]?01[- ]?02["\']\s*\)/,
    reason: "time.Now().UTC() followed by date formatting; wrap time.Now() with .In(biztime.DefaultLocation()) instead",
  },
  {
    name: "now_utc_inline",
    re: /time\.Now\(\)\.UTC\(\)\s*[,\)]/, // used inline in calls or assignments
    reason: "time.Now().UTC() at a call site; likely used for date comparisons/keys — convert to biztime.DefaultLocation()",
  },
];

const IGNORE_RE = /india-date-guard:ignore:\s*owner=\S+\s+issue=\S+\s+scope=\S+\s+expiry=\d{4}-\d{2}-\d{2}/;

function lineOfIndex(source, index) {
  return source.slice(0, index).split("\n").length;
}

function isExempt(source, line) {
  const lines = source.split("\n");
  // Check line itself and line above for ignore comment
  const window = [lines[line - 1] ?? "", lines[line - 2] ?? ""].join("\n");
  return IGNORE_RE.test(window);
}

export function findingsForSource(source) {
  const findings = [];
  const lines = source.split("\n");

  lines.forEach((text, idx) => {
    const line = idx + 1;
    if (IGNORE_RE.test(text)) return; // ignore line itself has the directive

    for (const { name, re, reason } of PATTERNS) {
      if (re.test(text)) {
        if (isExempt(source, line)) return;
        findings.push({
          line,
          rule: name,
          message: reason,
          text: text.trim().slice(0, 80),
        });
        break;
      }
    }
  });

  return findings;
}

function isGoBackendFile(rel) {
  return (
    rel.startsWith("backend/internal/") &&
    rel.endsWith(".go") &&
    !rel.includes("/build/")
  );
}

function trackedGoBackendFiles() {
  try {
    const out = execSync(`git ls-files -- 'backend/internal/**/*.go'`, {
      cwd: repo,
      encoding: "utf8",
    });
    return out
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean)
      .filter(isGoBackendFile);
  } catch {
    return [];
  }
}

function readBaselineFile() {
  const baselinePath = resolve(repo, "tools/agent-hooks/india-business-date-baseline.txt");
  try {
    const content = readFileSync(baselinePath, "utf8");
    const lines = content.split("\n").filter((l) => l.trim() && !l.startsWith("#"));
    const baseline = new Set();
    for (const line of lines) {
      const trimmed = line.trim();
      if (trimmed) {
        baseline.add(trimmed);
      }
    }
    return baseline;
  } catch {
    return new Set();
  }
}

function selfTest() {
  const testCases = [
    {
      name: "utc_truncate_day_bad",
      pass: false,
      src: "planned := dueAt.UTC().Truncate(24*time.Hour)",
    },
    {
      name: "utc_truncate_day_spaces",
      pass: false,
      src: "d := t.UTC().Truncate(  24  *  time.Hour  )",
    },
    {
      name: "utc_format_date_bad",
      pass: false,
      src: 'dateStr := now.UTC().Format("2006-01-02")',
    },
    {
      name: "utc_format_date_hyphen",
      pass: false,
      src: "dateStr := someTime.UTC().Format('2006-01-02')",
    },
    {
      name: "now_utc_then_format_bad",
      pass: false,
      src: "key := time.Now().UTC().Add(1*time.Hour).Format('2006-01-02')",
    },
    {
      name: "now_utc_inline_bad",
      pass: false,
      src: "reminder := makeKey(tenantID, eventID, time.Now().UTC())",
    },
    // Good patterns that should NOT flag
    {
      name: "biztime_business_day_good",
      pass: true,
      src: "planned := biztime.BusinessDayStart(dueAt)",
    },
    {
      name: "biztime_business_date_good",
      pass: true,
      src: 'dateStr := biztime.BusinessDate(now)',
    },
    {
      name: "in_default_location_good",
      pass: true,
      src: 'dateStr := time.Now().In(biztime.DefaultLocation()).Format("2006-01-02")',
    },
    {
      name: "utc_for_storage_ok",
      pass: true,
      src: 'stored := time.Now().UTC().Format(time.RFC3339Nano) // storage timestamp, not business date',
    },
    {
      name: "utc_for_unix_ok",
      pass: true,
      src: "timestamp := time.Now().UTC().Unix()",
    },
    {
      name: "utc_with_ignore_ok",
      pass: true,
      src: 'dateStr := now.UTC().Format("2006-01-02") // india-date-guard:ignore: owner=ravi issue=GH-123 scope=legacy expiry=2026-12-31',
    },
  ];

  let failures = 0;
  for (const tc of testCases) {
    const findings = findingsForSource(tc.src);
    const clean = findings.length === 0;
    if (clean !== tc.pass) {
      failures++;
      console.error(
        `india-date-guard self-test ${tc.name}: got ${clean ? "pass" : "fail"}, want ${tc.pass ? "pass" : "fail"}`
      );
      if (findings.length > 0) {
        console.error(`  findings: ${JSON.stringify(findings)}`);
      }
    }
  }

  if (failures > 0) {
    console.error(
      `india-date-guard: ${failures} self-test(s) failed`
    );
    process.exit(1);
  }
  console.log(
    `india-date-guard: all ${testCases.length} self-tests passed`
  );
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const baseline = readBaselineFile();
const findings = [];

for (const rel of trackedGoBackendFiles()) {
  const abs = resolve(repo, rel);
  let source;
  try {
    source = readFileSync(abs, "utf8");
  } catch {
    continue;
  }

  for (const f of findingsForSource(source)) {
    const key = `${rel}:${f.line}`;
    findings.push({ ...f, rel, key });
  }
}

// Filter to ONLY new offenders (baseline-ratchet)
const newOffenders = findings.filter((f) => !baseline.has(f.key));

if (newOffenders.length > 0) {
  console.error(
    `india-date-guard: ${newOffenders.length} NEW offender(s) — business dates must use biztime.BusinessDayStart/BusinessDate or .In(biztime.DefaultLocation())`
  );
  for (const f of newOffenders) {
    console.error(
      `  ${f.rel}:${f.line}: [${f.rule}] ${f.message}`
    );
    console.error(`    ${f.text}`);
  }
  console.error(
    "\nSee AGENTS.md → 'Treat Goat OS time semantics as India-business-calendar' and backend/internal/platform/biztime/"
  );
  process.exit(1);
}

const totalBaselined = baseline.size;
const resolved = totalBaselined - findings.filter((f) => baseline.has(f.key)).length;

console.log(`india-date-guard: ok`);
console.log(
  `  scanned ${trackedGoBackendFiles().length} backend Go file(s)`
);
if (baseline.size > 0) {
  console.log(
    `  baseline: ${totalBaselined} grandfathered offender(s)${
      resolved > 0 ? ` (${resolved} resolved)` : ""
    }`
  );
}
