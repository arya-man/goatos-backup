# Calendar Vaccination Backend Proof

Date: 2026-06-27

Scope: backend/contracts for the PHC Vaccination Calendar slice only.

## Kernel 13-Step Review

1. Canonical business event: published source-backed vaccination protocol rules, generated obligations/batches, linked SOP proof/verification/rework tasks, and config/source review tasks produce dated human Calendar events through the `calendar-vaccination-projector` production refresh path into `calendar_event_projections`.
2. Transaction boundary: nudge/snooze/reminder and escalation acknowledge/resolve writes persist idempotency, durable notification/snooze/escalation state, audit rows, status events, and outbox messages in one Postgres transaction.
3. Trigger rule: vaccination due work remains owned by the protocol/obligation engine; Calendar admits only projection rows with due/window, executable owner, source backing, and human action.
4. Obligation/task/review/batch: event rows carry source target type/id plus protocol/rule/batch/SOP/link detail for PHC, inventory, and admin-data-ops work.
5. Deadlines/reminders/nudges/escalations: list/detail expose due/window/reminder/escalation fields; reminder sweeper queues durable reminders; nudge, snooze, escalation acknowledge, and escalation resolve are backend actions.
6. Deadline crossing: `overdue`, `blocked`, `rework_due`, and escalation state are Calendar read-model statuses derived from source state, without mutating obligation truth.
7. Proof/evidence: detail blocks include source/rule, SOP proof, verification, stock, and history links; proof/rework rows are represented as dated human actions.
8. Verification/acceptance: verification pending/rejected/rework events carry verifier labels and proof/verification detail, preserving the SOP verification boundary.
9. Read models: Calendar list/detail/history are source-backed projection reads with action/audit/notification/snooze/escalation history and tenant/park/shed authorization scope; they do not reconstruct truth in frontend state.
10. Scale: list API enforces 45-day max, keyset cursor pagination, max 200 rows, tenant/owner/status/park/shed filters plus actor-scope predicates, and `CalendarVaccinationWidestList` query-plan validation.
11. Operations: projector and sweepers have bounded `-limit`; reminder/escalation sweepers require an explicit tenant and persist each event in its own transaction; outbox rows feed the relay/domain event path; audit/history rows carry tenant, event id, action, channel, trace, status, and idempotency metadata.
12. Local Docker parity: fresh Docker Postgres applies migrations, runs the same seed/projector/sweeper command binaries, and queries durable rows.
13. Seed/E2E data: seed covers CBE/CPT, PHC/Inventory/Admin Data Ops, due/overdue/future/in-progress/proof/verification/rework/deferred/blocked/snoozed/escalated statuses, negative system exclusion, scope-negative cases, reminder history, and escalation acknowledgement/resolution regressions.

## Verified Gates

- `cd backend && go test ./...`
- `cd backend && go test ./internal/calendar/... ./internal/permissions/... ./internal/platform/httpmiddleware ./internal/identity/adapters/http`
- `cd backend && go test ./cmd/calendar-reminder-sweeper ./cmd/calendar-vaccination-projector`
- `make api-client-check && ./tools/agent-hooks/check-contract-drift.sh`
- `bash backend/tests/integration/validate-postgres-migrations.sh`
- `bash backend/tests/integration/validate-sqlc-query-plans.sh`
- `make api-client-generate`
- 2026-06-27 escalation ack/resolve regression gates:
  - `cd backend && go test ./internal/calendar/adapters/postgres`
  - `cd backend && go test ./internal/calendar/app ./internal/permissions ./internal/calendar/adapters/http`
  - `bash backend/tests/integration/validate-postgres-migrations.sh`
  - `npm --prefix tools/contract-validation run validate`
  - `npm --prefix packages/api-client run generate`
- Docker-backed regression coverage:
  - production projection refresh from a source-backed vaccination obligation.
  - tenant/park/shed scoped list/detail/nudge enforcement.
  - escalation acknowledgement/replay/conflict, escalation resolution, and
    multi-level active ladder closure.
  - direct detail/history reads hide `system=true` events.
  - idempotency replay plus same-key/different-payload `409` conflicts.
  - widest list SQL/query-plan validation.
- Throwaway Docker seed proof:

```text
seeded calendar vaccination fixtures tenant=00000000-0000-4000-8000-000000000001 parks=CBE,CPT protocol_version=86000000-0000-4000-8000-000000000502
calendar reminder sweep queued=2 tenant=00000000-0000-4000-8000-000000000001

owner_key       events
admin_data_ops 1
inventory      3
phc            8

notification_type notifications
reminder          3
```
