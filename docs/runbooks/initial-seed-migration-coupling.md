# Initial Seed Migration Coupling

This rule prevents a repeat of the "seed succeeded, app is unavailable" failure.
When schema changes affect tables owned by initial setup or read-path closeout,
the same patch must update the seed path.

## Scale envelope (read this first)

The accepted ADR
[`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`](../decisions/operational-kernel-5k-50k-scale-envelope.md)
is the authority for the current deployment scale target (approximately 5,000
animals today, up to 50,000 within the year). Under that envelope the six named
screen projections — `calendar_event_projections`,
`process_integrity_projection_rows`, `vaccination_shed_projection_rows`,
`vaccination_execution_projection_rows`, and
`vaccination_operations_projection_rows`, plus Full Schedule's
`vaccination_schedule_projection_*` family — are dropped, and those screens are
served directly from canonical indexed SQL. There is no separate projector
schedule or partition-maintenance job for them at this envelope; one kernel
worker owns the operational cadences, and the split-worker/projection topology
stays recoverable from the `kernel-split-workers-v1` tag.

The coupling rule below is unchanged in spirit: a schema change that makes a
fresh environment unusable must be paired with the seed/closeout path. What
changes is the DEFAULT for a new read-model migration. Adding a derived table no
longer automatically requires a projector; under the ADR a NEW projection is
introduced only per measured hot read path via the scale-out ladder, and until
then the screen reads canonical SQL. Small indexed summaries that survive the
envelope (for example `vaccination_eligibility_rollups` and Counts summaries,
which are NOT in the ADR's projection-removal list) still follow the
seed/closeout recompute discipline described here.

None of this changes vaccination DATE/ANCHOR business semantics — trusted
completed history as base anchor, future recurrence calculated strictly after
the backend business date, blank/NA/Pending never backfilling synthetic-late
work, the V2 ET+TT course anchor plus Vaccination Plan offsets, HRMS roster/
manager/backup ownership, the constraint model, idempotency, and all test gates.
Those are orthogonal to how a screen is served and remain in force. Only the
projection-backed verification/enable steps are reframed.

## Rule

A migration that changes an initial-seed-sensitive table must also update at
least one of:

- the relevant `backend/cmd/seed-*` command;
- a seed/projection recompute command;
- a seed or projection regression test/E2E;
- a seed or clean-slate runbook that explains the new closeout step.

This is enforced by:

```bash
make seed-migration-guard
```

`make guardrails` and `make ci-local JOB=guardrails` run the same guard.

## What Counts As Seed-Sensitive

The guard watches DDL/DML against setup tables that make a fresh environment
usable:

- tenant, park, shed, goat identity, RFID, and source animal facts;
- HRMS roster, attendance/leave, positions, shed ownership, and position duties;
- founder/admin grants and org role catalog tables;
- protocol, SOP, capacity, obligation, completion, and proof/verification
  setup tables;
- any surviving app-visible summary/read-model table (for example
  `vaccination_eligibility_rollups`, Counts summaries, and any incremental
  dirty-scope/shard state a surviving summary depends on);
- notification/reminder/verification tables that make seeded work executable.

Under the 5k-to-50k envelope, the calendar/process-integrity/vaccination-screen
reads, including Full Schedule, are served from canonical indexed SQL rather than from the removed
projection tables, so a fresh environment becomes usable once the canonical rows
are seeded and those APIs return 200. That is why "seed green" for those screens
means the canonical-read APIs answer (for example `/vaccination/sheds`,
`/vaccination/execution`, `/vaccination/operations`, and `/vaccination/schedule`
serving from canonical indexed SQL), not "the projectors completed / no
`projection_unavailable`".

Pure index-only migrations are allowed without a seed change. Creating or
altering a read-model table is not index-only. But adding one no longer
automatically requires a projector: under the ADR the default is that a new
screen read is served from canonical SQL, and a projection (with its closeout
step) is added only when a measured hot read path earns it on the scale-out
ladder. If a migration DOES add or grow a surviving summary that the app reads
after seed (for example an eligibility-rollup or Counts summary), the seed
closeout must still explain or run the recompute that fills it.

## Table Ownership Classes

Every new table touched by setup must be classified in the same PR. Do not make
the seed command blindly fill every table.

| Class | Owner | How it is filled |
| --- | --- | --- |
| Source/canonical | Reviewed source files/importers | Seed/importer writes only real source truth: goats, RFID, HRMS roster, shed ownership, protocol/SOP config, trusted vaccination history. |
| Derived/read model | Canonical SQL by default; recompute only for a surviving summary | Seed never hand-writes these rows. Under the 5k-to-50k envelope the default is to serve the screen from canonical indexed SQL and add NO projector. Only a summary that survives the envelope (for example `vaccination_eligibility_rollups`, Counts) is rebuilt by a deterministic recompute from canonical tables; register that recompute in `tools/dev/seed-closeout.sh`. A new hot-path projection added later via the scale-out ladder registers its closeout step the same way. |
| Static catalog/config | Migration or reviewed config seed | Migration may insert safe global catalog rows; environment-specific config must come from source-backed seed/config, never guessed defaults. |
| Operational/audit/event | Runtime workers/events | Starts empty unless replaying real events. Notification attempts, dirty scopes, outbox-derived rows, reminder fires, and audit ledgers must not be faked during seed. |

`sop_task_scan_captures` and `sop_task_scan_attempts` are operational runtime
tables. A clean-slate seed must create both empty; rows are written only by
authenticated operator RFID reader events through the mobile outbox and app API.
Do not seed synthetic scan captures or attempts to make a vaccination drive look
complete. `sop_task_scan_captures` is the de-duplicated Submit draft source;
`sop_task_scan_attempts` is append-only reader audit for accepted, duplicate,
not-due, and unknown physical reads. Seed verification for these tables is only
that the migration applied and Submit can validate/finalize runtime captures
when they exist.

`sop_submissions.partition_label` is runtime submit identity, not source seed
truth. Clean-slate seed starts with no SOP submissions, so migration
`000147_sop_submissions_partition_label.sql` has no seed row to backfill. For an
already-seeded database, apply the migration before API/app startup; new
partition-scoped submits must write the physical partition label, while older
whole-shed submissions remain `NULL` and continue to read as whole-shed
history.

The standard setup shape is:

```text
migrate schema
-> seed canonical source truth
-> refresh Postgres planner statistics for bulk-loaded source tables
-> make seed-closeout
-> verify green gates
```

Do not start the API, admin-web, workers, or seed closeout against a database
whose migration head is behind the code being run. Local launchers and staging
deployments must apply that build's migrations first, then seed only canonical
source truth, then run any deterministic closeout for surviving summaries, then
verify that the canonical-read APIs answer. A deployment that cannot prove this
order is not green.

Where a summary recompute or a later-added projection IS registered in closeout,
its registration must be output-specific. A command that can build more than one
app-visible summary/read model must be called with explicit flags for each
output. In particular, any `-project-*` flag that defaults to false must appear
in `tools/dev/seed-closeout.sh` as `-project-...=true` on the command invocation
that owns that read model. The guard must verify the executed
`tools/dev/seed-closeout.sh --dry-run` output rather than raw shell source. A
generic command, commented example, disabled branch, or uncalled helper is not
enough proof: it can leave a default-off summary empty while the seed log still
says the recompute ran. Screens served from canonical indexed SQL under the
envelope have no such flag to register; their "seed green" is the canonical-read
API returning 200.

For an already-seeded database where a later migration adds a derived table,
apply migrations, run `make seed-closeout` for any surviving summary the table
feeds, then verify the affected APIs. Do not reseed source rows just to fill a
derived table, and do not add a projector when the screen already reads canonical
SQL correctly.

Where a screen is served from a projection that survives the envelope or is added
later via the scale-out ladder, the last-known-good serving contract still
applies. A canonical-SQL read cannot be stale relative to the canonical write, so
this contract governs only projection-backed reads. After the first successful
closeout of such a projection, it going stale must not make an operator page
unavailable when a usable serving projection still covers the request:

- no first projection/no serving rows: fail closed and run closeout/projector;
- requested date/window outside projected coverage: fail closed because rows may
  be missing;
- stale, yellow, rebuilding, failed, or over-TTL state with serving rows covering
  the request: serve last-known-good rows and expose freshness metadata while
  the projector repairs freshness.

This is part of the seed/migration contract, not a UI preference. Any future
projection migration added under the scale-out ladder must include a regression
test or reviewed runtime evidence for that behavior in addition to the closeout
step.

For a destructive clean-slate seed or any bulk import/backfill, the seed/import
must run `ANALYZE` on the canonical tables it bulk-loaded before read-model
projectors or latency gates run. Freshly truncated/empty tables can leave
Postgres with stale planner statistics; then a correct projector can choose a
pathological plan and time out. The stats refresh is infrastructure hygiene, not
business mutation, and it belongs between canonical seed commit and derived
read-model closeout.

## Local Single-DB Contract

Normal local laptop runtime is one database: API, admin-web, and mobile dev
must resolve the same Goat OS `DATABASE_URL`. They may auto-detect the single
`goatos-local-current` Docker Postgres, or fall back to
`127.0.0.1:5433/goatos`; they must not silently point one surface at host
Postgres and another surface at a Docker DB.

If multiple Goat OS app Postgres containers are visible, local launchers must
fail instead of guessing. E2E, proof, and load harnesses must not silently use
the normal `5433` app DB or an old hidden port. They source
`tools/dev/e2e-db-env.sh`, which fails closed unless `GOATOS_E2E_DATABASE_URL`
or `DATABASE_URL` is explicitly passed. Read-only checks may target the normal
app DB, and the `5433` URL must still be visible in the command/evidence when
they do. Mutating proof/load scripts that create goats/proofs, replay outbox,
insert history, or run migrations always refuse `5433`; there is no override for
mutating the normal app DB from E2E. Destructive/load tests must use an isolated
DB with owned seed/cleanup, such as the explicit GCP-kernel parity stack on
`55432`. That stack is not the normal laptop runtime DB.

Examples:

```bash
# Normal local API/admin/mobile database.
DATABASE_URL='postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable'

# Read-only check against the normal local DB.
DATABASE_URL='postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable'
psql "$DATABASE_URL" -c 'select count(*) from goats'

# Explicit isolated local GCP-kernel parity database.
make dev-local-kernel-up
GOATOS_E2E_DATABASE_URL='postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable'
```

## Why This Matters

The canonical source seed writes goats, HRMS, protocol config, completions, and
obligations. Under the 5k-to-50k envelope the calendar/process-integrity/
vaccination screens, including Full Schedule, read that canonical state directly
through indexed SQL, so a correctly seeded canonical environment makes those pages
usable without any projector step. A migration can still be "safe" for source data but break a
fresh setup if it grows a surviving summary the app reads (for example an
eligibility-rollup or Counts summary) and the seed does not recompute it, or if
it changes a canonical read path without the supporting index.

Concrete examples:

- serving Calendar/execution/operations/shed/Full Schedule from canonical SQL: a migration that
  adds the supporting index or column does not corrupt goat source data, and the
  screen is green as soon as the canonical rows are seeded and the API returns
  200 — there is no projection to warm;
- adding or growing `vaccination_eligibility_rollups` or a Counts summary does not
  corrupt goat source data, but the dependent view can read empty until the
  recompute runs; therefore that migration must be paired with the seed closeout
  command/test or runbook change that proves the summary is filled;
- introducing a NEW projection later via the scale-out ladder brings back the
  full projector-plus-closeout obligation for that one screen (see below).

## Required Closeout For A Surviving Summary Or A Newly Added Projection

The 5k-to-50k default is canonical-SQL reads with NO projector, so most new
read-model migrations need only the canonical index plus the API-returns-200
gate. The steps below apply when a migration adds or grows a summary that
survives the envelope (eligibility rollups, Counts) OR introduces a new
projection table via the scale-out ladder in
[`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`](../decisions/operational-kernel-5k-50k-scale-envelope.md).
In those cases update the seed flow to include:

1. the recompute/projector command or worker that fills the table;
2. the API or SQL green gate proving non-empty/current state for the tenant;
3. the runtime serving behavior for never-synced, stale last-known-good, and
   date/window coverage gaps (projection-backed reads only; a canonical-SQL read
   cannot be stale);
4. the E2E/report category that owns the proof, when an E2E is run.

Also prove the summary/projection is scale-shaped before it lands:

1. document the canonical source tables it derives from;
2. document the exact read predicates and ordering used by the API;
3. add tenant/scope/date/state/page indexes for those predicates;
4. use versioned/serving-state freshness instead of request-time full replay;
5. make a partitioning decision. Partition append-only or time-series tables
   when retention, write volume, or range pruning require it. Do not partition
   small per-tenant/per-shed summary tables just for aesthetics; record why a
   normal indexed table is enough.

For vaccination source seeding, keep
`docs/runbooks/vaccination-seed-source-date-contract.md` as the concrete
checklist.

## Reviewed No-Impact Exception

If a migration mentions a seed-sensitive table but truly does not affect setup,
add this complete marker on or immediately above the SQL operation:

```sql
-- seed-migration-guard:ignore owner=<name> issue=<url-or-id> reason=<short-reason> expiry=<YYYY-MM-DD>
```

Do not use the exception for "we will fix the seed later." The exception is only
for reviewed no-impact cases.

## R50-015 lock-safety + dedup migrations (2026-07-20, no seed impact)

Migrations `000003_r50_forward_compatibility.sql` and
`000004_r50_forward_compat_concurrent_indexes.sql` were amended to be lock-safe
(`-- +goose NO TRANSACTION` + `lock_timeout`, `NOT VALID` + separate `VALIDATE`,
`CREATE INDEX CONCURRENTLY` create-new-then-drop-old) and to fix a dedup that
kept the wrong duplicate row (`NOT EXISTS` -> `EXISTS`, so the earliest row
survives and the widened `CREATE UNIQUE INDEX CONCURRENTLY` no longer fails
42P10). These are refactors of EXISTING forward-compatibility catch-up
statements on `notification_requests`, `obligation_status_events`, and
`verification_items` — they add no new seed-owned rows, no new read-model, and
no new app-visible surface, so the seed/import path is unchanged. No seed
command or projection recompute needs updating; the coupling companion is this
runbook note plus the `seed-migration-guard:ignore` markers in the migrations.

## 000034 birth/death workflows (2026-07-27, no seed impact)

Migration `000034_birth_death_workflows.sql` creates `workflow_instances` and
`workflow_actions` and adds `goats.time_of_birth`.

- `workflow_instances` / `workflow_actions` are OPERATIONAL/EVENT tables per the
  classification above: rows are produced only by the tasks module's
  `goat.created` / `goat.exited` consumers and the operator answer/complete APIs
  on the production path. The seed never hand-writes a workflow row, so no seed
  command, projector, or closeout flag changes.
- `goats.time_of_birth` is an ADDITIVE, NULLABLE column that is backfilled
  NEVER: `NULL` means unknown, and every reader (the birth workflow opener)
  falls back to 07:00 IST on the DOB. Existing seed/import rows stay valid
  without touching any seed path; new source data may supply it through
  `CreateAdminGoatRequest.time_of_birth` when available.

No seed change is required; this runbook note is the coupling companion for the
`goats` table touch.
