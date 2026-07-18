#!/usr/bin/env node

// check-clinical-defer-states.mjs — blocks the C35-010 medical-safety anti-pattern:
// a PARTIAL clinical defer_states list on ANY authoring or seed surface. Per
// docs/preventive-care-vaccination/vaccination-rules.md the five clinical states
// (sick, under_treatment, recovering, quarantine, icu) are mandatory safety blocks, not
// optional planner choices: an animal in any of them must have its open
// vaccination work DEFERRED (held for recovery), never scheduled/cancelled.
//
// A non-empty defer_states literal that omits any mandatory clinical state is a
// violation. An empty list (`[]`) or an absent key is allowed — it maps to the
// engine's safe full default (backend/internal/protocol/domain/clinical_defer.go).
//
// Coverage (this is a whole-FILE scan, not line-by-line, so multiline literals
// are caught) across every authoring/seed surface:
//   - JSON / JS / TS object form:  "defer_states": [ ... ]  /  deferStates: [ ... ]
//   - Go struct literal:           DeferStates: []string{ ... } / []any{ ... }
//   - raw-string JSON in Go seeds, admin-web .ts/.tsx, the ops-console mock HTML.
// Test files are excluded: negative tests legitimately carry partial fixtures.
//
// The guard also asserts the canonical Go constant still lists exactly the five
// mandatory states, so nobody can silently weaken the single source of truth.
//
// Modes:
//   (default)     scan the whole tree and fail on any violation.
//   --self-test   run the built-in adversarial fixtures (incl. every historical
//                 bypass: multiline JSON, Go struct literal, TS/JS, mock HTML).
//
// Exception (must be COMPLETE — owner, issue, scope, and expiry all present):
//   clinical-defer-guard:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

export const MANDATORY_CLINICAL_DEFER_STATES = ["sick", "under_treatment", "recovering", "quarantine", "icu"];

// Whole-file, newline-spanning matchers. Each captures the array/brace inner text.
const MATCHERS = [
  // Go slice literal for either key form: `"defer_states": []string{...}` (the
  // real seed-vaccination-real map shape) or `DeferStates: []any{...}` (struct
  // field). Checked first; the JSON matcher would see `[]` here and skip.
  {
    name: "go_slice_defer",
    re: /(?:["']?defer_states["']?|\bDeferStates)\s*:\s*\[\](?:string|any)\{([\s\S]*?)\}/g,
  },
  // JSON / JS / TS object key: "defer_states": [...]  or  defer_states: [...]
  { name: "json_defer_states", re: /["']?defer_states["']?\s*:\s*\[([\s\S]*?)\]/gi },
  // camelCase TS/JS: deferStates: [...]  (NOT `deferStates: string[]`, which has
  // no `[` immediately after the colon).
  { name: "camel_deferStates", re: /\bdeferStates\s*:\s*\[([\s\S]*?)\]/g },
  // JS `||` fallback default: `e.defer_states || [...]` / `deferStates || [...]`.
  {
    name: "js_or_fallback",
    re: /(?:defer_states|deferStates)\s*\|\|\s*\[([\s\S]*?)\]/gi,
  },
];

const IGNORE_RE = /clinical-defer-guard:ignore:\s*owner=\S+\s+issue=\S+\s+scope=\S+\s+expiry=\d{4}-\d{2}-\d{2}/;

function tokensFrom(inner) {
  return inner
    .split(",")
    .map((t) => t.replace(/["'`\s]/g, "").toLowerCase())
    .filter((t) => t.length > 0);
}

function missingMandatory(tokens) {
  return MANDATORY_CLINICAL_DEFER_STATES.filter((m) => !tokens.includes(m));
}

function lineOfIndex(source, index) {
  return source.slice(0, index).split("\n").length; // 1-indexed
}

// A match is exempt if a COMPLETE ignore directive sits on the match's line or
// the line immediately above it.
function isExempt(source, matchIndex) {
  const lines = source.split("\n");
  const ln = lineOfIndex(source, matchIndex); // 1-indexed
  const window = [lines[ln - 1] ?? "", lines[ln - 2] ?? ""].join("\n");
  return IGNORE_RE.test(window);
}

// Returns array of findings: { line, tokens, missing, matcher }.
export function findingsForSource(source) {
  const findings = [];
  for (const { name, re } of MATCHERS) {
    re.lastIndex = 0;
    let m;
    while ((m = re.exec(source)) !== null) {
      const tokens = tokensFrom(m[1]);
      if (tokens.length === 0) continue; // empty list -> safe default
      const missing = missingMandatory(tokens);
      if (missing.length === 0) continue;
      if (isExempt(source, m.index)) continue;
      findings.push({ line: lineOfIndex(source, m.index), tokens, missing, matcher: name });
    }
  }
  return findings;
}

const SCAN_GLOBS = [
  "*.go",
  "*.sql",
  "*.json",
  "*.ts",
  "*.tsx",
  "*.js",
  "*.jsx",
  "*.mjs",
  "*.html",
];

function isTestOrExcluded(rel) {
  if (rel.endsWith("_test.go")) return true;
  if (/\.(test|spec)\.[jt]sx?$/.test(rel)) return true;
  if (/(^|\/)(__tests__|node_modules|build|dist|\.next)\//.test(rel)) return true;
  // the guard itself carries partial fixtures in its adversarial self-test
  if (rel.endsWith("tools/agent-hooks/check-clinical-defer-states.mjs")) return true;
  return false;
}

function trackedScanFiles() {
  const args = SCAN_GLOBS.map((g) => `'${g}'`).join(" ");
  const out = execSync(`git ls-files -- ${args}`, { cwd: repo, encoding: "utf8" });
  return out
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean)
    .filter((rel) => !isTestOrExcluded(rel));
}

function assertCanonicalConstantIntact() {
  const rel = "backend/internal/protocol/domain/clinical_defer.go";
  const src = readFileSync(resolve(repo, rel), "utf8");
  const m = src.match(/MandatoryClinicalDeferStates\s*=\s*\[\]string\{([^}]*)\}/);
  if (!m) {
    return `${rel}: canonical MandatoryClinicalDeferStates constant not found — the single source of truth was removed or renamed`;
  }
  const tokens = tokensFrom(m[1]);
  const missing = missingMandatory(tokens);
  const extra = tokens.filter((t) => !MANDATORY_CLINICAL_DEFER_STATES.includes(t));
  if (missing.length > 0 || extra.length > 0) {
    return `${rel}: canonical MandatoryClinicalDeferStates = [${tokens.join(
      ", ",
    )}], must be exactly [${MANDATORY_CLINICAL_DEFER_STATES.join(", ")}]`;
  }
  return null;
}

function runSelfTest() {
  const cases = [
    { name: "json_full_set", pass: true, src: '"defer_states":["sick","under_treatment","recovering","quarantine","icu"]' },
    { name: "json_superset", pass: true, src: '"defer_states":["sick","under_treatment","recovering","quarantine","icu","post_breeding_hold"]' },
    { name: "json_case_insensitive", pass: true, src: '"defer_states":["Sick","UNDER_TREATMENT","Recovering","ICU","Quarantine"]' },
    { name: "json_empty", pass: true, src: '"defer_states":[]' },
    { name: "json_partial", pass: false, src: '"defer_states":["icu","quarantine"]' },
    // historical bypass 1: multiline JSON
    { name: "json_multiline_partial", pass: false, src: '{\n  "defer_states": [\n    "icu",\n    "quarantine"\n  ]\n}' },
    { name: "json_multiline_full", pass: true, src: '{\n  "defer_states": [\n    "sick",\n    "under_treatment",\n    "recovering",\n    "icu",\n    "quarantine"\n  ]\n}' },
    // historical bypass 2: Go struct literal (field-name form, no `defer_states` string)
    { name: "go_struct_partial", pass: false, src: 'Eligibility{DeferStates: []string{"icu", "quarantine"}}' },
    { name: "go_struct_full", pass: true, src: 'Eligibility{DeferStates: []string{"sick", "under_treatment", "recovering", "icu", "quarantine"}}' },
    { name: "go_struct_any_partial", pass: false, src: 'DeferStates: []any{"icu", "quarantine"}' },
    // real seed-vaccination-real shape: quoted JSON key + Go slice value
    { name: "go_seed_map_partial", pass: false, src: '"defer_states": []string{"sick", "quarantine", "icu"}' },
    { name: "go_seed_map_full", pass: true, src: '"defer_states": []string{"sick", "under_treatment", "recovering", "quarantine", "icu"}' },
    // historical bypass 3: TS/JS camelCase and object literal
    { name: "ts_camel_partial", pass: false, src: "const e = { deferStates: ['icu', 'quarantine'] };" },
    { name: "ts_type_decl_ok", pass: true, src: "interface E { deferStates: string[]; }" },
    { name: "js_mock_partial", pass: false, src: "_elig(o){return {defer_states:['ICU','quarantine','sick']};}" },
    { name: "js_mock_full", pass: true, src: "_elig(o){return {defer_states:['ICU','quarantine','sick','under_treatment','recovering']};}" },
    { name: "js_or_fallback_partial", pass: false, src: "var def=e.defer_states||['ICU','quarantine','sick'];" },
    { name: "js_or_fallback_full", pass: true, src: "var def=e.defer_states||['ICU','quarantine','sick','under_treatment','recovering'];" },
    // exception handling: complete ignore passes, incomplete ignore still fails
    {
      name: "complete_ignore_passes",
      pass: true,
      src: '"defer_states":["icu"] // clinical-defer-guard:ignore: owner=ravi issue=GH-123 scope=legacy-demo expiry=2026-12-31',
    },
    {
      name: "incomplete_ignore_fails",
      pass: false,
      src: '"defer_states":["icu"] // clinical-defer-guard:ignore: legacy',
    },
  ];
  let failures = 0;
  for (const tc of cases) {
    const findings = findingsForSource(tc.src);
    const clean = findings.length === 0;
    if (clean !== tc.pass) {
      failures++;
      console.error(
        `clinical-defer-guard self-test ${tc.name}: got ${clean ? "pass" : "fail"}, want ${tc.pass ? "pass" : "fail"} (findings=${JSON.stringify(findings)})`,
      );
    }
  }
  if (failures > 0) {
    console.error(`clinical-defer-guard: ${failures} self-test(s) failed`);
    process.exit(1);
  }
  console.log(`clinical-defer-guard: all ${cases.length} self-tests passed`);
}

function runScan() {
  const violations = [];
  const constErr = assertCanonicalConstantIntact();
  if (constErr) violations.push(constErr);

  for (const rel of trackedScanFiles()) {
    let src;
    try {
      src = readFileSync(resolve(repo, rel), "utf8");
    } catch {
      continue;
    }
    for (const f of findingsForSource(src)) {
      violations.push(
        `${rel}:${f.line}: [${f.matcher}] partial clinical defer_states [${f.tokens.join(
          ", ",
        )}] omits mandatory safety state(s) [${f.missing.join(
          ", ",
        )}]; these must be deferred, not cancelled (leave the list empty for the safe default, or add the missing states)`,
      );
    }
  }

  if (violations.length > 0) {
    console.error("clinical-defer-guard: medical-safety violations found (C35-010):\n");
    for (const v of violations) console.error("  " + v);
    console.error(
      "\nSee docs/preventive-care-vaccination/vaccination-rules.md and backend/internal/protocol/domain/clinical_defer.go",
    );
    process.exit(1);
  }
  console.log("clinical-defer-guard: no partial clinical defer_states across authoring/seed surfaces");
}

const mode = process.argv[2];
if (mode === "--self-test") {
  runSelfTest();
} else {
  runScan();
}
