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
  "goat.exited",
  "vaccination.completed",
  "obligation.missed",
  "goat.obligations_canceled",
  "obligation.rescoped",
  "protocol.version.published",
];

const REQUIRED_FEATURES = ["shifting", "dead_birth", "feed_direction"];
const REQUIRED_SURFACES = ["backend", "frontend", "mobile"];
const REQUIRED_MOVEMENT_INTEGRATION = {
  contract: "shifting_completion_to_vaccination",
  activationPath: "backend/internal/identity/adapters/postgres/goat_relocate.go",
  producerFile: "backend/internal/identity/adapters/postgres/goat_relocate.go",
  requiredEvents: ["goat.location.changed", "goat.stage_changed"],
  requiredConsumers: [
    { eventType: "goat.location.changed", module: "vaccination", file: "backend/internal/vaccination/app/generation_handler.go" },
    { eventType: "goat.location.changed", module: "obligation", file: "backend/internal/obligation/app/shift.go" },
    { eventType: "goat.stage_changed", module: "vaccination", file: "backend/internal/vaccination/app/generation_handler.go" },
  ],
  authoritativeStageSelectionTokens: [
    "management_stage_mode",
    "target_management_stage",
    "animal_stage_lookup",
    "management_stage",
  ],
  transactionTokens: ["goat_identity_events", "outbox_messages", "UPDATE goats", "management_stage"],
  proofFile: "backend/tests/e2e/story_shifting_vaccination_handoff_test.go",
  proofTokens: [
    "CompleteShifting",
    "goat.location.changed",
    "goat.stage_changed",
    "GoatShiftedHandler",
    "GoatRecheckHandler",
    "management_stage",
    "scope_id",
  ],
};

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

function hasProducerEvidence(text, eventType) {
  if (!text.includes(eventType)) {
    return false;
  }
  return /\b(outbox|InsertOutbox|PublishEvent|event_type|EventType|platformoutbox|Insert.*Event|identity_events)\b/i.test(text);
}

function hasConsumerEvidence(text, eventType) {
  if (!text.includes(eventType)) {
    return false;
  }
  return /\bSubscribe\s*\(/.test(text);
}

// hasNoOpHandleEvent detects a HandleEvent whose body — across multiple lines, allowing comments
// and blank lines — is nothing but `return nil`. Such a handler subscribes but has no measurable
// consumer effect, which the registry must not accept as a consumer.
function hasNoOpHandleEvent(text) {
  return /HandleEvent\s*\([^)]*\)\s*error\s*\{(?:\s|\/\/[^\n]*)*return\s+nil\s*(?:\s|\/\/[^\n]*)*\}/m.test(text);
}

function sameConsumerBinding(actual, required) {
  return actual?.module === required.module && actual?.file === required.file;
}

function validateRequiredConsumer(featureLabel, required, byType, errors) {
  if (!required?.eventType || !required?.module || !required?.file) {
    errors.push(`${featureLabel} required consumer must declare eventType, module, and file`);
    return;
  }
  const event = byType.get(required.eventType);
  if (!event) {
    errors.push(`${featureLabel} required consumer references unregistered event ${required.eventType}`);
    return;
  }
  if (!(event.consumers || []).some((consumer) => sameConsumerBinding(consumer, required))) {
    errors.push(`${featureLabel} required consumer ${required.module} (${required.file}) is not registered on ${required.eventType}`);
  }
}

function validateMovementIntegrationContracts(registry, byType, read, fileExists) {
  const errors = [];
  const contracts = Array.isArray(registry.movementIntegrationContracts) ? registry.movementIntegrationContracts : [];
  const contract = contracts.find((entry) => entry.contract === REQUIRED_MOVEMENT_INTEGRATION.contract);
  if (!contract) {
    return [`required movement integration contract ${REQUIRED_MOVEMENT_INTEGRATION.contract} is missing`];
  }

  const label = `movement contract ${contract.contract}`;
  const activationPaths = Array.isArray(contract.activationPaths) ? contract.activationPaths : [];
  const producerFiles = Array.isArray(contract.producerFiles) ? contract.producerFiles : [];
  const requiredEvents = Array.isArray(contract.requiredEvents) ? contract.requiredEvents : [];
  const requiredConsumers = Array.isArray(contract.requiredConsumers) ? contract.requiredConsumers : [];

  if (!activationPaths.includes(REQUIRED_MOVEMENT_INTEGRATION.activationPath)) {
    errors.push(`${label} must activate on ${REQUIRED_MOVEMENT_INTEGRATION.activationPath}`);
  }
  if (!producerFiles.includes(REQUIRED_MOVEMENT_INTEGRATION.producerFile)) {
    errors.push(`${label} must register producer ${REQUIRED_MOVEMENT_INTEGRATION.producerFile}`);
  }
  for (const eventType of REQUIRED_MOVEMENT_INTEGRATION.requiredEvents) {
    if (!requiredEvents.includes(eventType)) errors.push(`${label} must require event ${eventType}`);
  }
  for (const required of REQUIRED_MOVEMENT_INTEGRATION.requiredConsumers) {
    if (!requiredConsumers.some((actual) => actual.eventType === required.eventType && sameConsumerBinding(actual, required))) {
      errors.push(`${label} must require consumer ${required.module} (${required.file}) on ${required.eventType}`);
    }
    validateRequiredConsumer(label, required, byType, errors);
  }

  // The contract is prospective on main and becomes mandatory the moment shifting's canonical
  // relocation writer appears in a PR. This lets main carry the rule before feature code exists.
  const active = activationPaths.some((rel) => fileExists(rel));
  if (!active) return errors;

  for (const rel of producerFiles) {
    if (!fileExists(rel)) errors.push(`${label} active producer file not found: ${rel}`);
  }
  for (const eventType of REQUIRED_MOVEMENT_INTEGRATION.requiredEvents) {
    const event = byType.get(eventType);
    if (!event || !(event.producerFiles || []).includes(REQUIRED_MOVEMENT_INTEGRATION.producerFile)) {
      errors.push(`${label} must register ${REQUIRED_MOVEMENT_INTEGRATION.producerFile} as a producer of ${eventType}`);
    }
  }

  const selection = contract.authoritativeStageSelection || {};
  const evidenceFiles = Array.isArray(selection.evidenceFiles) ? selection.evidenceFiles : [];
  if (!evidenceFiles.includes(REQUIRED_MOVEMENT_INTEGRATION.producerFile)) {
    errors.push(`${label} raise-time stage selection evidence must include ${REQUIRED_MOVEMENT_INTEGRATION.producerFile}`);
  }
  const evidenceText = evidenceFiles.filter((rel) => fileExists(rel)).map((rel) => read(rel)).join("\n");
  for (const token of REQUIRED_MOVEMENT_INTEGRATION.authoritativeStageSelectionTokens) {
    if (!evidenceText.includes(token)) {
      errors.push(`${label} raise-time stage selection evidence is missing ${token}`);
    }
  }
  // Resident stages may be offered as destination-stage choices, but the relocation writer must
  // consume the snapshotted explicit target and never infer one from the residents at apply time.
  if (/SELECT\s+DISTINCT\s+management_stage\s+FROM\s+goats/is.test(evidenceText)) {
    errors.push(`${label} raise-time stage selection must not infer the applied stage from resident goats`);
  }

  const transaction = contract.transactionEvidence || {};
  const transactionFiles = Array.isArray(transaction.evidenceFiles) ? transaction.evidenceFiles : [];
  if (!transactionFiles.includes(REQUIRED_MOVEMENT_INTEGRATION.producerFile)) {
    errors.push(`${label} atomic transaction evidence must include ${REQUIRED_MOVEMENT_INTEGRATION.producerFile}`);
  }
  const transactionText = transactionFiles.filter((rel) => fileExists(rel)).map((rel) => read(rel)).join("\n");
  for (const token of REQUIRED_MOVEMENT_INTEGRATION.transactionTokens) {
    if (!transactionText.includes(token)) errors.push(`${label} atomic transaction evidence is missing ${token}`);
  }

  const proofs = Array.isArray(contract.requiredProof) ? contract.requiredProof : [];
  const proof = proofs.find((entry) => entry.file === REQUIRED_MOVEMENT_INTEGRATION.proofFile);
  if (!proof || !fileExists(REQUIRED_MOVEMENT_INTEGRATION.proofFile)) {
    errors.push(`${label} requires real producer-to-consumer E2E proof ${REQUIRED_MOVEMENT_INTEGRATION.proofFile}`);
  } else {
    const proofText = read(REQUIRED_MOVEMENT_INTEGRATION.proofFile);
    for (const token of REQUIRED_MOVEMENT_INTEGRATION.proofTokens) {
      if (!proofText.includes(token)) errors.push(`${label} E2E proof is missing ${token}`);
    }
  }

  return errors;
}

// collectEventConstants maps Go event-constant identifiers to their string values across the
// given files, e.g. `const EventGoatCreated = "goat.created"` -> EventGoatCreated: goat.created.
function collectEventConstants(files, read) {
  const constants = new Map();
  const constRegex = /(?:const\s+|^\s*)([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([a-z0-9_.]+\.[a-z0-9_.]+)"/gm;
  for (const rel of files) {
    if (!rel.endsWith(".go") || rel.endsWith("_test.go")) continue;
    const text = read(rel);
    if (!text.includes("\"")) continue;
    for (const match of text.matchAll(constRegex)) {
      constants.set(match[1], match[2]);
    }
  }
  return constants;
}

// PREEXISTING_UNREGISTERED_SUBSCRIPTIONS is tracked registry debt: runtime events that were
// already subscribed before the runtime<->registry parity gate landed, with real producers
// (sopbridge verify fanout, verification repository outbox, obligation shift, counts projector)
// but no registry entry yet. SHRINK-ONLY: never add to this list — register the event in
// context/architecture/domain-event-registry.json with producer/consumer/e2e proof instead.
// A stale entry (no longer subscribed anywhere) also fails, so the list can only ratchet down.
const PREEXISTING_UNREGISTERED_SUBSCRIPTIONS = new Set([
  "counts.base_count_anchor.recorded",
  "counts.shifting_event.recorded",
  "vaccination.verify.rejected",
  "vaccination.verify.accepted",
  "goat.shifted",
]);

// validateRuntimeSubscriptionParity scans production Go code for bus.Subscribe(...) calls and
// requires every subscribed event to (a) exist in the registry and (b) have at least one
// registered consumer. This is the check that catches both an orphan runtime subscription to an
// unregistered event and a runtime subscription that the registry claims has no consumer.
function validateRuntimeSubscriptionParity(registry, files, read, { skipBaselineRatchet = false } = {}) {
  const errors = [];
  const seenBaselined = new Set();
  const registryEvents = new Map();
  for (const event of registry.events || []) {
    if (event.eventType) registryEvents.set(event.eventType, event);
  }
  const constants = collectEventConstants(files, read);
  const subscribeRegex = /\bSubscribe\s*\(\s*(?:"([^"]+)"|([A-Za-z_][A-Za-z0-9_.]*))\s*,/g;
  for (const rel of files) {
    if (!rel.startsWith("backend/internal/") && !rel.startsWith("backend/cmd/")) continue;
    if (!rel.endsWith(".go") || rel.endsWith("_test.go") || rel.includes("/testdata/")) continue;
    if (rel.includes("/platform/eventbus/")) continue; // bus implementation, not a subscriber
    const text = read(rel);
    if (!text.includes("Subscribe")) continue;
    for (const match of text.matchAll(subscribeRegex)) {
      let eventType = match[1];
      if (!eventType && match[2]) {
        const ident = match[2].includes(".") ? match[2].split(".").pop() : match[2];
        eventType = constants.get(ident);
        if (!eventType) continue; // non-event-typed identifier (dynamic subscription) — cannot resolve statically
      }
      if (!eventType) continue;
      const registered = registryEvents.get(eventType);
      if (!registered) {
        if (PREEXISTING_UNREGISTERED_SUBSCRIPTIONS.has(eventType)) {
          seenBaselined.add(eventType);
          continue;
        }
        errors.push(`runtime subscription to unregistered event ${eventType} in ${rel}`);
        continue;
      }
      if (!Array.isArray(registered.consumers) || registered.consumers.length === 0) {
        errors.push(`runtime subscription to ${eventType} in ${rel}, but registry declares no consumers for it`);
      }
    }
  }
  for (const eventType of PREEXISTING_UNREGISTERED_SUBSCRIPTIONS) {
    if (registryEvents.has(eventType)) {
      errors.push(`baselined event ${eventType} is now registered: remove it from PREEXISTING_UNREGISTERED_SUBSCRIPTIONS`);
    } else if (!seenBaselined.has(eventType) && !skipBaselineRatchet) {
      errors.push(`baselined event ${eventType} is no longer subscribed anywhere: remove it from PREEXISTING_UNREGISTERED_SUBSCRIPTIONS`);
    }
  }
  return errors;
}

function validateRegistry(root, registry, files = walk(root), read = (rel) => readText(root, rel), fileExists = (rel) => exists(root, rel), opts = {}) {
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
      ["e2eProof", "E2E proof"],
    ]) {
      if (!Array.isArray(event[field]) || event[field].length === 0) {
        errors.push(`${event.eventType} missing ${label} registration`);
      }
    }
    // consumers is optional: an event can be produced for audit without having a deployed consumer.
    if (!Array.isArray(event.consumers)) {
      errors.push(`${event.eventType} consumers must be an array (may be empty)`);
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
    const producerFiles = event.producerFiles || [];
    const consumerFiles = (event.consumers || []).map((consumer) => consumer.file).filter(Boolean);
    const producerSet = new Set(producerFiles);
    for (const rel of consumerFiles) {
      if (producerSet.has(rel)) {
        errors.push(`${event.eventType} consumer cannot be the same file as a producer: ${rel}`);
      }
      if (!fileExists(rel)) {
        errors.push(`${event.eventType} consumer file not found: ${rel}`);
      }
    }
    const producerText = producerFiles.filter((rel) => fileExists(rel)).map((rel) => read(rel)).join("\n");
    const consumerText = consumerFiles.filter((rel) => fileExists(rel)).map((rel) => read(rel)).join("\n");
    if (!hasProducerEvidence(producerText, event.eventType)) {
      errors.push(`${event.eventType} registered producer files do not prove durable event publication`);
    }
    // Consumers array may be empty for audit-only events with no deployed consumer.
    if (event.consumers && event.consumers.length > 0) {
      if (!hasConsumerEvidence(consumerText, event.eventType)) {
        errors.push(`${event.eventType} registered consumer files do not prove runtime subscription`);
      }
      // A registered consumer must have a measurable effect: a HandleEvent whose entire body
      // (across any number of lines/comments) is just `return nil` is a fake consumer claim.
      if (hasNoOpHandleEvent(consumerText)) {
        errors.push(`${event.eventType} consumer appears to be no-op (HandleEvent body is only 'return nil'); use empty consumers array for audit-only events`);
      }
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
    for (const eventType of feature.requiredEvents || []) {
      if (!byType.has(eventType)) {
        errors.push(`${feature.feature} requiredEvents includes unregistered event ${eventType}`);
      }
    }
    if (!Array.isArray(feature.requiredConsumers)) {
      errors.push(`${feature.feature} requiredConsumers must be an array (may be empty for planned consumers)`);
    }
    for (const required of feature.requiredConsumers || []) {
      if (!(feature.requiredEvents || []).includes(required.eventType)) {
        errors.push(`${feature.feature} required consumer ${required.module || "unknown"} references ${required.eventType}, which is not in requiredEvents`);
      }
      validateRequiredConsumer(`feature ${feature.feature}`, required, byType, errors);
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

  errors.push(...validateRuntimeSubscriptionParity(registry, files, read, opts));
  errors.push(...validateMovementIntegrationContracts(registry, byType, read, fileExists));

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
      requiredConsumers: [{ module: "x", eventType: "goat.created", file: "consumer-goat.created.go" }],
      surfaces: [...REQUIRED_SURFACES],
      e2eScenarios: ["happy path"],
    })),
    movementIntegrationContracts: [{
      contract: REQUIRED_MOVEMENT_INTEGRATION.contract,
      activationPaths: [REQUIRED_MOVEMENT_INTEGRATION.activationPath],
      producerFiles: [REQUIRED_MOVEMENT_INTEGRATION.producerFile],
      requiredEvents: [...REQUIRED_MOVEMENT_INTEGRATION.requiredEvents],
      requiredConsumers: structuredClone(REQUIRED_MOVEMENT_INTEGRATION.requiredConsumers),
      authoritativeStageSelection: { evidenceFiles: [REQUIRED_MOVEMENT_INTEGRATION.producerFile] },
      transactionEvidence: { evidenceFiles: [REQUIRED_MOVEMENT_INTEGRATION.producerFile] },
      requiredProof: [{ file: REQUIRED_MOVEMENT_INTEGRATION.proofFile }],
    }],
  };
  const virtualFiles = new Map([
    ["contract.md", "backend frontend mobile outbox idempotent dlq e2e"],
    ["backend/internal/identity/adapters/postgres/admin_goat_create.go", "INSERT INTO goats"],
  ]);
  for (const eventType of REQUIRED_EVENTS) {
    virtualFiles.set(`producer-${eventType}.go`, `${eventType}\nplatformoutbox.InsertOutboxMessage(ctx, tx, event_type)`);
    virtualFiles.set(`consumer-${eventType}.go`, `${eventType}\nfunc (h *H) Register(bus eventbus.Bus) { bus.Subscribe("${eventType}", h) }`);
    virtualFiles.set(`proof-${eventType}_test.go`, "proof");
  }
  const locationEvent = goodRegistry.events.find((event) => event.eventType === "goat.location.changed");
  const stageEvent = goodRegistry.events.find((event) => event.eventType === "goat.stage_changed");
  locationEvent.consumers.push(...REQUIRED_MOVEMENT_INTEGRATION.requiredConsumers.filter((consumer) => consumer.eventType === locationEvent.eventType));
  stageEvent.consumers.push(...REQUIRED_MOVEMENT_INTEGRATION.requiredConsumers.filter((consumer) => consumer.eventType === stageEvent.eventType));
  virtualFiles.set(
    "backend/internal/vaccination/app/generation_handler.go",
    'func Register(bus eventbus.Bus) { bus.Subscribe("goat.location.changed", shifted); bus.Subscribe("goat.stage_changed", recheck) }',
  );
  virtualFiles.set(
    "backend/internal/obligation/app/shift.go",
    'func Register(bus eventbus.Bus) { bus.Subscribe("goat.location.changed", shifted) }',
  );
  const fileList = Array.from(virtualFiles.keys());
  const read = (rel) => virtualFiles.get(rel) || "";
  const fileExists = (rel) => virtualFiles.has(rel);
  const errors = validateRegistry(root, goodRegistry, fileList, read, fileExists, { skipBaselineRatchet: true });
  if (errors.length) {
    throw new Error(`good registry failed self-test:\n${errors.join("\n")}`);
  }

  const badMissingMobile = structuredClone(goodRegistry);
  badMissingMobile.events[0].surfaces = ["backend", "frontend"];
  if (!validateRegistry(root, badMissingMobile, fileList, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("mobile"))) {
    throw new Error("missing mobile surface was not detected");
  }

  const badWriterFiles = [...fileList, "backend/internal/newfeature/repository.go"];
  virtualFiles.set("backend/internal/newfeature/repository.go", "UPDATE goats SET health_status='sick'");
  if (!validateRegistry(root, goodRegistry, badWriterFiles, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("direct goats writer"))) {
    throw new Error("unregistered direct goats writer was not detected");
  }

  const badSelfConsumer = structuredClone(goodRegistry);
  badSelfConsumer.events[0].consumers = [{ module: "x", file: badSelfConsumer.events[0].producerFiles[0] }];
  if (!validateRegistry(root, badSelfConsumer, fileList, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("same file as a producer"))) {
    throw new Error("producer-as-consumer registration was not detected");
  }

  const badMissingConsumerToken = structuredClone(goodRegistry);
  virtualFiles.set(badMissingConsumerToken.events[0].consumers[0].file, "func Register(bus eventbus.Bus) { bus.Subscribe(\"other.event\", h) }");
  if (!validateRegistry(root, badMissingConsumerToken, fileList, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("runtime subscription"))) {
    throw new Error("consumer missing event token was not detected");
  }

  const badStringOnlyProducer = structuredClone(goodRegistry);
  virtualFiles.set(badStringOnlyProducer.events[0].producerFiles[0], badStringOnlyProducer.events[0].eventType);
  if (!validateRegistry(root, badStringOnlyProducer, fileList, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("durable event publication"))) {
    throw new Error("string-only producer was not detected");
  }

  // No-op consumer: multi-line HandleEvent whose body is only comments + `return nil` — proves
  // the property (no measurable effect), not a single-line regex shape.
  const badNoOpConsumer = structuredClone(goodRegistry);
  const noOpFile = badNoOpConsumer.events[0].consumers[0].file;
  const savedNoOpFile = virtualFiles.get(noOpFile);
  virtualFiles.set(noOpFile, [
    `${badNoOpConsumer.events[0].eventType}`,
    `func (h *H) Register(bus eventbus.Bus) { bus.Subscribe("${badNoOpConsumer.events[0].eventType}", h) }`,
    `func (h *H) HandleEvent(ctx context.Context, e eventbus.Event) error {`,
    `	// intentionally does nothing: canonical reads serve this surface`,
    ``,
    `	return nil`,
    `}`,
  ].join("\n"));
  if (!validateRegistry(root, badNoOpConsumer, fileList, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("no-op"))) {
    throw new Error("multi-line no-op consumer was not detected");
  }
  virtualFiles.set(noOpFile, savedNoOpFile);

  const goodAuditOnlyEventFiles = fileList.filter((rel) => rel !== goodRegistry.events[0].consumers[0].file);
  const goodAuditOnlyEvent = structuredClone(goodRegistry);
  goodAuditOnlyEvent.events[0].consumers = [];
  if (validateRegistry(root, goodAuditOnlyEvent, goodAuditOnlyEventFiles, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("missing consumer") || err.includes("no consumers"))) {
    throw new Error("audit-only event with empty consumers and no runtime subscriber was incorrectly rejected");
  }

  // Parity: runtime subscription to an event that is NOT in the registry must fail (LIFE-004 shape).
  const orphanSubFiles = [...fileList, "backend/internal/orphan/handler.go"];
  virtualFiles.set("backend/internal/orphan/handler.go", [
    `const EventOrphanRequested = "vaccination.manual_campaign.requested"`,
    `func (h *H) Register(bus eventbus.Bus) { bus.Subscribe(EventOrphanRequested, h) }`,
  ].join("\n"));
  if (!validateRegistry(root, goodRegistry, orphanSubFiles, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("unregistered event vaccination.manual_campaign.requested"))) {
    throw new Error("runtime subscription to unregistered event was not detected");
  }
  virtualFiles.delete("backend/internal/orphan/handler.go");

  // Parity: runtime subscription to a registry event whose consumers list is empty must fail
  // (LIFE-003 shape: registry says no consumer, runtime still subscribes a handler).
  const emptyConsumerSub = structuredClone(goodRegistry);
  emptyConsumerSub.events[0].consumers = [];
  const emptySubFiles = [...goodAuditOnlyEventFiles, "backend/internal/fake/consumer.go"];
  virtualFiles.set("backend/internal/fake/consumer.go", [
    `const EventFake = "${emptyConsumerSub.events[0].eventType}"`,
    `func (h *H) Register(bus eventbus.Bus) { bus.Subscribe(EventFake, h) }`,
  ].join("\n"));
  if (!validateRegistry(root, emptyConsumerSub, emptySubFiles, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("registry declares no consumers"))) {
    throw new Error("runtime subscription to consumer-less registry event was not detected");
  }
  virtualFiles.delete("backend/internal/fake/consumer.go");

  // Parity: string-literal subscription is caught too.
  const literalSubFiles = [...fileList, "backend/internal/orphan2/handler.go"];
  virtualFiles.set("backend/internal/orphan2/handler.go", `func (h *H) Register(bus eventbus.Bus) { bus.Subscribe("totally.unregistered_event", h) }`);
  if (!validateRegistry(root, goodRegistry, literalSubFiles, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("unregistered event totally.unregistered_event"))) {
    throw new Error("string-literal subscription to unregistered event was not detected");
  }
  virtualFiles.delete("backend/internal/orphan2/handler.go");

  // A feature contract must prove its named downstream module is registered on the exact event.
  // Merely listing requiredEvents is not producer-to-consumer closure.
  const badFeatureConsumer = structuredClone(goodRegistry);
  badFeatureConsumer.futureFeatureContracts[0].requiredConsumers = [{
    eventType: "goat.created",
    module: "missing-module",
    file: "backend/internal/missing/handler.go",
  }];
  if (!validateRegistry(root, badFeatureConsumer, fileList, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("required consumer"))) {
    throw new Error("feature contract missing its required consumer binding was not detected");
  }

  // An activated relocation writer that infers the applied stage from resident goats instead of
  // consuming the snapshotted raise-time choice must fail even when unrelated files contain the
  // required contract words.
  const movementPath = "backend/internal/identity/adapters/postgres/goat_relocate.go";
  const badMovementFiles = [...fileList, movementPath, "unrelated/profile_notes.go"];
  virtualFiles.set(movementPath, "SELECT DISTINCT management_stage FROM goats");
  virtualFiles.set("unrelated/profile_notes.go", "management_stage_mode target_management_stage animal_stage_lookup management_stage");
  const badMovement = structuredClone(goodRegistry);
  badMovement.movementIntegrationContracts = [{
    contract: "shifting_completion_to_vaccination",
    activationPaths: [movementPath],
    producerFiles: [movementPath],
    requiredEvents: ["goat.location.changed", "goat.stage_changed"],
    requiredConsumers: [
      { eventType: "goat.location.changed", module: "x", file: "consumer-goat.location.changed.go" },
      { eventType: "goat.stage_changed", module: "x", file: "consumer-goat.stage_changed.go" },
    ],
    authoritativeStageSelection: {
      evidenceFiles: [movementPath, "unrelated/profile_notes.go"],
      requiredTokens: ["management_stage_mode", "target_management_stage", "animal_stage_lookup", "management_stage"],
      forbiddenPatterns: [{ pattern: "SELECT\\s+DISTINCT\\s+management_stage\\s+FROM\\s+goats", reason: "resident inference" }],
    },
    requiredProof: [{
      file: "proof-shifting-vaccination_test.go",
      mustContain: ["CompleteShifting", "GoatShiftedHandler", "GoatRecheckHandler"],
    }],
  }];
  virtualFiles.set("proof-shifting-vaccination_test.go", "CompleteShifting GoatShiftedHandler GoatRecheckHandler");
  if (!validateRegistry(root, badMovement, badMovementFiles, read, fileExists, { skipBaselineRatchet: true }).some((err) => err.includes("raise-time stage selection"))) {
    throw new Error("resident-derived destination stage was not rejected by the movement integration contract");
  }
  virtualFiles.delete(movementPath);
  virtualFiles.delete("unrelated/profile_notes.go");
  virtualFiles.delete("proof-shifting-vaccination_test.go");
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
