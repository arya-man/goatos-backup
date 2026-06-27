# Vaccination Edge-Case Code Coverage Audit

Date: 2026-06-27

Purpose: record what the current Goat OS vaccination slice actually handles in
code, which edge cases were missing or under-specified in the CEO-facing note,
and which cases remain partial, operationally gated, or not implemented. This is
an implementation audit, not product copy.

## Code Evidence Rule

Only backend, frontend, infra code, migrations, and tests count as implementation
evidence in this audit. Wiki, handbook, SOP notes, proof packs, and legacy
screens are useful for business expectation, but they are not used to mark
something as implemented.

## Post-Remediation Status

This audit was updated after the six kernel remediation commits pushed on
2026-06-27:

- `3c4adfe` wires goat move/exit lifecycle APIs, outbox events, and obligation
  shift/exit handlers.
- `89b35f9` adds the domain Pub/Sub consumer fan-out path.
- `39ded7c` adds dev Cloud Run job / Cloud Scheduler wiring plus a near-term
  Cloud Tasks queue/adapter for reminder/escalation dispatch.
- `75251a5` adds FCM and email notification gateway adapters.
- `3af33ca` adds the `phc_director` role and routes level-3 PHC escalation to
  that role.
- `b24158a` adds escalation acknowledgement/resolution APIs and closes the full
  active escalation ladder when a vaccination escalation is resolved.

So the old "missing six" are no longer accurate as blanket gaps. The remaining
caveats are narrower: Google resources are dev Terraform wiring until applied
and verified in target projects; DLQ still has outbox CLI replay plus Pub/Sub
DLQ policies but no ops triage UI or automatic DLQ alert; Opsgenie/PagerDuty-
style incident integration is still not implemented; and shift handling remains
a minimal re-scope of open, unbatched obligations rather than a full
eligibility/batch recompute.

## Code Files Checked

Implementation:

- `backend/internal/bootstrap/api.go`
- `backend/internal/identity/adapters/http/handler.go`
- `backend/internal/identity/adapters/postgres/admin_goat_create.go`
- `backend/internal/identity/adapters/postgres/goat_lifecycle.go`
- `backend/internal/identity/adapters/postgres/identifier_write_integration_test.go`
- `backend/internal/identity/app/goat_lifecycle.go`
- `backend/internal/procurement/adapters/postgres/goat_created_outbox.go`
- `backend/internal/procurement/app/service.go`
- `backend/internal/procurement/app/service_test.go`
- `backend/cmd/domain-event-consumer/main.go`
- `backend/internal/domainconsumer/adapters/pubsub/subscriber.go`
- `backend/internal/domainconsumer/app/service.go`
- `backend/internal/platform/eventbus/eventbus.go`
- `backend/internal/platform/eventbus/envelope.go`
- `backend/internal/platform/taskqueue/cloudtasks.go`
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
- `backend/internal/permissions/permissions.go`
- `backend/internal/permissions/routes.go`
- `backend/migrations/postgres/000001_phase_1_identity_foundation.sql`
- `backend/migrations/postgres/000074_obligation_engine.sql`
- `backend/migrations/postgres/000083_procurement_source_entry.sql`
- `backend/migrations/postgres/000086_calendar_vaccination_slice.sql`
- `backend/migrations/postgres/000087_notification_delivery_escalation_kernel.sql`
- `backend/migrations/postgres/000088_goat_lifecycle_identity_decisions.sql`
- `backend/migrations/postgres/000089_phc_director_escalation_role.sql`
- `backend/migrations/postgres/000090_escalation_ack_resolution_workflow.sql`
- `infra/envs/dev/main.tf`
- `infra/envs/dev/pubsub.tf`
- `infra/envs/dev/cloud_run_jobs.tf`
- `infra/envs/dev/cloud_tasks.tf`
- `infra/envs/dev/iam.tf`
- `infra/envs/dev/services.tf`
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
| Goat event -> canonical transaction | Implemented for admin goat create, procurement accepted intake, admin goat move, and admin goat exit. | `CreateAdminGoat`, `MoveGoat`, and `ExitGoat` write canonical goat/location/lifecycle state, audit/history, idempotency, identity events, and outbox rows in transaction. Procurement accepted intake also emits `goat.created` plus audit/outbox in the caller transaction. | `goat.stage_changed` event emission/handling is still not implemented. Lifecycle changes outside these APIs must emit the same events to get the same behavior. |
| Postgres as source of truth | Implemented. | Canonical tables include `goats`, `goat_identity_events`, `audit_log`, `outbox_messages`, `obligation_instances`, batches, SOP tasks, completions, projections, notification requests, and obligation escalation rows. | Google transports/jobs are operational delivery, not truth; Postgres remains canonical. |
| Outbox relay | Implemented as a CLI/service path. | `outbox-relay` claims pending rows, validates envelopes, publishes via logging, local eventbus, or GCP Pub/Sub adapter, then marks rows published/retry/dead-letter. `outbox-dlq` can list and replay selected failed/dead-letter rows to pending. | It is a relay binary; deployment/scheduling must run it continuously or frequently. DLQ replay is CLI-backed, not a full triage UI or automatic DLQ alert. |
| Pub/Sub publish/consume fan-out | Implemented for domain events. | Pub/Sub publisher adapter exists; `domain-event-consumer` receives messages from a domain subscription, validates envelopes, and dispatches to registered Goat OS handlers. Dev Terraform creates the outbox topic, domain subscription, analytics subscription, and DLQ topic/policies. | Google resources still need apply/verification per target environment. DLQ alerting/triage UI is not implemented. |
| DLQ management | Partially implemented. | Outbox rows can move to `dead_letter`; `outbox-dlq` lists and replays selected failed/dead-letter rows; dev Pub/Sub subscriptions have DLQ policy. | No checked DLQ triage UI or automatic DLQ alerting was found. |
| Local eventbus listeners | Implemented for current goat/vaccination handlers. | API bootstrap and domain-event-consumer register goat lifecycle handlers plus vaccination verification; outbox-relay local/eventbus mode registers the goat lifecycle handlers needed for outbox goat events. | API-process goat lifecycle subscriptions still depend on an event being published to that in-process bus; real goat lifecycle delivery is through outbox relay/local bus or Pub/Sub domain consumer. |
| Obligation consumer | Implemented for `goat.created`, canonical `goat.location.changed` / legacy `goat.shifted`, and `goat.exited` event delivery. | `GoatCreatedHandler` generates new-goat vaccination obligations; `GoatShiftedHandler` re-scopes open unbatched obligations; `GoatExitedHandler` cancels open obligations. | Existing-goat generation is not triggered by publish itself; it requires the generation/backfill job. Shift behavior is minimal and does not fully recompute eligibility or move already-batched work. |
| Projection refresher | Implemented as CLI/job code and dev Scheduler wiring. | `calendar-vaccination-projector` refreshes `calendar_event_projections`; dev Terraform schedules the Cloud Run job. | Verified deployment/apply is environment-specific. |
| Time sweeper | Implemented as CLI/job code and dev Scheduler wiring. | `obligation-sweeper` scans due obligations and groups them into shed/scope batches, with optional SOP task and stock reserve; dev Terraform schedules the job. | Verified deployment/apply is environment-specific. |
| Cloud Tasks | Implemented for near-term kernel dispatch. | `platform/taskqueue/cloudtasks.go` wraps Cloud Tasks, dev Terraform creates `near_term_kernel`, and reminder/escalation sweepers can enqueue the notification-dispatcher job URL. | Far-future business truth still lives in Postgres; Cloud Tasks is transport for near-term dispatch only. |
| Notifier/reminder | Implemented with configurable adapters. | Calendar nudge/reminder code inserts `notification_requests`, audit rows, outbox messages, and updates reminder state. `notification-dispatcher` claims queued/failed rows and sends local-stub, generic webhook, Slack webhook, email webhook, and FCM HTTP v1 requests with retry/backoff. | Real delivery depends on secrets/config; unconfigured channels fail visibly for retry/ops review. |
| Escalator | Implemented except incident vendor integration. | `calendar-escalation-sweeper` applies SLA thresholds, queues escalation notifications, updates projection escalation state, writes `obligation_escalations`, routes level 3 to `phc_director`, and level 4 to `ceo_internal`. Ack/resolve APIs update escalation state, audit/outbox/history, mark notifications read, and resolve all active ladder rows. | Opsgenie/PagerDuty-style incident integration is not implemented. |
| Waterfall SLA escalation | Implemented with caveats. | `calendar-escalation-sweeper` supports configurable level thresholds and idempotent queues; dev Terraform schedules it; resolution closes the full active escalation ladder. | Non-obligation Calendar events get escalation notifications but not `obligation_escalations` rows. |
| Frontend surfaces | Implemented for read/action surfaces, not full operator app. | Admin-web uses real backend APIs for vaccination execution, calendar, nudge/snooze, process integrity, and config. | Admin-web does not prove full field/operator SOP submission E2E; mobile/operator console remains separate. |

## Whole Event System Gap Check

The attached event-system explanation is directionally the right target model:
Postgres truth, outbox, relay, Pub/Sub, consumers, projections, sweeper, near-term
work scheduling, notifier/escalator, SOP execution, proof, verification, and
completion. The current code implements only part of that kernel end to end.

Code-backed today:

- Postgres canonical transaction and outbox rows for admin/procurement goat
  creation, admin goat move, and admin goat exit.
- Outbox relay with retry, exponential backoff, failed, and `dead_letter`
  statuses.
- Local/eventbus delivery path for `goat.created`, canonical
  `goat.location.changed` / legacy `goat.shifted`, and `goat.exited` when
  `outbox-relay` runs in eventbus mode.
- Domain Pub/Sub consumer fan-out through `domain-event-consumer`, with dev
  Pub/Sub topic/subscription/DLQ Terraform.
- Vaccination generation, duplicate suppression, per-shed obligation batching,
  SOP/proof fanout, verification, completion, booster scheduling, and
  projection refresh commands.
- Durable `notification_requests` rows for reminders/nudges, plus outbox/audit
  records.
- Notification dispatch worker for local-stub, configured generic webhook, and
  configured Slack webhook, email webhook, and FCM HTTP v1 channels, with
  delivery leases, retry backoff, and sent/failed marking.
- SLA escalation sweeper for overdue Calendar/Vaccination work, with
  operator/verifier -> park_head -> phc_director -> ceo_internal levels and
  obligation-backed escalation rows when the Calendar event maps to an
  obligation.
- Escalation acknowledgement/resolution APIs, status events, outbox/audit rows,
  notification read marking, and full active-ladder closure on resolution.
- Dev Cloud Run job / Cloud Scheduler Terraform for outbox relay, domain event
  consumer, vaccination generator, obligation sweeper, calendar projector,
  reminder sweeper, escalation sweeper, and notification dispatcher.
- Near-term Cloud Tasks queue/adapter for scheduler-triggered notification
  dispatch from reminder/escalation sweepers.
- Outbox DLQ list/replay CLI for selected failed/dead-letter rows.

Not code-backed end to end yet:

- DLQ management beyond CLI replay: no triage UI or automatic DLQ alerting was
  found.
- Opsgenie or equivalent incident integration.
- Production/staging application of dev Terraform and runtime secrets for
  Pub/Sub, Cloud Tasks, Scheduler, FCM, email, and Slack/webhook delivery.

## CEO Claims Now Live With Caveats

These two CEO-note claims were previously the riskiest. They are now code-backed
for the admin lifecycle APIs and outbox/domain-consumer path, with the caveats
below:

1. **"If goat shifts to another shed, pending vaccination work moves to the new
   shed."**
   `POST /admin/goats/{goat_id}/move` writes the canonical move event/outbox,
   and `GoatShiftedHandler` is registered in API bootstrap, outbox-relay
   eventbus mode, and domain-event-consumer. The implemented obligation behavior
   is still minimal: it re-scopes open, unbatched obligations; it does not move
   already-batched work or fully recompute eligibility.

2. **"If goat is dead / sold / exited, pending vaccination work is cancelled."**
   `POST /admin/goats/{goat_id}/exit` writes the canonical exit event/outbox,
   and `GoatExitedHandler` is registered in API bootstrap, outbox-relay
   eventbus mode, and domain-event-consumer. Procurement ineligible/excluded
   flows remain a separate wired path: procurement service calls
   `CancelOpenForGoat` directly, and the DB guard blocks active vaccination
   obligation writes for excluded/exited goats.

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
| `Config -> Due List -> Shed Drive -> SOP Execution -> Proof -> Verification -> Completion -> Alerts` | Mostly right, but each arrow depends on jobs/events. Alerts now have Pub/Sub, scheduler, Cloud Tasks, dispatcher, FCM/email/webhook/Slack, PHC Director escalation, and ack/resolve foundations in code. | Config publishes rules; generation/backfill creates obligations; sweeper creates drives; SOP/proof/verification completes work; calendar jobs project and queue reminders/escalations; notification-dispatcher sends configured local-stub/webhook/Slack/email/FCM channels; escalation ack/resolve closes the active SLA ladder. |
| Config is the approved vaccination rule. | Too broad. Publish gate is stricter than approval. | Config is a source-backed, approved, published protocol version with executable SOP and proof policy. |
| Config includes which vaccine / group / age / stage / dose / booster / SOP / proof / approver. | Right as a product model. Current source-backed roster is narrower. | This is the intended config model; only source-backed schedule rows generate obligations. Current dev schedule-backed row is ET/K1/day-21. |
| Config maintained in Admin / Data Ops -> Config -> Vaccination. | Correct for admin surface. | Keep. Add that PHC can draft/propose; COO/CEO publish per authority model. |
| Vaccination page only shows live work. | Correct directionally. | Keep. It is an operations/status surface, not the rule-authoring source of truth. |
| Once an approved rule is published, system checks goats and creates due list. | Overclaims automation. `PublishVersion` does not run old-goat generation. | After publish, existing goats need the generation/backfill job; new goats need `goat.created` outbox delivery to the handler; both paths are idempotent. |
| Existing goats get marked by background check. | True only if the CLI/job is run/scheduled. | Existing-goat due list is created by `generate-vaccination-obligations`, not automatically inside publish. |
| New goats are checked automatically. | Implemented through event delivery, not directly inside the create HTTP transaction. `goat.created` is written to identity events/outbox; outbox-relay local/eventbus mode and the Pub/Sub domain consumer can deliver it to the generation handler. | New goats are handled when `goat.created` is emitted to outbox and delivered by outbox-relay local/eventbus mode or the domain Pub/Sub consumer. |
| System groups due goats shed-wise. | Implemented by the `obligation-sweeper` job for existing due obligations. | Sweeper groups due obligations by shed/scope into one batch/drive when the worker/CLI runs; this is not automatic inside publish. |
| 45 goats in Shed A become one drive, not 45 tasks. | Correct for the sweeper batch model. | Keep, with "assuming the obligations share the same drive window/protocol scope." |
| Operator follows SOP, uploads video/proof, verifier checks. | Backend path and SOP seed exist; admin-web is not the operator app. | SOP/proof/verification exists in backend; field/operator UI E2E remains separate from this audit. |
| If approved: completed, stock updated, booster prepared. | Mostly true after accepted verification, but stock is best-effort/ledger-dependent. | Accepted verification marks completion, consumes/releases reserved stock where applicable, and schedules booster when an `after_previous_completion` rule exists. |
| If rejected: task stays open for correction/rework. | Implemented through verification rejected/rework fanout. | Keep. |
| Draft/not approved creates no work. | Correct; generation reads published versions. | Keep. |
| Trusted history avoids duplicate work. | Correct but narrow. | Trusted, reviewed matching HF/procurement/completion evidence suppresses matching dose generation. |
| Sick / ICU / quarantine kept on hold with reason. | Partially implemented and config-gated. Generation records `deferred` only when the published rule DSL has `eligibility.defer_states`; otherwise in-care sick/quarantine/ICU goats can get normal scheduled obligations. Full recovery re-evaluation is not proven. | The system can show a deferred/blocked reason only for rules configured with defer states; re-check-on-recovery should be tracked separately. |
| Goat shifts: pending work moves to new shed. | Implemented for admin move event delivery, with a limited behavior. | Admin goat move emits canonical `goat.location.changed`; the registered shift handler also keeps the legacy `goat.shifted` alias. The handler re-scopes open, unbatched obligations. Full eligibility recompute / already-batched drive migration is still not implemented. |
| Dead/sold/exited: pending work canceled. | Implemented for admin exit event delivery and procurement direct cancellation. | Admin goat exit emits `goat.exited`; registered handlers cancel open obligations. Procurement cancellation is separately wired; DB guard blocks active vaccination writes for excluded/exited goats. |
| Vaccination date passes: overdue/missed. | Yes, but UI states differ. | Calendar/execution show overdue; process-integrity can expose missed as blocked/gap reason. |
| Stock missing/expired blocks work. | Partial. Preview warns; reservation is best-effort; expired-lot hard guard not proven. | Stock shortage/expiry is surfaced as warning/blocker/readiness risk; hard execution blocking needs proof/fix. |
| Proof not uploaded -> pending proof. | Supported in read models / proof state. | Keep, but tie it to SOP/proof submission state. |
| Rule changes later: old completed work stays old; new work follows new approved rule. | Completed work immutability is right. Auto-cancel/supersede of old open work not confirmed. | Published/completed history stays under its version; open old-version obligations need explicit supersede/cancel policy. |
| Booster created after previous verified dose. | Correct. | Keep. |
| Calendar/Action Center/Alerts show due/overdue/missed/pending proof/pending verification/blocked/completed. | Mostly true, but Calendar is a projection; `missed` may appear as blocked/gap, not literal execution filter. Projection refresh is a CLI/job path, not proven as scheduled infra. | Calendar and work surfaces project due, overdue, proof, verification, blocked/deferred, completed, and missed/gap cases once projector has refreshed. |
| Reminder/nudge can be sent; if still not completed, escalate higher. | Implemented with operational caveats. Nudge/reminder/escalation rows can be queued; notification-dispatcher handles configured local-stub/webhook/Slack/email/FCM channels; calendar-escalation-sweeper applies SLA thresholds and routes level 3 to PHC Director; ack/resolve APIs exist. | Do not claim Opsgenie/PagerDuty integration or verified production deployment until those are applied/tested. |
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

12. **Reminders/nudges are queued, with configurable delivery adapters.**
   Calendar nudge/snooze APIs, reminder sweeper, `notification_requests`, audit,
   and outbox writes exist. `notification-dispatcher` now marks local-stub,
   configured generic webhook, configured Slack-webhook, email webhook, and FCM
   HTTP v1 requests sent/failed with retry backoff. Real delivery still depends
   on environment secrets/config.

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

18. **Outbox DLQ foundation plus CLI replay / Pub/Sub DLQ policies exist.**
    The outbox service can retry and then mark poison messages as `dead_letter`.
    `outbox-dlq` can list and replay selected failed/dead-letter rows back to
    pending. Dev Pub/Sub has domain and analytics subscriptions with DLQ
    policies. DLQ triage UI and automatic DLQ alerting are still not
    implemented.

## Implemented With Operational Caveats

1. **Existing-goat trigger after publish**
   Code exists as a CLI/job and dev Scheduler job, but `PublishVersion` still
   does not synchronously invoke it. Existing goats are materialized by
   `generate-vaccination-obligations`.

2. **`goat.created` / `goat.location.changed` / `goat.exited` delivery**
   The handlers are registered in API bootstrap, outbox-relay eventbus mode, and
   domain-event-consumer. Real lifecycle automation depends on the corresponding
   outbox event being emitted and delivered by the relay or Pub/Sub consumer.

3. **Outbox retry/dead-letter handling**
   The outbox service has retry/backoff, stale-publish reclaim, failed status,
   and `dead_letter` status when max attempts are exhausted. `outbox-dlq`
   provides bounded list/replay for selected terminal rows. This is not a full
   DLQ operation center: no triage screen or automatic alert on dead-letter rows
   was added.

4. **Goat shift handling**
   Canonical `goat.location.changed` is live for admin move events, with
   `goat.shifted` retained as a legacy alias in the handler. Shift handling is
   minimal: it re-scopes open, unbatched obligations; it does not fully
   re-evaluate eligibility, move already-batched work, or create individual
   catch-up work when the destination drive is already completed.

5. **Goat exited/dead/sold handling**
   `goat.exited` is live for admin exit events and procurement cancellation is
   wired directly. Any lifecycle update path outside those APIs must emit the
   same event or call the same cancellation boundary.

6. **Stock shortage / expiry**
   Impact preview warns on shortage and early expiry. FEFO pick/reserve/consume
   exists, but reservation is best-effort: no available stock is a no-op, and
   partial reserve is allowed. Do not claim hard stock-out/expired-lot blocking
   unless E2E proves the exact path.

7. **Missing DOB / entry date**
   `birth_age` rules need DOB and `post_arrival` rules need entry date. If that
   basis date is absent, generation increments `SkippedNoDueDate` and moves on;
   no obligation, defer reason, alert, or visible per-goat blocker was found.

8. **Escalation**
   Calendar escalation is now code-backed: configurable SLA thresholds, durable
   escalation notifications, `obligation_escalations`, PHC Director level 3,
   CEO/internal level 4, acknowledgement, resolution, history/audit/outbox, and
   full active-ladder closure on resolve. Opsgenie/PagerDuty-style incident
   integration is still not implemented.

9. **Calendar reminder delivery**
   `notification-dispatcher` claims queued/failed rows with leases and sends
   local-stub, configured webhook, configured Slack webhook, email webhook, and
   FCM HTTP v1 channels. Real delivery depends on configured URLs/tokens/service
   accounts in the target environment.

10. **Scheduler wiring**
   Dev Terraform defines Cloud Run Jobs and Cloud Scheduler jobs for outbox
   relay, domain event consumer, existing-goat generation, obligation sweeping,
   calendar projection refresh, reminder sweeping, escalation sweeping, and
   notification dispatch. It must still be applied and verified in each target
   Google project.

11. **Rule change in flight**
   Published versions are immutable and generated work keeps its version. A new
   version does not automatically rewrite old completed work. Automatically
   superseding/canceling already-open obligations from an older version was not
   confirmed in the checked code.

12. **Cold-chain enforcement**
   The SOP seed says cold chain must be verified before submitting. Runtime
   stores `cold_chain_verified`, but a hard block depends on the SOP/form-rule
   evaluator being active for the exact submission path. Keep this as partial
   until E2E or code proves the block.

13. **Adverse reaction follow-up**
    Reaction capture is present; automatic Health problem creation, treatment
    task creation, or escalation to Health/PHC was not found.

14. **Production/deployed delivery**
    Local backend proof and dev Terraform exist. External delivery still depends
    on target Google project apply, secrets, IAM, Pub/Sub, Cloud Tasks, Scheduler,
    notification vendor configuration, and deployed runtime verification.

## Not Implemented / Do Not Claim Yet

1. **DLQ triage UI / automatic DLQ alerting**
   Outbox dead-letter status, selected-row CLI list/replay, and Pub/Sub DLQ
   policies exist. No checked UI or automatic alerting path lets operations
   triage/escalate dead-lettered domain messages.

2. **Opsgenie/PagerDuty-style incident integration**
   SLA escalation rows and notification dispatch exist. An incident-management
   adapter/workflow is still not implemented.

3. **`goat.stage_changed` runtime trigger**
   The state-machine docs mention `goat.stage_changed`, and stage-driven rules
   exist in config, but no runtime `goat.stage_changed` handler was found.

4. **Manual campaign trigger**
   `manual_campaign` is allowed by schema and appears in config UI options, but
   generation explicitly skips it as manual. No campaign trigger handler was
   found.

5. **Full shift recompute**
   The spec-level behavior includes eligibility re-evaluation after shift,
   moving to destination batch, individual catch-up when needed, and canceling
   if no longer eligible. Current code only re-scopes open, unbatched
   obligations.

6. **Hard expired-stock prevention at execution**
   The checked stock code does not prove a hard runtime block for expired lots.
   Treat expiry as preview/readiness warning unless a later E2E or code path
   proves enforcement.

7. **Automatic recovery re-evaluation for deferred goats**
   The state-machine docs expect re-evaluation when ICU/quarantine/sick status
   clears. The current audit found visible deferral, but not a complete recovery
   trigger that reopens/regenerates due work.

8. **Automatic health follow-up for adverse reaction**
   No runtime path was found that turns `adverse_reaction=true` into a Health
   case, treatment plan, or follow-up obligation.

9. **Generating obligations from label-only vaccines**
   Do not claim due-list generation for PPR/FMD/HS/BQ until source-backed
   schedules are approved and published.

## Exact Gaps Missing From The Sent Message

- It does not say existing-goat generation is a separate backfill/job after
  publish.
- It does not say new-goat automation depends on `goat.created` outbox delivery
  through outbox-relay local/eventbus mode or the Pub/Sub domain consumer.
- It does not say DLQ is outbox-CLI-backed today: outbox rows can become
  `dead_letter`, `outbox-dlq` can list/replay selected terminal rows, and dev
  Pub/Sub subscriptions have DLQ policies, but redrive UI for Pub/Sub DLQ,
  DLQ triage UI, and automatic DLQ alerting do not exist.
- It does not say the generation trigger matrix is limited: SM-1 schedules
  `birth_age`, `post_arrival`, and `calendar`; `after_previous_completion` is
  booster-only after accepted verification; `manual_campaign` is skipped.
- It does not say `birth_age` silently skips goats with no DOB and
  `post_arrival` silently skips goats with no entry date, with no visible
  per-goat blocker found.
- It does not say Cloud Tasks/Scheduler are dev infra wiring and still need
  apply/verification in the target Google project.
- It does not say notification delivery depends on configured Slack/webhook/email
  URLs, FCM credentials, IAM, and runtime secrets.
- It does not say the SLA ladder is now operator/verifier -> park_head ->
  phc_director -> ceo_internal, with ack/resolve in code but no Opsgenie/
  PagerDuty integration.
- It does not say PPR/FMD/HS/BQ are label-only today, while ET is the
  schedule-backed local/dev row.
- It does not mention cold-chain verification and three proof subjects
  (shed, vial/lot, administration).
- It does not mention adverse reaction capture, and it would be wrong to claim
  automatic Health follow-up.
- It overstates stock blocking: current code warns/reserves/consumes best-effort,
  but hard stock-out/expired-lot enforcement is not fully proven.
- It overstates shift behavior if read as full recompute: current live code
  re-scopes open, unbatched obligations only.
- It overstates sick/ICU/quarantine hold: deferred status is written only when
  the published rule DSL includes `eligibility.defer_states`.
- It does not mention `goat.stage_changed` and `manual_campaign` are spec/UI
  concepts but not runtime implemented.
- It does not mention deferred goat recovery re-check is not proven.
- It does not mention backend local proof/dev Terraform is not the same as full
  admin-web / operator-mobile / deployed Google E2E.
- It does not mention open old-version obligations may need explicit
  supersede/cancel policy after config changes.

## Safer Addendum If Leadership Asks For Implementation Accuracy

Use this as the correction/addendum rather than rewriting the whole CEO note:

```text
Small implementation clarification:

The core vaccination chain is implemented in Goat OS code: canonical Postgres
rows, audit, outbox, outbox relay, Pub/Sub domain consumer, generation,
sweeper, shed drives, SOP/proof, verification, completion, booster scheduling,
Calendar/Action Center read models, reminders/nudges, notification dispatch,
SLA escalation, PHC Director routing, and escalation acknowledgement/resolution.

Existing goats are generated by the scheduled/backfill generation job after
publish, not inside the publish call itself. New goats are handled after the
goat.created outbox event is delivered by outbox-relay local/eventbus mode or
the Pub/Sub domain consumer.

Goat shift and exit are now code-backed through admin move/exit APIs and
outbox/domain event handlers. Shift currently re-scopes open, unbatched work;
it is not yet a full eligibility/batch recompute.

Alerts are durable rows. The dispatcher supports local-stub, webhook, Slack
webhook, email webhook, and FCM HTTP v1 when configured. Dev Cloud Scheduler,
Cloud Run Jobs, and Cloud Tasks wiring exists, but each target Google project
still needs apply/secret/IAM/runtime verification.

Remaining non-code-backed claims: DLQ triage UI/automatic DLQ alerting,
Opsgenie/PagerDuty incident integration, goat.stage_changed trigger,
manual_campaign trigger, hard expired-stock blocking, deferred-goat recovery
recheck, adverse-reaction health follow-up, and schedule-backed obligations for
label-only vaccines such as PPR/FMD/HS/BQ.
```

## Safe Engineering Summary

The core slice is real: source-backed config, canonical goat-create/move/exit
transactions with audit/outbox, outbox retry/dead-letter foundation plus
selected replay, Pub/Sub domain consumption, generation, per-shed batching,
SOP/proof fanout, verification, completion, stock reserve/consume foundation,
booster scheduling, Calendar/Action Center read models, reminder/nudge queueing,
Cloud Tasks near-term dispatch, notification adapters, scheduler/job wiring, PHC
Director escalation, and acknowledgement/resolution workflow exist in code.

The main risks to communicate internally are:

- publish does not by itself generate old-goat obligations;
- event automation depends on outbox relay / Pub/Sub domain consumer delivery;
- SM-1 generation only covers `birth_age`, `post_arrival`, and `calendar`;
- missing DOB/entry date skips generation without a visible per-goat blocker;
- outbox dead-letter status, selected CLI replay, and Pub/Sub DLQ policies
  exist, but DLQ triage UI and automatic DLQ alerting were not found;
- dev Cloud Tasks/Scheduler/Cloud Run Jobs exist but must be applied and
  verified in target Google projects;
- shift handling is live but minimal: open, unbatched obligation re-scope only;
- stock shortage/expiry is warning/best-effort, not a hard blocker yet;
- stage-change and manual-campaign triggers are not implemented;
- deferred sick/ICU/quarantine visibility depends on `eligibility.defer_states`;
- reminders/nudges/escalations can be queued and dispatched through configured
  adapters, but Opsgenie/PagerDuty incident integration was not found.
