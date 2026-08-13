---
name: kernel-scale-lens
description: >-
  Use when designing OR reviewing any trigger, obligation, reminder, deadline,
  escalation, sweeper, projection/read-model, notification, Calendar, Action
  Center, Protocol Adherence, or process-integrity path. Applies the operational
  kernel golden chain + the current 5k-50k scale-acceptance envelope (1-5M kept as
  the future certification gate). Thin entrypoint: detailed rules live in the
  canonical chapters linked below. Invoke before designing such a feature and when
  reviewing one.
---

# Operational kernel + scale lens — entrypoint

Every kernel feature must complete the golden chain and stay bounded, resumable,
and observable.

```
event → txn → audit/outbox → trigger → obligation → sweeper →
notify/escalate → proof → read-model → answer
```

This skill is a **table of contents**, not the rulebook. Open the canonical
chapters below; do not review from the summary.

## When this lens applies
- Designing/reviewing a trigger, obligation, reminder, deadline, escalation,
  sweeper/scheduler, projection/read-model, or notification.
- Any Calendar / Action Center / Protocol Adherence / process-integrity screen
  or the query that feeds it.
- Any bulk sweep, cohort generation, or state transition (missed/overdue/deferred/done).

## Canonical detail (read these — do NOT duplicate here)
- **Golden rule + system design:** [`context/architecture/operational-kernel.md`](../../../context/architecture/operational-kernel.md) · [`context/architecture/operational-kernel-system-design.md`](../../../context/architecture/operational-kernel-system-design.md).
- **Review chapter:** [`.agents/skills/goatos-code-review/references/kernel-and-scale.md`](../goatos-code-review/references/kernel-and-scale.md).
- **Release envelope (authority):** [`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`](../../../docs/decisions/operational-kernel-5k-50k-scale-envelope.md).
- **Serving-read freshness + date-window contract:** [`docs/decisions/high-scale-dashboard-projections.md`](../../../docs/decisions/high-scale-dashboard-projections.md).
- **Future 1M certification:** [`docs/decisions/one-million-postgres-readiness.md`](../../../docs/decisions/one-million-postgres-readiness.md).

## Machine gates
- `make e2e-integrity-guard` — E2E produces state via the real service/sweeper/
  projector, never seeded readback.
- `make sweeper-deployment-guard` · `make deployed-job-flags-guard` ·
  `make worker-stage-budgets-guard` · `make kernel-worker-retirement-gate-guard` —
  kernel worker deployment + budget + retirement safety.
- `make clinical-defer-guard` — mandatory clinical defer set. All registered in
  `tools/ci/guardrail-manifest.json`.

## HOW-TO: write meaningful notification copy (maintainer decision, 2026-08-02)

The `notify/escalate` step of the golden chain is not satisfied by a durable,
fail-closed row alone — the copy in that row must be MEANINGFUL, never
abstract. Applies to every notification type (vaccination, weighing, feed,
counts), not just vaccination.

A compliant Title/Body names:
- **Park** and **shed/partition** (when partition exists, never parent shed alone
  — see [`docs/decisions/operational-location-display-contract.md`](../../../docs/decisions/operational-location-display-contract.md)).
- **Vaccine/work-item name in human form** (`ET+TT`, `PPR · Booster` — never a
  raw config token like `et_tt_adult_w2`; use the vaccine display mapper, see
  `docs/decisions/scale-anti-patterns.md` UI-copy section).
- **Count** (animals/sheds).
- **A farm-readable due date in IST.**

A leadership escalation additionally names **which sheds are outstanding**,
not just a count — "Shed 2, Shed 5 still outstanding", not "2 sheds pending".

Defective (do not ship): `Title: "Vaccination(s) due soon"` /
`Body: "· 3"`, or `fmt.Sprintf("%d sheds is now live", len(buckets))`.

Compliant: `Title: parkName + " · " + shedLabel + " · " + vaccineLabel + " due"`
/ `Body: fmt.Sprintf("%d goats in %s (%s) need %s by %s IST", count, parkName, shedLabel, vaccineLabel, businessDate)`.

Full rule + before/after examples:
[`docs/decisions/2026-08-02-meaningful-notification-copy.md`](../../../docs/decisions/2026-08-02-meaningful-notification-copy.md).

Machine gate: `make notification-specificity-guard`
(`tools/agent-hooks/check-notification-specificity.mjs`), diff-scoped over
`backend/internal/notificationbridge/**`. Composes with, does not duplicate,
`make ui-vaccine-labels-guard` (raw-token humanization is that guard's job;
this one checks for missing park/shed/date specificity).

## At a glance (detail in the links above)
- **Golden-chain completeness:** every feature answers expected / followed / where
  broke / owns-next / due-by-when / evidence / escalation.
- **Bounded + resumable + forward-progress workers:** leases/cursors/idempotency
  keys, `FOR UPDATE SKIP LOCKED`, resume from saved cursor — never restart at zero.
- **Durable, fail-closed recorders:** notifications/reminders/escalations/review
  queues are persisted rows; a write failure fails closed.
- **Atomic transition + owned read-model:** one txn, rolls back on sync failure.
- **Serving-read freshness:** TTL > refresh schedule; date coverage = inclusive
  query vs exclusive bound (+1 day); LKG serves, uncovered fails closed; prune
  re-derives the serving version inside the DELETE.
- **Read-model default (envelope):** canonical indexed SQL (list keyset ~20,
  summary indexed aggregate); the five named screen projections are retired and
  scope-exempt from `scale-guard`, plan-tested — nothing else is.
