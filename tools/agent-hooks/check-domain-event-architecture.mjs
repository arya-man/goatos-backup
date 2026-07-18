#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const REQUIRED_EVENTS = [
  "goat.created",
  "goat.location.changed",
  "goat.stage_changed",
  "goat.health.changed",
  "goat.reproductive.changed",
  "goat.identity.changed",
  "vaccination.completed",
  "obligation.missed",
  "goat.obligations_canceled",
  "obligation.rescoped",
  "vaccination.manual_campaign.requested",
  "protocol.version.published",
];

const REQUIRED_FEATURES = ["shifting", "dead_birth", "feed_direction"];
const REQUIRED_SURFACES = ["backend", "frontend", "mobile"];

function readText(root, rel) {
  return fs.readFileSync(path.join(root, rel), "utf8");
}

function exists(root, rel) {
  return fs.existsSync(path.join(root, rel));
}

function walk(root, rel = "") {
  const abs = path.join(root, rel);
  const out = [];
  for (const entry of fs.readdirSync(abs, { withFileTypes: true })) {
    if (entry.name === ".git" || entry.name === "node_modules" || entry.name === "dist" || entry.name === "build") {
      continue;
    }
    const childRel = path.join(rel, entry.name);
    if (entry.isDirectory()) {
      out.push(...walk(root, childRel));
    } else {
      out.push(childRel.split(path.sep).join("/"));
    }
  }
  return out;
}

function requireArray(value, name, errors) {
  if (!Array.isArray(value) || value.length === 0) {
    errors.push(`${name} must be a non-empty array`);
    return [];
  }
  return value;
}

function validateRegistry(root, registry, files = walk(root), read = (rel) => readText(root, rel), fileExists = (rel) => exists(root, rel)) {
  const errors = [];

  if (!registry || typeof registry !== "object") {
    return ["registry is not an object"];
  }
  if (!registry.contractDoc || !fileExists(registry.contractDoc)) {
    errors.push("contractDoc must point at a real file");
  } else {
    const doc = read(registry.contractDoc).toLowerCase();
    for (const token of ["backend", "frontend", "mobile", "outbox", "idempotent", "dlq", "e2e"]) {
      if (!doc.includes(token)) {
        errors.push(`contractDoc is missing required token ${token}`);
      }
    }
  }

  for (const surface of REQUIRED_SURFACES) {
    if (!registry.surfaceRules || !String(registry.surfaceRules[surface] || "").trim()) {
      errors.push(`surfaceRules.${surface} is required`);
    }
  }

  const events = requireArray(registry.events, "events", errors);
  const byType = new Map();
  for (const event of events) {
    if (!event.eventType) {
      errors.push("event entry missing eventType");
      continue;
    }
    if (byType.has(event.eventType)) {
      errors.push(`duplicate eventType ${event.eventType}`);
    }
    byType.set(event.eventType, event);

    for (const surface of REQUIRED_SURFACES) {
      if (!Array.isArray(event.surfaces) || !event.surfaces.includes(surface)) {
        errors.push(`${event.eventType} must declare ${surface} surface impact`);
      }
    }
    for (const [field, label] of [
      ["producerFiles", "producer file"],
      ["consumers", "consumer"],
      ["e2eProof", "E2E proof"],
    ]) {
      if (!Array.isArray(event[field]) || event[field].length === 0) {
        errors.push(`${event.eventType} missing ${label} registration`);
      }
    }
    for (const rel of event.producerFiles || []) {
      if (!fileExists(rel)) {
        errors.push(`${event.eventType} producer file not found: ${rel}`);
      }
    }
    for (const proof of event.e2eProof || []) {
      if (!fileExists(proof)) {
        errors.push(`${event.eventType} E2E/integration proof not found: ${proof}`);
      }
    }
    if (!event.owningDoc || !fileExists(event.owningDoc)) {
      errors.push(`${event.eventType} owningDoc not found`);
    }
    const registeredFiles = [
      ...(event.producerFiles || []),
      ...(event.consumers || []).map((consumer) => consumer.file).filter(Boolean),
    ];
    const combined = registeredFiles.filter((rel) => fileExists(rel)).map((rel) => read(rel)).join("\n");
    if (!combined.includes(event.eventType)) {
      errors.push(`${event.eventType} is not present in registered producer/consumer files`);
    }
  }

  for (const eventType of REQUIRED_EVENTS) {
    if (!byType.has(eventType)) {
      errors.push(`required eventType ${eventType} is missing from registry`);
    }
  }
  for (const eventType of registry.requiredEventTypes || []) {
    if (!byType.has(eventType)) {
      errors.push(`requiredEventTypes includes unregistered event ${eventType}`);
    }
  }

  const featureContracts = requireArray(registry.futureFeatureContracts, "futureFeatureContracts", errors);
  const featureNames = new Set(featureContracts.map((feature) => feature.feature));
  for (const feature of REQUIRED_FEATURES) {
    if (!featureNames.has(feature)) {
      errors.push(`future feature ${feature} is missing from registry`);
    }
  }
  for (const feature of featureContracts) {
    for (const surface of REQUIRED_SURFACES) {
      if (!Array.isArray(feature.surfaces) || !feature.surfaces.includes(surface)) {
        errors.push(`${feature.feature} must declare ${surface} surface impact`);
      }
    }
    if (!Array.isArray(feature.requiredEvents) || feature.requiredEvents.length === 0) {
      errors.push(`${feature.feature} must list requiredEvents`);
    }
    if (!Array.isArray(feature.e2eScenarios) || feature.e2eScenarios.length === 0) {
      errors.push(`${feature.feature} must list e2eScenarios`);
    }
  }

  const allowedWriters = new Set((registry.allowedHerdMutationWriters || []).map((entry) => entry.path));
  const offenderRegex = /\b(INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+goats\b|pgx\.Identifier\s*\{\s*"goats"\s*\}/is;
  for (const rel of files) {
    if (!rel.startsWith("backend/internal/") && !rel.startsWith("backend/cmd/")) {
      continue;
    }
    if (rel.endsWith("_test.go") || rel.includes("/testdata/")) {
      continue;
    }
    if (!rel.endsWith(".go") && !rel.endsWith(".sql")) {
      continue;
    }
    const text = read(rel);
    if (offenderRegex.test(text) && !allowedWriters.has(rel)) {
      errors.push(`direct goats writer is not registered in domain-event-registry: ${rel}`);
    }
  }
  for (const rel of allowedWriters) {
    if (!fileExists(rel)) {
      errors.push(`allowedHerdMutationWriters path not found: ${rel}`);
    }
  }

  return errors;
}

function selfTest() {
  const root = "/virtual";
  const goodRegistry = {
    contractDoc: "contract.md",
    surfaceRules: { backend: "b", frontend: "f", mobile: "m" },
    requiredEventTypes: [...REQUIRED_EVENTS],
    events: REQUIRED_EVENTS.map((eventType) => ({
      eventType,
      producerFiles: [`producer-${eventType}.go`],
      consumers: [{ module: "x", file: `consumer-${eventType}.go` }],
      owningDoc: "contract.md",
      e2eProof: [`proof-${eventType}_test.go`],
      surfaces: [...REQUIRED_SURFACES],
    })),
    allowedHerdMutationWriters: [{ path: "backend/internal/identity/adapters/postgres/admin_goat_create.go" }],
    futureFeatureContracts: REQUIRED_FEATURES.map((feature) => ({
      feature,
      requiredEvents: ["goat.created"],
      surfaces: [...REQUIRED_SURFACES],
      e2eScenarios: ["happy path"],
    })),
  };
  const virtualFiles = new Map([
    ["contract.md", "backend frontend mobile outbox idempotent dlq e2e"],
    ["backend/internal/identity/adapters/postgres/admin_goat_create.go", "INSERT INTO goats"],
  ]);
  for (const eventType of REQUIRED_EVENTS) {
    virtualFiles.set(`producer-${eventType}.go`, eventType);
    virtualFiles.set(`consumer-${eventType}.go`, eventType);
    virtualFiles.set(`proof-${eventType}_test.go`, "proof");
  }
  const fileList = Array.from(virtualFiles.keys());
  const read = (rel) => virtualFiles.get(rel) || "";
  const fileExists = (rel) => virtualFiles.has(rel);
  const errors = validateRegistry(root, goodRegistry, fileList, read, fileExists);
  if (errors.length) {
    throw new Error(`good registry failed self-test:\n${errors.join("\n")}`);
  }

  const badMissingMobile = structuredClone(goodRegistry);
  badMissingMobile.events[0].surfaces = ["backend", "frontend"];
  if (!validateRegistry(root, badMissingMobile, fileList, read, fileExists).some((err) => err.includes("mobile"))) {
    throw new Error("missing mobile surface was not detected");
  }

  const badWriterFiles = [...fileList, "backend/internal/newfeature/repository.go"];
  virtualFiles.set("backend/internal/newfeature/repository.go", "UPDATE goats SET health_status='sick'");
  if (!validateRegistry(root, goodRegistry, badWriterFiles, read, fileExists).some((err) => err.includes("direct goats writer"))) {
    throw new Error("unregistered direct goats writer was not detected");
  }
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    console.log("domain-event-architecture guard self-test passed");
    return;
  }
  const root = process.cwd();
  const registryPath = "context/architecture/domain-event-registry.json";
  const registry = JSON.parse(readText(root, registryPath));
  const errors = validateRegistry(root, registry);
  if (errors.length) {
    console.error(`domain-event-architecture guard failed:\n- ${errors.join("\n- ")}`);
    process.exit(1);
  }
  console.log("domain-event-architecture guard passed");
}

main();
