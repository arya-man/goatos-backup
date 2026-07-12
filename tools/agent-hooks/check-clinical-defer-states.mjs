#!/usr/bin/env node

// check-clinical-defer-states.mjs — blocks the C35-010 medical-safety anti-pattern:
// a PARTIAL clinical defer_states list in production code/seeds. Per
// docs/preventive-care-vaccination/vaccination-rules.md the four clinical states
// (sick, under_treatment, quarantine, icu) are mandatory safety blocks, not
// optional planner choices: an animal in any of them must have its open
// vaccination work DEFERRED (held for recovery), never scheduled/cancelled.
//
// A non-empty defer_states literal in a production file (Go seed/DSL or SQL
// seed) that omits any mandatory clinical state is a violation. An empty list
// (`[]`) or an absent key is allowed — it maps to the engine's safe full default
// (see backend/internal/protocol/domain/clinical_defer.go). Test files are NOT
// scanned: negative tests legitimately carry partial fixtures to prove rejection.
//
// The guard also asserts the canonical Go constant still lists exactly the four
// mandatory states, so nobody can silently weaken the single source of truth.
//
// Modes:
//   (default)     scan the whole production tree and fail on any violation.
//   --self-test   run the built-in adversarial fixtures and exit.
//
// Escape hatch: a genuinely-bounded case may append
// `clinical-defer-guard:ignore: <reason>` on the offending line.

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

export const MANDATORY_CLINICAL_DEFER_STATES = ["sick", "under_treatment", "quarantine", "icu"];

// Match a defer_states literal: JSON array (`"defer_states":[...]`) or a Go slice
// literal (`defer_states": []string{...}` / `[]any{...}`). Capture the inner tokens.
const DEFER_JSON = /"defer_states"\s*:\s*\[([^\]]*)\]/g;
const DEFER_GO = /defer_states"?\s*:\s*\[\](?:string|any)\{([^}]*)\}/g;

function tokensFrom(inner) {
  return inner
    .split(",")
    .map((t) => t.replace(/["'\s]/g, "").toLowerCase())
    .filter((t) => t.length > 0);
}

function missingMandatory(tokens) {
  return MANDATORY_CLINICAL_DEFER_STATES.filter((m) => !tokens.includes(m));
}

// Returns array of findings: { line, tokens, missing }.
export function findingsForSource(source) {
  const findings = [];
  const lines = source.split("\n");
  lines.forEach((text, i) => {
    if (/clinical-defer-guard:ignore/.test(text)) return;
    for (const re of [DEFER_JSON, DEFER_GO]) {
      re.lastIndex = 0;
      let m;
      while ((m = re.exec(text)) !== null) {
        const tokens = tokensFrom(m[1]);
        if (tokens.length === 0) continue; // empty list -> safe default
        const missing = missingMandatory(tokens);
        if (missing.length > 0) {
          findings.push({ line: i + 1, tokens, missing });
        }
      }
    }
  });
  return findings;
}

function trackedProductionFiles() {
  const out = execSync("git ls-files -- 'backend/**/*.go' 'backend/**/*.sql'", {
    cwd: repo,
    encoding: "utf8",
  });
  return out
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean)
    .filter((rel) => !rel.endsWith("_test.go"));
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
    { name: "json_full_set", pass: true, src: '"defer_states":["sick","under_treatment","quarantine","icu"]' },
    { name: "json_superset", pass: true, src: '"defer_states":["sick","under_treatment","quarantine","icu","post_breeding_hold"]' },
    { name: "json_case_insensitive", pass: true, src: '"defer_states":["Sick","UNDER_TREATMENT","ICU","Quarantine"]' },
    { name: "json_empty", pass: true, src: '"defer_states":[]' },
    { name: "json_icu_quarantine_partial", pass: false, src: '"defer_states":["icu","quarantine"]' },
    { name: "json_drops_under_treatment", pass: false, src: '"defer_states":["sick","quarantine","icu"]' },
    { name: "json_single", pass: false, src: '"defer_states":["sick"]' },
    { name: "go_full_set", pass: true, src: 'defer_states": []string{"sick", "under_treatment", "icu", "quarantine"}' },
    { name: "go_partial", pass: false, src: 'defer_states": []any{"icu", "quarantine"}' },
    { name: "ignore_escape_hatch", pass: true, src: '"defer_states":["icu"] // clinical-defer-guard:ignore: legacy fixture' },
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

  for (const rel of trackedProductionFiles()) {
    let src;
    try {
      src = readFileSync(resolve(repo, rel), "utf8");
    } catch {
      continue;
    }
    for (const f of findingsForSource(src)) {
      violations.push(
        `${rel}:${f.line}: partial clinical defer_states [${f.tokens.join(
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
  console.log("clinical-defer-guard: no partial clinical defer_states in production code/seeds");
}

const mode = process.argv[2];
if (mode === "--self-test") {
  runSelfTest();
} else {
  runScan();
}
