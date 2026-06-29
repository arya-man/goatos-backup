# Vaccination Workflow Follow-Ups

Status: internal follow-up ledger for workflow-sized work. This is not the CEO
walkthrough. Keep detailed technical backlog here instead of
`ceo-vaccination-kernel-dev-guide.md`.

Scope: items that are not simple bug fixes. They need a full owner workflow,
policy decision, operator screen, load rehearsal, or Google runtime proof before
they should be presented as finished business behavior.

Out of scope for this cleanup: GitHub Actions/CI billing or workflow wiring, and
Google dev rollout execution. Those remain separate gates.

## Current Classification

| Area | Current safe state | Still needs full workflow/proof |
| --- | --- | --- |
| Critical ICU/quarantine guardrails | Direct unsafe ICU/quarantine changes fail closed before mutation. The approved guarded death path exists. | Build the full critical-action workflow: evidence checklist, computed classification, reviewer steps, separation-of-duties data, approval/release criteria, exception handling, and operator/reviewer screens. |
| Shed owner and separation checks | Missing or unreliable owner data cannot silently approve a critical action. | Populate and verify shed-owner/manager data, then test reviewer separation rules against the real roster. |
| Missed-dose response ownership | Missed work is recorded and visible in the vaccination views; the durable `obligation.missed` event is consumed and targets that obligation through the existing Calendar escalation path before the consumer acknowledges it. | Define the business owner, follow-up wording, escalation timing, closure rule, and how repeated missed work is handled. |
| Exhausted notification follow-up | Notification retry, terminal `exhausted` state, dispatch counter, audit row, `notification.exhausted` outbox event, incident-to-Slack fallback, and dev Terraform webhook secret wiring exist. | Add the full exhausted-alert operating workflow: operator queue, exported metric, owner assignment, reroute policy, and closure evidence. |
| Background event recovery ownership | Stale background `processing` rows can be reclaimed, and handlers use idempotent side-effect keys where implemented. | Decide the owner contract for cases where a handler partly succeeds and then fails before final acknowledgement; either make side effects share a transaction or require deterministic idempotency for every handler. |
| DLQ and replay operations | Local dead-letter/outbox repair surfaces and command paths exist. | Prove the Google Pub/Sub/native dead-letter path: inspect failed messages, reject same-key different-payload replay, replay safe messages, discard poison messages, and record operator reason. |
| Stock issue owner workflow | Vaccination work shows stock blockers and backend repair/reconcile paths exist. | Build the inventory-owner workflow for missing/expired/insufficient/reserved stock: action buttons, accountable owner, proof of fix, retry/release behavior, and escalation. |
| Protocol authoring workflow | Draft, publish, immutability, source-backed gates, and backend validation exist. | Make the admin authoring flow a single guided business workflow for save draft, edit rules, preview impact, publish, and recover/compensate safely after partial failure. |
| Production PHC roster and rule load | The local slice proves the engine with source-derived dev baseline data. | Load and approve the real PHC schedule/roster/rule set before production use. |
| Real notification channels | Dev Terraform defines Slack/incident webhook secret containers and wires them to the notification dispatcher without committing secret values. Dev-safe/local channels can still prove routing without contacting production people. | Populate secret versions, configure production-safe recipients/channels/escalation policy, and prove delivery ownership before live use. |
| Scale rehearsal | Query-plan guards and local functional checks exist. | Run large-herd and replay-storm rehearsals for generation, sweepers, relay backlog, idempotency, and read-model query behavior. |

## Backlog Mapping

The pasted review backlog maps as follows. The `R*`, `H*`, `E*`, `C*`, `M*`,
`D-*`, and `L*` codes are from the pasted senior-review backlog, while `OCK-*`
and `RVF-*` are from the Goal 1 closeout ledger.

- Already fixed in code or committed runtime config: R1, R2, H2, H7 for the
  active process-integrity Action Center path, R3, R4, R5, R7, E8, E6, E3,
  C3's durable evidence path, and D-drift publish-gate helper reuse.
- Already closed in the current OCK/RVF closeout ledger: herd-import OCK-033,
  OCK-035, OCK-036, OCK-037, OCK-038, and OCK-040.
- Deferred workflow items in this document: the remaining C3 operator workflow,
  M5 transaction-boundary/owner contract, E2, E4, D-saga, the dedicated L2
  orphan-schema lint, and the broader critical-action, stock-owner,
  production-rule, notification, and Google-runtime proof items.
- Google dev rollout execution remains a separate gate; this document only
  tracks the committed code/config posture and the work that must be proven
  there.

## Rule For Future Docs

- CEO-facing docs should explain what a reviewer sees, what to test, what a
  disabled button means, and what is not finished.
- Internal docs may use implementation language such as DLQ, outbox, Pub/Sub,
  idempotency, transaction boundary, and load harness.
- Do not mix the two in the CEO guide.
