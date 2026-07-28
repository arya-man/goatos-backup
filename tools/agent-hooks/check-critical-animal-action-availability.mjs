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
  return errors;
}

function selfTest() {
  const root = mkdtempSync(resolve(tmpdir(), "critical-animal-guard-"));
  try {
    write(root, DOC, "critical_action_guardrail_required quarantine ICU expected return date extension outbox process-integrity read models\n");
    for (const rel of REQUIRED_REFERENCES) write(root, rel, `see ${DOC}\n`);
    for (const [rel, token] of REQUIRED_CODE_TOKENS) write(root, rel, `${token}\n`);
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
