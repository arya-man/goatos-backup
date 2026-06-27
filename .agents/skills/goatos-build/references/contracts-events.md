# Contracts And Events Reference

Load this when adding or changing OpenAPI, JSON Schema, protobuf, event
contracts, outbox payloads, generated clients, decision records, or DLQ repair.

Canonical docs:

- `context/agents/ai-agent-context-and-protocols.md`
- `context/execution/next-contracts.md`
- `context/execution/calendar-vaccination-slice-parallel-handoff.md`
- `docs/decisions/calendar-ownership.md`
- `context/architecture/operational-kernel.md`
- `context/architecture/final-architecture.md`

Contract families:

```text
OpenAPI     app/admin/mobile/analytics APIs
JSON Schema form DSL, submissions, events, decision records, DLQ payloads
Protobuf    selected high-volume telemetry/internal service contracts only
```

Rules:

- Single source per contract family.
- Generate clients/structs outward; do not hand-copy DTOs.
- Every event has version, schema ref, idempotency key, occurred_at,
  recorded_at, actor, subject, trace, and evidence references where relevant.
- Domain event envelope uses both aggregate and subject when needed:
  `aggregate_type`/`aggregate_id` is the stream owner used for ordering,
  replay, and outbox routing; `subject_type`/`subject_id` is the entity the
  event is about when that differs from the aggregate.
- Events carry tenant/visibility scope as structured fields, not ad-hoc
  farm-only strings.
- Decision records capture proposal/approval/rejection/needs_review state,
  policy_version, evidence IDs, source records/media/events, reviewer, and
  reason. AI suggestions are proposals, never direct truth.
- Consumers must be idempotent.
- Replay and DLQ repair are part of the contract, not afterthoughts.
- Generated-client drift is active for `packages/api-client`: contract changes
  without regenerated OpenAPI TypeScript clients must fail CI.
- UI presentation contracts are first-class app API contracts. When backend owns
  admin-web/operator-mobile labels, navigation, filters, sort keys, chips,
  drawers, actions, disabled reasons, or summary/detail field sets, publish the
  shape in OpenAPI, regenerate clients, and validate contracts before frontend
  handoff. Do not let React page constants become the source of product truth.

Operational kernel contracts:

- Any feature that introduces a business trigger must define the domain event,
  idempotency key, outbox payload, consumer replay behavior, DLQ/repair behavior,
  and read-model projection contract.
- Reminder, nudge, deadline, notification, escalation, proof, verification, and
  completion actions must be durable backend contracts, never frontend-only
  state.
- Read-model contracts must expose enough state for command lenses to answer:
  expected process, followed/broken status, owner, next action, due time,
  evidence, reminder state, escalation state, and history.

Calendar contract rule:

- The shared Calendar summary contract is generic `CalendarEvent`; do not name
  the common Calendar base contract after vaccination.
- `/calendar/vaccination/events/*` may return vaccination-specific detail blocks,
  but the common contract must stay reusable for future event sources.
- Calendar list routes must declare bounded `date_from`/`date_to`, cursor
  pagination, owner pill filters, status filters, and protected action contracts
  for nudge/snooze/history.
- Calendar actions are contractually idempotent; same key plus different payload
  is an idempotency conflict with no side effects.
