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

2. **Canonical request-path guard**
   - Command: run the API latency probe with
     `tools/perf/hot-paths.canonical-reads.json`, then validate
     `tools/perf/request-path-usage.sql` with
     `tools/perf/request-path-evidence.mjs`.
   - Purpose: proves the hot HTTP paths read bounded canonical tables and do
     not touch deleted dashboard/schedule projection tables.
   - This guard replaces the old process-integrity projection latency command.
     A green report must not require warming copied dashboard tables before the
     API can serve.

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
     Protocol Adherence, Calendar, Full Schedule, execution, operations, and
     shed summary:
     `/vaccination/schedule`, `/vaccination/execution`,
     `/vaccination/operations`, and `/vaccination/sheds`.
   - Run with higher concurrency during staging certification, but keep a
     single-concurrency gate too because it exposes query latency without queue
     noise.

4. **Full high-scale certification**
   - Command: `make high-scale-kernel-e2e-certification` plus attached staging
     evidence from the 5k-50k canonical benchmark run.
   - Purpose: proves generation, outbox, sweeper, hot reads, p95/p99, DB
     pressure, retry/DLQ, and API/browser parity.

## Design Rule

Hot dashboards must read canonical Postgres tables through bounded, indexed,
keyset-paginated queries. They must not depend on copied dashboard projection
tables, and they must not rebuild broad tenant state in memory per request.
Redis can be added later as a short-TTL edge cache for already bounded reads,
but it is not the source of truth and it must not hide an unbounded query.

For Control Tower, Action Center, Calendar, Full Schedule, execution,
operations, and shed summary:

- canonical writes remain in Postgres transactions with audit/outbox;
- APIs query canonical obligations, completions, batches, goats, locations, SOP
  state, and protocol rules with tenant/scope/date/cursor predicates;
- seed/reseed validation must call the APIs directly after migration, seed
  closeout, obligation generation, and sweeper; it must not warm deleted
  dashboard projection tables;
- successful responses should not require a projection freshness state before
  returning data;
- staging reports include API p95/p99, response-size evidence, request-path
  evidence, and browser proof for the same checked-out SHA.

Calendar is included in the same canonical-read rule. It reconstructs event
rows from source events, obligations, batches, SOP/proof state, snoozes, and
notification state through bounded SQL. Completion history remains a bounded
canonical-history read backed by the accepted-history index. There is no manual
dashboard projection rebuild step for green performance evidence.
