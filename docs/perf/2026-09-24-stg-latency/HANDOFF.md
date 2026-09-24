# Handoff: where the stg latency work stands (2026-09-24 evening)

A new session (Claude or Codex, any account) can continue from this file and
`README.md`. Nothing has landed on main and nothing is deployed to stg. The
maintainer said: **push to PR #389 only, and do not land or deploy until they
explicitly say so.**

- PR: vgoats/goatos#389, branch `perf/stg-burst-and-login`
- Budget: every API p95 target 50–100 ms, acceptable 200–300 ms, hard max 500 ms
- Landing gate, when the maintainer asks to land:
  1. every judge is CLEAN;
  2. `audit/cache-correctness.md` shows zero stale-data (N) cells, and the read-your-writes E2E passes on every page;
  3. then `make land-main`, and deploy the exact landed SHA with `/Users/raviteja/bin/goatos-stg-deploy`;
  4. then run the post-deploy steps below.

## On #389 and judged CLEAN

| Area | Measured result |
|---|---|
| Analytics rollup: incremental watermark, non-fatal BigQuery, backoff, 15-day retention with GCS archive (000400, 000401) | A rerun reads 88 KB instead of 5.3 GB, and the dashboard tables are MD5-identical. This removes the 15–30 s spikes. |
| Admin-web: sign-in dedupe, 30 s per-user cache, read-your-writes cookie, per-call write-marker contract test | 1 login + 5 loads: 41 backend calls, now 15 |
| Auth pool isolation, 503 instead of 403 on DB-unavailable, allowlist max age + backoff | Session-events under saturation: 11.9 s, now 412 ms (p50) |
| Android: login doesn't block on session-events, no raw errors, denied sign-in doesn't wipe unsynced work, outbox v7 | Login during a DNS drop used to fail; now it works |
| Query rewrites: FCR segments, notification unread count, weight demographics | FCR ~2.0 s, now ~0.77 s |
| Stg DB flags `random_page_cost=1.1` + Query Insights | **Applied live** and mirrored in `infra/envs/stg/cloud_sql.tf` |
| Docs: this folder, banned-pattern catalog P1–P25 in `.agents/skills/scale-anti-patterns`, perf lens in `goatos-code-review` / `goatos-build`, rule docs, PR template | — |

## On #389 but NOT clean: fix before landing

| Area | Open judge findings | Where the in-progress work is |
|---|---|---|
| Shared read cache (`backend/internal/platform/readcache`) | See the list below this table. | `wip/perf-read-cache` (c44936389 is not yet judged) |
| Herd Signals live | N1–N7 + pen=partition **pushed as e96822461** (000402, 000404), re-judge in progress; open question: risk lag up to ~7.5 min for steadily reporting tags | `wip/perf-herd-live`. The worktree also had 19 uncommitted files mid-work; redo them if they're lost. |
| ~~Notifications stored unread counter (000403)~~ **CLEAN after the deadlock fix c6f6048ad** | See the list below this table. | `wip/perf-notifications` |

**Shared read cache.** Writers outside weighing never invalidate the cache. The full matrix is in `audit/cache-correctness.md` items 1–7:
- identity goat relocate, lifecycle, create, census, stage
- obligation `partition_move`
- procurement receive
- feed purchases and the feed-purchase importer
- feeddirection local evict
- the kernel carry-over close
- the herd-signals mapping
- locations and configuration

Also:
- the stale window must go to 0–5 s
- a campaign park move must evict both parks
- the listener ping needs a timeout
- the cache needs a byte bound
- warm-up must start after LISTEN, with jitter
- add a read-your-writes E2E regression test

**Herd Signals live.** F1–F4 are fixed. Still open:
- **N1:** the per-minute full risk pass is itself a load problem. Make it change-driven, with an hourly sweep, and don't hold a pool connection for the whole pass.
- **N2:** unclassified tags are shown inconsistently.
- **N3:** the displayed score and state can come from different sources.
- **N5:** the CSV export must use the persisted risk.
- **N6:** 000402 must follow the repo convention: `NOT VALID` checks and `CREATE INDEX CONCURRENTLY` in a `NO TRANSACTION` migration.
- **N7:** risk tracking column semantics.
- **Maintainer decision:** the comparison group is the pen, meaning the **partition the animal is in** ("Godel 1 – Part 1", "Castro 1", "Yashoda 1"), per `docs/agent-rules/partition-location.md`. It is not raw `goats.shed_id`, and it doesn't change with page filters.

**Notifications stored unread counter.**
- **HIGH, reproduced deadlock:** `CloseVaccinationBatch` makes several counter-touching statements in one transaction while the bridge fans out to several members at once. Fix it with one set-based resolve UPDATE per transaction, plus a 40P01 retry.
- Document the hot leadership count row.
- Resolve per-item notices early where the keys allow it.
- Make the backfill gate open automatically after deploy.

## Queue (full detail in `QUEUE.md`)

- **FCR/ADG precomputed rollup,** so cold loads stay under 500 ms. WIP is on `wip/perf-fcr-rollup` and isn't ready.
- **Real-stg bench follow-ups** (`REAL-STG-READONLY-BENCH.md`):
  - `/work-board/page` got slower (DB time 1,327 → 1,945 ms) and `/alerts/rows` too. Find which PR change caused it.
  - `vaccination/command`, `shed-dose-matrix`, `vaccination/sheds` and `app/vaccination/execution` still fail the budget.
- **Wave 2** (from `audit/`):
  - proof lookup `capture_sql.go:203`: 5.5 s, can be 8 ms
  - work-board vaccination check: 228 ms, can be 5 ms
  - vaccination caches keyed on the business date
  - drop canceled obligations from the totals (maintainer-approved)
  - calendar targets: 7 s
  - `obligation_instances` bloat
  - feed daily rollup
  - counts and alerts batching
  - `/identifiers/.../resolve` is broken on every call (a missing JOIN)
  - `workflows/{row_id}` should filter first
  - action-center
- **Wave D:** indexes for the tables with the most seq-scan reads (`obligation_instances`, `goats`, `weighing_observations`, `feed_direction_issue_rows`, and more; see `audit/db-wide.md`), a config-table cache, retention for the outbox and notifications, and dropping duplicate and unused indexes.
- **Wave G:** automated guardrails (see `audit/guardrails-design.md`).
- **Wave O:** a delta-only OCI sync tool, with no full dumps.
- **Wave R:** the final report, re-captured from stg after deploy with the same script as `baseline/`.

## After deploy (maintainer-triggered)

1. Run `cmd/backfill-notification-unread-counters -apply`, then run it again without `-apply`; it must report 0 mismatches.
2. Terraform apply for the analytics archive bucket and IAM (`infra/envs/stg/analytics_rollup.tf`). Firebase→BigQuery export is optional.
3. Re-capture the stg metrics with the baseline script, and write `REPORT.md` as before vs after.

## How to work on this (rules we've been following)

- **Keep 3 builders at most.** Each builder works in its own worktree off `origin/perf/stg-burst-and-login`, writes a failing test first, and measures before and after with `EXPLAIN (ANALYZE, BUFFERS)` plus endpoint timings.
- **Integrate, then judge.** Cherry-pick the builder's work onto the PR branch and push. An adversarial judge agent (read-only) reviews it, and its findings go back to the builder until the judge says CLEAN.
- **Never `git stash`,** never force-push `main`, and never run `gh pr merge`.
- **Write only to throwaway DBs.** Stg is read-only through `mesha_ceo_readonly` with `default_transaction_read_only=on`, and the OCI clone at `127.0.0.1:15432` is read-only too.
- **Watch migration numbers.** Main ends at 000399. This branch uses 000400–000403; check before adding a new one.

## Fast-track (started by maintainer request, 2026-09-24 evening)

Wave 2 is running as 4 parallel builders on top of the 3 already going, 7 in total. Each has its own worktree and branch off the PR head:

- `perf/w2-proofs`: the proof lookup (5.5 s, should be 8 ms), the broken identifiers resolve, workflows filter-first, goats search, the proof uploads index
- `perf/w2-vaccination`: cache keys that actually hit (with eviction), dropping canceled obligations from totals, the command-board limiter, the sheds CTE, `obligation_instances` autovacuum
- `perf/w2-workboard`: the work-board and alerts regression from the real-stg bench, the vaccination-today check (228 ms, should be 5 ms), batched counts, action-center, alerts low-stock, breakdown
- `perf/w2-calendar`: calendar targets (7 s) and events (2.8 s), the daily feed rollup, the demographics index, the feed-direction aborted-transaction retry bug

**Migration numbers will collide between these parallel branches.** Renumber at integration time; the duplicate-versions guard catches collisions.

## Resume checklist for the next session (any account)

1. Read this file, then `README.md`, then `QUEUE.md`.
2. In-progress builder work is backed up on origin as `wip/perf-*` branches:
   - `wip/perf-read-cache`: cache-writer invalidation, still in progress
   - `wip/perf-herd-live`: 2856c46ef adds the change-driven risk classifier; still in progress (N1–N7, pen = partition)
   - `wip/perf-base-origin`: FCR rollup WIP
   - `wip/perf-w2-proofs`, `wip/perf-w2-vaccination`, `wip/perf-w2-workboard`, `wip/perf-w2-calendar`: Wave 2; builders had just started
3. For each branch, `git log origin/perf/stg-burst-and-login..origin/<wip>` shows the builder commits not yet in the PR. Finish them, then cherry-pick onto the PR branch, push, and run an adversarial judge. Loop until the judge is CLEAN. Renumber migrations if they collide.
4. Keep pushing progress to this file and to `QUEUE.md`. Do not land or deploy until the maintainer says so.

## STOPPED for an account switch (2026-09-24, late evening)

All agents and the 10-minute loop are stopped. Every builder's work (committed, plus uncommitted work saved as "WIP (stopped for account switch)" commits) is pushed to origin. **None of these branches is in the PR yet, and none has been judged unless noted.**

| Branch | Commits beyond the PR | State | Next step |
|---|---|---|---|
| `wip/perf-herd-live` | 596220895 | Herd R1–R4 fix (lock-free SQL risk scoring, ingest fast path), not judged | Judge against the R1–R4 list below, then cherry-pick |
| `wip/perf-read-cache` | 3 | Per-request auth-lookup coalescing, plus cache-writer invalidation in progress (weighing full suite was running) | Finish `audit/cache-correctness.md` items 1–7 (goat, partition, procurement, feed-purchase, feeddirection-local, kernel carry-over, herd mapping, locations/config writers), the stale window to 0–5 s, and the read-your-writes E2E, then judge |
| `wip/perf-fcr-rollup` | 778ec44b7 | FCR/ADG precomputed rollup, early WIP | Continue (it was paused behind the cache fixes) |
| `wip/perf-w2-proofs` | 5 (last one WIP) | Proof lookup rewrite, identifiers JOIN, workflows, goats search, index | Finish (it was writing a red test for a narrowing function), then judge |
| `wip/perf-w2-vaccination` | 1 WIP | Just started: a red test for cache-scoped payloads | Continue the brief in `QUEUE.md` Wave 2 vaccination |
| `wip/perf-w2-workboard` | 1 WIP | Just started | Continue (it was starting a throwaway PG) |
| `wip/perf-w2-calendar` | 2 (includes the feeddirection aborted-tx bug fix) | Partial | Continue the calendar + feed rollup brief |

**Herd Signals R1–R4 judge findings,** from judging e96822461. N1–N7 and pen=partition are CLEAN.
- **R1 (high):** the classifier must not hold `FOR UPDATE` row locks during enrichment. Read without locks and write with an optimistic guard.
- **R2:** at ingest, set `risk_due_at=now()` when the movement state or pattern changes, or when motion drops far below baseline.
- **R3:** use a 24h baseline rollup so each batch is one SQL statement, and measure 50k.
- **R4:** make the tests deterministic by seeding the jitter and adding `ORDER BY tag_id` to the stale subquery.

**Don't go in circles:**
- Every item above already has its findings written down; don't re-audit.
- Take the branch, finish the listed step, judge it, cherry-pick it onto `perf/stg-burst-and-login`, and push.
- Migration numbers used so far: 000400–000404. Parallel branches may collide, so renumber when you integrate.
- Still no landing or deploy until the maintainer says so.
