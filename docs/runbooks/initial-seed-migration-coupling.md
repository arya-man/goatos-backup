# Initial Seed Migration Coupling

This rule prevents a repeat of the "seed succeeded, app is unavailable" failure.
When schema changes affect tables owned by initial setup or projection closeout,
the same patch must update the seed path.

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
- app-visible projection tables such as vaccination shed/execution/operations,
  process-integrity, calendar history/date markers, counts, and incremental
  dirty-scope/shard state tables;
- notification/reminder/verification tables that make seeded work executable.

Pure index-only migrations are allowed without a seed change. Creating or
altering a read-model table is not index-only: if the app reads it after seed,
the seed closeout must explain or run the projector that fills it.

## Table Ownership Classes

Every new table touched by setup must be classified in the same PR. Do not make
the seed command blindly fill every table.

| Class | Owner | How it is filled |
| --- | --- | --- |
| Source/canonical | Reviewed source files/importers | Seed/importer writes only real source truth: goats, RFID, HRMS roster, shed ownership, protocol/SOP config, trusted vaccination history. |
| Derived/read model | Projector/backfill | Seed never hand-writes these rows. A deterministic recompute/projector rebuilds them from canonical tables. Register the step in `tools/dev/seed-closeout.sh`. |
| Static catalog/config | Migration or reviewed config seed | Migration may insert safe global catalog rows; environment-specific config must come from source-backed seed/config, never guessed defaults. |
| Operational/audit/event | Runtime workers/events | Starts empty unless replaying real events. Notification attempts, dirty scopes, outbox-derived rows, reminder fires, and audit ledgers must not be faked during seed. |

The standard setup shape is:

```text
migrate schema
-> seed canonical source truth
-> make seed-closeout
-> verify green gates
```

For an already-seeded database where a later migration adds a derived table,
apply migrations, run `make seed-closeout`, then verify the affected APIs. Do
not reseed source rows just to fill a derived table.

## Why This Matters

The canonical source seed writes goats, HRMS, protocol config, completions, and
obligations. The live app often reads derived read models instead of replaying
canonical state. A migration can therefore be "safe" for source data but still
break a fresh setup if the seed does not warm the new projection.

Concrete example:

- adding `calendar_history_projection_rows` does not corrupt goat source data;
- but Calendar will be unavailable after seed until the calendar history
  projector creates rows and freshness state;
- therefore the migration must be paired with the seed closeout command/test or
  runbook change that proves the projection is filled.

## Required Closeout For New Projection Tables

When a migration adds a projection table that powers a visible page, update the
seed flow to include:

1. the projector/recompute command or worker that fills the table;
2. the API or SQL green gate proving non-empty/current state for the tenant;
3. the failure mode if the projection is stale or empty;
4. the E2E/report category that owns the proof, when an E2E is run.

Also prove the projection is scale-shaped before it lands:

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
