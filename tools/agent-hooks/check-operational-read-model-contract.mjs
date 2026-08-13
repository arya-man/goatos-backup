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

const COUNT_BY_WORK_STATE_ALLOWED_FIELDS = new Set(["work_state", "count"]);

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
  validateCountByWorkStateContract(root, errors);

  return errors;
}

function schemaBlock(text, schemaName) {
  const marker = `    ${schemaName}:`;
  const start = text.indexOf(marker);
  if (start === -1) return "";
  const rest = text.slice(start + marker.length);
  const next = rest.search(/\n    [A-Za-z0-9_]+:\n/);
  return next === -1 ? rest : rest.slice(0, next);
}

function openApiProperties(block) {
  const propertiesStart = block.indexOf("\n      properties:");
  if (propertiesStart === -1) return [];
  const rest = block.slice(propertiesStart + "\n      properties:".length);
  const nextSection = rest.search(/\n      [A-Za-z0-9_]+:/);
  const propertiesBlock = nextSection === -1 ? rest : rest.slice(0, nextSection);
  return [...propertiesBlock.matchAll(/\n        ([A-Za-z0-9_]+):/g)].map((match) => match[1]);
}

function tsSchemaFields(text, schemaName) {
  const marker = `        ${schemaName}: {`;
  const start = text.indexOf(marker);
  if (start === -1) return [];
  const rest = text.slice(start + marker.length);
  const end = rest.indexOf("\n        };");
  const block = end === -1 ? rest : rest.slice(0, end);
  return [...block.matchAll(/\n            ([A-Za-z0-9_]+)\??:/g)].map((match) => match[1]);
}

function validateCountByWorkStateContract(root, errors) {
  let openApi = "";
  let generatedTs = "";
  try {
    openApi = read(root, "contracts/openapi/app-api.yaml");
  } catch {
    errors.push("missing OpenAPI app contract: contracts/openapi/app-api.yaml");
    return;
  }
  try {
    generatedTs = read(root, "packages/api-client/src/generated/app-api.ts");
  } catch {
    errors.push("missing generated app API client: packages/api-client/src/generated/app-api.ts");
    return;
  }
  const openApiFields = openApiProperties(schemaBlock(openApi, "CountByWorkState"));
  const tsFields = tsSchemaFields(generatedTs, "CountByWorkState");
  for (const [source, fields] of [["OpenAPI", openApiFields], ["generated TS", tsFields]]) {
    const extras = fields.filter((field) => !COUNT_BY_WORK_STATE_ALLOWED_FIELDS.has(field));
    const missing = [...COUNT_BY_WORK_STATE_ALLOWED_FIELDS].filter((field) => !fields.includes(field));
    if (missing.length > 0 || extras.length > 0) {
      errors.push(`CountByWorkState ${source} fields must be work_state + count only; missing=[${missing.join(", ")}] extra=[${extras.join(", ")}]`);
    }
  }
}

function selfTest() {
  const root = mkdtempSync(resolve(tmpdir(), "orm-contract-guard-"));
  try {
    write(root, CONTRACT_DOC, REQUIRED_DOC_TOKENS.join("\n"));
    for (const rel of REQUIRED_REFERENCES) {
      write(root, rel, `see ${CONTRACT_DOC}\n`);
    }
    write(root, "contracts/openapi/app-api.yaml", `
components:
  schemas:
    CountByWorkState:
      type: object
      required:
        - work_state
        - count
      properties:
        work_state:
          type: string
        count:
          type: integer
`);
    write(root, "packages/api-client/src/generated/app-api.ts", `
export interface components {
    schemas: {
        CountByWorkState: {
            work_state: string;
            count: number;
        };
    };
}
`);
    const passing = validate(root);
    if (passing.length > 0) {
      throw new Error(`self-test failed: complete fixture should pass: ${passing.join("; ")}`);
    }
    write(root, "AGENTS.md", "missing reference\n");
    const missingReference = validate(root);
    if (!missingReference.some((error) => error.includes(`AGENTS.md must reference ${CONTRACT_DOC}`))) {
      throw new Error("self-test failed: validate did not detect a missing reference");
    }
    write(root, "packages/api-client/src/generated/app-api.ts", `
export interface components {
    schemas: {
        CountByWorkState: {
            work_state: string;
            count: number;
            drive_capacity_state?: string;
        };
    };
}
`);
    const staleGenerated = validate(root);
    if (!staleGenerated.some((error) => error.includes("CountByWorkState generated TS fields"))) {
      throw new Error("self-test failed: validate did not detect stale generated CountByWorkState fields");
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
  console.log("check-operational-read-model-contract: self-test passed");
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

console.log("Operational read model contract guard passed.");
