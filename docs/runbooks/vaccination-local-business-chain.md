# Vaccination Local Business-Chain Proof (pre-E2E, not E2E)

Date: 2026-06-26

Purpose: one repeatable local command/runbook sequence for the vaccination
business chain, plus an honest map of what is test-proven per-segment and the
captured single end-to-end run (CLOSED for the data plane, 2026-06-26).

This is **backend/integration proof, not Playwright E2E**. Logging-only outbox
publish does NOT count as delivery. `procurement_phc_handoffs.event_status =
emitted` means "outbox enqueued", not "downstream generation/read-models
succeeded", unless a real downstream reconciliation signal is added.

## Chain Under Proof

```text
source-backed published vaccination protocol  (Config: GET /protocols + publish gate)
  -> published vaccination SOP                 (/sops, /admin/sops)
  -> clean goat enters scope                   (Herd Register create OR accepted intake)
  -> goat.created delivered to generation handler   (outbox relay, eventbus mode)
  -> vaccination obligations generated         (SM-1)
  -> sweeper creates shed drive / batch / SOP task  (SM-4 / SM-4b / SM-4c)
  -> proof recorded via backend proof API      (generated app API)
  -> SOP answers submitted with proof refs     (generated app API)
  -> verifier accepts / rejects / reworks      (verification-queue API)
  -> accepted verification writes vaccination_completions + obligation status (SM-5a)
  -> CT / AC / PA / WF / Vaccination / shed / Passport read the same Postgres truth
```

Execution-path decision (pre-E2E): **direct generated API seed** (not an
admin-web operator console, not operator-mobile). Operator execution belongs to
the field app, which is out of the admin-web slice; admin-web must not show fake
"start SOP / upload proof / submit answers" buttons. See pre-E2E audit B4.

## Repeatable Command (one script)

The whole chain is captured by one repeatable, no-browser script that drives the
real local stack (admin-web `:3300`, api `:8080`, docker PG `127.0.0.1:55432`) and
prints concrete IDs plus a per-surface read-model check:

```bash
cd /Users/ravi/mesha/goatos
bash tools/dev/vaccination-chain-proof.sh
```

It uses the default local user (`ceo_internal`, which holds every permission the
chain needs), the seeded source-derived ET dev baseline (published version
`b011`, rule `b012` `ET-PRIMARY-1`, linked published SOP `b0..0002`, FEFO vaccine
lot `b002`), Herd Register `POST /admin/goats` as the entry path, `cmd/outbox-relay`
(`GOATOS_OUTBOX_PUBLISHER=eventbus`) as the delivery path, `cmd/obligation-sweeper`,
generated app proof/SOP/verification APIs, then the CT/AC/PA/WF/Vaccination/shed/
Passport read models. It ends in a `## CLOSED …` line with all IDs.

### Prerequisites (and the migration gap this run hit)

```bash
make dev-local        # local PG (docker :55432), api (:8080), admin-web (:3300)
# make dev-local reuses an already-healthy API and does NOT migrate by itself.
# The DB must be at migration head. This run found 000082
# (sop_task_review_fanouts / sop_task_submission_fanouts) UNAPPLIED on the local
# docker DB; SOP submission then 500s with:
#   relation "sop_task_submission_fanouts" does not exist (SQLSTATE 42P01)
# Fix is the approved goose/psql path (the migration file already exists, it was
# just not applied locally):
awk '/-- \+goose Up/{u=1} /-- \+goose Down/{u=0} u' \
  backend/migrations/postgres/000082_vaccination_rework_and_sop_review_fanout.sql \
  | psql "postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable" -v ON_ERROR_STOP=1
# Seed (idempotent): actor grant + source-derived ET dev baseline.
cd backend
export GOATOS_ENV=local
export DATABASE_URL="postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable"
go run ./cmd/seed-dev-grant -tenant-id <tenant> -user-id <user> -role ceo_internal
go run ./cmd/seed-vaccination-trigger              # protocol/inventory/SOP/lot fixtures
```

Manual equivalent of the script's steps (when running by hand):

```text
1. POST /admin/goats (park CBE + shed, K1 day-21 ET goat)  -> goat + goat.created outbox row
2. GOATOS_OUTBOX_PUBLISHER=eventbus go run ./cmd/outbox-relay   -> generation -> obligation_instances
3. go run ./cmd/obligation-sweeper -version-id <b011> -sop-version-id <b0..0002> \
     -vaccine-item-id <b001> -actor-id <user>              -> obligation_batch + SOP task
4. POST /app/proofs/uploads (x3: shed/vial_lot/administration, scope_type=task) + PUT bytes
5. POST /app/tasks/{task}/submissions (answers + 3 proof_refs; vaccine_lot_id = stock_id b002)
     -> vaccination_completion (recorded) via the in-process submission fanout
6. GET /vaccination/verification-queue ; POST /vaccination/completions/{id}/accept
     -> completion accepted + obligation completed
7. Config list (B3): GET /protocols?category=vaccination   (admin-web /config, generated client)
```

## What Is Test-Proven (passed in `go test ./...`, 2026-06-26)

Per-segment behavior is covered by passing integration tests against local PG:

- goat.created delivered to generation handler: `TestGoatCreatedHandlerGeneratesViaBus`.
- generation idempotent + defer visible: `TestSM1GenerationIdempotentAndDeferVisible`.
- trusted HF evidence suppresses matching dose (no double-dose):
  `TestGoatCreatedTrustedHFEvidenceSuppressesMatchingObligation`.
- shift/cancel re-scope and cancel open obligations: `TestSM2ShiftReScopesOpenObligations`,
  `TestSM3CancelOpenForGoat`.
- sweeper batch / SOP task / stock reserve: `TestSM4SweeperBatchesByScope`,
  `TestSM4bSpawnsSopTaskPerBatch`, `TestSM4cReservesStockPerBatch`.
- completion write: `TestSM5aMarkCompleted`.
- relay replay / dedup, no duplicate obligations: `TestObligationInsertIsIdempotent`,
  `TestStatusEventConcurrentDedup`, `TestStatusEventReserveBeforeInsertDedup`.
- procurement accepted-intake idempotency / replay (rejected/source-only excluded):
  `TestProcurementIdempotencyReserveSerializesConcurrentSameKey`,
  `TestProcurementIdempotentReplay`, `TestProcurementSourceEntryPostgresPaths`.
- Config list endpoint shape (B3): `TestListConfigsReturnsItemsAndDefaultsCategory`.

## Captured End-to-End Run (CLOSED — 2026-06-26, data plane)

The assembled single run (B1/B2/B5) is now CAPTURED via
`tools/dev/vaccination-chain-proof.sh` against the live local stack — no browser,
no Playwright. One representative run produced these concrete IDs:

```text
goat            d9dcfd30-37c4-4c0d-9d97-27333105642d  (park CBE / shed Mandela 1 - Part 1)
goat.created    event b942099b-6858-48d9-817c-1b84802c44e8  (outbox topic identity.events)
obligation      b005bb54-c94c-49ea-82e8-4e56a02a29cc  rule b012 (ET-PRIMARY-1), version b011  -> completed
batch           0cf0538e-99ab-40a3-b45c-50ea673d4789
SOP task        5ce5a723-8e5a-40ef-a100-c617f5f92731  (vaccination, scope=park)
proofs          shed 96c50c32 / vial_lot 7f9d3ada / administration e8d9a5b7  (local storage, completed)
submission      6deb70fb-1ec6-4f51-84a5-3e5553020fba
completion      2625595c-3111-4c20-a4e0-feab8cfd41d9  -> recorded -> accepted
```

`accept` returned `{"applied":true,"completed":true}`; the obligation moved to
`completed` with `completed_at` set, and `vaccination_completions` row to
`accepted`.

Read-model proof (same Postgres truth), recorded medium per surface:

| Surface | Endpoint | Result | Medium |
|---|---|---|---|
| Action Center | `GET /vaccination/action-center` | obligation row present, `work_state=completed`, `completed_count=1` | API |
| Workflows | `GET /vaccination/workflows/{row_id}` (`row_id=batch:<batch>:rule:b012:shed:<shed>`) | obligation + batch + completion present; chain nodes `config_published→obligation_generated→batch_opened→sop_task→proof_uploaded→verification→completion→next_due` | API |
| Goat Passport | `GET /goats/{goat_id}/passport` | goat + obligation + completion present | API |
| Shed drilldown | `GET /vaccination/execution/sheds/{shed_id}` | batch drive `workState=completed`, `severity=ok`; summary `completed=1` | API |
| Protocol Adherence | `GET /vaccination/adherence` | `summary.completed_count` counts the completion; row aggregates per batch/shed (no per-goat UUID) | API + SQL |
| /vaccination operations | `GET /vaccination/operations` | shed cohort `counts.accepted` counts the completion (no per-goat UUID) | API + SQL |
| Control Tower | `GET /control-tower/vaccination` | completed obligation correctly absent from gap alerts; `verification_backlog=0` after accept | API + SQL |

Aggregate surfaces (Adherence / Operations / Control Tower) reflect the chain by
scoped count rather than echoing the goat UUID; on a clean DB the single
`accepted` completion is provably this run's (SQL: one `accepted`
`vaccination_completions` row, one `completed` `obligation_instances` row).

Replay / idempotency (live, in the same script): the `goat.created` outbox row was
reset to `pending` and re-delivered through the eventbus relay, and the sweeper was
re-run — obligation count stayed `1`, completion count stayed `1`, sweeper reported
`batches=0`. This matches the committed dedup tests `TestObligationInsertIsIdempotent`,
`TestStatusEventConcurrentDedup`, `TestStatusEventReserveBeforeInsertDedup`,
`TestSM1GenerationIdempotentAndDeferVisible`.

## Remaining (external, NOT a local-code or data-plane blocker)

- **Production roster expansion.** Local/dev no longer waits on a vague PHC
  roster approval: the source-derived baseline is ET/K1/day-21 with K2=42, backed
  by `context/source-findings/phc-vaccination-roster-stage-proposal.md`. PPR/FMD/
  HS/BQ are `label-only closed` until a roster-expansion pass promotes any of
  them to `schedule-backed`; either state is closed for this local chain.
- **Google/prod provisioning** — see Infra / Prod Readiness below; external.

## Infra / Prod Readiness (cloud counterpart, 2026-06-26)

The B3 Config list/read change is **local-code complete and additive** (new
read-only `GET /protocols`, generated client, wired UI); it introduces no new
cloud resource requirement. The full cloud-readiness checklist for this chain —
IdP/JWKS auth, Pub/Sub topic/subscription/DLQ + IAM publisher/subscriber, relay
worker with `GOATOS_OUTBOX_PUBLISHER=pubsub`, sweeper job, Cloud Run API/admin-web,
Secret Manager/service accounts/least-privilege IAM, observability (API/relay/
sweeper/Pub/Sub/DLQ/DB pressure), migrations via the approved job, and query/load
guardrails — is already specified in:

- `context/execution/vaccination-trigger-closure-parallel-handoff.md` →
  "System Design Readiness Gate" / "Google dev/stage/prod components" /
  "Local-to-Google component equivalence".
- `docs/runbooks/google-cloud-environments.md`, `docs/runbooks/auth.md`,
  `docs/runbooks/observability.md`.

Uncreated Google resources (Cloud SQL for the target env, Artifact Registry
images, Cloud Run services, Pub/Sub topic/subscription/DLQ, Scheduler/sweeper
job, Secret Manager versions, IAM bindings) remain **external/provisioning
blockers under the Mesha/VGoats `vgoats.com` org** — they are NOT local-code
blockers, and none may be created/mutated from an unverified account/org/project.

E2E not run. E2E starts only when the pre-E2E audit E2E Start Gate is green.
