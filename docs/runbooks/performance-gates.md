# Performance Gates

Goat OS hot paths must be tested as latency contracts, not only as correctness
or visual smoke tests. The staging seed issue on 2026-07-11 showed why: a
correct Control Tower response can still be unusable when one request rebuilds
process state from raw obligations, SOP, completion, location, and workforce
tables.

## Required Gate Layers

1. **Static plan reachability**
   - Command: `make validate-sqlc-plans`
   - Purpose: catches obvious broad sequential scans and missing hot indexes.
   - Limitation: not enough by itself. A query can use indexes and still be slow
     because it groups/sorts/reconstructs too much state.

2. **DB hot-path latency**
   - Command:
     ```bash
     cd backend
     DATABASE_URL="$DATABASE_URL" go run ./cmd/process-integrity-latency-check \
       -iterations 10 \
       -warmup 1 \
       -max-action-center-p95 500ms \
       -max-control-tower-p95 500ms \
       -max-protocol-adherence-p95 500ms \
       -max-count-p95 250ms
     ```
   - Purpose: measures repository/service latency directly against Postgres.
   - Default behavior: rebuilds `process_integrity_projection_rows` first and
     fails unless the fresh projection serves the measured `as_of`. Use
     `-recompute-projection=false -require-projection=false` only for raw
     replay debugging, never for a green performance report.
   - The gate also marks the projection state `stale`, measures the hot reads
     again, and restores the original state. A green report must include
     `stale_action_center_repository` and `stale_control_tower_counts_only`.
     Stale projection reads must remain projected and fast; falling back to the
     canonical replay query is a scale regression.
   - This is the gate that should catch a 4s single process-integrity query even
     if the API and browser are otherwise healthy.

3. **API hot-path latency**
   - Command:
     ```bash
     GOATOS_API_BASE_URL="$GOATOS_API_BASE_URL" \
     GOATOS_BEARER_TOKEN="$GOATOS_BEARER_TOKEN" \
     GOATOS_TENANT_ID="$GOATOS_TENANT_ID" \
     node tools/perf/api-latency-gate.mjs \
       --manifest tools/perf/hot-paths.vaccination.json \
       --iterations 20 \
       --warmup 2 \
       --concurrency 1
     ```
   - Purpose: measures p50/p90/p95/p99 for real HTTP endpoints.
   - Hard API ceiling: `p90<=300ms`, `p95<=500ms`, and `p99<=1000ms`.
     Manifests may tighten these values but the runner rejects any manifest that
     relaxes them. Counts remain tighter at `p90=250ms`.
   - Vaccination-slice coverage includes Control Tower, Action Center,
     Protocol Adherence, Calendar, plus the live vaccinationexecution CTE reads:
     `/vaccination/execution`, `/vaccination/operations`, and
     `/vaccination/sheds`.
   - Run with higher concurrency during staging certification, but keep a
     single-concurrency gate too because it exposes query latency without queue
     noise.

4. **Full high-scale certification**
   - Command: `make high-scale-kernel-e2e-certification` plus attached staging
     evidence from `goatos-stg-1m-benchmark-v1`.
   - Purpose: proves generation, outbox, sweeper, projection refresh, hot reads,
     p95/p99, DB pressure, retry/DLQ, and projection parity.

## Design Rule

Hot dashboards must read projection/read-model tables or bounded counter tables.
They must not reconstruct broad process state from canonical transaction tables
on every request. Redis can be added later as a short-TTL edge cache for already
bounded reads, but it is not the source of truth and it must not hide an
unbounded query.

For Control Tower, Action Center, Calendar, and Protocol Adherence:

- canonical writes remain in Postgres transactions with audit/outbox;
- `process-integrity-projection-recompute` or the future incremental projector
  refreshes `process_integrity_projection_rows` after seed/import/canonical
  writes;
- recompute builds a new `projection_version`, atomically flips
  `process_integrity_projection_state.serving_projection_version`, then prunes
  old versions in bounded batches; it must not delete the live serving version
  before the replacement is ready;
- the same off-request transaction publishes
  `process_integrity_projection_summaries`; navigation counts, Control Tower
  cards, and adherence KPIs read those versioned summary grains rather than
  grouping all serving rows per request. Summary due windows use IST business
  dates; row-list boundaries are normalized to the same start/end-of-business-
  day contract so partial timestamps cannot produce mismatched totals;
- read APIs query the projection by tenant, category, scope, state, due window,
  owner, and cursor;
- a rebuild in progress on a tenant that already has a serving version keeps
  serving that last-known-good version (its state row stays `fresh`/`green` and
  is not flipped to `rebuilding`), so a scheduled replacement build never opens a
  503 gap over a compatible last-known-good projection. Only a genuine first-ever
  build (no serving pointer), a failed build with no prior serving version, an
  explicitly stale/failed state, or a projection older than the freshness TTL
  fails closed with a typed retryable `503`; the request path never replays
  canonical tables. The freshness TTL (7 minutes) sits above the 5-minute
  projector schedule so one jittered or slow refresh cycle still serves the
  last-known-good projection instead of flapping to 503;
- successful responses expose version, `projected_at`, `as_of`, freshness, and
  serving state;
- staging reports include scaled `EXPLAIN (ANALYZE, BUFFERS)` and API p95/p99.

Current residual: the five-minute scheduled process-integrity projector is a
full tenant rebuild. It is the repair/backfill path and is idempotent with an
atomic last-known-good version swap, but the event-driven dirty-scope
queue/worker is not implemented yet. Until that consumer lands, the API's
freshness-TTL fail-closed policy is the correctness boundary; a scheduler delay
beyond the 7-minute TTL is visible as `projection_stale`, not hidden by
request-time recompute. Because the TTL exceeds the 5-minute schedule, a single
late or slow rebuild cycle still serves the last-known-good version rather than
503-ing, but a build that runs longer than the TTL window is still a real gap
and is the reason the bounded incremental projector remains the required
follow-up.

Calendar follows the same freshness boundary through
`calendar_projection_state`: the off-request projector now runs the whole page
refresh, tombstone, and state publication inside one Repeatable Read
transaction, so a failed partial refresh rolls back atomically and the prior
committed `calendar_projection_state` row and event rows are left untouched —
the last-known-good generation keeps serving through ordinary transaction
atomicity rather than being made unavailable. It upserts state on first
bootstrap, publishes a fresh version only after the refresh/tombstone loop
commits, and marks genuine failures red only when there is no prior serving
version. List responses expose that version and timestamp; a missing/first-ever,
no-serving-version-failed, or older-than-TTL (7 minute) state returns a typed
`503`. Accepted completion history remains a bounded canonical-history exception
backed by `vaccination_completions_accepted_history_calendar_idx`. Two follow-ups
remain: the single large refresh transaction holds a longer lock/keeps more dead
tuples across a big multi-page rebuild, and Calendar still lacks the dirty-scope/
versioned multi-generation worker — both are folded into the bounded incremental
projector follow-up.

Manual process-integrity rebuild:

```bash
cd backend
DATABASE_URL="$DATABASE_URL" go run ./cmd/process-integrity-projection-recompute \
  -tenant-id "$GOATOS_TENANT_ID"
```
