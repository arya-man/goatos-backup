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
