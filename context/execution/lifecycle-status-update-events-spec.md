# Lifecycle status-update events → obligation recompute (DRAFT spec v2)

Status: **APPROVED for full-scope build (all-in, do NOT split the slice).**
Build the complete slice, verify (go build/test + smoke + adversarial verify +
the full 1M synthetic gate below), then **wait for explicit go before pushing
`main`.** No "handles 1M" wording anywhere until the full gate passes.
Pregnancy facts (`breeding_date` + `last_delivery_date`) are IN this slice per
the go-ahead — pregnancy timing is built for real, not review-only.

Scale claim: this design is **compatible with million-scale operation**. It does
**not** "handle 1M operations" until the bulk-operation kernel (below) and its 1M
synthetic verification gate exist and pass. Do not overclaim in code/docs/PR.

## Why

A goat/sheep status change should recompute vaccination obligations
(generate/cancel/defer/reopen) immediately and symmetrically, for both species,
via any entry path. Two holes: (1) reproductive changes are not event-driven
(no runtime write path emits), and (2) there is no bulk status-update. Root: no
event-emitting status-update path beyond single-goat exit/health/stage/location.

## Review corrections folded in (v1 → v2)

1. **Pregnancy behavior was overclaimed.** `pregnancyDeferReason`
   (`schedule_policy.go:227`) needs `BreedingDate` (pregnant→month) and
   `LastDeliveryDate` (post-delivery catch-up window). Verified: `breeding_date`
   / `last_delivery_date` are **not columns** in the goats schema and the
   eligible-goat query (`repository.go:1837`) does **not** select them, so both
   are always nil. Today a `pregnant` status yields only
   `pregnancy_month_review` (a review flag), and post-delivery catch-up never
   fires precisely. → Spec now **phases** this (Phase B) and does not claim exact
   month/window behavior until the date facts exist.
2. **Vocabulary source corrected.** Validation comes from **active
   `status_definitions(axis='reproductive')`** (adminui `repository.go:222`), NOT
   the legacy status-mapping table. `pregnant`, `non_pregnant`, `mother`,
   `milking`, `lactating` must be **normalized first** before adding any state.
3. **No generic `/status` endpoint.** Existing APIs are per-axis (`/exit`,
   `/stage`, `/health`). A unified route risks bypassing death/ICU/quarantine
   guardrails. Add a dedicated **`POST /admin/goats/{goat_id}/reproductive`**;
   bulk changes stay constrained by the **same per-axis validation + guardrails**.
4. **Event-contract work is in scope** (was missing): `goat.reproductive.changed`
   must be added to the domain-event envelope enum
   (`contracts/jsonschema/domain-event-envelope.schema.json`), OpenAPI, the
   generated admin-web client, `permissions/routes.go`, and UI option contracts.
5. **Import-update is a real feature.** Current import is create-focused. Updating
   matched existing animals needs: preview decisions, **match confidence**,
   evidence/source_ref, **row_version optimistic-concurrency protection**, and
   "**do not clobber newer live writes**" semantics.
6. **Herd Register placement confirmed** — per-goat reproductive edit lands in
   Counts → Herd Register (`herd-register.tsx:332`, today read-only display).

## Decisions (from review)

- **UI:** backend + per-goat Herd Register edit first; bulk backend designed now;
  **bulk UI is a fast follow (urgent), not same slice.**
- **Post-delivery state:** do **not** add a durable `delivered` value yet.
  Normalize/use `mother`/`lactating`. Exact catch-up timing requires
  `last_delivery_date` **or** a later `goat.delivery.recorded` event (Phase B).
- **Push:** wait-for-go. No unattended push.

## Design

### Event `goat.reproductive.changed`
- Mixed-species (goat + sheep); one event, one handler. Legacy `goat.*` namespace
  (documented naming debt; full rename is a separate refactor).
- Emitted transactionally with the reproductive write (outbox, same tx) — pattern
  of `goat.exited`/`goat.health.changed` in `goat_lifecycle.go`.
- Payload: tenant_id, goat_id, species, old_status, new_status, occurred_at,
  actor, trace_id, row_version.
- **Contract work:** add to envelope enum + OpenAPI + generated client +
  permissions + UI option contract (see review #4).

### Per-axis write path (identity) — no generic /status
- New dedicated `POST /admin/goats/{goat_id}/reproductive` → validates value
  against `status_definitions(axis='reproductive')`, updates
  `goats.reproductive_status`, emits `goat.reproductive.changed`, writes
  audit_log, all in one tx, idempotency-keyed.
- Health/exit/stage keep their existing dedicated endpoints + guardrails.

### Handler wiring
- Subscribe existing `GoatRecheckHandler` (`generation_handler.go`) to
  `goat.reproductive.changed` → `generateForGoat(...)` recompute. Register in
  `bootstrap/api.go`.

### Pregnancy facts — built in this slice (real timing, per go-ahead)
- Add `breeding_date` + `last_delivery_date` to the goats schema (migration),
  **populate them in the eligible-goat query** (`repository.go:1837`) so
  `EligibleGoat.BreedingDate`/`LastDeliveryDate` are non-nil, and add write-path
  support (per-goat reproductive endpoint accepts/sets them; import can carry
  them). Then `pregnancyDeferReason` (`schedule_policy.go:227`) produces exact
  month-4–5 skip and post-delivery catch-up instead of `pregnancy_month_review`.
- Post-delivery state reuses `mother`/`lactating` (no durable `delivered` value);
  `last_delivery_date` drives the catch-up window. A `goat.delivery.recorded`
  event is optional sugar, not required once the date fact exists.
- Tests must cover: pregnant + breeding_date → correct month skip; missing
  breeding_date → `pregnancy_month_review` (graceful, not wrong); delivered +
  last_delivery_date → catch-up reopens within window; both species.

### Import-update (matched existing animals)
- Extend import: matched existing + changed status → apply via the same
  event-emitting per-axis transition, with preview decision + match_confidence +
  source_ref/evidence + **row_version check** (reject/skip if a newer live write
  exists — no clobber). Per-row result surfaced in preview.

## Bulk-operation kernel (required for 1M; today's bulk is capped ~500 rows)

The per-goat path is kernel-shaped (per-goat tx, idempotency, outbox, async
recompute, no synchronous recompute-all). Bulk at 1M needs a real kernel:

1. **Durable bulk job ledger** — tables: `bulk_status_job` (id, tenant, actor,
   axis, params, total_rows, state, created/updated) + `bulk_status_job_row`
   (job_id, goat_id, axis, target, row_state
   {pending|claimed|applied|skipped|error|retry}, retry_count, failure_reason,
   expected_row_version, event_id, claimed_at, updated_at).
   **Resume truth is the row_state ledger, NOT a positional "last applied row_no"
   cursor.** Recovery = re-scan for `pending|retry` rows.
2. **Worker-claimed execution (no giant mutation tx)** — workers claim
   `pending|retry` rows via `FOR UPDATE SKIP LOCKED` and apply through **per-goat
   transition semantics or bounded micro-batches**. **No 2k/5k-row single mutation
   tx** unless the 1M gate proves that size safe; default to per-goat / small
   micro-batch. Each applied row commits its row_state + outbox event atomically.
3. **Row-level idempotency** — stable key =
   `job_id + goat_id + axis + target + expected_row_version/current_state`. A job
   replay or duplicate delivery cannot double-apply status or double-emit events;
   a row whose current state ≠ expected is skipped (no-clobber), not overwritten.
4. **Transactional outbox with real backpressure** — bounded relay drain PLUS
   **actual tenant/rate throttle controls** on bulk workers, outbox relay, and the
   recompute consumer (a 1M burst must not swamp vaccination recompute).
   `RunUntilDrained + limit` alone is explicitly NOT sufficient (req 8).
5. **Worker concurrency controls** — `FOR UPDATE SKIP LOCKED`, bounded worker
   pool, tenant-aware throttling so one job/tenant cannot starve others.
6. **Indexed recompute paths** — recompute is per-goat and index-backed; **no full
   goat scan per event** (verified against the existing indexed eligible-goat
   query).
7. **Replay/recovery** — process dies mid-run (e.g. row 437,221) → a new worker
   re-claims `pending|retry` rows from the ledger and finishes, with no
   double-apply and no stuck rows.
8. **1M synthetic verification gate** — MUST pass before any "handles 1M"
   wording. Pushes **1,000,000** synthetic status changes and asserts: **zero
   double-apply; zero missing outbox events; zero stuck pending/publishing/
   processing rows after drain; crash/retry resume mid-run; duplicate-delivery /
   replay handled; bounded DB locks; measured outbox drain time + recompute
   backlog; tenant throttle/rate control verified.**

## Scale / safety

- All writes idempotent (key + fingerprint in the write tx); tests: first /
  exact-replay / same-key-different-payload / downstream duplicate prevention.
- Bulk endpoint = preview + commit; commit enqueues the durable job (does not loop
  inline). Async workers drain it.
- India business-day time semantics for any due/window derivation.

## Slice contents — ONE push, do not split

All of the following ship together (all-in):
- event `goat.reproductive.changed` + full contract work (envelope enum, OpenAPI,
  generated admin-web client, permissions routes, UI option contracts)
- per-goat `POST /admin/goats/{goat_id}/reproductive` (dedicated, per-axis)
- `GoatRecheckHandler` wired to the new event; goat + sheep recompute
- Herd Register edit action (backend-owned option contract, not hardcoded labels)
- bulk-operation kernel: `bulk_status_job` + `bulk_status_job_row`, preview +
  commit-enqueues-job, SKIP-LOCKED worker claim, ledger-state resume, row-level
  idempotency (+ row_version no-clobber), micro-batch apply, tenant/rate throttle
- import matched-existing updates (preview + match confidence + source_ref +
  row_version no-clobber; emits the same event)
- pregnancy facts: `breeding_date` + `last_delivery_date` migration + query
  population + write path + tests (real month-4–5 skip + catch-up)
- the **1M synthetic verification gate** (all 8 assertions) — must pass before the
  "handles 1M" claim and before requesting go.
Only the **bulk status-update UI** is an explicit fast-follow (backend + Herd
Register per-goat edit are in this slice); everything else is in.

## Tests / gates (before push)

- Unit: reproductive transition emits event; value validated against
  `status_definitions`; handler recompute; `pregnancy_month_review` when
  BreedingDate nil.
- Integration (pg): per-goat + bulk-job + import-update emit events + recompute;
  idempotency matrix; **sheep** covered; row_version no-clobber; resume-from-cursor.
- **1M synthetic gate** (above).
- Contract: envelope enum + OpenAPI + generated client + permissions + UI options
  regenerate and check clean.
- `go build ./...`, `go test`, sqlc plan validation for new hot-table queries,
  typecheck/lint/`check:mock-fidelity` if FE touched, adversarial verify.

## Open confirmations for build

- Chunk size + worker/throttle defaults (propose 2,000 rows/chunk, tenant-bounded).
- Whether Phase B ships in the same push or as a follow-on (recommend follow-on;
  Phase A ships with honest review-flag pregnancy behavior).
- Exact reproductive vocabulary normalization set from `status_definitions`.

## Naming-debt note

`goat.reproductive.changed` keeps the `goat.*` namespace for consistency; covers
sheep. Namespace rename = separate refactor.
