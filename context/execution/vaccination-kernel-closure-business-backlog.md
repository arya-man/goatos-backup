# Vaccination Kernel Closure Business Backlog

Status: execution backlog.

Date: 2026-06-27

Purpose: document the remaining 9 build items needed to make the CEO vaccination
message fully literal in runtime behavior. This is not a rewrite of the kernel
architecture. The kernel spine exists; this file tracks the business closure
work still required on top of it.

Frontend implementation details for the required screens live in
`context/frontend/vaccination-kernel-closure-screen-requirements.md`. Use that
doc before building UI; these screens must match the existing admin-web mock
anatomy, theme tokens, hover/click states, filters, drawers, and pagination.

## Plain Answer

Frontend is required for some items, but not all.

| Area | Frontend required? | Why |
| --- | --- | --- |
| Auto generation after rule publish | Small admin status/action surface, yes. | Backend owns the job, but Admin/Data Ops needs publish progress, last generation status, and failures. |
| Always-running event delivery | No product frontend; ops surface later. | This is worker/deployment/observability. A DLQ/ops UI belongs under the DLQ item. |
| Full goat shift repair | Yes. | Users need to see batch/drive changed, stock reservation changed, and any repair exception. |
| Sick/ICU/quarantine defer + recovery | Yes. | PHC/Parks users need hold reason, recovery review, and re-created due work visible. |
| Hard stock blocking | Yes. | Execution UI must block bad stock and show exact stock reason/action owner. |
| DLQ operation center | Yes. | Ops/Data/Engineering need list, inspect, replay, discard/ack, and audit. |
| Automatic DLQ/worker alerts | Minimal frontend. | Mostly infra/monitoring, but Control Tower/ops surfaces should show kernel health. |
| Incident escalation adapter | Minimal frontend. | Mostly adapter/config; UI should show incident reference/status when created. |
| Stage-change and manual-campaign triggers | Yes. | Stage changes must be visible in goat/process history; manual campaigns need a creation/approval/execution surface. |

Minimal frontend does not mean local/hardcoded UI. Any visible Control Tower,
Ops, incident, or campaign surface must still use backend-owned contracts for
labels, chips, actions, disabled reasons, and field sets.

## Final Done Gate

Do not call the vaccination kernel done until all of these are true in code,
contracts, infra, UI, tests, and docs:

This is a target-state gate, not a current-state claim. As of 2026-06-27,
several items below are intentionally false in runtime and remain backlog work.

1. Published protocol/config and SOP versions are immutable. Any change creates a
   new version.
2. Existing obligations, batches, SOP tasks, submissions, proof, completions,
   boosters, calendar rows, Action Center rows, and audit history keep the
   protocol/rule/SOP version they were created from.
3. A new published config version has an explicit migration decision for open
   work: continue old work, cancel it, supersede it, or regenerate it. Silent
   mutation of old work is not allowed.
4. Replay, retry, duplicate event delivery, worker restart, and DLQ replay are
   idempotent and audited.
5. Sick/ICU/quarantine, goat shift, goat exit, stock block, proof missing,
   proof rejected, overdue/missed, booster, stage-change, manual campaign, and
   rule/SOP version-change cases all have visible read-model state and E2E
   coverage.
6. Backend UI contracts own every visible title, label, table column, chip,
   filter, action, disabled reason, drawer field, and empty/error message.
7. Million-goat scale is preserved: tenant/scope bounded queries, cursor or
   keyset pagination for large lists, indexed status scans, chunked workers,
   bounded memory, and no unbounded frontend lists.
8. A 5.5 extra-high review pass has checked architecture, permissions,
   idempotency, replay safety, scale, edge cases, frontend contract ownership,
   and docs. All blocker findings are fixed.
9. The final kernel diagram, README links, backlog, screen requirements, and E2E
   checklist match the code that was actually pushed to `main`.

## Business Source Anchors

These are the cross-checked business anchors from wiki/source graphs, visual
handbook graphs, legacy repo graphs, and Goat OS architecture docs.

| Source | Business meaning for this backlog |
| --- | --- |
| `Handbooks/PHC_Director.pdf` | PHC has weekly/daily operations, execution standards, stock control, and record management responsibilities. |
| `images/pdf-pages/handbooks-phc-director-page-002.png` | Visual graph links PHC weekly checklist to daily operations, execution standards, and stock control. |
| `Handbooks/Mesha-dept-directors.pdf` | Escalation hierarchy exists across director, manager, park head, and assistant manager roles. |
| `graphify-out/converted/Feed Directions Automation DB_7837b62c.md` | Shed/ICU/location variants are operationally meaningful; location/state changes cannot be ignored. |
| `slack-automation-scripts/AGENTS.md` | Slack is a legacy workflow and notification bridge only; inbound actions must route through Goat OS APIs, permissions, validation, audit, and idempotency. |
| `dashboard/AGENTS.md` | Dashboards should use typed/governed DTOs and must not become direct data-source or workflow-truth owners. |
| `procurement_app/AGENTS.md` | Field/mobile work depends on task, form, media, camera, and upload-queue APIs behind adapters. |
| `context/architecture/operational-kernel.md` | Every operational feature must pass through event, transaction, audit/outbox, trigger, obligation, scheduler, alert, proof, verification, and read-model layers. |
| `context/architecture/operational-kernel-system-design.md` | Frontend/mobile render backend-owned truth; future verticals plug in through events, rules, SOP/proof, projections, permissions, SLA, and analytics facts. |
| `docs/protocol-engine/obligation-engine.md` | Protocol versions are the immutable/effective-dated config truth; rules generate obligations that retain version/rule identity. |
| `context/execution/vaccination-edge-case-code-coverage.md` | Current exact implementation caveats for vaccination: publish/generation, shift repair, defer gating, stock gates, DLQ ops, stage/manual triggers, and incident integration. |

## Priority Backlog

Priority order: 1, 2, 3, 5, 4, 6, 7, 9, 8.

Sections are numbered by stable backlog ID, not execution order. Execute in the
priority order above unless a later planning doc explicitly supersedes it.
Dependency edge: item 2 is the event-delivery prerequisite for the runtime
automation in items 3, 4, and 9.

### 1. Auto Generation After Rule Publish

Business promise protected:

```text
Once an approved vaccination rule is published, system checks goats and creates the vaccination due list.
```

Current gap:

- `PublishVersion` publishes the rule/version.
- Existing-goat due-list generation exists as a job/CLI path.
- The publish action does not itself enqueue or run existing-goat generation.
- Protocol versions are intended to be immutable/effective-dated, but the
  runtime must enforce draft-only mutation for rules/triggers and must expose a
  controlled supersede/regenerate decision for open work when a new version is
  published.
- Verified current code state: `PublishProtocolVersion` only flips
  `status='draft'` rows to published, but `CreateRule` and `CreateTrigger` do
  not yet enforce draft-only mutation in the repository or database. Final Done
  Gate item 1 is therefore currently false.

Build required:

| Layer | Work |
| --- | --- |
| Backend | Emit `protocol.version.published` or enqueue a durable generation command after publish. Add idempotent generation run rows with status, cursor, counts, failures, and retry. Enforce published-version immutability: rules/triggers can only be added to draft versions. Add explicit open-work policy for config changes: continue, cancel, supersede, or regenerate. |
| Infra | Ensure the generation worker/job is scheduled or triggered in each target environment. |
| Frontend | Add publish/generation status in Admin/Data Ops: queued, running, completed, failed, last run, affected goat count, retry action. |
| Tests | Publish replay must not duplicate obligations. Failed generation must be retryable from the same run/cursor. Published versions reject later rule/trigger mutation. V2 publish does not silently rewrite V1 work; every open-work transition is explicit and audited. |

Done means:

- A source-backed PHC vaccination rule publish automatically starts existing-goat
  evaluation.
- Admin can see generation status and retry failures.
- Duplicate publish/replay does not duplicate obligations.
- Published config history remains explainable: every old due/completed row can
  still answer which protocol version, rule, SOP version, approval, and proof
  policy created it.
- If V2 replaces V1, old open V1 work is either left intact or moved through an
  explicit `canceled`/`superseded`/regenerated transition with audit. It is
  never modified silently.

### 2. Always-Running Event Delivery

Business promise protected:

```text
For new goats, whenever a goat is added, system automatically checks if vaccination is needed.
```

Current gap:

- `goat.created`, `goat.location.changed`, and `goat.exited` handlers are wired.
- The automation depends on outbox relay/local eventbus or Pub/Sub domain
  consumer running.

Build required:

| Layer | Work |
| --- | --- |
| Backend | Keep producers and consumers idempotent. Add health endpoints or status rows for relay/consumer lag. |
| Infra | Deploy `outbox-relay` and `domain-event-consumer` as continuously running services/jobs with restart policy, IAM, subscriptions, DLQ, and monitoring. |
| Frontend | No product screen required now. Later kernel health can appear in an ops dashboard. |
| Tests | Kill/restart relay, replay pending outbox, duplicate Pub/Sub delivery, and verify obligations are still exactly once by idempotency. |

Done means:

- Goat-created events are delivered without manual CLI action.
- Relay lag, publish failures, and consumer failures are visible to ops.

### 3. Full Goat Shift Repair

Business promise protected:

```text
If goat shifts to another shed, pending vaccination work moves to the new shed.
```

Current gap:

- Open, unbatched obligations re-scope to the new shed.
- Already-batched shed drives do not automatically move/replan.
- Runtime repair depends on item 2 event delivery for reliable
  `goat.location.changed` delivery.

Build required:

| Layer | Work |
| --- | --- |
| Backend | Add batch repair policy for shifted goats: remove from old open batch, update counts, release/re-reserve stock, add/merge into target shed batch or create repair batch, write audit/status events. |
| Frontend | Show moved-from/moved-to state in vaccination execution, Calendar/Action Center, and batch detail. Show repair exception if old drive is already in progress/completed. |
| Infra | No special infra beyond event delivery and jobs. |
| Tests | Shift before batch, after batch before execution, during execution, after completion, and repeated same shift. |

Done means:

- A goat never remains assigned to the wrong shed's pending vaccination drive
  without an explicit repair/exception state.

### 4. Sick / ICU / Quarantine Defer Plus Recovery

Business promise protected:

```text
If goat is sick / ICU / quarantine, vaccination is kept on hold with reason, not silently missed.
```

Current gap:

- Defer can work when the published rule DSL includes `eligibility.defer_states`.
- A rule without defer states can schedule a sick/ICU/quarantine goat normally.
- Recovery re-check is not fully proven as a runtime trigger.
- Recovery automation depends on item 2 event delivery for health/lifecycle
  recovery events.

Build required:

| Layer | Work |
| --- | --- |
| Backend | Add standard PHC vaccination defer policy or enforce defer-state config for vaccination publish. Emit lifecycle/health recovery events and re-evaluate deferred obligations when the goat becomes eligible. |
| Frontend | Show hold reason, defer owner, recovery review, due-after-recovery, and re-created work in PHC/Vaccination and Action Center. |
| Infra | Event delivery for health/lifecycle recovery events. |
| Tests | Sick before generation, sick after due, quarantine/ICU, recovery, repeated recovery event, and overdue-after-recovery. |

Done means:

- Sick/ICU/quarantine goats are visibly deferred or blocked with reason.
- When recovery happens, vaccination is rechecked and not forgotten.

### 5. Hard Stock Blocking

Business promise protected:

```text
If stock is missing / expired, work is shown as blocked due to stock issue.
```

Current gap:

- Stock preview, reserve, consume/release, and warnings exist.
- Missing/expired/quarantined stock is not yet a hard execution gate everywhere.

Build required:

| Layer | Work |
| --- | --- |
| Backend | Enforce stock gate before drive execution/proof submission: missing lot, expired lot, quarantined lot, insufficient quantity, wrong location, and cold-chain block must stop execution unless an authorized override exists. |
| Frontend | Execution UI must disable start/submit when blocked and show exact reason, owner, stock action, and override policy. Calendar/Action Center must show stock-blocked state. |
| Infra | Optional stock-alert notification path for critical shortage/expiry. |
| Tests | No stock, partial stock, expired lot, quarantined lot, wrong shed/park stock, concurrent reservation race, authorized override. |

Done means:

- A bad-stock vaccination drive cannot proceed silently.
- The responsible stock owner can see and fix the block.

### 6. DLQ Operation Center

Business promise protected:

```text
System does not silently lose automation. Failed kernel events can be inspected and repaired.
```

Current gap:

- Outbox rows can become failed/dead-letter.
- CLI can list/replay selected rows.
- Pub/Sub DLQ policy exists in dev infra.
- No operations triage UI exists.

Build required:

| Layer | Work |
| --- | --- |
| Backend | Add DLQ/read API: cursor-paginated tenant/scope-bounded list of dead-letter/failed events, filters, payload summary, error, attempts, trace, source, replay, discard/ack, and audit. |
| Frontend | Build Operations/Data Ops DLQ screen: cursor-paginated tenant/scope-bounded list, detail drawer, replay button, discard/ack, copied trace IDs, status history, permission gates. |
| Infra | Wire Pub/Sub DLQ redrive/inspection path or import Pub/Sub DLQ messages into the same ops surface. |
| Tests | Replay safe event, replay poison event, discard/ack, permission denied, duplicate replay, audit trail. |

Done means:

- Ops can fix failed domain messages without shell access.
- Replay/discard decisions are audited.

### 7. Automatic DLQ / Worker Alerts

Business promise protected:

```text
If automation breaks, responsible people are alerted instead of discovering missed vaccines later.
```

Current gap:

- Kernel has metrics/logging foundations and DLQ status.
- No checked automatic alerting path for DLQ count/outbox lag/worker failure.

Build required:

| Layer | Work |
| --- | --- |
| Backend | Emit metrics for oldest outbox age, failed/dead-letter count, Pub/Sub lag, consumer failure, scheduler failure, Cloud Tasks failure, notification failure, projection lag. |
| Infra | Cloud Monitoring alert policies and notification channels per dev/stg/prod. |
| Frontend | Minimal kernel health panel in Control Tower or Ops surface: green/yellow/red, last failure, link to DLQ. Minimal frontend still means backend-contract-owned visible labels, states, disabled reasons, and links. |
| Tests | Force failed outbox, stopped consumer, stuck scheduler, notification failure, and verify alert/health state. |

Done means:

- Kernel failures page/alert somebody before business users notice missing work.

### 8. Incident Escalation Adapter

Business promise protected:

```text
Critical missed SLA can escalate beyond app/email/FCM into real incident handling.
```

Current gap:

- Escalation waterfall, PHC Director role, CEO level, notifications, ack, and
  resolve exist.
- Opsgenie/PagerDuty-style incident adapter is not implemented.

Build required:

| Layer | Work |
| --- | --- |
| Backend | Add incident gateway port and adapter; create/update/resolve incident from critical escalation level; store external incident ID/status. |
| Infra | Secrets, IAM, webhook/vendor config, retry policy, environment-specific routing. |
| Frontend | Show incident reference/status in escalation detail; do not make vendor UI the source of truth. Minimal frontend still means backend-contract-owned visible labels, states, disabled reasons, and links. |
| Tests | Create incident, retry create failure, idempotent duplicate escalation, resolve incident, missing secret/config disabled state. |

Done means:

- A critical PHC missed-SLA can create and resolve a real incident through a
  replaceable adapter.

### 9. Stage-Change And Manual-Campaign Triggers

Business promise protected:

```text
Vaccination rules based on goat stage or manually launched campaign actually create work.
```

Current gap:

- `goat.stage_changed` is referenced in docs/config concepts but has no live
  runtime trigger/handler.
- `manual_campaign` exists in schema/config options, but SM-1 generation skips
  it today (`dueAt` returns `ok=false` for non-SM-1 trigger types). Manual
  campaign needs its own command path.
- Stage-change runtime automation depends on item 2 event delivery before it can
  be called live.

Build required:

| Layer | Work |
| --- | --- |
| Backend | Emit `goat.stage_changed` when stage changes; add handler to re-evaluate stage-based rules. Implement manual campaign command that creates campaign obligations/batches with approval, scope, reason, and idempotency. |
| Frontend | Show stage-change history on goat/process detail. Add cursor-paginated tenant/scope-bounded manual campaign creation/approval UI in Admin/Data Ops or PHC according to authority model. |
| Infra | Event delivery through same outbox/Pub/Sub/domain consumer path. |
| Tests | Stage K1->K2, duplicate stage event, campaign by shed/cohort, campaign cancel, campaign re-run, permissions and source/approval gates. |

Done means:

- Stage-based rules and manually launched PHC campaigns produce real obligations
  and drives without ad hoc scripts.

## Screen Requirement Summary

| Item | Screen needed before CEO-literal completion? | Screen |
| --- | --- | --- |
| 1 | Yes, small | Admin/Data Ops publish generation status and retry. |
| 2 | No product screen | Ops health can come through item 7. |
| 3 | Yes | Vaccination execution batch repair state plus Calendar/Action Center updates. |
| 4 | Yes | Deferred/recovery state in PHC/Vaccination, Goat Passport/process detail, Action Center. |
| 5 | Yes | Stock-blocked execution state and Inventory owner action. |
| 6 | Yes | DLQ Operation Center. |
| 7 | Minimal | Kernel health panel plus external monitoring alerts. |
| 8 | Minimal | Incident ID/status on escalation detail. |
| 9 | Yes | Stage history and manual campaign creation/approval. |

## Non-Negotiable Build Rules

- Backend remains the source of truth. Frontend renders contracts and actions.
- Slack remains notification/reference bridge only, not canonical SOP execution.
- Existing dashboard patterns can inform visuals, but no direct BigQuery/Sheet
  truth in the new workflow UI.
- Mobile field execution must use Goat OS task/form/media APIs with upload
  adapters, not Firebase-only direct coupling.
- Published config/SOP truth is append-only by version. Drafts may change;
  published versions may only be retired/superseded by explicit workflow.
- Every action that changes work state needs permissions, idempotency, audit,
  and event/outbox behavior where downstream work depends on it.
- Every item above needs tests for replay and duplicate delivery before it can
  be called kernel-complete.
- The final answer may say "done" only after the Final Done Gate in this file is
  satisfied and the extra-high review plus E2E pass have no blockers.
