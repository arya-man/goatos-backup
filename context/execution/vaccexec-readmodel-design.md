# vaccinationexecution shed read model (C35-002, vaccinationexecution half)

## Defect

`backend/internal/vaccinationexecution/adapters/postgres/repository.go` computes
`ShedSummary` (serving `GET /vaccination/sheds`), `ListVaccinationExecutionPage`, and
`VaccinationOperations` with live multi-CTE compute-on-read queries over
`obligation_instances` / `goats` / `vaccination_completions` / `obligation_status_events`. This
is baselined debt in `tools/scale-guard/baseline.txt`:

```
god-cte backend/internal/vaccinationexecution/adapters/postgres/repository.go 1 # owner=vaccination-platform issue=C35-002 expires=2026-09-30 reason=live execution reads need the pending versioned projection
```

Fast at the current seed size, this shape is fatal at 1-5M animals: every shed-summary page
request re-scans obligations/goats/completions and re-derives as-of status instead of doing an
indexed lookup against a materialized row.

`processintegrity` already solved the equivalent problem for Action Center / Protocol Adherence
(`process_integrity_projection_rows` / `process_integrity_projection_state`, migrations 000159 +
000160, `RecomputeProjection` in `backend/internal/processintegrity/adapters/postgres/repository.go`).
This change ports the same shape to the `ShedSummary` half of vaccinationexecution.

## What landed in this change (scoped increment, option b)

1. **Zero request-path behavior change.** `ShedSummary` / `GET /vaccination/sheds` is untouched:
   it still reads the live `shedSummarySQL` CTE in `repository.go`, still honors `as_of`, and its
   god-cte baseline entry is unchanged. This document does not close C35-002; it lands the
   infrastructure the follow-up flip needs.
2. **Read-model tables** (`backend/migrations/postgres/000167_vaccination_shed_projection.sql`):
   - `vaccination_shed_projection_rows` — one row per (tenant, projection_version, shed), holding
     exactly the fields `domain.ShedSummaryProjection` needs (park/shed identity, `Animals`,
     `DueAnimals`, `OpenCells`, `Sessions`, `Capacity`, `Status`, `LastDone`, `NextDue`), indexed
     for every filter/sort `ShedSummary` supports today (default status-priority order, park/shed
     alpha, due-desc, animals-desc, next-due, status filter, capacity filter, park/shed scope).
   - `vaccination_shed_projection_state` — one row per tenant tracking `projection_version`,
     `serving_projection_version`, `row_count`, `freshness_status`, `serving_state`, `last_error`.
   Both tables are created fresh in one migration (no `CONCURRENTLY` / `NO TRANSACTION` needed —
   there is no existing data or query traffic on them to lock against), already shaped like the
   *end state* of processintegrity's two-migration version-swap rollout (000159 + 000160) rather
   than repeating that two-step history.
3. **Projector builder**
   (`backend/internal/vaccinationexecution/adapters/postgres/shed_projection.go`,
   `Repository.RecomputeShedProjection`): replays the same
   alive → completions → asof_terminal → raw → effective → due_agg → shed_rows → scored →
   classified chain as `shedSummarySQL`, with the park/shed/search/status/capacity filters and
   `LIMIT/OFFSET` removed (a full, unfiltered per-tenant materialization). It:
   - stamps a new `projection_version` (epoch millis) and marks the tenant `rebuilding`;
   - `INSERT`s the full row set for that version in one transaction;
   - upserts `vaccination_shed_projection_state` with `serving_projection_version` set to the new
     version **inside the same transaction** as the row insert — a **version-swap**, never a live
     whole-tenant `DELETE` + reinsert;
   - on commit, prunes rows from now-superseded `projection_version`s in bounded batches
     (`pruneOldShedProjectionRowsWithBatchSize`, same chunked-loop shape as the obligation/
     idempotency sweepers — never one unbounded `DELETE`).
   The big-CTE `INSERT` is annotated `// scale-guard:ignore: off-request projector recompute
   only`, matching how `processIntegrityBaseSQL` is annotated — it is intentionally allowed to
   replay canonical state because it runs off the request path.
4. **Parity/shadow test**
   (`backend/internal/vaccinationexecution/adapters/postgres/shed_projection_test.go`): seeds a
   fixture with multiple sheds across every `ShedStatus`/`CapacityStatus` combination the existing
   `TestShedSummaryReadsSeededShed` / `TestShedSummaryOverdueOutranksCapacityHeadlines` tests use,
   calls the live `ShedSummary` and `RecomputeShedProjection` against the **same** `as_of`, and
   asserts the projection rows equal the live rows field-by-field (Animals, DueAnimals, OpenCells,
   Sessions, Capacity, Status, LastDone, NextDue) for every seeded shed. This is the proof that the
   projector reproduces exactly what the request path serves today.
5. **Refresh wiring** so the projection stays fresh in every environment, mirroring
   `process_integrity_projector` exactly:
   - `backend/cmd/vaccination-shed-projection-recompute/main.go` — a CLI, same shape as
     `backend/cmd/process-integrity-projection-recompute`.
   - `make vaccination-shed-projection-recompute` (Makefile) runs it locally.
   - `infra/envs/stg/cloud_run_jobs.tf` adds a `vaccination_shed_projector` entry to
     `local.kernel_jobs` (Cloud Run Job + Cloud Scheduler `*/5 * * * *`, reusing the
     `vaccination_generator` service account exactly like `process_integrity_projector` does).
     `process_integrity_projector` itself is stg-only today (not in `infra/envs/dev`), so this
     mirrors that same scope rather than inventing a dev job with no precedent.
   - `backend/Dockerfile` builds `vaccination-shed-projection-recompute` into `/app/bin/` alongside
     `process-integrity-projection-recompute`.
   - `backend/cmd/seed-vaccination-real/main.go` calls `RecomputeShedProjection` right after the
     existing `RecomputeProjection` (process-integrity) call — the deploy-seed population step, so
     a freshly seeded environment does not start with a cold shed projection.

## Explicitly NOT done here (follow-up)

`ShedSummary` does **not** read `vaccination_shed_projection_rows` yet. Flipping the request path
is a separate, later change, gated on:

- the scheduled `vaccination-shed-projection-recompute` Cloud Run Job actually running for a
  sustained period in an environment (so the projection is never cold on first flip);
- a decision on the read-time fallback behavior when a tenant has no serving projection version
  yet (processintegrity's answer was `domain.ErrProjectionUnavailable` — refuse the live CTE
  fallback rather than silently reintroducing the god-CTE on the request path; the same call needs
  to be made explicitly for `ShedSummary` before the flip, not inherited by default); and
- re-running the parity test against real seeded/staging data volumes, not only the unit fixture,
  immediately before the cutover.

Until that follow-up lands, `ShedSummary` remains the one and only serving path for
`GET /vaccination/sheds`, and the god-cte baseline entry for this file stays as-is.
