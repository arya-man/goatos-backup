# Domain Event Integration Contract

Date: 2026-07-18

Purpose: keep backend, admin-web, and mobile features on one operational
event spine. A screen, import, sheet seed, mobile offline command, worker, or
future module must not create its own private follow-up pipeline.

## Rule

Every business mutation follows this shape:

```text
command or import row
  -> canonical Postgres transaction
  -> audit row where product-relevant
  -> domain event + transactional outbox in the same transaction
  -> idempotent consumers
  -> obligations / batches / reminders / read models
  -> replay and DLQ repair proof
  -> backend contract rendered by admin-web and mobile
```

This applies to backend, frontend, and mobile. Frontend and mobile do not own
business truth. They send idempotent commands, render backend/Room state, and
show backend-owned disabled reasons, errors, and next actions.

## Feature Registration Packet

Before a feature that creates, imports, updates, moves, closes, or reclassifies
business state lands, it must register:

- Producer command/route/import/worker and canonical table(s) changed.
- Domain event type, schema version, idempotency key, aggregate, subject,
  tenant/park/shed visibility scope, trace, actor, and evidence fields.
- Transaction boundary proving canonical state, audit, and outbox cannot drift.
- Consumers, including replay order, idempotency behavior, and DLQ/repair path.
- Downstream obligations, batches, reminders, escalations, read models, and
  UI/mobile contract fields affected.
- E2E/integration proof that exercises the producer and at least one real
  consumer, not only a unit-level fake.
- Frontend/mobile behavior: generated client shape, command idempotency key,
  offline queue behavior if mobile writes, Room/cache invalidation, and visible
  error state.

### Audit-only events (maintainer decision 2026-07-19)

An event MAY be registered with `consumers: []` as an **audit-only** event when
its business effect is already applied transactionally by the producing command
and the event exists solely as durable lineage/audit trail. Requirements:

- The registry entry must carry a real durable producer and the event must pass
  envelope-schema validation (contract-validation test required in place of the
  producer-to-consumer E2E proof).
- No runtime bus may subscribe a handler to an audit-only event; the
  runtime-subscription <-> registry parity pass in
  `tools/agent-hooks/check-domain-event-architecture.mjs` enforces this in both
  directions (unregistered subscription fails; subscription to a zero-consumer
  event fails). A no-op handler is never an acceptable substitute.
- Promoting an audit-only event to consumed requires the full registration
  packet above, including the real-consumer E2E proof.

Current audit-only events: `goat.obligations_canceled`, `obligation.rescoped`,
`obligation.in_progress`.

The machine registry is `context/architecture/domain-event-registry.json`.
`make domain-event-architecture-guard` must pass before landing.

## Herd Mutation Law

Any mutation to a live animal's identity, shed, park, stage, health,
reproductive state, lifecycle, exit, or import facts must go through a registered
producer or seed path. Goat OS does not support live goats without a real park
and shed in normal data. Seed/import may fill missing park/shed/stage properties
from approved source context while the system is still being built; it must not
fabricate vaccination history.

Normal eligible animals are never sent to a manual-review escape hatch just
because planning is hard. Vaccination generation must open or reopen a legal
window when a held animal returns; the sweeper must place it inside its legal
range or the +1 week hold window, maximizing animals up to policy and squeezing
last-safe-day overflow when required.

Manual review is reserved for system invariant failure, not routine planning:
missing registered writer, impossible DB window, runtime config/proof mismatch,
or an unconfigured same-priority cap conflict. Such failures must be loud in
proof/CI and must not block unrelated valid animals from batching.

## Shed Movement → Vaccination Closure

A shed shift is not complete when only `goats.shed_id` changes. The destination
shed owns an active operational profile; resolve it from `shed_profiles` through
`animal_stage_lookup`, snapshot its profile ID and `row_version` when the move is
authorized, and revalidate that snapshot when the second approval/completion gate applies. Resident goats
are observations and must never be queried as the authority for a destination
stage. A missing, inactive, ambiguous, incompatible, or changed profile blocks
completion without partial state or count effects.

Approval alone records intent and emits no location/stage fact. Operator completion alone also emits
no location/stage fact while approval is absent. The transaction recording the second of Park Head
approval and operator completion:

1. locks and validates the goat, source shed, destination shed, and approved
   destination profile snapshot;
2. updates `shed_id` and `management_stage` together;
3. records identity audit/history;
4. publishes a per-animal `goat.location.changed` event and, when different, a
   `goat.stage_changed` event through the transactional outbox; and
5. applies source/destination count legs from the snapshotted source and
   destination stages, never one old stage on both legs.

Vaccination then has two distinct required effects. The location consumer
rescopes open scheduled/due/deferred work and planned batches to the destination
shed while preserving in-progress/completed history. The location/stage recheck
consumer recomputes eligibility and schedule from current authoritative facts.
Watermarks/idempotency must prevent stale or replayed movement events from
undoing newer state.

Movement may change operational stage because the configured destination profile
requires it; it may not infer or fabricate pregnancy, lactation, health,
reproductive, or medical confirmation. Those facts remain separate
authoritative commands/events even when they influence whether a move is legal.

The acceptance proof is a production-path E2E beginning with the second approval/completion gate
and ending after the real Vaccination rescope and recheck handlers have produced
the correct shed-scoped result. Separate producer integration tests and consumer
unit/integration tests do not prove the handoff and do not satisfy this contract.
`movementIntegrationContracts.shifting_completion_to_vaccination` in the
registry activates mechanically when the canonical shifting relocation writer
appears.

## Future Feature Examples

Shifting:

- Producer: authorized + operator-completed shifting emits `goat.location.changed` and
  emits `goat.stage_changed` when the destination shed profile changes the
  operational stage.
- Deployed consumers: vaccination rescope/recheck and obligation rescope. Future
  consumers such as feed direction and counts must register before activation;
  planned modules are not described as already implemented.
- Edge cases: sick to normal, ICU/quarantine entry and exit, death/cull/sale
  closure, pregnant/mother state, and stale out-of-order move events.

Dead birth:

- Producer: reproductive/birth command emits reproductive/birth event(s).
- Consumers: breeding history, health follow-up, feed direction for mother,
  vaccination eligibility only for live registered offspring.
- Dead offspring do not create vaccination obligations; mother state changes
  still trigger downstream policy checks.

Feed direction:

- Producer/consumer mix: consumes goat stage, location, health, reproductive,
  and feed protocol publish events; produces feed obligations/batches.
- It must use the same outbox/idempotency/replay/DLQ contract as vaccination,
  not a private Slack-only or frontend-only workflow.

## E2E Floor

Every feature in this family needs at least these proof shapes:

- Single command and sheet/import path both produce canonical state and event.
- Backend consumer runs and updates downstream obligations/read models.
- Replay of the same event is idempotent.
- Out-of-order stale event cannot undo newer state.
- Death/cull/sale cancels open work and preserves completed history.
- Held states such as sick, ICU, late pregnancy, and recovering reopen or defer
  through the same event chain when they change.
- Admin-web renders backend-owned contract state; mobile writes, when present,
  use an offline idempotent command and later converge through the same event.
