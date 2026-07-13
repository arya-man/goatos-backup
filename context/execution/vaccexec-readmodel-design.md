# vaccinationexecution CQRS read models (C35-002)

**Status: request-path flip LANDED.** `ShedSummary` / `GET /vaccination/sheds` now reads
`vaccination_shed_projection_rows` exclusively via an indexed `(tenant_id, projection_version, ...)`
lookup (`shedSummaryProjectedSQL`, `backend/internal/vaccinationexecution/adapters/postgres/
repository.go`). The god-cte baseline entry for this file has been removed from
`tools/scale-guard/baseline.txt`. Sections 1 and "Explicitly NOT done here" below describe the
PRIOR increment (infra-only, read unchanged) for history; see "Flip landed" at the bottom for what
changed.

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
This series ports the same shape to `ShedSummary`, `ListVaccinationExecutionPage`, and `VaccinationOperations`.

## What landed in this change (scoped increment, option b)

1. **Projection-only request paths.** `GET /vaccination/sheds`, `GET /vaccination/execution`, and `GET /vaccination/operations`
   read versioned projection tables through indexed filters/keysets. Missing, stale, or incompatible
   snapshots return retryable `projection_unavailable`; neither endpoint falls back to a live CTE.
   Responses carry projection version, projected/as-of instants, status, and lag seconds. Explicit
   historical `as_of` reads require an exact matching snapshot; live reads accept at most five minutes
   of lag within matching `Asia/Kolkata` business-date horizon buckets.
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
5. **Refresh wiring** so all vaccinationexecution projections stay fresh in every
   environment:
   - `backend/cmd/vaccination-shed-projection-recompute/main.go`,
     `backend/cmd/vaccination-execution-projection-recompute/main.go`, and
     `backend/cmd/vaccination-operations-projection-recompute/main.go` are CLI
     entry points with the same target guards as
     `backend/cmd/process-integrity-projection-recompute`.
   - Make targets run all three locally.
   - `tools/dev/run-local-stack-supervised.sh` starts a local projection refresher
     with the API/admin-web so local pages do not go stale five minutes after a
     seed.
   - `infra/envs/dev/cloud_run_jobs.tf` and `infra/envs/stg/cloud_run_jobs.tf`
     schedule process-integrity plus vaccination `execution` and `operations`
     projector jobs. The shed read model stays fresh through the bounded
     `vaccination-projection-worker`; full shed recompute remains the
     seed/backfill repair command.
   - `backend/Dockerfile` builds all three vaccination projector commands into
     `/app/bin/` alongside `process-integrity-projection-recompute`.
   - `backend/cmd/seed-vaccination-real/main.go` recomputes process-integrity,
     shed, execution, and operations projections after the canonical seed writes
     complete. A freshly seeded environment must not start with cold
     `projection_unavailable` vaccination surfaces.

## Explicitly not complete

## Flip landed (this change)

The three gating items above are resolved as follows:

- **Cold-projection behavior**: `ShedSummary` calls `shedProjectionServingVersion` on every request;
  when the tenant has no serving version yet it returns `domain.ErrProjectionUnavailable` (never a
  live CTE fallback), and the HTTP handler maps that to `503 projection_unavailable` — the exact
  answer flagged as an open decision above, now made the same way processintegrity made it.
- **Sustained-refresh / cold-start risk**: accepted as a deploy-sequencing concern (the recompute
  CLI/Cloud Run Job must run at least once per tenant before traffic depends on it — the same
  requirement processintegrity already carries), not a code gate. `backend/cmd/seed-vaccination-real/
  main.go` already primes it at deploy-seed time (item 5 above).
- **Parity proof**: `TestRecomputeShedProjectionMatchesLiveShedSummary`
  (`shed_projection_test.go`) now compares `RecomputeShedProjection`'s output against a test-only
  copy of the god-CTE (`canonicalShedSummarySQLForParity`, since production no longer contains that
  query) AND against the flipped `ShedSummary` request path itself, proving the live read matches the
  canonical answer field-by-field. Real-Postgres, not a mocked repository.

What changed in `repository.go`:

- `shedSummarySQL` (the god-CTE) is deleted from production code. `shed_projection.go`'s
  `vaccinationShedProjectionInsertSQL` remains the only place that shape exists, and it is
  off-request (the projector), annotated `// scale-guard:ignore`.
- `ShedSummary` is now a thin dispatcher: look up the serving projection version, then read
  `shedSummaryProjectedSQL` (an indexed lookup, no joins/aggregation — every column is precomputed).
  It no longer calls `CapacityConfig` (capacity/session/status are already baked into the projection
  row by `RecomputeShedProjection`).
- `q.AsOf`/`q.DueBefore` are accepted for API compatibility (and still clamp future values at the
  HTTP layer) but no longer drive historical reconstruction in the projected read — the projection
  always serves its latest successful recompute. True point-in-time historical reconstruction of shed
  status is out of scope for this rollup; `obligation_status_events` remains the audit trail for that.
- `tools/scale-guard/baseline.txt`'s `god-cte ... repository.go` line is removed (not just reduced);
  `make scale-guard` now proves the request path via the absence of a baseline entry, not a
  grandfathered count.
- `backend/tests/integration/validate-sqlc-query-plans.sh`'s `validate_vaccination_shed_projection_plan`
  gained a capacity-filter check and a check matching the exact production request shape (default
  CASE-based status sort + all five filters + `LIMIT/OFFSET`), proving the flipped read is an indexed
  lookup, not a sequential scan.
- Three E2E kernel stories (`story_ac_capacity_eligibility_exclusions_test.go`,
  `story_ad_live_asof_guard_test.go`, `story_af_capacity_breach_overdue_headline_test.go`) now call
  `RecomputeShedProjection` (the real production projector, same pattern as `fx.PI.RecomputeProjection`
  elsewhere in the suite) before reading `ShedSummary`, since the request path no longer computes an
  answer on demand.
## Remaining scale work

- Migration 000169 stores the cohort x protocol operations matrix with human-name keyset ordering and
  a version/freshness state row; its request path has no canonical fallback.
- The current projector is a full-tenant off-request repair/rebuild, maintained by the scheduled
  vaccination projector/sweeper. This removes latency from requests but is **not** the final 1M steady
  state. A durable dirty-scope queue and bounded tenant/park/shed/date shard worker with checkpoint,
  retry, and DLQ state must make scoped incremental refresh the normal path; full rebuild then becomes
  repair only. Do not certify C35-002 or 1M readiness until that worker and staging cardinality proof land.
