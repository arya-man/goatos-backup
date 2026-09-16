# Budget alert noise fix — landing progress

Scope: budget Pub/Sub notification suppression only. Preserve immediate new
threshold and non-budget alerts, INR 45,000 budget, and current main Google OIDC
support. Use dedicated GCS state with generation fencing across instances.

Base: 83213b43550e57ba6fbc2da04dd0dc29d80dac04 (fresh main, 2026-09-16).
Candidate SHA: recorded in the local landing receipt after commit.

Done:
- Repeated above-threshold updates are suppressed until a 24-hour reminder is due.
- Pub/Sub publication time, not historical threshold maxima, determines freshness.
  Fresh lower forecasts still remind; old/equal publications do not replay alerts.
- Suppressed and below-threshold publications advance the freshness watermark.
- Shared-token/OIDC authentication fails closed. Signed Google OIDC supports the
  existing main audience/service-account configuration and validates verified email.
- Dedicated private bucket/IAM and required Cloud Run state configuration included.
- Independent counter-review passed after resolving both authentication drift and
  declining-forecast reminder findings. No product API/DB/media path changes.

Proof before migration into the clean landing worktree:
- Original HTTP regression: three identical updates emitted 3 messages; fixed emits
  1 (67% fewer messages in this deterministic replay, not measured live savings).
- Both review defects reproduced with failing tests before correction.
- `go test -race ./cmd/cost-alert-bridge -count=1`: PASS (6.696s).
- `go vet ./cmd/cost-alert-bridge` with a task-owned GOCACHE: PASS.
- Signed-JWT real-validator tests: valid OIDC without query token; wrong signature,
  audience, issuer, expiry, email/verification and missing configuration rejected.
- Tests cover 24h reminder boundary, fresh lower forecast after 25h, stale replay,
  clear/suppressed updates, concurrency, restart, lease expiry, Slack/store failures,
  budget/account/month identity and GCS generation preconditions.
- HCL parsing and deployed-job-flags guard/self-test: PASS.

Known environment failures: vet initially exhausted disk, then an external
shared-cache cleanup removed imports. Isolated cache resolved both. No current
source-test failure. Slack/GCS commit is not atomic: an ambiguous accepted Slack
send followed by lost response/state commit can duplicate after lease recovery.

Pending: focused proof on fresh main; exact-SHA `make land-main` receipt and push.
Deployment state: no deployment requested or performed. Terraform provisioning and
live revision/config/readback remain separate from this source landing.

During/after landing, mutable SHA, CI result and push state are recorded in the
worktree git directory's `cost-alert-landing-progress.md` and standard CI receipts,
so recording the push does not itself invalidate the certified source SHA.

Final preflight on fresh main: focused race suite PASS (3.166s). First landing
attempt identified scale-guard's row-fanout heuristic on single-budget CAS retries.
Four narrowly scoped annotations now document the five-attempt retry bound and
one completion write; TestBudgetContentionHasBoundedRetries proves exhaustion
with exactly five loads/saves and no delivery. Scale guard PASS, focused race
suite PASS (6.530s), and independent final-delta counter-review PASS. The complete
landing gate must run again for this final revision; no receipt reuse from the
failed initial attempt.
