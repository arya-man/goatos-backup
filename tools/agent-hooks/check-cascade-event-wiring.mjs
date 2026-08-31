#!/usr/bin/env node

// check-cascade-event-wiring.mjs — blocks the three "operator-cascade chain is silently broken"
// anti-patterns. The cascade chain is:
//
//   write path mutates scheduling-relevant state   (Bug Class C: must EMIT its cascade event)
//     -> outbox relay dispatches onto the domain bus (Bug Class B: handler must be on the DURABLE bus)
//       -> sweeper re-selects work under the operator cap (Bug Class A: must net SESSION-reserved load)
//
// Every link is statically checkable, and every link has already broken once in production while
// isolated unit/E2E tests stayed green. See docs/decisions/scale-anti-patterns.md ->
// "Operator-cascade wiring anti-patterns".
//
// Rules:
//   1. handler-not-on-durable-bus          (Bug Class B)
//   2. stale-durable-bus-exemption         (Bug Class B — exemption must stay honest)
//   3. operator-cap-ignores-session-load   (Bug Class A)
//   4. cascade-write-without-event         (Bug Class C)
//   5. bus-builder-missing-verification-applier (Bug Class D — a HAND-LISTED domain-bus builder
//      that omits a verifier-verdict applier; rule 1 cannot see it on a non-durable builder)
//
// Modes:
//   (default)     whole-tree audit. These are absolute wiring invariants, not diff-scoped style
//                 rules: a handler dropped off the durable bus by an unrelated commit must fail
//                 the very next CI run, so this guard never trusts a diff base.
//   --self-test   run the built-in adversarial fixtures and exit.
//
// Escape hatch: a genuinely-safe case may append `cascade-guard:ignore: <reason>` on the line
// (rules 3 and 4). Rule 1 has no inline hatch — an unregistered handler must be declared in
// DURABLE_BUS_EXEMPTIONS below with a written reason and its real registration site.
//
// Deterministic, offline: pure text parsing. No network, no build, no database.

import { readFileSync, readdirSync, existsSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// The ONLY buses the real outbox relay / domain-event consumer actually dispatch to.
// kernelstages.BuildDomainBus backs the in-process relay publisher and the continuous
// domain-consumer stage; cmd/domain-event-consumer/main.go backs the standalone Pub/Sub consumer
// binary. A handler that is not on BOTH is dead in at least one real deployment mode.
export const DURABLE_BUS_FILES = [
  "backend/internal/kernelstages/bus.go",
  "backend/cmd/domain-event-consumer/main.go",
];

// Buses that exist but are NOT the durable dispatch path: bootstrap/api.go is the API process's
// own in-process bus, and domainconsumer/wiring/bus.go is wired into no cmd/* binary at all (only
// tests). Registering a handler here and nowhere else is exactly the Bug Class B false green.
export const NON_DURABLE_BUS_FILES = [
  "backend/internal/bootstrap/api.go",
  "backend/internal/domainconsumer/wiring/bus.go",
];

// Handlers deliberately kept OFF the durable buses. Each entry must name the file that really does
// register it and why that is correct; rule 2 fails if that claim goes stale.
export const DURABLE_BUS_EXEMPTIONS = {
  VerificationNotifier: {
    registeredIn: "backend/internal/bootstrap/api.go",
    reason:
      "legacy in-process notifier for vaccination.verify.accepted/rejected. Its durable-path replacement, " +
      "notificationbridge.VerificationEventConsumer, IS registered on both durable buses and covers the same " +
      "two event seams; this one stays on the API process's own bus for the synchronous in-request " +
      "notification path only. Retiring it is a separate cutover — until then it must NOT be double-registered " +
      "on the durable buses (that would duplicate every notification).",
  },
};

const isGo = (rel) => rel.endsWith(".go") && !rel.endsWith("_test.go");

// ---------------------------------------------------------------------------
// text helpers
// ---------------------------------------------------------------------------

// stripComments removes whole-line `//` comments and `/* */` blocks so a rule can never be
// satisfied by prose. Trailing inline comments are left alone (cheap, and no rule below can be
// satisfied by one).
export function stripComments(source) {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .split("\n")
    .filter((l) => !l.trim().startsWith("//"))
    .join("\n");
}

// goFuncBlocks splits a Go file into top-level func blocks (column-zero `func`). Heuristic by
// design: it only needs to keep a body's tokens together, not to parse Go.
export function goFuncBlocks(source) {
  const lines = source.split("\n");
  const blocks = [];
  let cur = null;
  lines.forEach((line, i) => {
    if (/^func[\s(]/.test(line)) {
      if (cur) blocks.push(cur);
      cur = { name: /^func\s*(?:\([^)]*\)\s*)?([A-Za-z0-9_]+)/.exec(line)?.[1] ?? "<anon>", startLine: i + 1, lines: [line] };
    } else if (cur) {
      cur.lines.push(line);
    }
  });
  if (cur) blocks.push(cur);
  return blocks.map((b) => ({ name: b.name, startLine: b.startLine, body: b.lines.join("\n") }));
}

// ---------------------------------------------------------------------------
// Rule 1/2 — Bug Class B: eventbus handlers must be on every DURABLE bus
// ---------------------------------------------------------------------------

// handlerTypesIn returns the receiver type names that expose `Register(bus eventbus.Bus)`.
export function handlerTypesIn(source) {
  const out = [];
  const re = /func\s*\(\s*\w+\s+\*?([A-Za-z0-9_]+)\s*\)\s*Register\s*\(\s*\w+\s+eventbus\.Bus\s*\)/g;
  let m;
  while ((m = re.exec(source)) !== null) out.push(m[1]);
  return out;
}

// registersHandler reports whether a bus file constructs `New<Type>(...)` and registers it.
// Accepts both the chained form (`pkg.NewX(dep).WithY(z).Register(bus)`) and the split form
// (`h := pkg.NewX(dep)` ... `h.Register(bus)`).
export function registersHandler(busSource, typeName) {
  const code = stripComments(busSource);
  if (
    /RegisterVerificationAppliers\s*\(/.test(code) &&
    VERIFICATION_APPLIER_CONSTRUCTORS.includes(typeName)
  ) {
    return true;
  }
  const chained = new RegExp(`New${typeName}\\s*\\([^;\\n]*\\)(?:\\s*\\.\\w+\\([^;\\n]*\\))*\\s*\\.Register\\s*\\(`);
  if (chained.test(code)) return true;
  const assigned = new RegExp(`(\\w+)\\s*:?=\\s*[\\w.]*New${typeName}\\s*\\(`).exec(code);
  if (assigned && new RegExp(`\\b${assigned[1]}\\.Register\\s*\\(`).test(code)) return true;
  return false;
}

// validateHandlerRegistration is the pure Bug Class B validator. All inputs injected so the
// self-test can feed adversarial fixtures.
//   handlerTypes: [{ type, rel }]
//   durableBusSources / nonDurableBusSources: { rel: source }
export function validateHandlerRegistration({ handlerTypes, durableBusSources, nonDurableBusSources = {}, exemptions = DURABLE_BUS_EXEMPTIONS }) {
  const findings = [];

  for (const { type, rel } of handlerTypes) {
    const exempt = exemptions[type];
    const missing = Object.entries(durableBusSources)
      .filter(([, src]) => !registersHandler(src, type))
      .map(([busRel]) => busRel);

    if (exempt) {
      // Rule 2: the exemption must still describe reality.
      const declared = exempt.registeredIn;
      const declaredSource = nonDurableBusSources[declared] ?? durableBusSources[declared];
      if (declaredSource === undefined) {
        findings.push({
          rule: "stale-durable-bus-exemption",
          rel,
          message: `${type} is exempted with registeredIn "${declared}", but that file is not a known bus file — update DURABLE_BUS_EXEMPTIONS`,
        });
      } else if (!registersHandler(declaredSource, type)) {
        findings.push({
          rule: "stale-durable-bus-exemption",
          rel,
          message: `${type} is exempted as "registered in ${declared}", but ${declared} does not register it — the exemption is now hiding a handler that is registered NOWHERE`,
        });
      }
      continue;
    }

    if (missing.length > 0) {
      const alsoOn = Object.entries(nonDurableBusSources)
        .filter(([, src]) => registersHandler(src, type))
        .map(([busRel]) => busRel);
      const tail = alsoOn.length
        ? ` It IS registered in ${alsoOn.join(", ")} — but nothing real dispatches to that bus, so every event is silently dropped in production.`
        : "";
      findings.push({
        rule: "handler-not-on-durable-bus",
        rel,
        message:
          `${type} has Register(bus eventbus.Bus) but is missing from ${missing.join(", ")}.` +
          tail +
          " Register it on EVERY durable bus, or add it to DURABLE_BUS_EXEMPTIONS with a written reason.",
      });
    }
  }
  return findings;
}

// ---------------------------------------------------------------------------
// Rule 5 — Bug Class D: every domain-bus builder must register EVERY verifier-verdict applier
// ---------------------------------------------------------------------------

// The defect class is the HAND LIST itself, not any single omission: five files build a domain
// event bus, and each hand-maintained consumer list is one edit away from dropping an applier.
// domainconsumer/wiring/bus.go hand-listed consumers and carried ONLY the weighing applier, so a
// verdict routed through it silently no-opped shifting + feed distribution + feed packing + feed
// transport. Rule 1 could not see it: that file is a NON-durable bus, so a handler registered on
// both durable buses passes rule 1 while this builder is missing it entirely.
export const DOMAIN_BUS_BUILDERS = [
  "backend/internal/bootstrap/api.go",
  "backend/internal/kernelstages/bus.go",
  "backend/internal/domainconsumer/wiring/bus.go",
  "backend/cmd/domain-event-consumer/main.go",
  "backend/cmd/outbox-relay/main.go",
];

// The durable verifier-verdict appliers, by constructor name. A builder satisfies the rule by calling the
// shared eventwiring.RegisterVerificationAppliers (preferred — one list, cannot drift) or by
// registering all of them explicitly.
export const VERIFICATION_APPLIER_CONSTRUCTORS = [
  "ShiftingVerificationHandler",
  "MilkPreparationVerificationHandler",
  "FeedDistributionVerificationHandler",
  "FeedPackingVerificationHandler",
  "FeedTransportVerificationHandler",
  "FeedWastageVerificationHandler",
  "PCCareVerificationHandler",
  "HealthVerificationHandler",
  "VerificationVerdictHandler",
];

export const REQUIRED_VERIFICATION_APPLIER_CALL_TOKENS = {
  "backend/internal/bootstrap/api.go": ["feedDirectionRepo", "countsApprovalRepo", "countsRepo", "weighingRepo", "weighingVerificationBridge", "pcCareRepo", "healthRepo"],
  "backend/internal/kernelstages/bus.go": [
    "feedDirectionRepo",
    "countsApprovalRepo",
    "countsMilkPreparationRepo",
    "weighingRepo",
    "weighingverificationbridge.New",
    "pccarepg.NewRepository",
    "healthRepo",
  ],
  "backend/internal/domainconsumer/wiring/bus.go": ["stores.feed", "stores.shifting", "stores.milkPreparation", "stores.weighing", "stores.weighingAck", "stores.pcCare", "stores.health"],
  "backend/cmd/domain-event-consumer/main.go": [
    "feedDirectionRepo",
    "countsApprovalRepo",
    "countsMilkPreparationRepo",
    "weighingRepo",
    "weighingverificationbridge.New",
    "pccarepg.NewRepository",
    "healthRepo",
  ],
  "backend/cmd/outbox-relay/main.go": [
    "feedDirectionRepo",
    "countsApprovalRepo",
    "countsMilkPreparationRepo",
    "weighingRepo",
    "weighingVerificationBridge",
    "pccarepg.NewRepository",
    "healthRepo",
  ],
};

function verificationApplierCallLine(source) {
  const code = stripComments(source);
  return code.split("\n").find((line) => /RegisterVerificationAppliers\s*\(/.test(line)) ?? "";
}

// findingsForBusBuilderSource reports appliers a builder neither delegates nor registers.
export function findingsForBusBuilderSource(source, rel) {
  const code = stripComments(source);
  if (/RegisterVerificationAppliers\s*\(/.test(code)) {
    const requiredTokens = REQUIRED_VERIFICATION_APPLIER_CALL_TOKENS[rel] ?? [];
    const call = verificationApplierCallLine(source);
    const missingTokens = requiredTokens.filter((token) => !call.includes(token));
    if (missingTokens.length === 0) return [];
    return [
      {
        rule: "bus-builder-verification-applier-helper-miswired",
        rel,
        line: null,
        message:
          `${rel} calls eventwiring.RegisterVerificationAppliers but the call is missing ${missingTokens.join(", ")}. ` +
          "The shared helper is only safe when the composition root passes the feed store, milk-preparation store, weighing store, weighing ack bridge, and pc-care store it owns.",
      },
    ];
  }
  const missing = VERIFICATION_APPLIER_CONSTRUCTORS.filter((type) => !registersHandler(source, type));
  if (missing.length === 0) return [];
  return [
    {
      rule: "bus-builder-missing-verification-applier",
      rel,
      line: null,
      message:
        `${rel} builds a domain event bus from a HAND LIST that omits ${missing.join(", ")}. ` +
        "Every verifier approve/rework routed through this builder is a silent no-op for those modules. " +
        "Call eventwiring.RegisterVerificationAppliers(bus, feed, shifting, weighing, log) — the shared " +
        "single registration — instead of re-listing consumers here.",
    },
  ];
}

// ---------------------------------------------------------------------------
// Rule 3 — Bug Class A: operator cap must net session-reserved load
// ---------------------------------------------------------------------------

// Any sweeper-side function that consumes a DB-reported operator capacity (`operator.Cap`) is a
// SELECTION-limiting decision. The DB value is only net of load committed by PRIOR sweeper runs;
// what the CURRENT sweep session has already reserved for that (tenant, park, date, operator) lives
// in SweepSession.vaccinationOperatorLoads. Consuming Cap without subtracting it lets a later
// rule-version in the same sweep re-read the cached pre-session snapshot and over-select past the
// per-operator/day cap (observed: 221-223 animals against a 200 cap on a real reseed).
export function findingsForOperatorCapSource(source) {
  const findings = [];
  for (const block of goFuncBlocks(source)) {
    if (/cascade-guard:ignore/.test(block.body)) continue;
    if (!/\boperator(?:s\s*\[[^\]]*\])?\.Cap\b/.test(stripComments(block.body))) continue;
    if (/vaccinationOperatorLoad\s*\(/.test(block.body)) continue;
    findings.push({
      line: block.startLine,
      rule: "operator-cap-ignores-session-load",
      message:
        `${block.name} consumes operator.Cap (DB-reported remaining capacity) without subtracting ` +
        "session.vaccinationOperatorLoad(tenantID, parkID, plannedDate, operatorID); a later due-group/rule-version " +
        "in the SAME sweep re-reads the cached pre-session snapshot and over-selects past the per-operator/day cap",
    });
  }
  return findings;
}

// ---------------------------------------------------------------------------
// Rule 4 — Bug Class C: scheduling-relevant writes must emit their cascade event
// ---------------------------------------------------------------------------

// Each rule: a write that changes operator capacity / availability / tenant capacity config, and
// the cascade event that write MUST enqueue in the same transaction. Seed CLIs under backend/cmd/
// are out of scope on purpose: a seed run is always followed by an explicit generate/replan step.
export const CASCADE_WRITE_RULES = [
  {
    id: "workforce-position-cap-or-roster",
    // UPDATE workforce_positions ... SET vaccination_daily_animal_cap = / week_off_weekday =
    match: /UPDATE\s+workforce_positions[\s\S]{0,1200}?(?:vaccination_daily_animal_cap|week_off_weekday)\s*=/i,
    requireAny: ["vaccination.capacity.changed", "vaccination.roster.changed"],
    what: "an UPDATE of a vaccination operator seat's daily animal cap / week-off",
  },
  {
    id: "tenant-vaccination-capacity-config",
    match: /(?:INSERT\s+INTO|UPDATE)\s+vaccination_capacity_config\b/i,
    requireAny: ["vaccination.capacity.changed"],
    what: "a write to the tenant-wide vaccination_capacity_config (protocol publish path)",
  },
  {
    id: "operator-assignment-config",
    match: /(?:INSERT\s+INTO|UPDATE)\s+vaccination_operator_assignment_config\b/i,
    requireAny: ["vaccination.capacity.changed", "vaccination.roster.changed"],
    what: "a write to vaccination_operator_assignment_config (active operators/day, default operator)",
  },
];

export function findingsForCascadeWriteSource(source, rules = CASCADE_WRITE_RULES) {
  const code = stripComments(source);
  if (/cascade-guard:ignore/.test(source)) return [];
  const findings = [];
  for (const rule of rules) {
    if (!rule.match.test(code)) continue;
    if (rule.requireAny.some((evt) => code.includes(evt))) continue;
    findings.push({
      line: null,
      rule: "cascade-write-without-event",
      message:
        `${rule.what} mutates scheduling-relevant state but this file never enqueues ` +
        `${rule.requireAny.join(" / ")}. OperatorConfigReplanHandler is then never notified and already-planned ` +
        "future drives keep the stale cap/roster. Enqueue the cascade event to outbox_messages in the SAME transaction.",
    });
  }
  return findings;
}

// ---------------------------------------------------------------------------
// driver
// ---------------------------------------------------------------------------

function walkGo(dir) {
  const out = [];
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["vendor", "node_modules", "testdata", ".git"].includes(entry.name)) continue;
      out.push(...walkGo(path));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (isGo(rel)) out.push(rel);
    }
  }
  return out;
}

const read = (rel) => readFileSync(join(repo, rel), "utf8");

function selfTest() {
  // --- Rule 1: the exact Bug Class B shape -----------------------------------
  const handlerSrc = `
package app
func (h *OperatorConfigReplanHandler) Register(bus eventbus.Bus) { bus.Subscribe("x", h) }
`;
  const types = handlerTypesIn(handlerSrc);
  if (!types.includes("OperatorConfigReplanHandler")) throw new Error("self-test: handler type not discovered");

  const durableWithout = {
    "backend/internal/kernelstages/bus.go": "obligationapp.NewGoatShiftedHandler(r).Register(bus)",
    "backend/cmd/domain-event-consumer/main.go": "obligationapp.NewGoatShiftedHandler(r).Register(bus)",
  };
  const nonDurableWith = {
    "backend/internal/bootstrap/api.go": "obligationapp.NewOperatorConfigReplanHandler(r).Register(bus)",
    "backend/internal/domainconsumer/wiring/bus.go": "obligationapp.NewOperatorConfigReplanHandler(r).Register(bus)",
  };
  const classB = validateHandlerRegistration({
    handlerTypes: [{ type: "OperatorConfigReplanHandler", rel: "x.go" }],
    durableBusSources: durableWithout,
    nonDurableBusSources: nonDurableWith,
    exemptions: {},
  });
  if (!classB.some((f) => f.rule === "handler-not-on-durable-bus")) {
    throw new Error("self-test: handler registered ONLY on non-durable buses was not flagged");
  }
  if (!classB[0].message.includes("bootstrap/api.go")) {
    throw new Error("self-test: finding must name the dead bus the handler was registered on");
  }

  // half-registered (one durable bus only) is still a failure.
  const halfDurable = {
    "backend/internal/kernelstages/bus.go": "obligationapp.NewOperatorConfigReplanHandler(r).Register(bus)",
    "backend/cmd/domain-event-consumer/main.go": "obligationapp.NewGoatShiftedHandler(r).Register(bus)",
  };
  if (
    !validateHandlerRegistration({
      handlerTypes: [{ type: "OperatorConfigReplanHandler", rel: "x.go" }],
      durableBusSources: halfDurable,
      exemptions: {},
    }).some((f) => f.rule === "handler-not-on-durable-bus")
  ) {
    throw new Error("self-test: handler on only ONE durable bus was not flagged");
  }

  // fully registered (chained ctor) -> clean.
  const bothDurable = {
    "backend/internal/kernelstages/bus.go": "obligationapp.NewOperatorConfigReplanHandler(r).Register(bus)",
    "backend/cmd/domain-event-consumer/main.go":
      "vaccinationapp.NewOperatorConfigReplanHandler(c).WithClosureProjector(s).Register(bus)",
  };
  if (
    validateHandlerRegistration({
      handlerTypes: [{ type: "OperatorConfigReplanHandler", rel: "x.go" }],
      durableBusSources: bothDurable,
      exemptions: {},
    }).length !== 0
  ) {
    throw new Error("self-test: false positive on a fully-registered handler");
  }

  // split ctor/registration form is accepted.
  if (!registersHandler("h := obligationapp.NewFooHandler(r)\nh.Register(bus)", "FooHandler")) {
    throw new Error("self-test: split construct-then-register form not recognised");
  }
  // a commented-out registration must NOT count.
  if (registersHandler("// obligationapp.NewFooHandler(r).Register(bus)", "FooHandler")) {
    throw new Error("self-test: commented-out registration counted as registered");
  }

  // --- Rule 2: exemption must stay honest ------------------------------------
  const staleExempt = validateHandlerRegistration({
    handlerTypes: [{ type: "VerificationNotifier", rel: "x.go" }],
    durableBusSources: { "backend/internal/kernelstages/bus.go": "", "backend/cmd/domain-event-consumer/main.go": "" },
    nonDurableBusSources: { "backend/internal/bootstrap/api.go": "// nothing here anymore" },
    exemptions: DURABLE_BUS_EXEMPTIONS,
  });
  if (!staleExempt.some((f) => f.rule === "stale-durable-bus-exemption")) {
    throw new Error("self-test: stale exemption (declared site no longer registers it) not flagged");
  }
  const honestExempt = validateHandlerRegistration({
    handlerTypes: [{ type: "VerificationNotifier", rel: "x.go" }],
    durableBusSources: { "backend/internal/kernelstages/bus.go": "", "backend/cmd/domain-event-consumer/main.go": "" },
    nonDurableBusSources: {
      "backend/internal/bootstrap/api.go": "notificationbridge.NewVerificationNotifier(a, b, c, log).Register(bus)",
    },
    exemptions: DURABLE_BUS_EXEMPTIONS,
  });
  if (honestExempt.length !== 0) throw new Error("self-test: false positive on an honest exemption");

  // --- Rule 5: Bug Class D ----------------------------------------------------
  const builderHandList = `
	weighingapp.NewVerificationVerdictHandler(weighingpg.NewRepository(pool, queryTimeout), logger).Register(bus)
	calendarapp.NewObligationMissedHandler(calendarService).Register(bus)
`;
  const handListFindings = findingsForBusBuilderSource(builderHandList, "backend/internal/domainconsumer/wiring/bus.go");
  if (!handListFindings.some((f) => f.rule === "bus-builder-missing-verification-applier")) {
    throw new Error("self-test: a bus builder hand-listing only the weighing applier was not flagged");
  }
  if (!handListFindings[0].message.includes("FeedDistributionVerificationHandler")) {
    throw new Error("self-test: rule 5 finding must name the missing applier(s)");
  }
  if (findingsForBusBuilderSource("\teventwiring.RegisterVerificationAppliers(bus, feed, shifting, weighing, log)\n", "x.go").length !== 0) {
    throw new Error("self-test: false positive on a builder that delegates to the shared registration");
  }
  const miswiredSharedCall = findingsForBusBuilderSource(
    "\teventwiring.RegisterVerificationAppliers(bus, nil, countsApprovalRepo, countsMilkPreparationRepo, weighingRepo, nil, nil, logger)\n",
    "backend/cmd/domain-event-consumer/main.go",
  );
  if (!miswiredSharedCall.some((f) => f.rule === "bus-builder-verification-applier-helper-miswired")) {
    throw new Error("self-test: a production builder with a miswired shared registration was not flagged");
  }
  const explicitAll = VERIFICATION_APPLIER_CONSTRUCTORS.map((t) => `\tpkg.New${t}(r, log).Register(bus)\n`).join("");
  if (findingsForBusBuilderSource(explicitAll, "x.go").length !== 0) {
    throw new Error("self-test: false positive on a builder registering all five appliers explicitly");
  }
  if (
    findingsForBusBuilderSource("// eventwiring.RegisterVerificationAppliers(bus, feed, shifting, weighing, log)\n", "x.go")
      .length !== 1
  ) {
    throw new Error("self-test: a commented-out shared registration satisfied rule 5");
  }

  // --- Rule 3: Bug Class A ----------------------------------------------------
  const capBad = `
func totalVaccinationOperatorCap(operators []domain.DriveOperatorCapacity, fallbackCap int32) int32 {
	var total int32
	for _, operator := range operators {
		if operator.Cap > 0 {
			total += operator.Cap
		}
	}
	return total
}
`;
  if (!findingsForOperatorCapSource(capBad).some((f) => f.rule === "operator-cap-ignores-session-load")) {
    throw new Error("self-test: operator.Cap summed without session load was not flagged");
  }
  const capGood = `
func totalVaccinationOperatorCap(tenantID, parkID string, plannedDate time.Time, operators []domain.DriveOperatorCapacity, fallbackCap int32, session *SweepSession) int32 {
	var total int32
	for _, operator := range operators {
		remaining := operator.Cap
		remaining -= session.vaccinationOperatorLoad(tenantID, parkID, plannedDate, operator.OperatorID)
		if remaining > 0 {
			total += remaining
		}
	}
	return total
}
`;
  if (findingsForOperatorCapSource(capGood).length !== 0) {
    throw new Error("self-test: false positive on a session-netted operator cap");
  }
  const capIgnored = capBad.replace("var total int32", "var total int32 // cascade-guard:ignore: pure display sum");
  if (findingsForOperatorCapSource(capIgnored).length !== 0) {
    throw new Error("self-test: cascade-guard:ignore escape hatch not honoured for rule 3");
  }
  // a `.Cap` read on a NON-operator receiver must not trip the rule.
  const capUnrelated = `
func describeBatch(batch domain.Batch) string {
	return fmt.Sprintf("%d", batch.Cap)
}
`;
  if (findingsForOperatorCapSource(capUnrelated).length !== 0) {
    throw new Error("self-test: false positive on a non-operator .Cap read");
  }

  // --- Rule 4: Bug Class C ----------------------------------------------------
  const writeBad = `
UPDATE workforce_positions
SET week_off_weekday = CASE WHEN $8 THEN nullif($9, '') ELSE week_off_weekday END,
    vaccination_daily_animal_cap = CASE WHEN $10 THEN $11::int ELSE vaccination_daily_animal_cap END
WHERE tenant_id = $1::uuid
`;
  if (!findingsForCascadeWriteSource(writeBad).some((f) => f.rule === "cascade-write-without-event")) {
    throw new Error("self-test: operator-seat cap/week-off update with no cascade event was not flagged");
  }
  const writeGood = `${writeBad}\nif err := enqueue("vaccination.capacity.changed"); err != nil { return err }\n`;
  if (findingsForCascadeWriteSource(writeGood).length !== 0) {
    throw new Error("self-test: false positive on a cap update that DOES enqueue the cascade event");
  }
  // a comment naming the event must NOT satisfy the rule.
  const writeCommentOnly = `${writeBad}\n// TODO: emit vaccination.capacity.changed here\n`;
  if (!findingsForCascadeWriteSource(writeCommentOnly).some((f) => f.rule === "cascade-write-without-event")) {
    throw new Error("self-test: a commented-out cascade event satisfied rule 4");
  }
  const capacityConfigBad = "INSERT INTO vaccination_capacity_config (tenant_id, max_per_day) VALUES ($1, $2)";
  if (!findingsForCascadeWriteSource(capacityConfigBad).length) {
    throw new Error("self-test: tenant capacity-config write with no cascade event was not flagged");
  }
  const assignmentConfigBad = "UPDATE vaccination_operator_assignment_config SET active_operators_per_day = $2";
  if (!findingsForCascadeWriteSource(assignmentConfigBad).length) {
    throw new Error("self-test: operator-assignment-config write with no cascade event was not flagged");
  }
  const unrelated = "UPDATE workforce_positions SET position_tier = $2 WHERE position_id = $1";
  if (findingsForCascadeWriteSource(unrelated).length !== 0) {
    throw new Error("self-test: false positive on a non-scheduling position column");
  }

  console.log("cascade-event-wiring self-test: ok (5 rules, 21 adversarial fixtures)");
}

function run() {
  const findings = [];

  // Rule 1/2 — durable-bus registration.
  const missingBus = DURABLE_BUS_FILES.filter((rel) => !existsSync(join(repo, rel)));
  if (missingBus.length) {
    console.error(`cascade-event-wiring: FAIL — durable bus file(s) missing: ${missingBus.join(", ")}`);
    console.error("If a bus moved, update DURABLE_BUS_FILES in this guard in the same commit.");
    process.exit(1);
  }
  const durableBusSources = Object.fromEntries(DURABLE_BUS_FILES.map((rel) => [rel, read(rel)]));
  const nonDurableBusSources = Object.fromEntries(
    NON_DURABLE_BUS_FILES.filter((rel) => existsSync(join(repo, rel))).map((rel) => [rel, read(rel)]),
  );

  const backendInternal = walkGo(join(repo, "backend/internal"));
  const handlerTypes = [];
  for (const rel of backendInternal) {
    for (const type of handlerTypesIn(read(rel))) handlerTypes.push({ type, rel });
  }
  findings.push(
    ...validateHandlerRegistration({ handlerTypes, durableBusSources, nonDurableBusSources }).map((f) => ({ ...f })),
  );

  // Rule 5 — every domain-bus builder registers every verdict applier.
  const missingBuilders = DOMAIN_BUS_BUILDERS.filter((rel) => !existsSync(join(repo, rel)));
  if (missingBuilders.length) {
    console.error(`cascade-event-wiring: FAIL — domain bus builder file(s) missing: ${missingBuilders.join(", ")}`);
    console.error("If a builder moved, update DOMAIN_BUS_BUILDERS in this guard in the same commit.");
    process.exit(1);
  }
  for (const rel of DOMAIN_BUS_BUILDERS) {
    findings.push(...findingsForBusBuilderSource(read(rel), rel));
  }

  // Rule 3 — sweeper-side operator cap.
  const sweeperFiles = walkGo(join(repo, "backend/internal/obligation/app"));
  for (const rel of sweeperFiles) {
    for (const f of findingsForOperatorCapSource(read(rel))) findings.push({ ...f, rel });
  }

  // Rule 4 — cascade-relevant writes.
  for (const rel of backendInternal) {
    for (const f of findingsForCascadeWriteSource(read(rel))) findings.push({ ...f, rel });
  }

  if (findings.length) {
    console.error(
      `cascade-event-wiring: ${findings.length} anti-pattern(s) (see docs/decisions/scale-anti-patterns.md -> "Operator-cascade wiring anti-patterns")`,
    );
    for (const f of findings) console.error(`- ${f.rule} ${f.rel}${f.line ? `:${f.line}` : ""}: ${f.message}`);
    process.exit(1);
  }
  console.log(
    `cascade-event-wiring: ok (${handlerTypes.length} eventbus handler(s) on ${DURABLE_BUS_FILES.length} durable bus(es); ` +
      `${DOMAIN_BUS_BUILDERS.length} bus builder(s) register all ${VERIFICATION_APPLIER_CONSTRUCTORS.length} verdict appliers; ` +
      `` +
      `${sweeperFiles.length} sweeper file(s) net session-reserved operator load; ` +
      `${backendInternal.length} backend/internal file(s) scanned for un-cascaded scheduling writes)`,
  );
}

if (process.argv.includes("--self-test")) selfTest();
else run();
