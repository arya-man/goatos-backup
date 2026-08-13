#!/usr/bin/env node

// check-role-scoped-ui-contract.mjs — role differences on an admin-web page must come from the
// backend page contract (capability-gated controls / option_groups), never from a hardcoded
// role-string branch or literal chip/select markup with no contract gate behind it.
//
// INCIDENT (STG, 2026-08-12): the /verify page (apps/admin-web/features/verification-review/
// verification-review-page.tsx) grew an oversight filter set -- module chips, a capture-date
// range picker, a shed filter, status chips -- meant for the CEO's oversight view. Because
// admin-web pages are single, role-agnostic components (the SAME component serves /verify and
// /verify?scope_mode=company), the filters rendered for EVERY role that can open the page,
// including RoleVerifier. The fix (docs/decisions/role-scoped-ui-is-capability-gated.md) is:
// role differences come ONLY from (a) permission-gated endpoints and (b) capability-driven page
// contracts -- never a role-string conditional, never a per-role page copy.
//
// This guard is narrow and textual, matching its sibling guards
// (check-operational-partition-identity.mjs, check-leadership-verifier-surface-separation.mjs).
// It checks a fixed, EXTENDABLE list of admin-web page files under features/verification* for
// two regressions:
//
//   1. role-string-conditional — the component branches on a role/permission STRING literal
//      (e.g. `role === "verifier"`, `.includes("ceo_internal")`) instead of reading a control or
//      option_group off the backend page contract. Role-scoped rendering belongs in the backend
//      compiler (backend/internal/adminui/app/compiler.go), not in the component.
//   2. oversight-chrome-without-contract-gate — the file renders the KNOWN oversight-only chrome
//      (a cross-module chip row keyed off `filter_options.modules`, or the capture-date range
//      picker `<ActionsDateFilter`) without ALSO reading a capability off the page contract
//      (`controlEnabled(pageContract, ...)`) to gate it. A page can legitimately have NEITHER
//      pattern (most admin-web pages have no oversight/verifier split at all); the violation is
//      having the oversight markers with no contract-driven gate anywhere in the file.
//
// Modes:
//   (default)     scan the fixed TARGET_FILES list (below).
//   --self-test   run adversarial fixtures and exit.
//
// Blind spots (native Grep/Read must still catch these):
//   1. A gate token present anywhere in the file passes check 2 even if it does not actually wrap
//      the oversight markers -- this guard does not parse JSX, it looks for co-occurrence. The
//      frontend contract test (verification-review-page.contract.test.mjs) asserts the actual
//      wiring with source-shape regexes; this guard is the fast, cross-file CI tripwire.
//   2. Role checks hidden behind a helper function (`isOversight(role)`) are not detected -- only
//      literal string comparisons against known role/permission tokens are flagged.
//   3. New oversight-only chrome that doesn't match the two known markers (module chips, the
//      capture-date picker) will not be recognized until this guard's marker list is extended.

import { readFileSync, existsSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { tmpdir } from "node:os";

const repo = process.env.ROLE_SCOPED_UI_GUARD_TEST_REPO
  ? resolve(process.env.ROLE_SCOPED_UI_GUARD_TEST_REPO)
  : resolve(import.meta.dirname, "../..");

// EXTENDABLE: add a page file here when it grows a role-differentiated filter/control set that
// must be contract-driven. Paths are relative to the repo root.
export const TARGET_FILES = [
  "apps/admin-web/features/verification-review/verification-review-page.tsx",
  // oversight-analytics.tsx renders the CEO/PC-Director-only aggregate section (KPI strip,
  // pending-by-module backlog, per-verifier activity) -- the SAME oversight capability
  // (permissions.VerificationOversee) as the filters above, added 2026-08-12 alongside the
  // oversight_analytics page-contract control.
  "apps/admin-web/features/verification-review/oversight-analytics.tsx",
];

// Role/permission string literals that must never drive a rendering branch directly in a
// component file. Backend permission constants (backend/internal/permissions/permissions.go) and
// their string values, plus the role constants, both count -- a component comparing against
// either has reimplemented the backend's authorization decision on the client.
const ROLE_STRING_PATTERN =
  /(?:role|permission)\s*(?:===|==|!==|!=)\s*["'](?:verifier|ceo_internal|pc_director|growth_director|feed_director|health_director|verification\.review|verification\.verdict|verification\.act|verification\.oversee)["']/i;

// Known oversight-only chrome markers on the /verify page. See the incident report above.
const OVERSIGHT_MARKERS = [/modules\.map\(/, /<ActionsDateFilter/];

// A capability read off the backend page contract. Any of these shapes count as a real gate.
const CONTRACT_GATE_PATTERN = /controlEnabled\(\s*pageContract\s*,\s*["'][a-zA-Z0-9_]+["']/;

export function checkRoleStringConditional(rel, source) {
  const findings = [];
  const cleaned = stripComments(source);
  const match = cleaned.match(ROLE_STRING_PATTERN);
  if (match) {
    findings.push({
      rule: "role-string-conditional",
      message: `${rel}: renders a branch keyed on the role/permission string literal "${match[0]}". Role-scoped rendering must come from the backend page contract (a capability-gated control or option_group), never from a role/permission string compared in the component. See docs/decisions/role-scoped-ui-is-capability-gated.md.`,
    });
  }
  return findings;
}

export function checkOversightChromeHasContractGate(rel, source) {
  const findings = [];
  const cleaned = stripComments(source);
  const hasOversightMarker = OVERSIGHT_MARKERS.some((pattern) => pattern.test(cleaned));
  if (!hasOversightMarker) return findings;
  if (!CONTRACT_GATE_PATTERN.test(cleaned)) {
    findings.push({
      rule: "oversight-chrome-without-contract-gate",
      message: `${rel}: renders known oversight-only chrome (a module-chip row and/or the capture-date range picker) with no controlEnabled(pageContract, ...) gate anywhere in the file. These filters were built for CEO/director oversight and must not render for every role that can open this page (STG incident, 2026-08-12). Gate them on a capability read from the page contract, e.g. controlEnabled(pageContract, "oversight_filters", false).`,
    });
  }
  return findings;
}

function stripComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/\/\/.*$/gm, "");
}

function checkFile(rel, source) {
  return [...checkRoleStringConditional(rel, source), ...checkOversightChromeHasContractGate(rel, source)];
}

export function runSelfTest() {
  const tempDir = mkdtempSync(join(tmpdir(), "role-scoped-ui-guard-"));
  const testCases = [
    // GOOD: the real fix's shape -- oversight markers present, gated by a contract control.
    {
      name: "good-contract-gated-oversight-chrome",
      content: `
const oversightFiltersEnabled = controlEnabled(pageContract, "oversight_filters", false);
{oversightFiltersEnabled && modules.length > 1 ? (
  <div>{modules.map((option) => <Link key={option.key} />)}</div>
) : null}
{oversightFiltersEnabled ? <ActionsDateFilter basePath={PATHNAME} /> : null}
`,
      shouldFail: false,
    },
    // GOOD: a page with neither pattern at all (no oversight/verifier split) is unaffected.
    {
      name: "good-no-oversight-chrome",
      content: `
export function SomePage({ pageContract }) {
  return <div>{copy(pageContract, "title")}</div>;
}
`,
      shouldFail: false,
    },
    // BAD: the pre-fix shape -- module chips and the date picker render with no contract gate.
    {
      name: "bad-oversight-chrome-ungated",
      content: `
{modules.length > 1 ? (
  <div>{modules.map((option) => <Link key={option.key} />)}</div>
) : null}
<ActionsDateFilter basePath={PATHNAME} />
`,
      shouldFail: true,
    },
    // BAD: role-string conditional standing in for the backend contract.
    {
      name: "bad-role-string-conditional",
      content: `
export function VerificationReviewPage({ role, pageContract }) {
  return role === "verifier" ? <VerifierQueue /> : <OversightQueue />;
}
`,
      shouldFail: true,
    },
    // BAD: permission string literal compared directly.
    {
      name: "bad-permission-string-conditional",
      content: `
if (permission !== "verification.oversee") return null;
`,
      shouldFail: true,
    },
  ];

  const results = [];
  for (const testCase of testCases) {
    const testPath = join(tempDir, `${testCase.name}.tsx`);
    const dirPath = testPath.substring(0, testPath.lastIndexOf("/"));
    mkdirSync(dirPath, { recursive: true });
    writeFileSync(testPath, testCase.content);
    const findings = checkFile(testCase.name, testCase.content);
    const passed = (findings.length > 0) === testCase.shouldFail;
    results.push({ name: testCase.name, passed, shouldFail: testCase.shouldFail, findings: findings.length });
  }
  rmSync(tempDir, { recursive: true });

  const allPassed = results.every((r) => r.passed);
  console.log("\nROLE-SCOPED UI CONTRACT SELF-TEST");
  console.log("==================================\n");
  for (const result of results) {
    const status = result.passed ? "✓ PASS" : "✗ FAIL";
    const expected = result.shouldFail ? "(should fail)" : "(should pass)";
    console.log(`${status} ${result.name} ${expected}`);
    if (!result.passed) {
      console.log(`     Expected ${result.shouldFail ? "findings" : "no findings"}, got ${result.findings} findings`);
    }
  }
  console.log();
  process.exit(allPassed ? 0 : 1);
}

export function main() {
  const args = process.argv.slice(2);
  if (args.includes("--self-test")) {
    runSelfTest();
    return;
  }

  const findings = [];
  for (const rel of TARGET_FILES) {
    const abs = join(repo, rel);
    if (!existsSync(abs)) continue;
    const source = readFileSync(abs, "utf-8");
    findings.push(...checkFile(relative(repo, abs), source));
  }

  if (findings.length > 0) {
    console.log("ROLE-SCOPED UI CONTRACT VIOLATIONS\n");
    for (const f of findings) {
      console.log(`[${f.rule}] ${f.message}\n`);
    }
    process.exit(1);
  } else {
    console.log("✓ role-scoped UI chrome is capability-gated from the page contract");
    process.exit(0);
  }
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
