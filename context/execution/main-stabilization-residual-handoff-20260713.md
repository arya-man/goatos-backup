# MAIN STABILIZATION — RESIDUAL QUALITY HANDOFF (2026-07-13)

**Status: uncommitted handoff note. Not pushed.** For the lane currently
stabilizing `main` (the one that landed `4baf5b64 → af069f9e → d8d30294 →
72af14a2` on 2026-07-13). `main` is GREEN — `make guardrails` and
`make validate-migrations` both exit 0 — so none of the below is a red-main
blocker. They are three residual quality items found while executing the
stabilization review; each collides with files your lane is actively editing, so
they are handed to you rather than push-raced.

Base for the findings: `72af14a2` (latest `origin/main` at write time).

---

## A. Freeze doc is stale — says ACTIVE while main is green and lanes are pushing

`context/execution/main-stabilization-status-20260713.md` still opens with
**"PUSH FREEZE ACTIVE … main's `make guardrails` / `validate-migrations` is RED"**
and its BLOCKED section still describes the `000179` hot-index break as unfixed.
Reality: that break was fixed by your `4baf5b64` (the `000171→000179` allowlist
realign), the parity + india-date guards were addressed by `af069f9e`, and main
is green. Four stabilization pushes have landed *since* the freeze note — the
freeze is de-facto over but the doc still tells everyone to hold.

**Action:** flip the doc to LIFTED (or archive it) and record the three closures
(`000179` exemption, parity guard, india-date baseline) so held branches
(`fix/verifier-app-contract-wire`, `fix/verification-web-wire-and-qa`) can be
re-verified and landed.

---

## B. The genuine india-date bug was grandfathered, not fixed (P2, real)

The stabilization review flagged the india-date guard's 22 offenders and asked
for triage: fix the genuine business-day bugs, grandfather only the
absolute-instant false positives.

- **21 of 22 are true false positives** — `now_utc_inline` on ABSOLUTE instants
  (escalation `acknowledged_at`/`resolved_at` writes in
  `calendar/adapters/postgres/repository.go:652,663,684`; the obligation-reschedule
  `occurredAt` transaction instant at `vaccinationexecution/adapters/http/handler.go`;
  test-setup "now"s that `stableSameLocalDayDueAt` re-localizes to Asia/Kolkata).
  UTC is valid for a physical instant. Grandfathering these is correct.
- **1 of 22 is a REAL bug** and was grandfathered instead of fixed:
  `backend/internal/calendar/adapters/postgres/repository_integration_test.go:99`
  ```go
  base := time.Now().UTC().Truncate(24 * time.Hour)   // UTC midnight, wrong day boundary in IST
  ```
  This `[utc_truncate_day]` is exactly the pattern the guard exists to catch — a
  day boundary defined by UTC, not the India business calendar. It seeds the
  calendar projection window and the `DateFrom`/`DateTo` query, so near IST
  midnight the test's "day" is off by one. `af069f9e` added
  `…repository_integration_test.go:99` to `india-business-date-baseline.txt`
  (silencing it) rather than fixing it.

**Recommended fix (verified green locally):**
```go
// add import: "github.com/vgoats/goatos/backend/internal/platform/biztime"
base := biztime.BusinessDayStart(time.Now())
```
then remove the `repository_integration_test.go:99` line from
`india-business-date-baseline.txt`. `go vet` + test-compile clean;
`node tools/agent-hooks/check-india-business-date.mjs` stays green (0 new).
Note: adding the import shifts line numbers in that file, so regenerate the
file's baseline entries against the post-fix tree (the current baseline line
numbers were already drifted before `af069f9e`). `goat_lifecycle.go:546,549`
remain the real `utc_format_date` burndown debt — separate follow-up.

---

## C. Emulator-parity coverage gap — smoke retired, only in-process logic remains (decision needed)

`208f6b39` deleted `tools/dev/local-gcp-kernel-parity-smoke.sh` (correctly — it
did `update outbox_messages set status='pending'`, a derived-table mutation that
trips `check-e2e-kernel-integrity.sh`). `af069f9e` then reduced
`tools/agent-hooks/check-local-gcp-kernel-parity.sh` to a compose-config check
only, and the runbook now claims the behavior "is covered by the consumer path +
`make high-scale-kernel-e2e-*` gates."

**That claim is only partly accurate.** `high-scale-kernel-e2e-all.sh` runs
**in-process Go tests** (`cmd/domain-event-consumer`, `outbox-relay`,
`outbox-dlq`, idempotency tests) — the dedup *logic* and DLQ+replay *logic* are
covered in-process, and `domain_event_duplicate_skipped` is asserted only by the
in-process unit test `backend/internal/domainconsumer/app/service_test.go`. Real
Pub/Sub topic/sub/IAM/**DLQ** proof is a *separate* staged-GCP item gated behind
`GOATOS_KERNEL_E2E_PUBSUB_PROOF`.

**Gap:** nothing automated now exercises the wired **Pub/Sub-emulator** path
end-to-end (relay → official emulator → consumer redelivery → durable-event-id
dedup → obligation-count-unchanged → DLQ subscription). Only in-process logic +
the manual `dev-local-kernel-up` stack remain.

**Decision for your lane (you own the retirement decision):**
- If in-process + staged-GCP proof is deemed sufficient parity coverage, **make
  the runbook claim precise** (say the emulator path is proven manually via
  `dev-local-kernel-up`, not by an automated gate) so the doc isn't
  over-claiming.
- If you want the automated emulator assertion back, restore a **kernel-safe**
  smoke: drive all derived state through the production path (goat insert is an
  allowed input fact; `backfill-goat-created` command; reads only) and replace
  the banned `update outbox_messages` redelivery with a **messaging-layer
  republish** — add a source-topic inspect subscription in
  `tools/dev/pubsub-emulator-bootstrap.sh`
  (`goatos-local-outbox-events-inspect`), `:pull` the goat.created copy from it,
  and `:publish` those exact bytes back to `goatos-local-outbox-events`. That
  redelivery mechanism passes `check-e2e-kernel-integrity.sh` (no derived-table
  mutation) and preserves the durable-event-id dedup assertion, unchanged
  obligation count, projections, worker-success, and DLQ-subscription checks the
  original smoke had.

---

## Verification already done (on the pre-`af069f9e` trees, both gates green)
- `000179` hot-index exemption realign (your `4baf5b64`) — `make validate-migrations`
  applies `000179`/`000180` clean; "Migration validation passed".
- Truncate → `biztime.BusinessDayStart` + baseline regen — `make guardrails` exit 0
  (india-date ok, scale RATCHET PASS, parity PASS, all sub-guards green).
