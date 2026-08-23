# Herd Signals Partition Retention

Operational contract for `backend/cmd/herd-signals-partition-maintenance`
(migration `000201_herd_signal_packets_partition_maintenance.sql`), which
keeps `herd_signal_packets` — the daily-partitioned raw BLE/UDP packet table
(migration `000200`) — safe to ingest into and bounded in size. Design
background: `docs/modules/herd-signals-system-design.md` Section 3.

## What this job does, once per run

1. **`herd_signal_packets_ensure_future_partitions(days_ahead=14)`** —
   idempotent. Creates any missing daily partition from today through
   today+14. Keeps the pre-created runway (migration 000200) topped up so
   ingest never has to fall back to the `DEFAULT` catch-all partition.
2. **`herd_signal_packets_prune_expired_partitions(retention_days=14)`** —
   `DROP TABLE` on every daily partition entirely older than 14 days.

Both functions are hardcoded, in SQL, to `public.herd_signal_packets` only
(`'public.herd_signal_packets'::regclass`, and a `relkind = 'p'` guard on the
prune side). **This job cannot be pointed at any other table** — not by a
flag, not by an env var, not by a typo in this binary or in a raw `psql -c`
invocation of the same functions. It is structurally impossible for a run of
this job to drop a partition belonging to `herd_signal_activity_windows` or
`herd_signal_tag_latest` (those tables aren't partitioned at all, so
"dropping their partition" isn't even an operation that exists), so an
aggregate table can never be taken down as a side effect of raw-packet
retention.

## IRREVERSIBLE: read this before touching `retention-days`

Dropping a partition is `DROP TABLE`. **Raw packets in a dropped partition
are gone — not archived, not soft-deleted, not recoverable from within Goat
OS.** Nothing on any request path reads `herd_signal_packets` today; what
survives past the retention window is only the *derived* state
(`herd_signal_activity_windows` at its own 24h/30d/13mo per-tier retention,
`herd_signal_tag_latest`'s live snapshot) plus anything already re-derived
before the raw rows aged out. If you need raw packets for forensics or to
re-derive activity windows under new decoding rules, you have exactly the
retention window (default 14 days) to do it before that data is
unrecoverable.

Lowering `-retention-days` below 14 trades that forensics/re-derivation
window for storage cost. Storage cost is the *symmetric*, recoverable side of
this trade (keep a few extra days, pay a few extra GB); data loss is the
*asymmetric*, irreversible side. When in doubt, don't lower it.

## Scheduling

| Env | Cloud Run Job | Cloud Scheduler | Command |
|---|---|---|---|
| dev | `goatos-dev-herd-signals-partition-maintenance` (`infra/envs/dev/cloud_run_jobs.tf`, `local.kernel_jobs.herd_signals_partition_maintenance`) | **Yes** — `21 0 * * *` (00:21 IST), same `google_cloud_scheduler_job "kernel"` `for_each` every other kernel job uses | `-timeout=90s -days-ahead=14 -retention-days=14` |
| stg | `goatos-stg-herd-signals-partition-maintenance` (`infra/envs/stg/herd_signals_partition_maintenance.tf`) | **No** — stg has no `google_cloud_scheduler_job` resource for *any* Cloud Run Job today (the `scheduler` runtime SA in `infra/envs/stg/main.tf` is reserved for this but nothing binds it yet). This job is declared and invokable but **must be triggered manually** until that infra is added. | same args, run manually (below) |

### Manual stg trigger (until Cloud Scheduler is wired for stg)

Run this daily, by hand or via whatever external cron the team is using for
stg break-glass operations, until stg gets the same scheduler wiring dev has:

```bash
gcloud run jobs execute goatos-stg-herd-signals-partition-maintenance \
  --project=goatos-stg --region=asia-south1 --wait
```

**This is a real gap, not a formality.** If nobody runs this command daily on
stg, stg's `herd_signal_packets` runway will exhaust and new packets will
start landing in the `DEFAULT` partition (silently destroying pruning), and
raw packets will accumulate unbounded (~65 GB/day at the release envelope —
see `docs/modules/herd-signals-system-design.md` Section 1.5). Whoever owns
stg infra should add a `google_cloud_scheduler_job` pointed at this Cloud Run
Job, following the exact `infra/envs/dev/cloud_run_jobs.tf`
`google_cloud_scheduler_job "kernel"` pattern, as a follow-up.

### Dev (verify it ran, or trigger off-schedule)

```bash
gcloud run jobs execute goatos-dev-herd-signals-partition-maintenance \
  --project=<dev-project-id> --region=<dev-region> --wait
```

## What healthy looks like

Every run (dev on schedule, stg whenever it's manually triggered) should log
two structured lines and emit two metrics:

```text
ensure-future-partitions: N created (days-ahead=14)
prune-expired-partitions: M dropped (retention-days=14)
herd-signals-partition-maintenance complete created=N dropped=M days_ahead=14 retention_days=14
```

Metrics (`backend/internal/platform/kmetrics/herdsignalspartitionmaintenance.go`,
OTel -> the same collector/Grafana pipeline every other kernel job's metrics
go through):

- `kernel.herd_signals_partition_maintenance.run.duration` (histogram,
  labeled `outcome=succeeded|failed|dry_run`)
- `kernel.herd_signals_partition_maintenance.partitions_created` (counter)
- `kernel.herd_signals_partition_maintenance.partitions_dropped` (counter)

**Steady state, once the table is older than the retention window:** both
counters should show roughly **1 partition/day** — one new day's partition
created, one 14-days-ago partition dropped, every run. `created` will read
higher than 1 only on the very first run in a new environment (topping up
the initial 14-day runway in one shot) or after a scheduling gap (catching
up). `dropped` reads 0 for the table's first `retention_days` (nothing is
old enough to drop yet) — that is expected, not a bug, until the table has
been ingesting for that long.

## What to check if it looks unhealthy

- **`partitions_created` stuck at 0 for more than a day, and ingest starts
  failing or logging DEFAULT-partition writes**: the job has stopped running
  (dev: check the Cloud Scheduler job's last execution / Cloud Run Job
  execution history; stg: nobody ran the manual command). Run it manually
  immediately — `ensure_future_partitions` is idempotent and safe to run as
  often as needed.
- **`partitions_dropped` stuck at 0 well past the retention window**: the job
  has stopped running, or `-retention-days` was raised. Check the same
  execution history. This is lower urgency than a missing-partition ingest
  outage (it's a slow storage-cost bleed, not a correctness break), but left
  unaddressed it is the ~65 GB/day growth this design explicitly exists to
  bound.
- **`outcome=failed`**: read the Cloud Run Job execution logs for the error —
  both SQL functions raise loudly (`RAISE EXCEPTION`) rather than silently
  no-op on a bad input (e.g. `retention_days < 1`, or the table somehow no
  longer being partitioned), so a failed run means something genuinely wrong,
  not a transient no-op.
- **A query against `herd_signal_packets` unexpectedly scans every
  partition** instead of pruning to one: check whether the query filters on
  `received_date` (the actual partition key) rather than `received_at` — see
  `docs/modules/herd-signals-system-design.md` Section 3.1. Not this job's
  fault, but the most likely cause of "partitioning isn't helping" reports.

## Local / OCI verification

Against the OCI dev database
(`source /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env`,
tunnel via `/Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel`):

```bash
cd backend
# DATABASE_URL must already be exported (e.g. `set -a; source
# /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env; set +a` -- a plain,
# non-exported `source` will NOT put it in the child process env).
go run ./cmd/herd-signals-partition-maintenance -days-ahead=14 -retention-days=14
# or use the Makefile target, which passes the same flags:
make herd-signals-partition-maintenance
```

Running it twice in a row should show `0 created` the second time (the
runway is already topped up) and `0 dropped` unless a full day has passed
(nothing new fell out of the retention window in the meantime) —
`herd_signal_packets_ensure_future_partitions` is fully idempotent by
construction.
