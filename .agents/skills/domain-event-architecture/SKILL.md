---
name: domain-event-architecture
description: Use when adding or reviewing any Goat OS backend, frontend, or mobile feature that creates, imports, updates, moves, closes, or consumes business state through domain events, outbox, CRUD, sheet imports, mobile offline writes, shifting, dead birth, feed direction, vaccination, or future operational modules.
version: 0.1.0
user-invocable: true
argument-hint: "[feature/module/event]"
---

# Domain Event Architecture Lens

Load this lens before changing any business mutation path in backend, admin-web,
or mobile. It fronts the canonical contract:

```text
context/architecture/domain-event-integration-contract.md
context/architecture/domain-event-registry.json
.agents/skills/goatos-build/references/contracts-events.md
.agents/skills/goatos-code-review/references/kernel-and-scale.md
```

Also load `frontend.md` for admin-web changes and `mobile.md` for Android/offline
changes.

Hard rule:

- Register the producer, event, consumer, replay/DLQ behavior, and E2E proof in
  `context/architecture/domain-event-registry.json`.
- Backend canonical state, audit when relevant, and transactional outbox must be
  one unit.
- Frontend/mobile send idempotent commands and render backend-owned contracts;
  they do not create business follow-up state.
- Direct animal-table writers must already be registered, or the guard fails.
- Registering a consumer is not wiring it. Only
  `backend/internal/kernelstages/bus.go` (`BuildDomainBus`) and
  `backend/cmd/domain-event-consumer/main.go` are buses the real outbox relay
  dispatches to. `backend/internal/bootstrap/api.go` is the API process's own
  in-process bus, and `backend/internal/domainconsumer/wiring/bus.go` is wired
  into no `cmd/*` binary (tests only). Every `Register(bus eventbus.Bus)` type
  must be registered on BOTH durable buses, and the wiring assertion must name
  the production bus builder — an E2E that constructs its own bus proves handler
  logic and nothing about dispatch. `make cascade-event-wiring-guard` fails
  closed on this; deliberate omissions go in `DURABLE_BUS_EXEMPTIONS` with a
  reason and the file that really registers the handler.
- A write that mutates a scheduler input (vaccination operator cap, week-off,
  status/validity, tenant `vaccination_capacity_config`,
  `vaccination_operator_assignment_config`) must enqueue its cascade event
  (`vaccination.capacity.changed` / `vaccination.roster.changed`) to
  `outbox_messages` in the SAME transaction as the state change. Coverage is per
  WRITE PATH: a sibling endpoint already emitting the event does not cover a new
  one. See `docs/decisions/scale-anti-patterns.md` -> "Operator-cascade wiring
  anti-patterns".
- A shed-movement writer activates the prospective
  `shifting_completion_to_vaccination` contract. It must resolve the destination
  operational stage from active `shed_profiles -> animal_stage_lookup`, snapshot
  profile ID + `row_version`, atomically update shed and stage on verified
  completion, publish per-animal location/stage events, and prove the real
  Vaccination rescope + eligibility-recheck handoff in one E2E. Inferring the
  destination stage from resident goats or proving producer and consumers only
  in separate tests is a blocked anti-pattern.
- Movement never invents pregnancy, lactation, health, or reproductive truth.
  Those facts change only through their authoritative clinical workflows.

Required check:

```bash
make domain-event-architecture-guard
make ci-local JOB=common
```

The common local-CI job is mandatory enforcement. Wiring a guard only into the
legacy `JOB=guardrails` compatibility helper does not protect normal PR/local
runs and fails `guardrail-registration-guard`.
