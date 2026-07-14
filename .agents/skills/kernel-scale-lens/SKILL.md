---
name: kernel-scale-lens
description: >-
  Use when designing OR reviewing any trigger, obligation, reminder, deadline,
  escalation, sweeper, projection/read-model, notification, Calendar, Action
  Center, Protocol Adherence, or process-integrity path. Applies the operational
  kernel golden lens + the current 5k-50k scale-acceptance envelope (with the
  1M/1-5M bar kept as the future certification gate). Invoke before designing
  such a feature and when reviewing one.
---

# Operational kernel + 5k-50k scale lens (1M is the future gate)

Canonical: `context/architecture/operational-kernel.md`. Pairs with the
`scale-anti-patterns` skill (technical scale) — this is the kernel/business scale.

**Current release scale target (authority):**
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md` sets the accepted
deployment envelope at **5,000-50,000 animals** on one modular kernel worker +
Postgres. That ADR is the authority for operational-kernel deployment scale; it
narrows the 1M/1-5M topology to future certification work, not a present release
requirement. The kernel rigor below is UNCHANGED — this is a scope reframe of the
acceptance bound, not a safety downgrade.

## The golden chain — every feature must complete it
```
event → txn → audit/outbox → trigger → obligation → sweeper →
notify/escalate → proof → read-model → answer
```
Every feature must answer: what process was **expected**, was it **followed**,
where did it **break**, who **owns next action**, what is **due by when**, what
**evidence** proves it, what **alert/escalation** fires when a deadline is crossed.

## Kernel-scale bar — current gate is the 5k-50k envelope
The kernel shape below is a **release invariant at every scale**; only the
acceptance BOUND is reframed. The CURRENT gate is the 5k-50k envelope; the
1M/1-5M bar is kept explicitly as the FUTURE certification gate.
- The whole chain must be **bounded, resumable, observable**.
- **No request path may replay canonical kernel tables** when a projection/read
  model is required — request does an indexed lookup. Under the 5k-50k envelope,
  the five named screen reads (Calendar, process-integrity, vaccination shed/
  execution/operations) instead serve canonical indexed SQL directly (list =
  keyset ~20; summary = indexed aggregate) and carry the scoped scale-guard
  exemption — see "Read-model default" below. compute-on-read stays BANNED
  everywhere else.
- Workers use **leases, cursors, idempotency keys, retry-safe failure records** —
  keyset-chunked `FOR UPDATE SKIP LOCKED` claim (copy the obligation/idempotency
  sweeper). No silent ACK/drop of kernel work. The 5k-50k runtime is a **single
  kernel worker** with advisory-lock-serialized stages (min-instances >= 2 for
  HA); this does not relax lease/cursor/idempotency discipline.
- State transitions (missed/overdue/deferred/done) stay correct **under bulk
  sweeps**; cohort generation is tenant/park/shed scoped, resumable, idempotent.
- Sick/quarantine/death/recovery rules work **in batch** (see the mandatory
  clinical-defer set — `make clinical-defer-guard`).

### Current acceptance proof (5k-50k)
- Query-plan proof runs at the **upper bound ~500k obligation rows** (50k animals
  x retained obligations), not just the 5k list case, on the existing tenant/
  status/due/scope indexes. Prove BOTH read shapes: keyset **list** reads (~20 rows,
  genuinely bounded) and **summary aggregates** (Control Tower gaps, adherence,
  process-integrity counts — indexed scans whose cost grows with open-obligation
  count). A green plan at 5k is NOT proof for the 50k aggregate.
- Single-worker cadence/backlog validation: no stage's p95 exceeds its cadence
  interval and no eligible backlog ages past its product freshness window at the
  envelope's workload.

### Future certification gate (1M / 1-5M) — kept, not deleted
The 1M/1-5M kernel-scale acceptance bar remains the certification target for the
future scale-out ladder (screen-specific projection tables, per-stage worker
extraction, partitioning — one measured boundary at a time). Do NOT design new
services, queues, schedules, partitions, or projectors for it until measured
workload requires them, but every design must still be shaped so it can reach
that bar without a rewrite. The 1M-scale documents remain authoritative research
and regression material.

## Read-model default under the 5k-50k envelope
Authority: `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. In the
active runtime, screens read **canonical indexed SQL by default** — list reads are
keyset-paginated at ~20 rows; summary reads are indexed aggregates. **Zero of the
five named screen projections run in the active runtime**:
`calendar_event_projections`, `process_integrity_projection_rows`,
`vaccination_shed_projection_rows`, `vaccination_execution_projection_rows`,
`vaccination_operations_projection_rows`. They are preserved by the pre-cutover Git
tag + the ADR inventory, not kept alive as unused runtime infrastructure. Serving a
canonical read cannot be stale relative to the canonical write, so this also deletes
the projection-drift bug class for these five screens.

Two summaries **survive** as read models and keep the compute-on-write contract:
`vaccination_eligibility_rollups` and the counts summaries. The obligation
sweeper's operational stage also remains the backstop for time-derived state
(due/missed) — a genuine clock effect, not a projection.

### scale-guard: the five canonical reads are scoped-exempt, not disabled
The five named canonical screen reads are the compute-on-read / god-CTE shape that
`make scale-guard` blocks mechanically. Under this envelope they are **exempted per
read** with the sanctioned scoped annotation, NOT by disabling the guard:

```
// scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md
```

- The guard stays **fully active for every other path** in `backend/internal/**`;
  compute-on-read stays banned everywhere except these five reads.
- Each exempted read MUST be query-plan-tested per the current acceptance proof
  (both list and aggregate shapes, aggregate at ~500k rows). An exempted read that
  is not plan-tested is a defect.
- When a screen later earns its own projection on the scale-out ladder, the
  annotation is removed and that read returns under guard enforcement.

The other scale anti-patterns stay banned in full (compute-on-read/god-CTE, capped
read-time rollup, full MV refresh, projection-rebuild failures, N+1 query, N+1
fan-out, OFFSET pagination, non-SARGable predicate, polling full scan,
non-terminating loop). This is a scoped exemption for measured, plan-tested envelope
reads — never a blanket safety downgrade.

## Atomic transition + derived read-model (hard contract)
A state transition and the sync of any read model it OWNS are **ONE transaction**.
Do the derived upsert + parity check INSIDE the same tx as the state change; a
sync failure rolls the whole transition back (no status flip, no outbox, no audit,
no partial read model). Post-commit "best-effort" sync only for an already-
committed replay. Canonical: `PublishVersionWithCapacity` + its rollback
regression test.

## Serving-read freshness + date-window contract (projection reads)
Applies to any read STILL served from a projection behind a freshness/coverage gate
— the surviving `vaccination_eligibility_rollups` / counts summaries, and any
projection reintroduced on the scale-out ladder. Under the 5k-50k envelope the five
named screen reads serve canonical indexed SQL and are NOT gated on projection
freshness (see "Read-model default"); this contract governs them again the moment a
screen earns back its own projection. The rule below is UNCHANGED. Full rule +
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
