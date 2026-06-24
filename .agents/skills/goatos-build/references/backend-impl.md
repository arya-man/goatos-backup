# Backend Implementation Reference

Load this when extending or reviewing the Go backend.

Canonical docs:

- `docs/decisions/go-backend-stack.md`
- `backend/AGENTS.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/protocol-engine/obligation-engine.md`
- `docs/protocol-engine/state-machines.md`
- `docs/phc-vaccination/TRD.md`
- `docs/phc-vaccination/V1-FOUNDATION-SPEC.md`
- `context/execution/vaccination-process-integrity-backend-handoff.md`
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
backend/internal/identity          Goat Passport read/write identifier surface
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
  -> process-integrity projection
  -> Control Tower / Action Center / Protocol Adherence / Workflow drilldowns
```

The dashboard exists to prove the configured process is being followed. Backend
must expose process state as source of truth; frontend must not guess it. For the
current slice, read `context/execution/vaccination-process-integrity-backend-handoff.md`
before changing vaccination projections, Action Center APIs, Control Tower APIs,
or SOP/proof/verification completion flow.

## Process-Integrity And Handoff Guardrails

- Keep one process-integrity command model. Vaccination may be the default
  domain today; procurement must later feed the same CT/AC/PA/WF surfaces through
  `?domain=procurement`, not a nested procurement command engine.
- Keep one SOP/proof/verification engine. Procurement source-health, dispatch,
  transit, and arrival proof extend existing SOP/proof modules and policies; do
  not create a procurement-only SOP runtime or media path.
- Treat `procurement_phc_handoffs.event_status` as tracking only. The accepted
  intake handoff is proven only when tests show the handler/consumer generated
  vaccination obligations and CT/AC/PA/WF/Vaccination read models reflect them.
- Large dirty SOP/contract changes must be audited as extensions of current
  modules before new prompts build on them.

## Current API/RBAC Surface

Keep the live surface focused:

- Goat search/passport/timeline/identifier add-retire.
- Protocol config and publish/version APIs.
- Obligation and vaccination APIs.
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
- Do not scan the full herd in API paths. Use indexed lookups, bounded limits,
  keyset pagination, chunked workers, and query-plan coverage for hot paths.
- Backend auth/RBAC is the security boundary. Frontend visibility is not
  authority.
- Construct loggers via `backend/internal/platform/observability`.
- Emit durable domain events through outbox where downstream status/projection
  consumers will need them.

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

Postgres migration history may still contain old tables so existing local/dev
databases can migrate forward safely. Do not treat migration history as active
runtime design.

## Verification

For backend cleanup or implementation, run the narrow package tests first, then
the full backend suite when feasible:

```bash
cd /Users/ravi/mesha/goatos/backend
go test ./internal/identity/...
go test ./...
```

For OpenAPI changes:

```bash
cd /Users/ravi/mesha/goatos
make api-client-generate
make api-client-check
./tools/agent-hooks/check-contract-drift.sh
```
