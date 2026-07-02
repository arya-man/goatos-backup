# Vaccination Kernel Closure Ledger

Status: implementation closure ledger.

Date: 2026-06-27

Purpose: track the 9 business/kernel items needed to make the CEO vaccination
message runtime-true. This file now records the current code-backed state after
the kernel closure work, plus the final verification gates that must pass before
the task is called done.

Frontend screen requirements remain in
`context/frontend/vaccination-kernel-closure-screen-requirements.md`. Any visible
UI must use backend-owned contracts for labels, titles, columns, chips, disabled
reasons, actions, drawers, filters, pagination, and empty/error states.

## CEO-Literal Runtime Promise

```text
Config -> Due List -> Shed Drive -> SOP Execution -> Proof -> Verification -> Completion -> Alerts
```

This now maps to code as:

| Step | Runtime owner |
| --- | --- |
| Config | Source-backed immutable protocol/SOP versions. Drafts can change; published versions cannot be silently mutated at app or DB level. |
| Due List | Vaccination generation with durable run rows and idempotent obligation keys. |
| Shed Drive | Obligation sweeper groups due obligations by shed/scope and records stock-block state when reservation fails. |
| SOP Execution | SOP tasks/submissions bridge into vaccination completions. |
| Proof | Proof/cold-chain/lot checks are enforced for batch completion materialization and direct accept stock gate. |
| Verification | Accept/reject/rework drives completion, rework, atomic stock consume/release, and booster scheduling. |
| Completion | Completed work keeps source protocol/rule/SOP version identity. |
| Alerts | Calendar reminders, notification dispatcher, escalation waterfall, ack/resolve, kernel health, and DLQ operations exist. |

## Current 9-Item Status

| ID | Item | Code status | UI status | Remaining caveat |
| --- | --- | --- | --- | --- |
| 1 | Auto generation after rule publish | Implemented: protocol publish calls vaccination generation through a durable retryable run row; CLI also records runs; published protocol rows and child rules/triggers are immutable after publish. | Minimal status still needs richer Config/Data Ops display of run history/retry. | Full open-work supersede/cancel/regenerate policy for old-version open work remains explicit-business-workflow territory, not silent mutation. |
| 2 | Always-running event delivery | Implemented: outbox relay, local/eventbus mode, Pub/Sub publisher, domain-event consumer, goat-created/move/exit/stage/manual handlers wired. | Product UI not required; kernel health/DLQ surfaces cover ops visibility. | Target envs still need deployed workers, Terraform apply, IAM/secrets, and runtime verification. |
| 3 | Full goat shift repair | Improved: move API emits `goat.location.changed`; handler re-scopes open unbatched work and detaches/re-scopes planned batched obligations with stock-reconcile marker. | Execution/Action Center can show repair context through backend state; richer repair drawer can be built from contracts. | In-progress/completed drive migration remains an explicit exception/rework policy, not automatic mutation. |
| 4 | Sick/ICU/quarantine defer plus recovery | Partially implemented: defer is visible when published rule includes `eligibility.defer_states`; stage/location recheck handler can regenerate on relevant events. | Defer/blocked states appear through process/read models where projected. | Publish gate should keep enforcing/standardizing Preventive Care (PC) defer policy; automatic health recovery events are still only as good as producer coverage. |
| 5 | Hard stock blocking | Implemented for core vaccination path: FEFO ignores expired lots, reservation fails hard on missing/insufficient/expired stock, batch records `stock_block`, completion requires lot+cold-chain for batch-backed accepts, and inventory movement plus balance update is atomic. | Execution can render stock-block state from backend context; Inventory owner action screens may be expanded. | Authorized override workflow is not implemented; direct non-batch completion still supports legacy/manual paths. |
| 6 | DLQ operation center | Implemented: `/operations/dlq` list, replay, discard, idempotent repair ledger, audit, generated client, and admin-web DLQ route/screen. | Implemented as Operations DLQ screen with filters/detail/actions using backend contracts. | Pub/Sub DLQ import/redrive into the same UI is still environment/ops work. |
| 7 | Automatic DLQ / worker alerts | Improved: `/operations/kernel-health` reports pending/publishing/failed/dead-letter/oldest/last-published state; Control/Ops can consume it. | Minimal kernel health contract exists; richer Control Tower strip can render it. | External Cloud Monitoring alert policies must still be applied per environment. |
| 8 | Incident escalation adapter | Implemented as replaceable incident webhook adapter for `incident`/`opsgenie`/`pagerduty` channels, with dispatcher env config. | Incident reference/status can be added to escalation detail from notification/escalation state. | Vendor-specific bidirectional acknowledgement/resolution sync is not implemented; Goat OS remains source of truth. |
| 9 | Stage-change and manual-campaign triggers | Implemented: `POST /admin/goats/{goat_id}/stage` validates active tenant stage lookup and emits canonical `goat.stage_changed`; recheck handler is wired; `POST /vaccination/manual-campaigns` runs `manual_campaign` rows with durable run idempotency by campaign plus `as_of`. | Frontend server helpers exist; full campaign creation/approval screen remains a product surface. | Campaign authority/approval UX can be expanded, but the backend trigger now exists. |

## Screen Requirement Summary

| Item | Required screen level |
| --- | --- |
| 1 | Config/Data Ops generation run status/history/retry. |
| 2 | No product screen; ops health via item 7. |
| 3 | Batch repair/exception state in Vaccination Execution, Action Center, Workflow detail. |
| 4 | Defer/recovery state in Preventive Care (PC) / Vaccination, Goat detail, Action Center. |
| 5 | Stock-blocked execution state and inventory-owner action link. |
| 6 | Operations DLQ Center, implemented. |
| 7 | Kernel health strip/panel; external monitoring remains infra. |
| 8 | Incident reference/status on escalation detail. |
| 9 | Stage history plus manual campaign create/approval/run status screen. |

## Final Done Gate For This Goal

This goal can be called complete only after:

1. Backend targeted tests pass for identity, vaccination, obligation, inventory,
   outbox, notification, protocol, bootstrap, and permissions.
2. Full backend test or an explicitly documented equivalent pass is run.
3. Admin-web typecheck, eslint, UI contract literal guard, IA guard, and
   mock-fidelity guard pass.
4. Contract drift/OpenAPI client checks pass.
5. The extra-high review agent checks architecture, replay/idempotency, RBAC,
   scale, frontend contract ownership, docs, and push readiness.
6. Review blockers are fixed.
7. The final operational-kernel diagram/docs match code.
8. Changes are committed and pushed to `main` using the Mesha/VGoats remote.

## Business Caveats To Communicate Honestly

- Published versions are immutable; changing config or SOP creates a new version.
- Old completed work keeps the old version. Open old-version work needs an
  explicit continue/cancel/supersede/regenerate decision; the kernel must not
  silently rewrite history.
- At-least-once delivery is expected. Idempotency, replay, DLQ, and audit are
  the safety model.
- Dev Terraform/code is not the same as verified prod/stg runtime. Target
  Google projects still need apply, secrets, IAM, and runtime checks.
- Vendor adapters are replaceable transports. Postgres/audit/outbox/read models
  remain the source of truth.
