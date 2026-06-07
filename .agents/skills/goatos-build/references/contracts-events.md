# Contracts And Events Reference

Load this when adding or changing OpenAPI, JSON Schema, protobuf, event
contracts, outbox payloads, generated clients, decision records, or DLQ repair.

Canonical docs:

- `context/agents/ai-agent-context-and-protocols.md`
- `context/execution/next-contracts.md`
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
- Contract drift check becomes real when generated clients exist: contract
  change without regenerated clients must fail CI.
