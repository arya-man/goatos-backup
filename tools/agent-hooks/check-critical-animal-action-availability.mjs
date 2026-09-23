#!/usr/bin/env node
import { mkdirSync, mkdtempSync, rmSync, writeFileSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import process from "node:process";

const DOC = "docs/features/critical-animal-action-guardrails.md";
const REQUIRED_REFERENCES = [
  "AGENTS.md",
  "SKILLS.md",
  ".agents/skills/goatos-build/SKILL.md",
  "docs/architecture/operational-read-model-contract.md",
];
const REQUIRED_CODE_TOKENS = [
  ["backend/internal/identity/app/goat_lifecycle.go", "critical_action_guardrail_required"],
  ["backend/internal/identity/adapters/postgres/goat_lifecycle.go", "ErrGuardrailRequired"],
  ["backend/internal/identity/adapters/postgres/goat_relocate.go", "ErrClinicalDestinationTag"],
];


// The statuses the guardrail path MUST treat as critical. Token presence is not
// enough: measured 2026-09-23, deleting "quarantine" from criticalHealthStatus()
// in goat_lifecycle.go — so every quarantine transition skips the guardrail —
// left this guard green, because the literal
// `critical_action_guardrail_required` was still somewhere in the file. What
// matters is which statuses the function actually routes.
const CRITICAL_STATUS_FUNC = ["backend/internal/identity/app/goat_lifecycle.go", "criticalHealthStatus"];
const CRITICAL_STATUSES = ["quarantine", "icu"];

// Balanced-brace body of `func <name>(` in Go source, or null when absent.
export function goFunctionBody(source, name) {
  const start = source.search(new RegExp(`func\\s+${name}\\s*\\(`));
  if (start < 0) return null;
  const open = source.indexOf("{", start);
  if (open < 0) return null;
  let depth = 0;
  for (let i = open; i < source.length; i += 1) {
    if (source[i] === "{") depth += 1;
    else if (source[i] === "}") {
      depth -= 1;
      if (depth === 0) return source.slice(open + 1, i);
    }
  }
  return null;
}

export function criticalStatusErrors(source) {
  const [rel, fn] = CRITICAL_STATUS_FUNC;
  const body = goFunctionBody(source, fn);
  if (body === null) {
    return [`${rel}: ${fn}() not found — the critical-status routing this guard exists to protect is gone or renamed`];
  }
  const errors = [];
  for (const status of CRITICAL_STATUSES) {
    if (!new RegExp(`"${status}"`, "i").test(body)) {
      errors.push(
        `${rel}: ${fn}() no longer routes "${status}" to the critical-action guardrail — ${status} transitions would bypass it`,
      );
    }
  }
  return errors;
}

function read(root, rel) {
  return readFileSync(resolve(root, rel), "utf8");
}

function write(root, rel, text) {
  const path = resolve(root, rel);
  mkdirSync(resolve(path, ".."), { recursive: true });
  writeFileSync(path, text, "utf8");
}

function validate(root) {
  const errors = [];
  let doc = "";
  try {
    doc = read(root, DOC);
  } catch {
    return [`missing critical animal action guardrail doc: ${DOC}`];
  }
  for (const token of [
    "critical_action_guardrail_required",
    "quarantine",
    "ICU",
    "expected return date",
    "extension",
    "outbox",
    "process-integrity read models",
  ]) {
    if (!doc.includes(token)) errors.push(`${DOC} is missing required token: ${token}`);
  }
  for (const rel of REQUIRED_REFERENCES) {
    let text = "";
    try {
      text = read(root, rel);
    } catch {
      errors.push(`missing required critical-action reference file: ${rel}`);
      continue;
    }
    if (!text.includes(DOC)) errors.push(`${rel} must reference ${DOC}`);
  }
  for (const [rel, token] of REQUIRED_CODE_TOKENS) {
    let text = "";
    try {
      text = read(root, rel);
    } catch {
      errors.push(`missing critical-action guard file: ${rel}`);
      continue;
    }
    if (!text.includes(token)) errors.push(`${rel} must keep interim critical-action guard token: ${token}`);
  }
  try {
    errors.push(...criticalStatusErrors(read(root, CRITICAL_STATUS_FUNC[0])));
  } catch {
    errors.push(`missing critical-action guard file: ${CRITICAL_STATUS_FUNC[0]}`);
  }
  return errors;
}

function selfTest() {
  const root = mkdtempSync(resolve(tmpdir(), "critical-animal-guard-"));
  try {
    write(root, DOC, "critical_action_guardrail_required quarantine ICU expected return date extension outbox process-integrity read models\n");
    for (const rel of REQUIRED_REFERENCES) write(root, rel, `see ${DOC}\n`);
    for (const [rel, token] of REQUIRED_CODE_TOKENS) write(root, rel, `${token}\n`);
    const goodStatusFunc = [
      "func criticalHealthStatus(status string) bool {",
      "\tswitch strings.TrimSpace(status) {",
      '\tcase "quarantine", "icu":',
      "\t\treturn true",
      "\tdefault:",
      "\t\treturn false",
      "\t}",
      "}",
      "",
      "// critical_action_guardrail_required",
    ].join("\n");
    write(root, CRITICAL_STATUS_FUNC[0], goodStatusFunc);
    const passing = validate(root);
    if (passing.length) throw new Error(`self-test failed: complete fixture should pass: ${passing.join("; ")}`);
    write(root, REQUIRED_REFERENCES[0], "missing link\n");
    const missingReference = validate(root);
    if (!missingReference.some((error) => error.includes(`${REQUIRED_REFERENCES[0]} must reference ${DOC}`))) {
      throw new Error("self-test failed: missing reference was not detected");
    }
    write(root, REQUIRED_CODE_TOKENS[0][0], "ordinary health update\n");
    const missingCodeGuard = validate(root);
    if (!missingCodeGuard.some((error) => error.includes("critical-action guard token"))) {
      throw new Error("self-test failed: missing code guard was not detected");
    }
    // The planted violation that used to slip through: the token is still in the
    // file, but quarantine no longer routes to the guardrail.
    const droppedQuarantine = goodStatusFunc.replace('case "quarantine", "icu":', 'case "icu":');
    const droppedErrors = criticalStatusErrors(droppedQuarantine);
    if (!droppedErrors.some((error) => error.includes('no longer routes "quarantine"'))) {
      throw new Error(`self-test failed: dropping quarantine from criticalHealthStatus was not detected: ${droppedErrors.join("; ")}`);
    }
    if (criticalStatusErrors(goodStatusFunc).length !== 0) {
      throw new Error("self-test failed: a correct criticalHealthStatus was flagged");
    }
    if (criticalStatusErrors("package app\n").length === 0) {
      throw new Error("self-test failed: a missing criticalHealthStatus was not detected");
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
  console.log("check-critical-animal-action-availability: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const errors = validate(process.cwd());
if (errors.length) {
  console.error("Critical animal action availability guard failed:");
  for (const error of errors) console.error(`- ${error}`);
  process.exit(1);
}
console.log("Critical animal action availability guard passed.");
