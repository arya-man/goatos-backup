#!/usr/bin/env node
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import process from "node:process";

const CONTRACT_DOC = "docs/architecture/operational-read-model-contract.md";
const REQUIRED_DOC_TOKENS = [
  "Contract Invariant",
  "Grain Invariant",
  "Pagination Invariant",
  "Scope Invariant",
  "Cross-Surface Golden Fixtures",
  "Vertical Onboarding Checklist",
];

const REQUIRED_REFERENCES = [
  "AGENTS.md",
  "SKILLS.md",
  ".agents/skills/goatos-build/SKILL.md",
  ".agents/skills/goatos-build/references/architecture.md",
  ".agents/skills/goatos-build/references/contracts-events.md",
  ".agents/skills/goatos-build/references/frontend-mobile.md",
  ".agents/skills/goatos-code-review/SKILL.md",
  ".agents/skills/goatos-code-review/references/aggregates-and-projections.md",
  ".agents/skills/goatos-code-review/references/frontend.md",
  ".agents/skills/goatos-code-review/references/mobile.md",
  "docs/engineering/backend-go-postgres-quality.md",
  "docs/frontend/admin-web-engineering-quality.md",
  "docs/mobile/README.md",
  "docs/runbooks/local-ci.md",
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
    doc = read(root, CONTRACT_DOC);
  } catch {
    return [`missing operational read model contract: ${CONTRACT_DOC}`];
  }

  for (const token of REQUIRED_DOC_TOKENS) {
    if (!doc.includes(token)) {
      errors.push(`${CONTRACT_DOC} is missing required section/token: ${token}`);
    }
  }

  for (const rel of REQUIRED_REFERENCES) {
    let text = "";
    try {
      text = read(root, rel);
    } catch {
      errors.push(`missing required operational-read-model reference file: ${rel}`);
      continue;
    }
    if (!text.includes(CONTRACT_DOC)) {
      errors.push(`${rel} must reference ${CONTRACT_DOC}`);
    }
  }

  return errors;
}

function selfTest() {
  const root = mkdtempSync(resolve(tmpdir(), "orm-contract-guard-"));
  try {
    write(root, CONTRACT_DOC, REQUIRED_DOC_TOKENS.join("\n"));
    for (const rel of REQUIRED_REFERENCES) {
      write(root, rel, `see ${CONTRACT_DOC}\n`);
    }
    const passing = validate(root);
    if (passing.length > 0) {
      throw new Error(`self-test failed: complete fixture should pass: ${passing.join("; ")}`);
    }
    write(root, "AGENTS.md", "missing reference\n");
    const missingReference = validate(root);
    if (!missingReference.some((error) => error.includes(`AGENTS.md must reference ${CONTRACT_DOC}`))) {
      throw new Error("self-test failed: validate did not detect a missing reference");
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
  console.log("check-operational-read-model-contract: self-test passed (discoverability only)");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const root = process.cwd();
const errors = validate(root);
if (errors.length > 0) {
  console.error("Operational read model contract guard failed:");
  for (const error of errors) console.error(`- ${error}`);
  process.exit(1);
}

console.log("Operational read model contract discoverability guard passed. Semantic Go/OpenAPI/TS/Kotlin/UI compliance is covered by targeted tests, not this guard.");
