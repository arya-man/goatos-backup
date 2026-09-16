# Daily analytics refresh

Staging retains the disposable topology introduced by `e02726189`: no Cloud
Scheduler jobs. Recent daily workflows (feed proof times, market reminders)
use the kernel worker's shared operational cadence and business-date state.
Analytics follows that same cadence; the existing Cloud Run Job remains the
bounded compute boundary and manual repair entrypoint.

After 03:15 Asia/Kolkata, the five-minute operational tick atomically claims a
persisted lease for yesterday. The job request pins that source date,
`app_events`, a three-day replacement lookback, and a 25-minute work timeout.
The Cloud Run execution timeout remains 30 minutes. Optional export adapters
finish before the invocation records its final `analytics.rollup_run` status.
A dispatch response is never completion proof.

A 40-minute lease survives worker restarts and prevents duplicate replicas from
dispatching the same date. Retries first list active executions; a queued or
running execution prevents another dispatch even after lease expiry. This also
covers a lost POST response. Listing is bounded by the stage's 20-second deadline
and 100 pages; errors fail closed and are retried after the lease. Completed
failures retry; a final successful audit since the day's first claim suppresses
further dispatch. Replacements remain idempotent because Cloud Run's run API has
no exactly-once request key. Historical audit success before the first claim does
not suppress the new refresh.

The worker has job-scoped `roles/run.jobsExecutorWithOverrides` and
`roles/run.viewer`, not project-wide execution authority. The runtime job path
is configured in Terraform and the normal Cloud Deploy worker update. No inline
analytics SQL or long-running computation occupies the shared operational lane.

Grafana shows the last successful job age (red above 30 hours) and latest status
and source date. This is global job health, not a tenant attribution or a claim
that an arbitrary old-date manual backfill refreshed today's coverage.
