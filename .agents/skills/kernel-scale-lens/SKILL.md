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
