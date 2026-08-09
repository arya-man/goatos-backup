# Operational Task-Kernel Non-Deviation

Status: accepted

Date: 2026-08-10

## Decision

Goat OS is one event-driven, interlinked task/ticketing waterfall:

```text
business event -> domain transaction + audit/outbox -> real owner + clock
-> bounded hierarchy -> acknowledgement-gated contact waterfall -> proof
-> separately owned verification/sign-off -> close/reopen rollup -> shared reads
```

Modules own domain facts and execution state machines. No module may create,
retain as canonical, preserve, or exempt a separate app-visible task authority,
owner fallback, clock/overdue rule, scheduler, contact/escalation ladder,
verification queue, or screen-only follow-up source.

The default adapter writes domain and task coordination through one
transaction-aware port. A recorded strict module boundary changes direction,
not participation: the module atomically writes domain state, audit,
idempotency, and a complete outbox event; a shared-kernel consumer outside the
module materializes task state in a receipt-backed, idempotent, version-fenced
transaction with lag visibility, bounded replay, and source reconciliation.

Weighing uses the outward-only form. It never reads generic task, SOP,
obligation, roster, herd, vaccination, or lifecycle state, and generic state
never gates scan, submit, verdict, reopen, or close. Its campaign/bucket,
assigned operator, authored date, capture, proof, verdict, close, and reopen
facts still materialize into shared owner/clock, hierarchy, Today, contact,
sign-off, and rollup state.

Every activated app-visible task has a real owner. Failed owner resolution
creates a separate, durably owned configuration/operations exception and keeps
the task shadowed. Operator and verifier/sign-off work are sibling leaves.
Acknowledgement stops contacts, not the work clock. Authorized descendant
reopen propagates upward.

## Precedence

This is the governing active decision for operational coordination. Where an
older module PRD, TRD, alerts decision, guard, or implementation describes a
module-private task/scheduler/clock/contact/verification/read authority, that
description is pre-cutover compatibility behavior, not target architecture.
Preserve it only long enough to shadow, reconcile, suppress duplicates, retain
rollback, and prove retirement.

The 5k-to-50k topology ADR still governs process count, worker consolidation,
and canonical indexed reads. Weighing free-flow/isolation still governs physical
execution. Neither permits a private coordination island.

## Enforcement status

This ADR and its execution documents are the policy foundation. They do not
pretend the current repository already has semantic machine enforcement. F0 in
`context/repo-audits/current-whole-project-remediation-ledger.md` must add the
module-integration registry, non-deviation guard with adversarial tests,
machine-readable proof/review receipts, honest guard-registration checks, and
the exact-head single-PR landing gate before any implementation batch can close.

## Required companion sources

- `context/architecture/operational-kernel.md`
- `context/execution/operational-task-kernel-remediation-plan.md`
- `context/execution/defect-prevention-execution-contract.md`
- `context/execution/operational-kernel-program-state.md`
- `context/repo-audits/current-whole-project-remediation-ledger.md`

Changing this decision requires explicit maintainer authority plus same-change
updates to those sources, the applicable module docs, structural guards,
adversarial tests, and ordinary affected CI.
