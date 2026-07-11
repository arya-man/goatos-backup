# Scale Audit Fix E2E Report - 2026-07-11

Status: local/backend E2E proof complete; staging 1M certification not run.

## Scope

This report covers the verified scale-audit fixes from the Claude/Codex review:

1. Obligation park-consolidation pagination could reread the same full page.
2. Process-integrity projection recompute deleted the tenant serving table before rebuilding it.
3. Vaccinationexecution live CTE reads needed explicit latency-gate evidence before a projection rewrite decision.

Out of scope for this patch: a full outbox-fed incremental `obligation_process_state` projector and the `goatos-stg-1m-benchmark-v1` certification run.

## Fixes

### 1. Obligation Park-Consolidation Pagination

Result: fixed.

The sweeper now passes a keyset cursor through `ListUnbatchedShedDueForParkConsolidation`. The cursor matches the repository order:

```text
park_id, rule_id, target_species, target_animal_stage, due_at, obligation_id
```

This prevents the exact-page loop from repeatedly loading the first page when `len(rows) == page`. Regression coverage forces a two-page exact-boundary case and asserts the final empty page is reached.

### 2. Process-Integrity Projection Recompute

Result: hardened.

The recompute path no longer clears the serving `process_integrity_projection_rows` generation before inserting. It now:

1. Marks projection state as `rebuilding`.
2. Writes the new generation by `(tenant_id, projection_version, row_id)`.
3. Atomically flips `serving_projection_version` to the new generation after the row count is known.
4. Prunes older non-serving generations in bounded batches after the fresh generation is committed.

During a rebuild, reads continue to serve the last good `serving_projection_version`. A failed projection state does not serve stale rows, and a rebuilding state with no serving version does not serve rows.

This is still a canonical replay projector, not the final incremental outbox projector. The important change is that rebuild failure leaves the last good projection intact, and slow rebuilds do not immediately force dashboard reads back to canonical CTEs.

### 3. Vaccinationexecution Hot Reads

Result: gated, not rewritten.

The live CTE paths remain source-backed reads. They are now included in the API latency gate manifest:

```text
/vaccination/execution?limit=50
/vaccination/operations?limit=50
/vaccination/sheds?limit=50
```

The high-scale E2E report checklist now requires explicit p95/p99 evidence for these endpoints. If the staging run fails the threshold, the next fix is a durable execution read model; this patch avoids a speculative projection rewrite without measured failure.

## Verification

Commands run:

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

Results:

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

This is not a 1M staging certification report. The full certification gate remains:

```bash
make high-scale-kernel-e2e-certification
```

That run must attach staging evidence for Cloud SQL shape, migration head, seed checksum, scaled `EXPLAIN (ANALYZE, BUFFERS)`, API p95/p99, outbox/sweeper/projection metrics, and the `goatos-stg-1m-benchmark-v1` baseline.

## Final Verdict

The verified correctness issues are fixed or gated:

- Park consolidation no longer has the full-page reread failure mode.
- Process-integrity recompute no longer deletes the serving projection before rebuilding.
- Vaccinationexecution live reads are now part of the E2E latency gate and cannot be silently waved through.

Remaining production-scale work is the final incremental process-state projector and the staging 1M certification run.
