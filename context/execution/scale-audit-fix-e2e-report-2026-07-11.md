# Scale Audit Fix E2E Report - 2026-07-11

Status: local/backend E2E proof complete; staging 1M certification not run.

## Scope

This report explains the scale-audit fixes that were verified after the staging
Control Tower slowdown review. It is not a generic changelog. Each item below
states what behavior was under test, why it matters at 1M-goat scale, how the
test sets up the failure mode, what code path it executes, and what assertions
prove the fix.

Covered in this report:

1. Park-consolidation pagination in the obligation sweeper.
2. Process-integrity projection recompute and stale/rebuilding read behavior.
3. Vaccinationexecution hot-read latency gates.

Out of scope for this report: the final outbox-fed incremental
`obligation_process_state` projector and the `goatos-stg-1m-benchmark-v1`
certification run.

## 1. Park-Consolidation Pagination

### What this test is about

The obligation sweeper groups due shed-level vaccination obligations into a
single park-level drive when multiple sheds in the same park can be handled in
one visit. At small scale this is a convenience feature. At large scale this is
a liveness risk: the sweeper pages through candidate obligations, so every page
loop must make forward progress.

The original failure mode was that an exact full page could be returned again
and again. If page size was 2 and the repository kept returning the same 2
candidates, the worker could loop forever, hold resources, and never finish
batching the rest of the tenant.

### Test setup

The regression tests use a fake sweeper repository, not a mock UI. The fake repo
returns park-consolidation candidates ordered by the same keyset cursor used by
the real repository:

```text
park_id, rule_id, target_species, target_animal_stage, due_at, obligation_id
```

One test seeds four candidate obligations across four sheds in the same park and
sets the service page size to 2. That forces an exact-page boundary:

```text
page 1: obl-1, obl-2
page 2: obl-3, obl-4
page 3: empty page, proving the scan terminated
```

A separate negative test sets `repeatParkPage=true` so the fake repository keeps
returning the same full page. That simulates the bug directly.

### Action executed

The test calls:

```text
consolidateParkDrives(context, tenant-1, version-1, SweepConfig{ParkConsolidation: default}, dueBefore)
```

That is the same service path used by the sweeper when it builds park-level
vaccination drives from unbatched shed obligations.

### Assertions that passed

- The happy-path exact-boundary case calls the candidate list method exactly 3
  times: two data pages plus the final empty page.
- The happy path creates exactly 1 park batch and attaches all 4 candidate
  obligations.
- The failure-path test does not hang. It returns an explicit error containing
  `pagination did not advance`.
- The progress guard compares the last cursor from the current page with the
  previous cursor, so a non-advancing repository page fails fast instead of
  looping.

### Source evidence

```text
backend/internal/obligation/app/park_consolidation.go
backend/internal/obligation/app/park_consolidation_test.go
TestConsolidateParkDrivesPagesParkCandidatesWithCursor
TestConsolidateParkDrivesFailsWhenCandidateCursorDoesNotAdvance
```

## 2. Process-Integrity Projection Recompute

### What this test is about

Control Tower, Action Center, Protocol Adherence, and process-integrity views
must read a durable projection. They cannot reconstruct the whole obligation
state machine from raw obligation, SOP, proof, completion, goat, protocol, and
workforce tables on every dashboard request.

The original scale risk was twofold:

1. The read path could fall back to canonical replay when the projection became
   stale.
2. The recompute path could delete tenant projection rows before the replacement
   generation was fully ready.

At 1M goats, either behavior is dangerous. Canonical replay becomes a dashboard
pileup, and delete-then-reinsert creates a long transaction, WAL pressure, and a
window where hot reads have no safe projection to serve.

### Test setup

The integration tests boot a disposable Postgres database with the real schema.
They seed process-integrity source rows and then run the real repository:

```text
repo := processintegrity/postgres.NewRepository(pool, 5s timeout)
repo.RecomputeProjection(tenant, as_of)
```

The tests then manipulate projection state deliberately:

- insert stale projection rows with an old `projection_version`
- mark the projection `stale`
- mark the projection `rebuilding`
- mark the projection `failed`
- clear `serving_projection_version`
- stamp a row with a marker string so the read path can prove it served the
  projection row instead of replaying canonical source tables

### Action executed

The tests execute the real repository methods:

```text
RecomputeProjection
projectionReadable
ListRows
CountByWorkState
EXPLAIN processIntegrityProjectionRowsSQL
EXPLAIN processIntegrityProjectionCountsSQL
```

This covers both write-side projection generation and read-side dashboard
behavior.

### Assertions that passed

- Recompute writes a new projection generation keyed by
  `(tenant_id, projection_version, row_id)` before serving it.
- The serving pointer moves to the new `serving_projection_version` only after
  the new generation exists.
- Old non-serving rows are pruned after the serving generation is safe.
- Multi-batch prune drains all stale rows even when the stale generation is
  larger than one prune batch.
- The currently serving projection rows are not pruned.
- A `stale` projection with a serving version remains readable.
- A `rebuilding` projection with a serving version remains readable, so the
  dashboard serves the last good data while recompute runs.
- A `failed` projection is not served.
- A `rebuilding` projection with no serving version is not served.
- A stale projection row marked with `served-from-stale-projection` is returned
  by `ListRows`, proving the read path used projection rows instead of canonical
  replay.
- The hot read plan touches `process_integrity_projection_rows`.
- The hot read plan does not replay these canonical source tables:
  `obligation_instances`, `obligation_status_events`,
  `vaccination_completions`, `sop_tasks`, `sop_submissions`, `goats`,
  `protocol_versions`, `protocol_rules`, or `workforce_members`.
- The plan uses an index scan, bitmap index scan, or index-only scan.

### Source evidence

```text
backend/internal/processintegrity/adapters/postgres/repository.go
backend/internal/processintegrity/adapters/postgres/repository_integration_test.go
TestProcessIntegrityProjectionRecomputePrunesStaleGenerationRows
TestProcessIntegrityProjectionPruneExhaustsMultipleBatches
TestProcessIntegrityProjectionReadableDuringStaleAndRebuildStates
TestProcessIntegrityProjectionServesStaleProjectionInsteadOfCanonicalReplay
TestProcessIntegrityProjectionReadPlanDoesNotReplayCanonicalTables
TestProcessIntegrityProjectionInsertSQLUpsertsRows
```

## 3. Vaccinationexecution Hot-Read Latency Gates

### What this test is about

The vaccination execution surfaces answer operational questions such as which
sheds need work, which drives are active, which animals are blocked, and which
execution rows need proof or verification. These endpoints are user-facing hot
paths. A green correctness test is not enough if a single request takes multiple
seconds on a tiny staging dataset.

This patch did not rewrite vaccinationexecution into a new projection. Instead,
it added explicit latency gates so slow read paths cannot be waved through
silently. If the gate fails, the next fix is a durable execution read model with
indexed reads.

### Test setup

The API latency manifest includes the existing dashboard hot paths plus the
vaccinationexecution endpoints:

```text
/control-tower/vaccination?category=vaccination
/vaccination/action-center?category=vaccination&limit=50
/vaccination/action-center/counts?category=vaccination
/vaccination/adherence?category=vaccination&limit=50
/calendar/vaccination/events?limit=50
/vaccination/execution?limit=50
/vaccination/operations?limit=50
/vaccination/sheds?limit=50
```

The process-integrity database latency command also measures the fresh
projection path and then intentionally marks the projection stale so the stale
path is measured too.

### Action executed

The local/backend verification ran syntax and test coverage for the gate code,
and the staging workflow can run the same gates against a live API:

```bash
node tools/perf/api-latency-gate.mjs --manifest tools/perf/hot-paths.vaccination.json
cd backend && go run ./cmd/process-integrity-latency-check
```

### Assertions that passed

- The hot-path manifest parses as valid JSON.
- Every manifest entry has an explicit threshold.
- The API latency gate records p50, p90, p95, p99, and max latency for each
  endpoint.
- The gate exits non-zero when any endpoint exceeds its p90, p95, or p99
  threshold.
- Process-integrity latency checks require a fresh projection before measuring.
- Process-integrity latency checks also measure the stale-projection serving
  path so a future canonical fallback regression is visible.
- The staging target guard rejects the wrong environment when
  `GOATOS_ENV=stg`, so the live check cannot accidentally run against a local
  database while claiming staging evidence.

### Source evidence

```text
tools/perf/api-latency-gate.mjs
tools/perf/hot-paths.vaccination.json
backend/cmd/process-integrity-latency-check/main.go
docs/runbooks/performance-gates.md
```

## Verification Commands

Commands run for this report:

```bash
cd backend && go test ./internal/obligation/app ./internal/processintegrity/adapters/postgres
cd backend && go test ./internal/obligation/...
cd backend && go test ./internal/processintegrity/...
cd backend && go test ./internal/vaccinationexecution/...
cd /Users/ravi/mesha/goatos && bash backend/tests/integration/validate-postgres-migrations.sh
node -e "JSON.parse(require('fs').readFileSync('tools/perf/hot-paths.vaccination.json','utf8')); console.log('hot-paths manifest ok')"
bash -n tools/dev/high-scale-kernel-e2e-all.sh
node --check tools/perf/api-latency-gate.mjs
```

Observed result summary:

```text
PASS backend/internal/obligation/app
PASS backend/internal/obligation/adapters/postgres
PASS backend/internal/obligation/...
PASS backend/internal/processintegrity/adapters/postgres
PASS backend/internal/processintegrity/...
PASS backend/internal/vaccinationexecution/...
PASS postgres migration validation
PASS hot-paths manifest JSON parse
PASS high-scale-kernel-e2e-all.sh syntax
PASS api-latency-gate.mjs syntax
```

## Certification Boundary

This is not a 1M staging certification report. The full certification gate
remains:

```bash
make high-scale-kernel-e2e-certification
```

That certification must attach staging evidence for Cloud SQL shape, migration
head, seed checksum, scaled `EXPLAIN (ANALYZE, BUFFERS)`, API p95/p99,
outbox/sweeper/projection metrics, and the `goatos-stg-1m-benchmark-v1`
baseline.

## Final Verdict

The local/backend scale-audit proof is green for the three defects covered here:

- Park consolidation cannot silently loop on a non-advancing full page.
- Process-integrity reads keep serving the last good projection during stale or
  rebuilding states and do not replay canonical source tables on the hot path.
- Vaccinationexecution hot reads are now part of latency-gate coverage, so slow
  endpoints must be fixed or explicitly certified before handoff.

Remaining production-scale work is the final incremental process-state projector
and the staging 1M certification run.
