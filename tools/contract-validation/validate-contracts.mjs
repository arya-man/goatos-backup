import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

import SwaggerParser from "@apidevtools/swagger-parser";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { parse as parseYaml } from "yaml";

const toolDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(toolDir, "../..");

const requiredFiles = [
  "contracts/jsonschema/domain-event-envelope.schema.json",
  "contracts/jsonschema/decision-record.schema.json",
  "contracts/openapi/app-api.yaml",
  "contracts/openapi/admin-api.yaml",
  "contracts/openapi/analytics-api.yaml"
];

const exampleChecks = [
  {
    file: "contracts/examples/events/event-envelope-goat-identifier-added.json",
    kind: "jsonschema",
    schemaFile: "contracts/jsonschema/domain-event-envelope.schema.json"
  },
  {
    file: "contracts/examples/decisions/decision-record-merge-approved.json",
    kind: "jsonschema",
    schemaFile: "contracts/jsonschema/decision-record.schema.json"
  },
  {
    file: "contracts/examples/app/goat-search-response.json",
    kind: "openapi",
    specFile: "contracts/openapi/app-api.yaml",
    schemaName: "GoatSearchResponse"
  },
  {
    file: "contracts/examples/app/goat-passport-response.json",
    kind: "openapi",
    specFile: "contracts/openapi/app-api.yaml",
    schemaName: "GoatPassportResponse"
  },
  {
    file: "contracts/examples/admin/conflict-review-response.json",
    kind: "openapi",
    specFile: "contracts/openapi/admin-api.yaml",
    schemaName: "ConflictDetailResponse"
  },
  {
    file: "contracts/examples/admin/resolve-conflict-merge-response.json",
    kind: "openapi",
    specFile: "contracts/openapi/admin-api.yaml",
    schemaName: "ResolveConflictResponse"
  },
  {
    file: "contracts/examples/admin/idempotent-replay-response.json",
    kind: "openapi",
    specFile: "contracts/openapi/admin-api.yaml",
    schemaName: "ResolveConflictResponse"
  },
  {
    file: "contracts/examples/analytics/identity-counts-response.json",
    kind: "openapi",
    specFile: "contracts/openapi/analytics-api.yaml",
    schemaName: "IdentityCountsResponse"
  },
  {
    file: "contracts/examples/shared/error-envelope.json",
    kind: "openapi",
    specFile: "contracts/openapi/app-api.yaml",
    schemaName: "ErrorEnvelope"
  }
];

function resolveRepo(...segments) {
  return path.join(repoRoot, ...segments);
}

async function readJson(relativePath) {
  const raw = await fs.readFile(resolveRepo(relativePath), "utf8");
  return JSON.parse(raw);
}

async function readYaml(relativePath) {
  const raw = await fs.readFile(resolveRepo(relativePath), "utf8");
  return parseYaml(raw);
}

async function fileExists(relativePath) {
  try {
    await fs.access(resolveRepo(relativePath));
    return true;
  } catch {
    return false;
  }
}

async function findFiles(relativeDir, predicate) {
  const absoluteDir = resolveRepo(relativeDir);
  const entries = await fs.readdir(absoluteDir, { withFileTypes: true });
  const files = [];

  for (const entry of entries) {
    const childRelative = path.join(relativeDir, entry.name);
    if (entry.isDirectory()) {
      files.push(...await findFiles(childRelative, predicate));
    } else if (predicate(childRelative)) {
      files.push(childRelative);
    }
  }

  return files.sort();
}

function createAjv() {
  const ajv = new Ajv2020({
    allErrors: true,
    strict: false,
    validateSchema: true
  });
  addFormats(ajv);
  return ajv;
}

function assertValid(validator, payload, label) {
  if (validator(payload)) {
    return;
  }

  const details = (validator.errors ?? [])
    .map((error) => `${error.instancePath || "/"} ${error.message}`)
    .join("\n");
  throw new Error(`${label} failed validation:\n${details}`);
}

async function validateRequiredFiles() {
  const missing = [];
  for (const relativePath of requiredFiles) {
    if (!await fileExists(relativePath)) {
      missing.push(relativePath);
    }
  }

  if (missing.length > 0) {
    throw new Error(`Missing required contract files:\n${missing.join("\n")}`);
  }
}

async function validateJsonSchemas() {
  const schemaFiles = await findFiles("contracts/jsonschema", (file) => file.endsWith(".schema.json"));
  const ajv = createAjv();

  for (const schemaFile of schemaFiles) {
    const schema = await readJson(schemaFile);
    ajv.compile(schema);
  }

  return schemaFiles.length;
}

async function validateOpenApiSpecs() {
  const specFiles = await findFiles("contracts/openapi", (file) => file.endsWith(".yaml") || file.endsWith(".yml") || file.endsWith(".json"));

  for (const specFile of specFiles) {
    await SwaggerParser.validate(resolveRepo(specFile));
    await readYaml(specFile);
  }

  return specFiles.length;
}

async function dereferenceSpec(specFile, cache) {
  if (!cache.has(specFile)) {
    cache.set(specFile, await SwaggerParser.dereference(resolveRepo(specFile), { dereference: { circular: "ignore" } }));
  }
  return cache.get(specFile);
}

async function validateExamples() {
  const allExamples = await findFiles("contracts/examples", (file) => file.endsWith(".json"));
  const covered = new Set(exampleChecks.map((check) => check.file));
  const uncovered = allExamples.filter((file) => !covered.has(file));

  if (uncovered.length > 0) {
    throw new Error(`Contract examples need explicit schema checks:\n${uncovered.join("\n")}`);
  }

  const derefCache = new Map();
  let count = 0;

  for (const check of exampleChecks) {
    const payload = await readJson(check.file);
    const ajv = createAjv();

    if (check.kind === "jsonschema") {
      const schema = await readJson(check.schemaFile);
      const validator = ajv.compile(schema);
      assertValid(validator, payload, check.file);
    } else if (check.kind === "openapi") {
      const spec = await dereferenceSpec(check.specFile, derefCache);
      const schema = spec.components?.schemas?.[check.schemaName];
      if (!schema) {
        throw new Error(`Missing OpenAPI schema ${check.schemaName} in ${check.specFile}`);
      }
      const validator = ajv.compile(schema);
      assertValid(validator, payload, check.file);
    } else {
      throw new Error(`Unknown example check kind: ${check.kind}`);
    }

    count += 1;
  }

  return count;
}

async function main() {
  await validateRequiredFiles();
  const jsonSchemaCount = await validateJsonSchemas();
  const openApiCount = await validateOpenApiSpecs();
  const exampleCount = await validateExamples();

  console.log(`Validated ${openApiCount} OpenAPI specs, ${jsonSchemaCount} JSON Schemas, and ${exampleCount} example payloads.`);
  console.log("Generated-client drift checks deferred until generated clients exist.");
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
});
