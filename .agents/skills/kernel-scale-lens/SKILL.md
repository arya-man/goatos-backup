---
name: kernel-scale-lens
description: >-
  Use when designing OR reviewing any trigger, obligation, reminder, deadline,
  escalation, sweeper, projection/read-model, notification, Calendar, Action
  Center, Protocol Adherence, or process-integrity path. Applies the operational
  kernel golden lens + the 1M kernel-scale acceptance bar. Invoke before
  designing such a feature and when reviewing one.
---

# Operational kernel + 1M kernel-scale lens

Canonical: `context/architecture/operational-kernel.md`. Pairs with the
`scale-anti-patterns` skill (technical scale) — this is the kernel/business scale.

## The golden chain — every feature must complete it
```
event → txn → audit/outbox → trigger → obligation → sweeper →
notify/escalate → proof → read-model → answer
```
Every feature must answer: what process was **expected**, was it **followed**,
where did it **break**, who **owns next action**, what is **due by when**, what
**evidence** proves it, what **alert/escalation** fires when a deadline is crossed.

## Kernel-scale bar (1M animals, release invariant)
- The whole chain must be **bounded, resumable, observable**.
- **No request path may replay canonical kernel tables** when a projection/read
  model is required — request does an indexed lookup on the read model.
- Workers use **leases, cursors, idempotency keys, retry-safe failure records** —
  keyset-chunked `FOR UPDATE SKIP LOCKED` claim (copy the obligation/idempotency
  sweeper). No silent ACK/drop of kernel work at scale.
- State transitions (missed/overdue/deferred/done) stay correct **under bulk
  sweeps**; cohort generation is tenant/park/shed scoped, resumable, idempotent.
- Sick/quarantine/death/recovery rules work **in batch** (see the mandatory
  clinical-defer set — `make clinical-defer-guard`).

## Atomic transition + derived read-model (hard contract)
A state transition and the sync of any read model it OWNS are **ONE transaction**.
Do the derived upsert + parity check INSIDE the same tx as the state change; a
sync failure rolls the whole transition back (no status flip, no outbox, no audit,
no partial read model). Post-commit "best-effort" sync only for an already-
committed replay. Canonical: `PublishVersionWithCapacity` + its rollback
regression test.

## Serving-read freshness + date-window contract (projection reads)
For any read served from a projection behind a freshness/coverage gate
(Vaccination execution/operations/shed, CT/AC/PA, Calendar). Full rule +
code anchors: `docs/decisions/high-scale-dashboard-projections.md` →
"Serving-Read Freshness Contract".
- **TTL > refresh schedule.** Serving TTL is an AGE bound; if it equals the
  refresh cadence, jitter + build time opens a 503 gap. Keep TTL above schedule
  (5-min schedule → 7-min TTL). It is ORTHOGONAL to date coverage — **never widen
  the TTL to mask a coverage bug** (fresh by age ≠ compatible by date).
- **Date window = inclusive-query vs exclusive-bound.** A read that expands an
  inclusive query `date_to` by +1 day must be covered by a projected window whose
  stored `date_to` is that exclusive bound; provision the projector 1 day beyond
  the max query range (45d ⇒ 46d). FIXED-date tests seed the window around their
  fixed dates, not `now±N`. Calendar/day-based projectors must default to
  business-day midnight windows that cover the supported UI windows (Monday-start
  weeks and previous/current/next first-to-last-day month picker requests); `now-24h` leaves a
  midnight gap and usually misses valid week/month reads.
- **LKG.** A rebuild on an already-serving tenant keeps serving the prior version;
  a failed rebuild never clobbers LKG. Only first-ever/no-serving-version or an
  uncovered requested date/window fails closed. Stale/yellow/rebuilding/failed/
  over-TTL projections that still have serving rows covering the request must
  return LKG rows with freshness metadata, not a page-down 503.
- **Canonical-history bypass.** A read served ENTIRELY from a bounded canonical
  index (completed/accepted history) must NOT be gated on hot-projection freshness;
  gate on the exact query shape (`status=completed` only), not the endpoint.
- **Prune re-derives the serving version INSIDE the DELETE** (`WITH serving AS
  (SELECT serving_projection_version ...) ... WHERE projection_version <>
  serving...`). Never trust a version captured before the txn/advisory-lock
  released — an overlapping newer build's rows get deleted, leaving a green pointer
  to zero rows (silent wrong-empty, not 503).

## Idempotency (every write path)
Accept/derive a stable key; persist key + semantic fingerprint in the same txn as
the side effects; exact replay returns the original result with no new side
effects; same-key different-payload replay is rejected. Test: first call, exact
replay, same-key different-payload, downstream duplicate prevention.
`ON CONFLICT DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key` alone is
NOT sufficient when later code still mutates state.

## E2E integrity (proof, not seeded readback)
An E2E must produce obligations/completions/verifications/projections via the SAME
service/API/consumer/sweeper/projector used in production — never seed the derived
state. Enforced by `make e2e-integrity-guard`.
