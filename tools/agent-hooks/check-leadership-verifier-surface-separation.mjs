#!/usr/bin/env node

// check-leadership-verifier-surface-separation.mjs — Leadership Videos and Verifier Queue
// are SEPARATE SCREENS with no shared UI layer.
//
// INCIDENT REPORT (2026-08-06)
// On 2026-08-06, the leadership Videos nav href (shown to CEO/Directors in their
// bottom nav or drawer) was repointed at a verifier queue route (/verify/...).
// This meant every time a verifier UI component changed (filters, verdicts, queue
// layout), the leadership screen instantly reflected the change — CEO's view was no
// longer independent. Worse, when verifier routing evolved, the leadership entry
// point sometimes led to pages with VERDICT BUTTONS where the CEO could see but
// not click (gated on verification.verdict), creating UX confusion about what was
// readable vs. actionable.
//
// The rule is simple: Leadership is an AUDIT/OVERVIEW surface (read-only evidence
// trail with guidance cards), while Verifier is an ACTION surface (work queue with
// verdict buttons). They MUST NOT share routes, screens, composables, or ViewModels.
//
// This guard enforces three checks:
//
//   1. backend-nav-no-verify-route — the backend bootstrap_copy.go emits leadership
//      navigation hrefs that MUST NOT resolve to /verify*, /verify/action, or routes
//      with an "action" segment. Leadership nav must use the leadershipVideosHref()
//      function which routes to /verify?module=X&status=all (read-only filtered queue),
//      never a direct verifier route.
//   2. mobile-leadership-no-verifier-imports — Android/Kotlin screen files owned by
//      leadership (under `.../leadership/` directories in feature modules) must NOT
//      import or call verifier composables, ViewModels, or utilities from feature-verify.
//   3. mobile-leadership-no-verdict-controls — Android/Kotlin leadership screen files
//      must not render buttons, controls, or handlers for "approve", "reject", "rework",
//      or "reassign" verdicts. These are verifier-only actions.
//
// Modes:
//   (default)     diff-scoped: scan only changed files vs $MOBILE_GUARD_BASE (or origin/main).
//   --all         audit the entire codebase (backlog view).
//   --self-test   run adversarial fixtures and exit.
//
// Blind spots (native Grep/Read must still catch these):
//   1. Routes assembled at runtime (string concatenation, query param builders) may
//      not be caught by textual scanning of literal string hrefs.
//   2. Imports hidden behind wildcard imports (*) or indirect delegation through
//      intermediate modules are not visible to a textual Kotlin scanner.
//   3. Verdict control handlers defined in base classes or interfaces outside the
//      scanned file are not detected by this guard.
//   4. Leadership screen files not placed under a `.../leadership/` directory are
//      not recognized as leadership-owned and will be skipped by the mobile check.

import { readFileSync, readdirSync, existsSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const repo = process.env.LEADERSHIP_VERIFIER_GUARD_TEST_REPO
  ? resolve(process.env.LEADERSHIP_VERIFIER_GUARD_TEST_REPO)
  : resolve(import.meta.dirname, "../..");
const SELF_PATH = fileURLToPath(import.meta.url);

const BOOTSTRAP_FILE = "backend/internal/workforce/app/bootstrap_copy.go";
const ANDROID_ROOT = "apps/goatos-android";

// Leadership nav item key patterns in the bootstrap registry
const LEADERSHIP_CONTRIBUTIONS_PATTERN = /contributions\s*:\s*\[\]moduleNavContribution/;
const NAV_VIDEO_ITEM = /key:\s*"videos"[^}]*?href:\s*([^,}]+)/;

// Leadership route pattern — per-feature leadership videos routes
// Leadership items are ONLY in the "contributions" array in bootstrap_copy.go,
// NOT in the "reviewContributions" array (which is verifier-only and correctly uses /verify*).
// Leadership routes follow the pattern /<feature>/videos: /vaccination/videos, /weighing/videos, etc.
// The rule only scans the "contributions" section to distinguish leadership nav from verifier nav.
const LEADERSHIP_ROUTE_PATTERN = /^\/[a-z_]+\/videos$/; // e.g. /vaccination/videos, /weighing/videos
const VERIFIER_CONTRIBUTIONS_PATTERN = /reviewContributions\s*:\s*\[\]moduleNavContribution/;

// Files/directories to scan for leadership screen ownership
const LEADERSHIP_DIR_PATTERN = /[/\\]leadership[/\\]/i;

// Verifier imports and controls to forbid in leadership files
const FORBIDDEN_VERIFIER_IMPORTS = [
  /import\s+.*from\s+['"]*.*feature\.verify/i,
  /import\s+.*VerifyDetailScreen/i,
  /import\s+.*VerifyQueueScreen/i,
  /import\s+.*VerifyDetailViewModel/i,
  /from\s+["']sg\.mesha\.goatos\.feature\.verify/,
];

const VERDICT_CONTROL_PATTERNS = [
  /\b(approve|reject|rework|reassign)\s*\(/gi,
  /onclick.*approve/i,
  /onclick.*reject/i,
  /text\s*=\s*"Approve"/i,
  /text\s*=\s*"Reject"/i,
  /text\s*=\s*"Rework"/i,
  /contentDescription.*[Aa]pprove/i,
  /contentDescription.*[Rr]eject/i,
];

/**
 * Resolve what a helper function actually returns by reading its definition from the file.
 * Recursively resolves nested helper calls with depth and cycle guards.
 * Returns the resolved route prefix or null if unresolvable (e.g., returns from parameter or variable).
 */
function resolveHelperReturnValue(helperName, bootstrapSource, depth = 0, visited = new Set()) {
  const MAX_DEPTH = 5;

  if (depth > MAX_DEPTH) {
    return null; // Depth exceeded, unresolvable
  }

  if (visited.has(helperName)) {
    return null; // Cycle detected, unresolvable
  }

  visited.add(helperName);

  // Find the function definition: func leadershipVideosHref(...) string { ... }
  // Be conservative: require the signature to explicitly return string
  const funcPattern = new RegExp(
    `func\\s+${helperName}\\s*\\([^)]*\\)\\s*string\\s*\\{([^}]*)\\}`,
    "s"
  );

  const match = bootstrapSource.match(funcPattern);
  if (!match) {
    return null; // Function not found
  }

  const body = match[1];

  // Try to extract a return statement
  // Simple pattern: look for 'return "..." or 'return <func>(...)'
  const returnMatch = body.match(/return\s+("(?:[^"\\]|\\.)*"|(\w+)\s*\([^)]*\))/);
  if (!returnMatch) {
    return null; // No resolvable return found
  }

  const returnExpr = returnMatch[1];

  // Case 1: String literal return
  const literalMatch = returnExpr.match(/^"((?:[^"\\]|\\.)*)"$/);
  if (literalMatch) {
    return literalMatch[1]; // Return the string value
  }

  // Case 2: Function call return (e.g., verifyQueueHref(...))
  const callMatch = returnExpr.match(/^(\w+)\s*\(/);
  if (callMatch) {
    const nestedHelperName = callMatch[1];
    return resolveHelperReturnValue(nestedHelperName, bootstrapSource, depth + 1, visited);
  }

  // Case 3: Variable or parameter return (unresolvable)
  return null;
}

/**
 * Check that a leadership nav href resolves to a LEADERSHIP-OWNED route, NOT a verifier route.
 *
 * CRITICAL FIX (2026-08-06 incident): This rule RESOLVES helper calls to see what they
 * ACTUALLY return, not just checking the helper name. The defect was that leadershipVideosHref()
 * is named like it's correct, but it RETURNS "/verify?module=...&status=all" — still a verifier route!
 *
 * This guard now:
 * 1. Detects direct /verify* routes (obviously wrong)
 * 2. Detects direct verifyQueueHref() calls (verifier-only helper)
 * 3. RESOLVES generic helper calls to what they actually return (catches deceptive names)
 * 4. Fails CLOSED on unresolvable helpers (return from param/var, runtime-built routes)
 *
 * Leadership and Verifier MUST have SEPARATE routes on SEPARATE screens.
 * Even if wrapped in a helper with a promising name, if it resolves to /verify*, it is WRONG.
 */
export function checkBootstrapNavRoute(rel, lineNum, href, bootstrapSource = "") {
  const findings = [];

  // Trim quotes
  const cleanedHref = href.trim().replace(/^["']|["']$/g, "");

  // Check 1: Direct /verify* routes are obviously wrong
  if (/^\/verify/.test(cleanedHref)) {
    findings.push({
      rule: "backend-nav-direct-verifier-route-in-leadership",
      message: `${rel}:${lineNum}: leadership "videos" nav item is a direct verifier route: ${cleanedHref}. Leadership (audit/overview, read-only) and Verifier (action queue, verdict casting) are SEPARATE surfaces on SEPARATE routes. Leadership nav must resolve to a leadership-owned route like ${LEADERSHIP_ROUTE_PATTERN}..., NOT /verify* (incident 2026-08-06).`,
    });
    return findings;
  }

  // Check 2: Detect function call patterns (e.g., leadershipVideosHref(...), verifyQueueHref(...), or any other helper)
  const callMatch = cleanedHref.match(/^(\w+)\s*\(/);
  if (callMatch) {
    const helperName = callMatch[1];

    // Special case: verifyQueueHref is obviously a verifier-only helper
    if (helperName === "verifyQueueHref") {
      findings.push({
        rule: "backend-nav-verify-queue-href-in-leadership",
        message: `${rel}:${lineNum}: leadership "videos" nav calls verifyQueueHref() directly. This is a verifier-only helper. Leadership (audit/overview, read-only) and Verifier (action queue, verdict casting) are SEPARATE surfaces. Leadership nav must point to a leadership-owned route like ${LEADERSHIP_ROUTE_PATTERN}..., NOT verifyQueueHref() (incident 2026-08-06).`,
      });
      return findings;
    }

    // Resolve what the helper actually returns
    if (bootstrapSource) {
      const resolvedRoute = resolveHelperReturnValue(helperName, bootstrapSource);

      if (resolvedRoute === null) {
        // Unresolvable: return from parameter, variable, or runtime-built
        findings.push({
          rule: "backend-nav-helper-unresolvable-route",
          message: `${rel}:${lineNum}: leadership "videos" nav calls ${helperName}() but the helper's return value cannot be statically resolved (e.g., returns from a parameter, variable, or is runtime-built). Leadership nav must be a RESOLVABLE LITERAL route starting with ${LEADERSHIP_ROUTE_PATTERN}, NOT a dynamic/unresolvable helper (incident 2026-08-06).`,
        });
        return findings;
      }

      // Check if resolved route is a verifier route
      if (/^\/verify/.test(resolvedRoute)) {
        findings.push({
          rule: "backend-nav-helper-returns-verifier-route",
          message: `${rel}:${lineNum}: leadership "videos" nav calls ${helperName}() but that helper RESOLVES to a verifier route: ${resolvedRoute}. Leadership and Verifier MUST be on SEPARATE routes and SEPARATE screens. The helper's name may be deceptive; the returned route is what matters. Leadership nav must resolve to a route starting with ${LEADERSHIP_ROUTE_PATTERN}... (incident 2026-08-06).`,
        });
        return findings;
      }

      // Check if resolved route matches the expected leadership route pattern
      if (!LEADERSHIP_ROUTE_PATTERN.test(resolvedRoute)) {
        findings.push({
          rule: "backend-nav-helper-wrong-route-prefix",
          message: `${rel}:${lineNum}: leadership "videos" nav calls ${helperName}() which resolves to: ${resolvedRoute}. Leadership nav must match the pattern /<feature>/videos (e.g., /vaccination/videos), not ${resolvedRoute}. (incident 2026-08-06).`,
        });
        return findings;
      }
    } else {
      // No bootstrap source provided; fail closed for any unrecognized helper
      findings.push({
        rule: "backend-nav-helper-unverified",
        message: `${rel}:${lineNum}: leadership "videos" nav calls ${helperName}() but no bootstrap source was provided to verify its return value. Leadership nav helpers must be statically verifiable (incident 2026-08-06).`,
      });
      return findings;
    }
  }

  // If we get here, it's a literal string that doesn't start with /verify
  // Check if it matches the expected leadership route pattern
  if (!LEADERSHIP_ROUTE_PATTERN.test(cleanedHref)) {
    findings.push({
      rule: "backend-nav-wrong-route-prefix",
      message: `${rel}:${lineNum}: leadership "videos" nav route is: ${cleanedHref}. Leadership nav must match the pattern /<feature>/videos (e.g., /vaccination/videos), not ${cleanedHref}. (incident 2026-08-06).`,
    });
  }

  return findings;
}

/**
 * Check that a Kotlin file in a leadership directory does not import from feature-verify.
 */
export function checkKotlinLeadershipImports(rel, source) {
  const findings = [];

  // Only check files under .../leadership/ directories
  if (!LEADERSHIP_DIR_PATTERN.test(rel)) {
    return findings;
  }

  // Strip comments
  const cleaned = stripComments(source);

  for (const pattern of FORBIDDEN_VERIFIER_IMPORTS) {
    if (pattern.test(cleaned)) {
      findings.push({
        rule: "mobile-leadership-no-verifier-imports",
        message: `${rel}: leadership screen file imports from feature-verify module. Leadership and Verifier are separate surfaces (context/architecture/verifier-app-and-flow.md; incident 2026-08-06). Leadership is audit/overview (read-only video trail); Verifier is action (verdict queue). A leadership screen must never render or depend on verifier composables, ViewModels, or utilities. Move the needed logic into a shared core module if both need it.`,
      });
    }
  }

  return findings;
}

/**
 * Check that a Kotlin file in a leadership directory does not render verdict controls.
 */
export function checkKotlinLeadershipVerdictControls(rel, source) {
  const findings = [];

  // Only check files under .../leadership/ directories
  if (!LEADERSHIP_DIR_PATTERN.test(rel)) {
    return findings;
  }

  // Strip comments
  const cleaned = stripComments(source);

  for (const pattern of VERDICT_CONTROL_PATTERNS) {
    const matches = cleaned.match(pattern);
    if (matches) {
      findings.push({
        rule: "mobile-leadership-no-verdict-controls",
        message: `${rel}: leadership screen renders a verdict control ("${matches[0]}"). Leadership has read-only access to the video evidence trail; only the Verifier role casts verdicts (approval, rejection, rework assignment). Verdict buttons belong in feature-verify screens only. Leadership sees the outcome of the verdict, not the verdict action itself. If this is a display-only status chip, rename it to avoid "approve/reject" terminology.`,
      });
      break; // Report first finding only
    }
  }

  return findings;
}

function stripComments(text) {
  return text
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/\/\/.*$/gm, "")
    .replace(/"[^"]*"/g, "\"\"") // Remove string contents to avoid matching quoted strings
    .replace(/'[^']*'/g, "''");
}

/**
 * Self-test fixtures for all three checks.
 */
export function runSelfTest() {
  const tempDir = mkdtempSync(join(tmpdir(), "leadership-verifier-guard-"));
  const testCases = [
    // GOOD: Leadership nav using correct per-feature leadership route
    {
      name: "bootstrap-good-leadership-href",
      file: "bootstrap_copy.go",
      content: `contributions: []moduleNavContribution{
{key: "videos", labelKey: "nav.videos", href: "/vaccination/videos", shared_key: "", priority: 3},
},`,
      shouldFail: false,
    },
    // BAD: Leadership nav pointing directly to /verify route
    {
      name: "bootstrap-bad-verify-route",
      file: "bootstrap_copy.go",
      content: `{key: "videos", labelKey: "nav.videos", href: "/verify/vaccination", shared_key: "", priority: 3},`,
      shouldFail: true,
    },
    // BAD: Leadership nav calling verifyQueueHref directly (not wrapped by leadershipVideosHref)
    {
      name: "bootstrap-bad-verify-queue-href",
      file: "bootstrap_copy.go",
      content: `{key: "videos", labelKey: "nav.videos", href: verifyQueueHref("vaccination"), priority: 3},`,
      shouldFail: true,
    },
    // ADVERSARIAL: Leadership nav calls leadershipVideosHref (LOOKS correct by name) but
    // that helper RETURNS a verifier route. This is the 2026-08-06 incident defect.
    // The OLD guard passes this (wrong). The NEW guard FAILS it (correct).
    {
      name: "bootstrap-bad-leadership-videos-href-returns-verify",
      file: "bootstrap_copy.go",
      content: `{key: "videos", labelKey: "nav.videos", href: leadershipVideosHref("vaccination"), priority: 3},
func leadershipVideosHref(module string) string {
  return "/verify?module=" + module + "&status=all"
}`,
      shouldFail: true, // NEW GUARD SHOULD FAIL THIS — the helper returns /verify*...
    },
    // ADVERSARIAL: Helper with RENAME-ESCAPE — a new name (leadershipGalleryHref) whose body
    // returns /verify*. The OLD guard hardcoded "leadershipVideosHref", so this bypasses it.
    // The NEW guard RESOLVES the body, so it FAILS correctly.
    {
      name: "bootstrap-bad-leadership-gallery-href-returns-verify",
      file: "bootstrap_copy.go",
      content: `{key: "videos", labelKey: "nav.videos", href: leadershipGalleryHref("vaccination"), priority: 3},
func leadershipGalleryHref(module string) string {
  return "/verify?module=" + module
}`,
      shouldFail: true, // NEW GUARD CATCHES THIS (old guard would miss it — name-based escape)
    },
    // ADVERSARIAL: Two-hop chain — leadership href calls helperA, helperA returns helperB's result,
    // helperB returns /verify*. Must recursively resolve.
    {
      name: "bootstrap-bad-two-hop-chain-returns-verify",
      file: "bootstrap_copy.go",
      content: `{key: "videos", labelKey: "nav.videos", href: leadershipHelperA("vaccination"), priority: 3},
func leadershipHelperA(module string) string {
  return leadershipHelperB(module)
}
func leadershipHelperB(module string) string {
  return "/verify?module=" + module + "&status=closed"
}`,
      shouldFail: true, // NEW GUARD RECURSIVELY RESOLVES (old guard misses nested chains)
    },
    // ADVERSARIAL: Helper returns from a parameter (unresolvable) — must fail closed
    {
      name: "bootstrap-bad-unresolvable-parameter-route",
      file: "bootstrap_copy.go",
      content: `{key: "videos", labelKey: "nav.videos", href: leadershipUnresolvable("videos"), priority: 3},
func leadershipUnresolvable(routeBase string) string {
  return routeBase + "/vaccination"
}`,
      shouldFail: true, // NEW GUARD FAILS CLOSED (return is from parameter, not a literal)
    },
    // GOOD: Leadership Kotlin file with no verifier imports
    {
      name: "kotlin-good-leadership-no-imports",
      file: "feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/leadership/WeighingLeadershipScreen.kt",
      content: `package sg.mesha.goatos.feature.weighing.leadership

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.Text

@Composable
fun LeadershipVideosScreen() {
  Text("Videos")
}`,
      shouldFail: false,
    },
    // BAD: Leadership Kotlin file importing from feature-verify
    {
      name: "kotlin-bad-verify-import",
      file: "feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/leadership/BadScreen.kt",
      content: `package sg.mesha.goatos.feature.weighing.leadership

import androidx.compose.foundation.layout.Column
import sg.mesha.goatos.feature.verify.VerifyDetailScreen

@Composable
fun LeadershipScreen() {
  VerifyDetailScreen()
}`,
      shouldFail: true,
    },
    // BAD: Leadership Kotlin file with verdict controls
    {
      name: "kotlin-bad-verdict-button",
      file: "feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/leadership/VerdictScreen.kt",
      content: `package sg.mesha.goatos.feature.weighing.leadership

import androidx.compose.material3.Button
import androidx.compose.material3.Text

@Composable
fun LeadershipVideosScreen() {
  Button(onClick = { approve(videoId) }) {
    Text("Approve Video")
  }
}`,
      shouldFail: true,
    },
    // GOOD: Non-leadership file with verdict controls (should pass - only leadership is checked)
    {
      name: "kotlin-verify-with-verdict",
      file: "feature-verify/src/main/kotlin/sg/mesha/goatos/feature/verify/VerifyScreen.kt",
      content: `package sg.mesha.goatos.feature.verify

import androidx.compose.material3.Button
import androidx.compose.material3.Text

@Composable
fun VerifyDetailScreen() {
  Button(onClick = { approve(videoId) }) {
    Text("Approve Video")
  }
}`,
      shouldFail: false,
    },
  ];

  const results = [];

  for (const testCase of testCases) {
    const testPath = join(tempDir, testCase.file);
    const dirPath = testPath.substring(0, testPath.lastIndexOf("/"));

    // Create directories
    if (!existsSync(dirPath)) {
      mkdirSync(dirPath, { recursive: true });
    }

    writeFileSync(testPath, testCase.content);

    // Run checks
    let findings = [];
    if (testCase.file.endsWith(".go")) {
      const match = testCase.content.match(NAV_VIDEO_ITEM);
      if (match) {
        findings.push(...checkBootstrapNavRoute(testCase.file, 1, match[1], testCase.content));
      }
    } else if (testCase.file.endsWith(".kt")) {
      findings.push(...checkKotlinLeadershipImports(testCase.file, testCase.content));
      findings.push(...checkKotlinLeadershipVerdictControls(testCase.file, testCase.content));
    }

    const passed = (findings.length > 0) === testCase.shouldFail;
    results.push({
      name: testCase.name,
      passed,
      shouldFail: testCase.shouldFail,
      findings: findings.length,
    });
  }

  // Clean up
  rmSync(tempDir, { recursive: true });

  // Report
  const allPassed = results.every((r) => r.passed);
  console.log("\nLEADERSHIP-VERIFIER SURFACE SEPARATION SELF-TEST");
  console.log("================================================\n");

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

/**
 * Main guard: scan diff or all files
 */
export function main() {
  const args = process.argv.slice(2);

  if (args.includes("--self-test")) {
    runSelfTest();
    return;
  }

  const allFiles = args.includes("--all");
  const findings = [];

  // Check 1: Bootstrap nav routes — LEADERSHIP contributions ONLY (not reviewContributions/verifier)
  if (existsSync(join(repo, BOOTSTRAP_FILE))) {
    const content = readFileSync(join(repo, BOOTSTRAP_FILE), "utf-8");
    const lines = content.split("\n");

    let inLeadershipContributions = false;
    let inVerifierReviewContributions = false;
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];

      // Detect which section we're in (check reviewContributions FIRST to avoid substring match with contributions)
      if (/reviewContributions\s*:\s*\[\]moduleNavContribution/i.test(line)) {
        inLeadershipContributions = false;
        inVerifierReviewContributions = true;
      } else if (/^\s*contributions\s*:\s*\[\]moduleNavContribution/i.test(line)) {
        inLeadershipContributions = true;
        inVerifierReviewContributions = false;
      }

      // ONLY check leadership contributions (not reviewer/verifier contributions)
      if (inLeadershipContributions && line.includes("key:") && line.includes("videos")) {
        const hrefMatch = line.match(/href:\s*([^,}]+)/);
        if (hrefMatch) {
          findings.push(...checkBootstrapNavRoute(relative(repo, BOOTSTRAP_FILE), i + 1, hrefMatch[1], content));
        }
      }
    }
  }

  // Check 2 & 3: Android leadership files
  if (existsSync(join(repo, ANDROID_ROOT))) {
    const getChangedFiles = () => {
      if (allFiles) {
        return getKotlinFiles(join(repo, ANDROID_ROOT));
      } else {
        // Get diff-scoped files
        const base = process.env.MOBILE_GUARD_BASE || "origin/main";
        try {
          const result = spawnSync("git", ["diff", base, "--name-only"], { cwd: repo, encoding: "utf-8" });
          if (result.status === 0) {
            return result.stdout
              .split("\n")
              .filter((f) => f.endsWith(".kt") && f.includes("/main/kotlin/"))
              .map((f) => join(repo, f));
          }
        } catch (e) {
          // Fall back to all files
        }
        return getKotlinFiles(join(repo, ANDROID_ROOT));
      }
    };

    for (const file of getChangedFiles()) {
      try {
        const content = readFileSync(file, "utf-8");
        const rel = relative(repo, file);
        findings.push(...checkKotlinLeadershipImports(rel, content));
        findings.push(...checkKotlinLeadershipVerdictControls(rel, content));
      } catch (e) {
        // Skip unreadable files
      }
    }
  }

  // Report
  if (findings.length > 0) {
    console.log("LEADERSHIP-VERIFIER SURFACE SEPARATION VIOLATIONS\n");
    for (const f of findings) {
      console.log(`[${f.rule}] ${f.message}\n`);
    }
    process.exit(1);
  } else {
    console.log("✓ Leadership and Verifier surfaces are properly separated");
    process.exit(0);
  }
}

function getKotlinFiles(rootDir) {
  const files = [];
  const walk = (dir) => {
    try {
      for (const file of readdirSync(dir)) {
        const path = join(dir, file);
        const stat = require("node:fs").statSync(path);
        if (stat.isDirectory()) {
          walk(path);
        } else if (file.endsWith(".kt") && dir.includes("/main/kotlin/")) {
          files.push(path);
        }
      }
    } catch (e) {
      // Ignore permission errors
    }
  };
  walk(rootDir);
  return files;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
