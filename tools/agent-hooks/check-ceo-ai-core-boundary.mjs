#!/usr/bin/env node

// check-ceo-ai-core-boundary.mjs — enforces the ceo_ai reporting/assistant
// boundary: the leadership-assistant namespace (`ceo_ai` SQL schema + ceo_ai_*
// assistant tables) must NEVER sit between the core Backend <-> Frontend <->
// Mobile operator layers.
//
// Direction of allowed dependency:
//   core BE  -> DB / read models                (operator source of truth)
//   assistant (ceoai) -> reads core data via Mesha read APIs / MCP / read-only SQL
// NEVER: a core operator read path (processintegrity/Control Tower, calendar,
// vaccination, counts, ...) reaching INTO `ceo_ai.*` for its own runtime data.
//
// This exact anti-pattern shipped once: the Control Tower query joined
// `ceo_ai.vaccine_label_for(...)` purely for a display label, so dropping the
// reporting schema 500'd a core operator screen (SQLSTATE 3F000). Label
// composition now lives in Go (vaccination/domain.DoseDisplayLabel). See
// docs/decisions/ceo-ai-reporting-boundary.md.
//
// Rule: no SQL access to the `ceo_ai` reporting schema or `ceo_ai_*` assistant
// tables from core backend runtime code:
//   - backend/internal/**   EXCEPT backend/internal/ceoai/**   (the assistant module owns it)
// The pattern targets real SQL usage only — a schema-qualified function/view
// call (`ceo_ai.vaccine_label_for(...)`, `FROM ceo_ai.some_view`) or a
// FROM/JOIN/INTO/UPDATE against a ceo_ai table. i18n copy keys ("ceo_ai.title"),
// telemetry event names (ceo_ai_admin_trace_open), and assistant feature-dir
// names are NOT data-flow coupling and are intentionally not matched.
// Comment-only lines, test files, and generated/migration dirs are exempt.
// Cube models and backend/migrations legitimately own ceo_ai and are out of scope.
//
// The BE<->FE<->mobile boundary for the assistant is documented (the assistant
// UI bubble is allowed global chrome; core pages must not route their data
// through /api/ceo-ai). See docs/decisions/ceo-ai-reporting-boundary.md. FE/
// mobile do not issue SQL, so the machine gate here is the backend SQL layer.
//
// Modes:
//   (default)     scan the tree, fail on any violation.
//   --self-test   run the built-in adversarial fixtures.
//
// Exception (must be COMPLETE — owner, issue, scope, expiry all present):
//   ceo-ai-boundary:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Real SQL access to the ceo_ai namespace. Two forms, both requiring a SQL
// context so i18n copy keys / telemetry names never match:
//   1. schema-qualified function/view call:  ceo_ai.<ident>(   or  ceo_ai.<ident> as/whitespace after FROM/JOIN
//   2. FROM/JOIN/INTO/UPDATE against a ceo_ai schema or ceo_ai_* table
const REF_RES = [
  /\bceo_ai\.[a-z_][a-z0-9_]*\s*\(/i, // ceo_ai.vaccine_label_for(
  /\b(?:from|join|into|update)\s+ceo_ai[._][a-z0-9_]+/i, // FROM ceo_ai.view / JOIN ceo_ai_table
];
const IGNORE_RE =
  /ceo-ai-boundary:ignore:\s*owner=\S+\s+issue=\S+\s+scope=\S+\s+expiry=\d{4}-\d{2}-\d{2}/;

// Core backend runtime roots to scan, with the assistant-owned prefix that is
// allowed to reference ceo_ai. FE/mobile issue no SQL; their assistant boundary
// is documented, not machine-gated here.
const SCAN_ROOTS = [{ root: "backend/internal/", allow: ["backend/internal/ceoai/"] }];

const SCAN_EXT = /\.(go|sql)$/;

function isCommentLine(line) {
  const t = line.trim();
  return (
    t.startsWith("//") ||
    t.startsWith("--") ||
    t.startsWith("*") ||
    t.startsWith("/*") ||
    t.startsWith("#")
  );
}

function isExcluded(rel) {
  if (rel.endsWith("_test.go")) return true;
  if (/\.(test|spec)\.[jt]sx?$/.test(rel)) return true;
  if (/Test\.kt$/.test(rel)) return true;
  if (/(^|\/)(__tests__|node_modules|build|dist|\.next|generated)\//.test(rel)) return true;
  // the guard itself carries ceo_ai fixtures in its self-test
  if (rel.endsWith("tools/agent-hooks/check-ceo-ai-core-boundary.mjs")) return true;
  return false;
}

function scannedRoot(rel) {
  for (const entry of SCAN_ROOTS) {
    if (rel.startsWith(entry.root)) {
      if (entry.allow.some((a) => rel.startsWith(a))) return null; // assistant-owned, allowed
      return entry.root;
    }
  }
  return null;
}

// Returns [{line, text}] for offending non-comment lines in a source string.
export function findingsForSource(source) {
  const findings = [];
  const lines = source.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (isCommentLine(line)) continue;
    if (!REF_RES.some((re) => re.test(line))) continue;
    // allow a complete inline/preceding ignore directive
    const window = [line, lines[i - 1] ?? ""].join("\n");
    if (IGNORE_RE.test(window)) continue;
    findings.push({ line: i + 1, text: line.trim() });
  }
  return findings;
}

function trackedFiles() {
  const out = execSync("git ls-files", { cwd: repo, encoding: "utf8" });
  return out
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean)
    .filter((rel) => SCAN_EXT.test(rel))
    .filter((rel) => !isExcluded(rel))
    .filter((rel) => scannedRoot(rel) !== null);
}

function runScan() {
  const violations = [];
  for (const rel of trackedFiles()) {
    let src;
    try {
      src = readFileSync(resolve(repo, rel), "utf8");
    } catch {
      continue;
    }
    for (const f of findingsForSource(src)) {
      violations.push(`${rel}:${f.line}: core layer references ceo_ai reporting/assistant namespace -> ${f.text}`);
    }
  }
  if (violations.length > 0) {
    console.error("ceo-ai-boundary-guard: core BE/FE/mobile layers must not depend on the ceo_ai reporting/assistant namespace:\n");
    for (const v of violations) console.error("  " + v);
    console.error(
      "\nThe assistant reads core data (read APIs / MCP / read-only SQL); core layers must not read ceo_ai.*.",
    );
    console.error("Move the logic into a core Go/SQL source, or (if genuinely assistant-owned) into backend/internal/ceoai/**.");
    console.error("See docs/decisions/ceo-ai-reporting-boundary.md");
    process.exit(1);
  }
  console.log("ceo-ai-boundary-guard: no core-layer dependency on the ceo_ai reporting/assistant namespace");
}

function runSelfTest() {
  const cases = [
    { name: "sql_function_call", pass: false, src: "SELECT ceo_ai.vaccine_label_for(x) FROM t" },
    { name: "sql_from_view", pass: false, src: "  SELECT a FROM ceo_ai.animal_current_scope" },
    { name: "sql_join_table", pass: false, src: "  LEFT JOIN ceo_ai_messages m ON m.id = x" },
    { name: "go_comment_exempt", pass: true, src: "// former SQL ceo_ai.vaccine_label_for join, now in Go" },
    { name: "sql_comment_exempt", pass: true, src: "  -- reporting schema (ceo_ai) note" },
    { name: "i18n_copy_key_ok", pass: true, src: '"ceo_ai.title": "Ask Mesha",' },
    { name: "telemetry_name_ok", pass: true, src: 'Open: "ceo_ai_admin_trace_open",' },
    { name: "hyphen_doc_ref_ok", pass: true, src: 'log.Info("see docs/decisions/ceo-ai-reporting-boundary.md")' },
    { name: "unrelated_ok", pass: true, src: "SELECT vaccine_label FROM protocol_rules" },
    {
      name: "complete_ignore_passes",
      pass: true,
      src: "SELECT ceo_ai.foo(x) -- ceo-ai-boundary:ignore: owner=ravi issue=GH-1 scope=legacy expiry=2026-12-31",
    },
    { name: "incomplete_ignore_fails", pass: false, src: "SELECT ceo_ai.foo(x) -- ceo-ai-boundary:ignore: legacy" },
  ];
  let failures = 0;
  for (const tc of cases) {
    const clean = findingsForSource(tc.src).length === 0;
    if (clean !== tc.pass) {
      failures++;
      console.error(`ceo-ai-boundary-guard self-test ${tc.name}: got ${clean ? "pass" : "fail"}, want ${tc.pass ? "pass" : "fail"}`);
    }
  }
  if (failures > 0) {
    console.error(`ceo-ai-boundary-guard: ${failures} self-test(s) failed`);
    process.exit(1);
  }
  console.log(`ceo-ai-boundary-guard: all ${cases.length} self-tests passed`);
}

const mode = process.argv[2];
if (mode === "--self-test") {
  runSelfTest();
} else {
  runScan();
}
