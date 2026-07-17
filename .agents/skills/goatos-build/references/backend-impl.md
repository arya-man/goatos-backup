# Backend Implementation Reference

Load this when extending or reviewing the Go backend.

Canonical docs:

- `docs/decisions/go-backend-stack.md`
- `backend/AGENTS.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/protocol-engine/obligation-engine.md`
- `docs/protocol-engine/state-machines.md`
- `context/architecture/operational-kernel.md`
- `docs/preventive-care-vaccination/TRD.md`
- `docs/preventive-care-vaccination/V1-FOUNDATION-SPEC.md`
- `context/execution/vaccination-process-integrity-backend-handoff.md`
- `context/execution/calendar-vaccination-slice-parallel-handoff.md`
- `docs/decisions/calendar-ownership.md`
- `context/execution/sop-vaccination-backend-handoff.md`

## Current Backend Shape

```text
backend/cmd/api                    API process entrypoint
backend/cmd/migrate                migration runner
backend/cmd/outbox-relay           local/dev outbox relay
backend/cmd/seed-dev-grant         local dev grant seed
backend/cmd/seed-dev-email-grants  local dev email grants seed
backend/cmd/mint-dev-token         local bearer token helper

backend/internal/bootstrap         explicit constructor wiring
backend/internal/platform          auth, middleware, observability, pg helpers
backend/internal/identity          Animal Passport read/write identifier surface
backend/internal/protocol          config/rule/version authority
backend/internal/obligation        obligation/status engine foundation
backend/internal/vaccination       vaccination execution/proof/verification
backend/internal/inventory         stock/ledger foundation
backend/internal/sop               SOP template/policy foundation
backend/internal/tasks             task/SOP execution foundation
backend/internal/locations         park/shed foundation data
backend/internal/workforce         owner/operator foundation data
backend/internal/outbox            event egress foundation
backend/internal/feed              feed direction foundation
```

The active build is the vaccination process-integrity slice:

```text
Admin config/SOP policy
  -> protocol versions and rules
  -> vaccination obligations and drives
  -> proof and verification state
  -> vaccination execution context
  -> Calendar vaccination due-work projection and actions
  -> process-integrity projection
  -> Control Tower / Action Center / Protocol Adherence / Workflow drilldowns
```

The dashboard exists to prove the configured process is being followed. Backend
must expose process state as source of truth; frontend must not guess it. For the
current slice, read `context/execution/vaccination-process-integrity-backend-handoff.md`
before changing vaccination projections, Action Center APIs, Control Tower APIs,
or SOP/proof/verification completion flow.

Every backend feature must satisfy the operational kernel: canonical transaction
plus audit/idempotency/outbox, trigger evaluation, obligation/work item or
process exception, sweeper/reminder/deadline handling, durable notification or
escalation request, proof/verification where required, and read models that show
process followed/broken/owner/next action. Do not build feature-local schedulers,
queues, alert paths, or frontend-owned process truth.

Vaccination drive planning rule: per-animal due dates are not execution-drive
boundaries. Generation creates one obligation per animal/rule/dose, but SM-4
must club compatible due obligations into the highest-output valid shed/park
drive inside the authored safe window and one-time batching hold. Do not group
vaccination batches by exact `due_at` or exact window before the planner scores
compatible work. Exact-date micro-drives are allowed only when no compatible work
can be safely clubbed before the earliest selected animal's last safe date.
Backfill/window sweeps must keep `asOf` separate from `dueBefore`: `asOf` is the
operational sweep day for hold/backdating/planned-date math, while `dueBefore`
is only the obligation eligibility cutoff. Batched Calendar, Vaccination
Execution, and Process Integrity rows must render, sort, and classify by
`obligation_batches.planned_date`, falling back to obligation `due_at` only for
unbatched rows. Run `make vaccination-drive-clubbing-guard` after changing
generation, sweeper, drive planner, Calendar/process projections, or vaccination
seed data.

For Calendar work, read
`context/execution/calendar-vaccination-slice-parallel-handoff.md` and
`docs/decisions/calendar-ownership.md` before adding routes, projections,
workers, seed data, or admin-web contracts. Calendar is a time lens over current
vaccination due work, not a source of truth and not the full vaccination matrix.
Use a generic `CalendarEvent` contract with vaccination detail blocks; keep
nudge/snooze durable and idempotent outside the projection.

## Process-Integrity And Handoff Guardrails

- Keep one process-integrity command model. Vaccination may be the default
  domain today; procurement must later feed the same CT/AC/PA/WF surfaces through
  `?domain=procurement`, not a nested procurement command engine.
- Keep one SOP/proof/verification engine. Procurement source-health, dispatch,
  transit, and arrival proof extend existing SOP/proof modules and policies; do
  not create a procurement-only SOP runtime or media path.
- Treat `procurement_pc_handoffs.event_status` as tracking only. The accepted
  intake handoff is proven only when tests show the handler/consumer generated
  vaccination obligations and CT/AC/PA/WF/Vaccination read models reflect them.
- Large dirty SOP/contract changes must be audited as extensions of current
  modules before new prompts build on them.

## Current API/RBAC Surface

Keep the live surface focused:

- Goat search/passport/timeline/identifier add-retire.
- Protocol config and publish/version APIs.
- Obligation and vaccination APIs.
- Calendar vaccination list/detail/history/nudge/snooze APIs only after they are
  registered in `backend/internal/permissions/routes.go`, covered by
  route-registry tests, bounded by date-window/query-plan tests, and backed by
  canonical Postgres state.
- SOP/task foundation APIs that serve the current config/execution slice.
- Locations/workforce APIs only as foundation data for park/shed/owner context.

Do not re-add old Phase 1 review APIs for import runs, conflict queues,
candidate queues, correction queues, legacy sync, counts dashboards, mortality
dashboards, or reporting counters unless the product scope is explicitly
reopened.

## Rules

- Handlers stay thin: parse request, call app service, write contract-shaped
  response/error envelope.
- App services own behavior, state transitions, idempotency, and error mapping.
- Domain/app/ports must not import HTTP or pgx.
- Postgres adapters satisfy module-owned ports and keep SQL tenant-scoped.
- Triggers, reminders, deadline alerts, and notifications must use shared
  kernel ports/adapters and durable Postgres/outbox state. Google SDKs, Redis,
  Slack, FCM, email, Opsgenie/PagerDuty-style webhooks, or Cloud Tasks clients
  stay in adapters, not domain/app logic.
- Do not scan the full herd in API paths. Use indexed lookups, bounded limits,
  keyset pagination, chunked workers, and query-plan coverage for hot paths.
- Backend auth/RBAC is the security boundary. Frontend visibility is not
  authority.
- Every new protected route must be registered in
  `backend/internal/permissions/routes.go` with permission/role tests before the
  frontend depends on it. Calendar read/history should require calendar/domain
  read semantics; Calendar actions such as nudge/snooze need explicit action
  permission and tenant/park/shed scope checks.
- Construct loggers via `backend/internal/platform/observability`.
- Emit durable domain events through outbox where downstream status/projection
  consumers will need them.
- In pinned-clock tests, derive every time-sensitive fixture field from the same
  anchor passed to the production path. Mixing that anchor with SQL `now()` or
  another `time.Now()` creates wall-clock-dependent eligibility failures.

## Idempotent Write Path Checklist

Before merging or reviewing any mutating route, importer, worker, webhook,
consumer, server action, or state transition, document and prove:

- The source of the idempotency key or deterministic operation identity.
- The storage table/index for the key plus semantic request fingerprint.
- The transaction boundary that owns key insert/lookup, domain state changes,
  and outbox/event writes.
- Exact replay behavior: return the original result without calling downstream
  effects, emitting duplicate events, or advancing state again.
- Conflict behavior: the same key with a different semantic payload must be
  rejected or return the original result with no new side effects.
- Postgres adapters branch before side effects when an existing key is found.
  Do not rely on `ON CONFLICT DO UPDATE` with only `idempotency_key =
  EXCLUDED.idempotency_key` if later code still updates state from the replay
  body.
- Add/attach paths that create canonical identity rows persist idempotency before
  creating or mutating those rows.
- Outbox producers, consumers, import replays, webhook handlers, and background
  workers carry an operation id and dedupe before doing work.
- Tests cover first write, exact replay, same-key different-payload replay,
  downstream event/outbox dedupe, and concurrent retry behavior when the path can
  be retried in parallel.

## Removed From Runtime

These modules/commands were old dashboard/import/review surfaces and are no
longer active backend runtime code:

```text
backend/cmd/rfid-import
backend/cmd/rfid-apply
backend/cmd/rebuild-identity-counters
backend/cmd/update-identity-counters
backend/internal/counts
backend/internal/mortality
backend/internal/reporting
backend/internal/legacy_import
backend/internal/legacy_sync
old identity Import Review / conflict queue / candidate / correction use cases
```

For the clean-slate V1 base correction, do not preserve those old tables or
public fields in the final schema, SQLC snapshots, OpenAPI, generated clients,
admin-web copy, or seed data. If a fresh branch still shows old dashboard/BQ
port names such as `legacy_import_*`, `legacy_sync_*`, BQ snapshot mirrors,
goat identity counters, `identity_state`, `source_confidence`, `old_tag`,
`sheet_row_id`, `external_system_id`, `origin_type='unknown'`, or `legacy_bq*`
source contexts in active contracts, treat that as cleanup work before
implementation proceeds.

## Verification

For backend cleanup or implementation, run the narrow package tests first, then
the full backend suite when feasible:

```bash
cd /path/to/goatos/backend
go test ./internal/identity/...
go test ./...
```

For OpenAPI changes:

```bash
cd /path/to/goatos
make api-client-generate
make api-client-check
./tools/agent-hooks/check-contract-drift.sh
```
