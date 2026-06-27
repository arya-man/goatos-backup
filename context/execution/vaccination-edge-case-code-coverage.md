# Vaccination Edge-Case Code Coverage Audit

Date: 2026-06-27

Purpose: record what the current Goat OS vaccination slice actually handles in
code, which edge cases were missing or under-specified in the CEO-facing note,
and which cases are still partial or not wired. This is an implementation audit,
not product copy.

## Code Evidence Rule

Only backend, frontend, infra code, migrations, and tests count as implementation
evidence in this audit. Wiki, handbook, SOP notes, proof packs, and legacy
screens are useful for business expectation, but they are not used to mark
something as implemented.

## Code Files Checked

Implementation:

- `backend/internal/bootstrap/api.go`
- `backend/internal/identity/adapters/postgres/admin_goat_create.go`
- `backend/internal/identity/adapters/postgres/identifier_write_integration_test.go`
- `backend/internal/procurement/adapters/postgres/goat_created_outbox.go`
- `backend/internal/procurement/app/service.go`
- `backend/internal/procurement/app/service_test.go`
- `backend/internal/platform/eventbus/eventbus.go`
- `backend/internal/outbox/domain/types.go`
- `backend/internal/outbox/app/service.go`
- `backend/internal/outbox/adapters/postgres/repository.go`
- `backend/internal/outbox/adapters/publisher/eventbus/publisher.go`
- `backend/internal/outbox/adapters/publisher/pubsub/publisher.go`
- `backend/internal/outbox/adapters/publisher/pubsub/gcp.go`
- `backend/cmd/outbox-relay/main.go`
- `backend/cmd/outbox-dlq/main.go`
- `backend/cmd/backfill-goat-created/main.go`
- `backend/internal/protocol/app/publish.go`
- `backend/internal/vaccination/app/generation.go`
- `backend/internal/vaccination/app/generation_handler.go`
- `backend/cmd/generate-vaccination-obligations/main.go`
- `backend/internal/obligation/app/sweeper.go`
- `backend/cmd/obligation-sweeper/main.go`
- `backend/internal/sopbridge/vaccination_submission.go`
- `backend/internal/sopbridge/verify_fanout.go`
- `backend/internal/vaccination/app/verification_handler.go`
- `backend/internal/vaccination/app/completion.go`
- `backend/internal/vaccination/app/booster.go`
- `backend/internal/obligation/app/shift.go`
- `backend/internal/obligation/app/cancel.go`
- `backend/internal/calendar/domain/types.go`
- `backend/internal/calendar/app/service.go`
- `backend/internal/calendar/adapters/postgres/repository.go`
- `backend/cmd/calendar-vaccination-projector/main.go`
- `backend/cmd/calendar-reminder-sweeper/main.go`
- `backend/cmd/calendar-escalation-sweeper/main.go`
- `backend/internal/notification/app/service.go`
- `backend/internal/notification/adapters/postgres/repository.go`
- `backend/internal/notification/adapters/gateway/gateway.go`
- `backend/cmd/notification-dispatcher/main.go`
- `backend/internal/vaccinationexecution/adapters/postgres/repository.go`
- `backend/internal/processintegrity/adapters/postgres/repository.go`
- `backend/internal/adminui/app/service.go`
- `backend/migrations/postgres/000001_phase_1_identity_foundation.sql`
- `backend/migrations/postgres/000074_obligation_engine.sql`
- `backend/migrations/postgres/000083_procurement_source_entry.sql`
- `backend/migrations/postgres/000086_calendar_vaccination_slice.sql`
- `backend/migrations/postgres/000087_notification_delivery_escalation_kernel.sql`
- `infra/envs/dev/main.tf`
- `infra/envs/dev/pubsub.tf`
- `infra/envs/dev/README.md`
- `apps/admin-web/features/process-integrity/action-center.tsx`
- `apps/admin-web/features/process-integrity/work-board.tsx`
- `apps/admin-web/features/phc-vaccination/status-matrix.tsx`
- `apps/admin-web/features/vaccination-execution/work-state.ts`
- `apps/admin-web/features/calendar/*`
- `apps/admin-web/features/config/rule-dsl.ts`
- `apps/admin-web/lib/api/server.ts`

Reference / legacy code checked, but not counted as Goat OS runtime
implementation:

- `/Users/ravi/mesha/dashboard/app/api/vaccination/route.ts`
- `/Users/ravi/mesha/dashboard/app/(dashboard)/vaccination/page.tsx`
- `/Users/ravi/mesha/slack-automation-scripts/health_manager_attendance.js`

## Kernel Layer Code Audit

This checks the screenshot architecture against actual code, not docs.

| Kernel layer | Code status | Evidence | Actual miss / caveat |
| --- | --- | --- | --- |
| Goat event -> canonical transaction | Implemented for admin goat create and procurement accepted intake. | `CreateAdminGoat` writes goat row, location history, identity decision/event, audit row, outbox row, idempotency row, then commits once. Procurement accepted intake also emits `goat.created` plus audit/outbox in the caller transaction. | General `goat.shifted`, `goat.exited`, and `goat.stage_changed` event emission is not proven for all lifecycle paths. |
| Postgres as source of truth | Implemented. | Canonical tables include `goats`, `goat_identity_events`, `audit_log`, `outbox_messages`, `obligation_instances`, batches, SOP tasks, completions, projections, and notification requests. | Good foundation, but some lifecycle handlers are not wired into runtime. |
| Outbox relay | Implemented as a CLI/service path. | `outbox-relay` claims pending rows, validates envelopes, publishes via logging, local eventbus, or GCP Pub/Sub adapter, then marks rows published/retry/dead-letter. `outbox-dlq` can list and replay selected failed/dead-letter rows to pending. | It is a relay binary; deployment/scheduling must run it continuously or frequently. DLQ replay is CLI-backed, not a full triage UI or automatic DLQ alert. |
| Pub/Sub publish side | Partially implemented. | Pub/Sub publisher adapter exists and `infra/envs/dev/pubsub.tf` creates outbox topic, DLQ topic, and an analytics subscription with `dead_letter_policy`. | The DLQ policy found is for the analytics subscription. No domain Pub/Sub subscriber/consumer code was found that receives messages and invokes Goat OS handlers. Fan-out listeners are not end-to-end implemented through Pub/Sub yet. |
| DLQ management | Partially implemented. | Outbox rows can move to `dead_letter`; `outbox-dlq` lists and replays selected failed/dead-letter rows; Pub/Sub analytics subscription has a DLQ topic/policy. | No checked DLQ triage UI or automatic DLQ alerting was found. Pub/Sub DLQ is still analytics-only, not domain subscriber redrive. |
| Local eventbus listeners | Partially implemented. | API bootstrap registers vaccination verification handlers and also subscribes `GoatCreatedHandler`, but the checked API process only feeds that bus from SOP verification fanout. Outbox relay eventbus mode registers and feeds `goat.created` from outbox messages. | The API-process `GoatCreatedHandler` subscription appears inert for real goat-created flow; actual new-goat generation depends on outbox relay running in local/eventbus mode. `goat.shifted` and `goat.exited` handlers exist and have integration tests, but were not registered in the checked runtime paths. |
| Obligation consumer | Implemented for `goat.created` only when outbox relay dispatches it, partial for other goat lifecycle events. | `NewGoatCreatedHandler` calls `GenerateForGoat`; generation suppresses trusted existing evidence and writes obligation rows/idempotency. | Existing-goat generation is not triggered by publish itself; it requires the generation/backfill CLI/job. New-goat generation requires `goat.created` outbox delivery through outbox-relay eventbus mode because Pub/Sub has no domain consumer. |
| Projection refresher | Implemented as CLI/job code. | `calendar-vaccination-projector` refreshes `calendar_event_projections` from canonical vaccination state. | No Terraform/infra Cloud Scheduler job resource was found for this command. |
| Time sweeper | Implemented as CLI/job code. | `obligation-sweeper` scans due obligations and groups them into shed/scope batches, with optional SOP task and stock reserve. | No Terraform/infra Cloud Scheduler job resource was found for this command. |
| Cloud Tasks | Not implemented in checked code. | Search did not find Cloud Tasks client, queue resources, or `tasks.googleapis.com` usage. | The screenshot's "Cloud Tasks near-term only" box should not be claimed yet. |
| Notifier/reminder | Partially implemented. | Calendar nudge/reminder code inserts `notification_requests`, audit rows, outbox messages, and updates reminder state. `notification-dispatcher` claims queued/failed rows, marks local-stub/webhook/Slack-webhook sends sent/failed, and retries with backoff. | Real FCM/email vendor adapters are not implemented; unconfigured channels fail visibly. Admin UI top-bar notifications are still disabled in the checked service code. |
| Escalator | Partially implemented. | `calendar-escalation-sweeper` applies SLA thresholds, queues escalation notifications, updates projection escalation state, and writes `obligation_escalations` for obligation-backed events. | The role ladder is bounded by current DB roles: operator/verifier -> park_head -> admin -> ceo_internal. No PHC-director role, acknowledgement/resolution workflow, or Opsgenie integration was added. |
| Waterfall SLA escalation | Partially implemented. | `calendar-escalation-sweeper` supports configurable level thresholds and idempotent escalation queues. | Requires scheduling/deployment; no Terraform Scheduler job was found/added in this pass. Non-obligation Calendar events get escalation notifications but not `obligation_escalations` rows. |
| Frontend surfaces | Implemented for read/action surfaces, not full operator app. | Admin-web uses real backend APIs for vaccination execution, calendar, nudge/snooze, process integrity, and config. | Admin-web does not prove full field/operator SOP submission E2E; mobile/operator console remains separate. |

## Whole Event System Gap Check

The attached event-system explanation is directionally the right target model:
Postgres truth, outbox, relay, Pub/Sub, consumers, projections, sweeper, near-term
work scheduling, notifier/escalator, SOP execution, proof, verification, and
completion. The current code implements only part of that kernel end to end.

Code-backed today:

- Postgres canonical transaction and outbox rows for admin/procurement goat
  creation.
- Outbox relay with retry, exponential backoff, failed, and `dead_letter`
  statuses.
- Local/eventbus delivery path for `goat.created` when `outbox-relay` runs in
  eventbus mode.
- Vaccination generation, duplicate suppression, per-shed obligation batching,
  SOP/proof fanout, verification, completion, booster scheduling, and
  projection refresh commands.
- Durable `notification_requests` rows for reminders/nudges, plus outbox/audit
  records.
- Notification dispatch worker for local-stub, configured generic webhook, and
  configured Slack webhook channels, with delivery leases, retry backoff, and
  sent/failed marking.
- SLA escalation sweeper for overdue Calendar/Vaccination work, with
  operator/verifier -> park_head -> admin -> ceo_internal levels and
  obligation-backed escalation rows when the Calendar event maps to an
  obligation.
- Outbox DLQ list/replay CLI for selected failed/dead-letter rows.
- Pub/Sub topic infrastructure and an analytics subscription DLQ policy in dev.

Not code-backed end to end yet:

- Domain Pub/Sub subscriber fan-out that receives events and calls Goat OS
  handlers.
- DLQ management beyond CLI replay: no triage UI or automatic DLQ alerting was
  found.
- Cloud Tasks queueing/dispatch for near-term work.
- Terraform/infra Cloud Scheduler jobs for generation, sweeper, projector,
  reminder sweeper, or outbox relay.
- FCM/email vendor notification adapters.
- PHC-director-specific escalation role, acknowledgement/resolution workflow,
  and Opsgenie-style incident integration.
- Opsgenie or equivalent incident integration.

## Stated But Not Live

These two CEO-note claims are the riskiest because they read like live runtime
guarantees, but the generic event paths are not wired:

1. **"If goat shifts to another shed, pending vaccination work moves to the new
   shed."**
   Code exists for this as `GoatShiftedHandler`, and tests manually register it
   on an in-process bus. The checked live wiring in API bootstrap and
   `outbox-relay` does not register it. Also, the implemented behavior is
   minimal: it only re-scopes open, unbatched obligations; it does not move
   already-batched work or fully recompute eligibility.

2. **"If goat is dead / sold / exited, pending vaccination work is cancelled."**
   Code exists for generic `GoatExitedHandler`, and tests manually register it
   on an in-process bus. The checked live wiring in API bootstrap and
   `outbox-relay` does not register it. Procurement ineligible/excluded flows
   are a separate exception: procurement service calls `CancelOpenForGoat`
   directly, and the DB has a guard that blocks active vaccination obligations
   for procurement-excluded or exited/dead/sold goats on obligation writes. That
   guard is not the same as automatically cancelling already-open obligations
   after a generic goat lifecycle change.

## Non-Code Context: Not Implementation Proof

- Graphify text graph: `/Users/ravi/mesha/graphify-out/graph.json`
- Graphify visual graph:
  `/Users/ravi/mesha/graphify-visuals-clean-out/graphify-out/graph.json`
- `graphify-out/wiki/PHC_Director_&_Vet_Roles.md`
- `graphify-out/wiki/Health_Director_Interface.md`
- `wiki/graphify-out/wiki/Health_Operations_&_Treatment.md`
- `graphify-out/wiki/PHC_Daily_Operations.md`
- `source-material/sop-playground-local/playground.html`
- `dashboard/app/api/vaccination/route.ts`
- `dashboard/app/(dashboard)/vaccination/page.tsx`

## Requirement Cross-Check Against Wiki, Handbook, SOP, And Legacy

This section explains business expectations only. It does not upgrade any item
to "implemented" unless the code sections above prove it.

1. **Handbook supports PHC operating expectations, not runtime proof.**
   The PHC/Health handbook graph confirms vaccination/disease protocol
   execution by field vet/para-vet, daily task tracking with Park Head, video
   documentation/verification, cold-chain expectations, medicine/vaccine stock
   controls, health data documentation, and quarantine expectations. These are
   business requirements. They do not by themselves prove the current Goat OS
   runtime enforces every item.

2. **Legacy vaccination dashboard is read-only status reporting.**
   The legacy dashboard reads
   `goatos-sheets.ceo_dashboard.vaccination_dashboard` and renders shed/group
   cells with `Overdue`, `Due Soon`, `Up To Date`, or no record. It does not
   contain config authoring, goat-created triggers, obligation generation, SOP
   execution, proof upload, verification, stock ledger updates, or booster
   generation. Use it only as evidence that leadership expects a shed/group
   vaccination status matrix.

3. **Legacy Slack automation is not Goat OS vaccination alert delivery.**
   Legacy Slack scripts exist in the workspace, but the checked script is for a
   separate health-manager attendance workflow, not Goat OS vaccination reminder
   or escalation delivery. Do not count legacy Slack automation as proof that
   this vaccination slice sends Slack alerts.

4. **Only ET is schedule-backed in the current source-derived dev baseline.**
   The source audit closed PPR, FMD, HS, and BQ as `label-only closed`: they are
   valid SOP/vocabulary labels, but no goat-applicable schedule math with
   publishable PHC/vet approval metadata was found. ET / Enterotoxaemia K1 day
   21 is the current schedule-backed local/dev row. Do not imply every vaccine
   label in the SOP picker can generate obligations.

5. **SOP/proof shape is stronger than the CEO note says.**
   The seeded canonical SOP requires vaccine batch, cold-chain verification,
   shed proof video, vial/lot proof video, goat scan, dose, route/site,
   administered time, adverse reaction field, and administration proof video.
   The local business-chain proof used three proof uploads: shed, vial/lot, and
   administration. The CEO note says "video / proof"; safer technical wording is
   "configured proof policy, currently video proof for shed, vial/lot, and
   administration in the canonical seed."

6. **Adverse reaction is missing from the sent message and only partially
   runtime-backed.**
   The SOP seed has `adverse_reaction` and `adverse_reaction_notes`, with a
   required-if rule for notes. `vaccination_completions` stores
   `adverse_reaction` and has a future `adverse_reaction_problem_id`, but the
   fanout currently persists the boolean only; no automatic health problem /
   follow-up task creation was found. Do not claim health follow-up automation
   yet.

7. **Cold chain is a required SOP/proof concept, not fully proven as a stock
   safety gate.**
   Handbook and SOP seed both support cold-chain verification. Current runtime
   records `cold_chain_verified` from SOP answers. A hard "false cold chain
   blocks all completion/stock use" path is not proven end to end unless the SOP
   form-rule evaluator is active for that submission path.

8. **Procurement/Holding Farm history is trusted-evidence suppression, not a
   general history import promise.**
   Trusted HF/procurement evidence can suppress matching dose generation, but
   only when the evidence is reviewed/trusted and matches protocol/rule/dose
   timing. It is not "any vaccination history means no obligation."

9. **Admin-web is not the field operator app.**
   The backend/data plane has a captured local run from goat create through
   proof/SOP submission, verification, completion, and read models. The runbook
   explicitly says that direct generated API seed was used, not an admin-web
   operator console and not operator-mobile. Do not claim full operator UI E2E
   proof from this audit.

## Claim-By-Claim Audit Of The Sent CEO Note

| Sent claim | Current status | Safer internal wording |
| --- | --- | --- |
| `Config -> Due List -> Shed Drive -> SOP Execution -> Proof -> Verification -> Completion -> Alerts` | Mostly right, but each arrow depends on jobs/events. Alerts now have worker foundations, not full vendor/cloud completion. | Config publishes rules; generation/backfill creates obligations; sweeper creates drives; SOP/proof/verification completes work; calendar jobs project and queue reminders/escalations; notification-dispatcher sends local-stub/webhook/Slack-webhook channels. |
| Config is the approved vaccination rule. | Too broad. Publish gate is stricter than approval. | Config is a source-backed, approved, published protocol version with executable SOP and proof policy. |
| Config includes which vaccine / group / age / stage / dose / booster / SOP / proof / approver. | Right as a product model. Current source-backed roster is narrower. | This is the intended config model; only source-backed schedule rows generate obligations. Current dev schedule-backed row is ET/K1/day-21. |
| Config maintained in Admin / Data Ops -> Config -> Vaccination. | Correct for admin surface. | Keep. Add that PHC can draft/propose; COO/CEO publish per authority model. |
| Vaccination page only shows live work. | Correct directionally. | Keep. It is an operations/status surface, not the rule-authoring source of truth. |
| Once an approved rule is published, system checks goats and creates due list. | Overclaims automation. `PublishVersion` does not run old-goat generation. | After publish, existing goats need the generation/backfill job; new goats need `goat.created` outbox delivery to the handler; both paths are idempotent. |
| Existing goats get marked by background check. | True only if the CLI/job is run/scheduled. | Existing-goat due list is created by `generate-vaccination-obligations`, not automatically inside publish. |
| New goats are checked automatically. | Handler exists, but real delivery is narrower than the earlier wording. `goat.created` is written to identity events/outbox; the checked API in-process bus subscription is not fed by goat creation. Outbox relay local/eventbus mode can deliver it. Pub/Sub publish exists, but no Pub/Sub consumer was found. | New goats are handled only when `goat.created` is emitted to outbox and `outbox-relay` dispatches it to the vaccination generation handler in local/eventbus mode; Pub/Sub fan-out is not end-to-end yet. |
| System groups due goats shed-wise. | Implemented by the `obligation-sweeper` job for existing due obligations. | Sweeper groups due obligations by shed/scope into one batch/drive when the worker/CLI runs; this is not automatic inside publish. |
| 45 goats in Shed A become one drive, not 45 tasks. | Correct for the sweeper batch model. | Keep, with "assuming the obligations share the same drive window/protocol scope." |
| Operator follows SOP, uploads video/proof, verifier checks. | Backend path and SOP seed exist; admin-web is not the operator app. | SOP/proof/verification exists in backend; field/operator UI E2E remains separate from this audit. |
| If approved: completed, stock updated, booster prepared. | Mostly true after accepted verification, but stock is best-effort/ledger-dependent. | Accepted verification marks completion, consumes/releases reserved stock where applicable, and schedules booster when an `after_previous_completion` rule exists. |
| If rejected: task stays open for correction/rework. | Implemented through verification rejected/rework fanout. | Keep. |
| Draft/not approved creates no work. | Correct; generation reads published versions. | Keep. |
| Trusted history avoids duplicate work. | Correct but narrow. | Trusted, reviewed matching HF/procurement/completion evidence suppresses matching dose generation. |
| Sick / ICU / quarantine kept on hold with reason. | Partially implemented and config-gated. Generation records `deferred` only when the published rule DSL has `eligibility.defer_states`; otherwise in-care sick/quarantine/ICU goats can get normal scheduled obligations. Full recovery re-evaluation is not proven. | The system can show a deferred/blocked reason only for rules configured with defer states; re-check-on-recovery should be tracked separately. |
| Goat shifts: pending work moves to new shed. | Handler/repo/tests exist, but live bootstrap/outbox registration was not found. Minimal only. | Code can re-scope open, unbatched obligations; live event wiring and full shift recompute are still gaps. |
| Dead/sold/exited: pending work canceled. | Generic `goat.exited` handler/repo/tests exist, but live bootstrap/outbox registration was not found. Procurement ineligible/excluded flows do call `CancelOpenForGoat` directly, and a DB guard blocks active vaccination writes for excluded/exited goats. | Claim automatic generic exit cancellation only after `goat.exited` is emitted and the handler is wired. Procurement cancellation is a separate wired path. |
| Vaccination date passes: overdue/missed. | Yes, but UI states differ. | Calendar/execution show overdue; process-integrity can expose missed as blocked/gap reason. |
| Stock missing/expired blocks work. | Partial. Preview warns; reservation is best-effort; expired-lot hard guard not proven. | Stock shortage/expiry is surfaced as warning/blocker/readiness risk; hard execution blocking needs proof/fix. |
| Proof not uploaded -> pending proof. | Supported in read models / proof state. | Keep, but tie it to SOP/proof submission state. |
| Rule changes later: old completed work stays old; new work follows new approved rule. | Completed work immutability is right. Auto-cancel/supersede of old open work not confirmed. | Published/completed history stays under its version; open old-version obligations need explicit supersede/cancel policy. |
| Booster created after previous verified dose. | Correct. | Keep. |
| Calendar/Action Center/Alerts show due/overdue/missed/pending proof/pending verification/blocked/completed. | Mostly true, but Calendar is a projection; `missed` may appear as blocked/gap, not literal execution filter. Projection refresh is a CLI/job path, not proven as scheduled infra. | Calendar and work surfaces project due, overdue, proof, verification, blocked/deferred, completed, and missed/gap cases once projector has refreshed. |
| Reminder/nudge can be sent; if still not completed, escalate higher. | Partially implemented. Nudge/reminder/escalation rows can be queued; notification-dispatcher marks local-stub/webhook/Slack-webhook channels sent/failed; calendar-escalation-sweeper applies SLA thresholds. | Do not claim FCM/email vendor delivery, PHC-director-specific role routing, acknowledgement/resolution workflow, Opsgenie, or Cloud Scheduler deployment yet. |
| System shows why blocked or missed. | Partially true. Process-integrity/execution blocker reasons exist; not every path has a polished reason. | Work surfaces expose blocker/gap reasons where derived from canonical state. |

## Covered In Code, But Missing Or Too Light In The CEO Note

1. **Publish gate is stricter than "approved rule".**
   A publishable vaccination config must have a source system from
   `vaccinations_db`, `phc`, or `vet`; a `source_ref`; `review_status=approved`;
   `approved_by`; RFC3339 `approved_at`; a linked executable `sop_version_id`;
   and a real proof policy. Unsourced/manual/extracted config does not publish.

2. **Publishing is not the same as generating old-goat work.**
   `PublishVersion` only publishes the version. Existing goats need the
   generation job (`generate-vaccination-obligations`) to materialize
   `obligation_instances`. New goats use the `goat.created` handler only when
   the outbox event is delivered to it.

3. **Canonical transaction plus outbox is real for goat creation.**
   Admin goat creation and procurement accepted-intake paths write the domain
   row, audit row, identity event, outbox message, and idempotency state inside
   one transaction before event delivery. This part of the kernel is code-backed.

4. **Live generation trigger matrix is narrower than the config vocabulary.**
   SM-1 generation schedules only `birth_age`, `post_arrival`, and `calendar`
   trigger types. `after_previous_completion` is handled later by SM-7 booster
   scheduling after accepted verification. `manual_campaign` is skipped by
   generation and has no campaign handler in the checked code.

5. **DOB-less / entry-date-less goats can be silently skipped by generation.**
   For a `birth_age` rule, a goat with no DOB increments `SkippedNoDueDate` and
   receives no obligation, no deferred event, and no visible per-goat blocker.
   For a `post_arrival` rule, the same happens when `entry_date` is absent.
   This is an operational gap unless the rule uses a trigger whose basis data is
   guaranteed for the target cohort.

6. **Generation fails closed if trusted-history suppression cannot run.**
   The generation service refuses to generate if the completion-evidence reader
   is absent, because otherwise it could double-dose goats that already have
   trusted imported / holding-farm evidence.

7. **Duplicate-prevention is code-backed.**
   Obligation generation uses deterministic idempotency keys, vaccination
   completions are unique per `(tenant, obligation, goat)`, SOP fanout is
   idempotent, verification accept/reject is idempotent, and calendar actions
   use idempotency fingerprints.

8. **Trusted history is narrow.**
   Duplicate suppression is not "any old history". It checks trusted
   procurement/HF vaccination evidence with `review_status='trusted'`,
   `reviewed_at`, protocol/rule/dose match, and timing guards.

9. **Deferred/sick/ICU/quarantine is represented only when configured.**
   Generation creates the obligation and writes a visible `deferred` status
   event only when the published version DSL includes `eligibility.defer_states`
   and the goat is in a non-`alive` in-care lifecycle state. Without
   `defer_states`, sick/quarantine/ICU goats can receive normal scheduled
   obligations. Do not describe this as a fully separate paused-task workflow
   unless the UI/E2E proves that path end to end.

10. **Missed vaccine is not always a literal `missed` UI status.**
   Execution and process-integrity code derive `overdue`, `blocked`,
   `proof_pending`, `verification_pending`, `owner_missing`, etc. A missed
   obligation can surface as a blocked/gap reason such as "missed" in the
   process-integrity lens, while Calendar mostly exposes `overdue` and related
   due-work states.

11. **Calendar is projection-based.**
   Calendar events come from `calendar_event_projections`. The projection job
   (`calendar-vaccination-projector`) must run/refresh from canonical
   obligations, batches, SOP tasks, proof, and verification state.

12. **Reminders/nudges are queued, not guaranteed delivered to every channel.**
   Calendar nudge/snooze APIs, reminder sweeper, `notification_requests`, audit,
   and outbox writes exist. `notification-dispatcher` now marks local-stub,
   configured generic webhook, and configured Slack-webhook requests sent/failed
   with retry backoff. FCM/email vendor adapters are still not implemented.

13. **Booster generation is tied to accepted verification.**
    The booster is scheduled only after an accepted completion, using the actual
    `administered_at` and `max(offset_days, min_gap_days)` for the next
    `after_previous_completion` rule.

14. **Impact preview has stock/expiry warnings before publish/execution.**
    The vaccination impact preview computes eligible goats, catch-up goats,
    batches, dose requirements, available stock, earliest expiry, and warning
    strings.

15. **Cold-chain proof is part of the canonical SOP seed.**
    The seeded vaccination SOP requires cold-chain verification and video proof
    subjects for shed, vial/lot, and administration. The sent note's generic
    "video / proof" wording misses this operational expectation.

16. **Adverse reaction capture exists, but follow-up automation does not.**
    The SOP/config/schema include adverse reaction capture. The current fanout
    stores the boolean on `vaccination_completions`; no automatic Health case or
    follow-up task creation was found.

17. **Not every named vaccine is schedule-backed.**
    PPR, FMD, HS, and BQ are currently SOP/vocabulary labels only. ET is the
    schedule-backed local/dev baseline. Due-list generation must stay tied to
    source-backed published protocol rows, not to labels in a dropdown.

18. **Outbox DLQ foundation plus CLI replay exists.**
    The outbox service can retry and then mark poison messages as `dead_letter`.
    `outbox-dlq` can list and replay selected failed/dead-letter rows back to
    pending. Dev Pub/Sub also has an analytics subscription DLQ policy. DLQ
    triage UI and automatic DLQ alerting are still not implemented.

## Partially Implemented Or Operationally Gated

1. **Existing-goat trigger after publish**
   Code exists as a CLI/job, but there is no evidence in the checked files that
   `PublishVersion` automatically invokes it. Operations must run/schedule
   `generate-vaccination-obligations` after publishing a version.

2. **`goat.created` trigger**
   The handler exists and is subscribed on the API in-process bus, but the
   checked API process feeds that bus from SOP verification fanout, not from
   goat creation. Goat creation writes `goat.created` into identity events and
   `outbox_messages`. The working checked delivery path is `outbox-relay` in
   local/eventbus mode, which decodes outbox envelopes and publishes them to its
   in-process bus. Pub/Sub publish exists, but a Pub/Sub subscriber that
   delivers `goat.created` into the handler was not found.

3. **Outbox retry/dead-letter handling**
   The outbox service has retry/backoff, stale-publish reclaim, failed status,
   and `dead_letter` status when max attempts are exhausted. `outbox-dlq`
   provides bounded list/replay for selected terminal rows. This is CLI
   management, not a full DLQ operation center: no triage screen or automatic
   alert on dead-letter rows was added.

4. **Goat shift handling**
   `goat.shifted` handler and repository code exist, with integration tests, but
   the checked API bootstrap/outbox relay only registers `goat.created` and
   vaccination verification handlers. Also, shift handling is explicitly
   minimal: it re-scopes open, unbatched obligations; it does not fully
   re-evaluate eligibility, move already-batched work, or create individual
   catch-up work when the destination drive is already completed.

5. **Goat exited/dead/sold handling**
   `goat.exited` cancellation handler and repository code exist, with
   integration tests, but the checked API bootstrap/outbox relay does not
   register this handler. Unless another composition path wires it, open
   obligations will not be canceled automatically from a real emitted event.
   Procurement rejected/deferred/blocked/source-only/excluded paths are different:
   they are wired through `WithVaccinationCanceler(obligationRepo)` and call
   `CancelOpenForGoat` directly. The DB guard also blocks active vaccination
   obligation writes for excluded/exited goats, but it does not by itself emit a
   cancellation event for an already-open generic goat lifecycle change.

6. **Stock shortage / expiry**
   Impact preview warns on shortage and early expiry. FEFO pick/reserve/consume
   exists, but reservation is best-effort: no available stock is a no-op, and
   partial reserve is allowed. The current FEFO pick orders by `expiry_date` but
   does not visibly filter out already-expired lots in the checked query. Do not
   claim hard stock-out/expired-lot blocking unless E2E proves the exact path.

7. **Missing DOB / entry date**
   `birth_age` rules need DOB and `post_arrival` rules need entry date. If that
   basis date is absent, generation increments `SkippedNoDueDate` and moves on;
   no obligation, defer reason, alert, or visible per-goat blocker was found.

8. **Escalation**
   Calendar projections have `escalation_state`, the UI can display it, and
   seeded/dev rows include pending escalation examples. The
   `calendar-escalation-sweeper` command now applies configurable SLA thresholds,
   queues escalation
   notifications, updates projection state, and creates `obligation_escalations`
   for obligation-backed Calendar events. The ladder is limited to existing DB
   roles: operator/verifier -> park_head -> admin -> ceo_internal.

9. **Calendar reminder delivery**
   The sweeper queues reminder notification requests and outbox messages for
   due vaccination events. `notification-dispatcher` claims queued/failed rows
   with leases and marks local-stub, configured webhook, and configured
   Slack-webhook channels sent/failed. Email and FCM vendor delivery are still
   not implemented.

10. **Scheduler wiring**
   CLI/job code exists for outbox relay, existing-goat generation, obligation
   sweeping, calendar projection refresh, reminder sweeping, escalation
   sweeping, notification dispatch, and DLQ replay. No checked Terraform/infra
   code defines Cloud Scheduler jobs for those commands.

11. **Rule change in flight**
   Published versions are immutable and generated work keeps its version. A new
   version does not automatically rewrite old completed work. However,
   automatically superseding/canceling already-open obligations from an older
   version was not confirmed in the checked code.

12. **Cold-chain enforcement**
   The SOP seed says cold chain must be verified before submitting. Runtime
   stores `cold_chain_verified`, but a hard block depends on the SOP/form-rule
   evaluator being active for the exact submission path. Keep this as partial
   until E2E or code proves the block.

13. **Adverse reaction follow-up**
    Reaction capture is present; automatic Health problem creation, treatment
    task creation, or escalation to Health/PHC was not found.

14. **Production/deployed delivery**
    Local backend proof exists. External delivery still depends on target Google
    resources, Pub/Sub subscriber/consumer code, Cloud Run/Scheduler/outbox
    configuration, notification processors, and verified deployment context.

## Not Implemented / Do Not Claim Yet

1. **Domain Pub/Sub consumer / listener fan-out**
   Pub/Sub publish support and topic infra exist, but no checked backend binary
   receives Pub/Sub messages and dispatches them to the Goat OS domain handlers.

2. **DLQ triage UI / automatic DLQ alerting**
   Outbox dead-letter status, selected-row CLI list/replay, and an analytics
   Pub/Sub DLQ policy exist. No checked UI or automatic alerting path lets
   operations triage/escalate dead-lettered domain messages.

3. **Cloud Tasks near-term queue**
   No checked backend or infra code uses Cloud Tasks clients, queues, or
   `tasks.googleapis.com`. Do not claim Cloud Tasks is part of the current
   execution kernel.

4. **Cloud Scheduler resources for the vaccination jobs**
   Job binaries exist, but no checked Terraform resource wires scheduler jobs for
   generation, obligation sweeper, projector, reminder sweeper, escalation
   sweeper, notification dispatcher, DLQ monitoring/replay, or outbox relay.

5. **FCM/email notification vendor adapters**
   `notification-dispatcher` handles local-stub, configured generic webhook,
   and configured Slack webhook channels. FCM and email vendor adapters are not
   implemented; unconfigured channels fail visibly for retry/ops review.

6. **`goat.stage_changed` runtime trigger**
   The state-machine docs mention `goat.stage_changed`, and stage-driven rules
   exist in config, but no runtime `goat.stage_changed` handler was found.

7. **Manual campaign trigger**
   `manual_campaign` is allowed by schema and appears in config UI options, but
   generation explicitly skips it as manual. No campaign trigger handler was
   found.

8. **Full shift recompute**
   The spec-level behavior includes eligibility re-evaluation after shift,
   moving to destination batch, individual catch-up when needed, and canceling
   if no longer eligible. Current code only re-scopes open, unbatched
   obligations.

9. **Full business-specific SLA escalation lifecycle**
   `calendar-escalation-sweeper` implements configurable threshold levels using
   existing DB roles. Do not claim PHC Director routing, acknowledgement/
   resolution workflow, Opsgenie/PagerDuty integration, or Cloud Scheduler
   deployment yet.

10. **Hard expired-stock prevention at execution**
   The checked stock code does not prove a hard runtime block for expired lots.
   Treat expiry as preview/readiness warning unless a later E2E or code path
   proves enforcement.

11. **Automatic recovery re-evaluation for deferred goats**
   The state-machine docs expect re-evaluation when ICU/quarantine/sick status
   clears. The current audit found visible deferral, but not a complete recovery
   trigger that reopens/regenerates due work.

12. **Automatic health follow-up for adverse reaction**
   No runtime path was found that turns `adverse_reaction=true` into a Health
   case, treatment plan, or follow-up obligation.

13. **Generating obligations from label-only vaccines**
   Do not claim due-list generation for PPR/FMD/HS/BQ until source-backed
   schedules are approved and published.

## Exact Gaps Missing From The Sent Message

- It does not say existing-goat generation is a separate backfill/job after
  publish.
- It does not say new-goat automation depends on `goat.created` event delivery,
  specifically outbox-relay running in local/eventbus mode. The API-process
  `GoatCreatedHandler` subscription is not fed by goat creation, and Pub/Sub
  consumer delivery is not implemented end to end yet.
- It does not say DLQ is CLI-managed today: outbox rows can become
  `dead_letter`, `outbox-dlq` can list/replay selected terminal rows, and dev
  analytics Pub/Sub has a DLQ policy, but no DLQ triage UI or automatic DLQ
  alerting exists.
- It does not say the generation trigger matrix is limited: SM-1 schedules
  `birth_age`, `post_arrival`, and `calendar`; `after_previous_completion` is
  booster-only after accepted verification; `manual_campaign` is skipped.
- It does not say `birth_age` silently skips goats with no DOB and
  `post_arrival` silently skips goats with no entry date, with no visible
  per-goat blocker found.
- It does not say the screenshot-style Cloud Tasks path is not present in code.
- It does not say scheduler resources for generation/sweeper/projector/reminder/
  escalation/notification jobs were not found in infra.
- It does not say notification delivery is partial: `notification-dispatcher`
  handles local-stub, configured generic webhook, and configured Slack webhook,
  but FCM/email vendor adapters are not implemented.
- It does not say role/SLA waterfall alerting is bounded by current code:
  `calendar-escalation-sweeper` escalates through existing DB roles only
  (operator/verifier -> park_head -> admin -> ceo_internal), not PHC Director or
  arbitrary hierarchy roles.
- It does not say PPR/FMD/HS/BQ are label-only today, while ET is the
  schedule-backed local/dev row.
- It does not mention cold-chain verification and three proof subjects
  (shed, vial/lot, administration).
- It does not mention adverse reaction capture, and it would be wrong to claim
  automatic Health follow-up.
- It overstates stock blocking: current code warns/reserves/consumes best-effort,
  but hard stock-out/expired-lot enforcement is not fully proven.
- It overstates shift/dead/sold automation: handlers and tests exist, but live
  event registration was not found in the checked bootstrap/relay paths. The
  procurement excluded/ineligible cancellation path is wired separately, and the
  DB guard blocks active vaccination writes for excluded/exited goats, but this
  is not generic `goat.exited` event automation.
- It overstates sick/ICU/quarantine hold: deferred status is written only when
  the published rule DSL includes `eligibility.defer_states`.
- It overstates missed/escalation: missed can surface as overdue or blocked/gap;
  reminder/nudge/escalation queueing and worker delivery foundations exist, but
  DLQ alerting, FCM/email vendors, PHC Director routing, escalation ack/resolve,
  Opsgenie/PagerDuty, and Cloud Scheduler deployment are not done.
- It does not mention `goat.stage_changed` and `manual_campaign` are spec/UI
  concepts but not runtime implemented.
- It does not mention deferred goat recovery re-check is not proven.
- It does not mention backend local proof is not the same as full admin-web /
  operator-mobile / deployed Google E2E.
- It does not mention open old-version obligations may need explicit
  supersede/cancel policy after config changes.

## Safer Addendum If Leadership Asks For Implementation Accuracy

Use this as the correction/addendum rather than rewriting the whole CEO note:

```text
Small implementation clarification:

The core vaccination data chain is implemented in Goat OS, but the full
screenshot-style kernel is only partial today. Canonical Postgres rows, audit,
outbox, outbox-relay local eventbus delivery, generation, sweeper, SOP/proof,
verification, completion, booster scheduling, and read models exist in code.
Outbox retry/dead-letter status, selected DLQ replay, notification dispatch for
local-stub/webhook/Slack-webhook channels, and configurable Calendar SLA
escalation also exist in code. Pub/Sub subscriber fan-out, Cloud Tasks,
Scheduler infra wiring, FCM/email delivery, PHC-director-specific escalation,
acknowledgement/resolution workflow, and Opsgenie-style incident escalation are
not fully implemented in the checked code.

After a rule is published, existing goats need the generation/backfill job to
create due work. New goats are handled only when the goat.created outbox event is
delivered to the vaccination handler. In checked code, that means outbox-relay
running in local/eventbus mode; the API-process handler subscription is not fed
by goat creation, and the Pub/Sub path has no domain consumer. The sweeper then
groups due goats into shed drives. The binaries exist, but Scheduler resources
for these jobs were not found in the checked infra.

Dead-letter handling should also be described carefully. The outbox can mark
poison messages as dead-lettered, and `outbox-dlq` can list/replay selected
failed/dead-letter rows. Dev Pub/Sub has an analytics subscription DLQ policy.
There is still no checked DLQ triage UI or automatic alert on dead-lettered
vaccination/domain messages.

Two edge cases from the note are not live as generic automation yet: goat shed
shift and goat dead/sold/exited cleanup. Handlers exist and tests prove them,
but they are not registered in the checked API/outbox runtime wiring. Procurement
ineligible/excluded cancellation is wired separately and the DB blocks active
vaccination writes for excluded/exited goats, but that does not replace generic
goat.exited event automation.

The current source-backed dev vaccine schedule is ET / Enterotoxaemia. PPR, FMD,
HS, and BQ are present as SOP/vocabulary labels only until PHC/vet source data
approves schedule, dose, booster, and proof rules for them.

Generation trigger support is also narrower than the config vocabulary: SM-1
schedules birth_age, post_arrival, and calendar rules. Boosters are created
after accepted verification. Manual campaigns are not generated by the checked
runtime. A birth_age rule skips goats with no DOB, and a post_arrival rule skips
goats with no entry date.

Stock shortage, expiry, missed-dose escalation, generic goat shift/death cleanup,
deferred-goat recovery, and missing-DOB visibility are the main areas to keep
auditing. Some code/tests exist, but not every path is wired into the runtime
flow yet.

Alerts/reminders/escalations are durable rows from the Calendar/notification
path. `notification-dispatcher` marks local-stub, configured webhook, and
configured Slack webhook sends sent/failed with retry backoff. FCM/email vendor
delivery is not implemented. `calendar-escalation-sweeper` applies SLA levels
through existing DB roles (operator/verifier -> park_head -> admin ->
ceo_internal), but not PHC Director routing, acknowledgement/resolution, or
Opsgenie/PagerDuty-style escalation.
```

## Safe Engineering Summary

The core slice is real: source-backed config, canonical goat-create transaction
with audit/outbox, outbox retry/dead-letter foundation plus selected replay,
generation, per-shed batching, SOP/proof fanout, verification, completion,
stock reserve/consume foundation, booster scheduling, Calendar/Action Center
read models, reminder/nudge queueing, notification dispatch foundation, and SLA
escalation sweeping exist in code.

The main risks to communicate internally are:

- publish does not by itself generate old-goat obligations;
- new-goat generation depends on outbox-relay local/eventbus delivery; the API
  bus subscription is not fed by goat creation;
- SM-1 generation only covers `birth_age`, `post_arrival`, and `calendar`;
- missing DOB/entry date skips generation without a visible per-goat blocker;
- Pub/Sub publishing exists, but domain subscriber fan-out was not found;
- outbox dead-letter status and selected CLI replay exist, but DLQ triage UI and
  automatic DLQ alerting were not found;
- Cloud Tasks and Cloud Scheduler resource wiring were not found;
- shift/exited handlers exist but are not visibly wired in the checked runtime;
- procurement excluded/ineligible cancellation is wired separately, and the DB
  guard blocks active vaccination writes for excluded/exited goats;
- stock shortage/expiry is warning/best-effort, not a hard blocker yet;
- stage-change and manual-campaign triggers are not implemented;
- deferred sick/ICU/quarantine visibility depends on `eligibility.defer_states`;
- reminders/nudges/escalations can be queued and locally/webhook dispatched, but
  FCM/email adapters, PHC Director routing, escalation acknowledgement/
  resolution, Opsgenie/PagerDuty, and Scheduler deployment were not found.
