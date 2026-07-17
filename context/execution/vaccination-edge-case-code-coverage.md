# Vaccination Edge-Case Code Coverage Audit

Date: 2026-06-27

Purpose: record what the Goat OS vaccination slice actually handles in code,
which claims are runtime-backed, and which caveats remain. Wiki, handbook, SOP,
and legacy repo notes are business context only; they are not implementation
proof.

## Code-Backed Kernel Status

| Kernel area | Current code status | Evidence |
| --- | --- | --- |
| Canonical goat events | Admin goat create, procurement accepted intake, admin move, admin exit, and admin stage changes are canonical transactions with idempotency, audit/history, identity event, and outbox rows. | `backend/internal/identity/adapters/postgres/admin_goat_create.go`, `goat_lifecycle.go`, `backend/internal/procurement/adapters/postgres/goat_created_outbox.go`, `backend/migrations/postgres/000088_goat_lifecycle_identity_decisions.sql`, `000096_goat_stage_identity_decision.sql` |
| Event delivery | Outbox relay supports logging/local eventbus/Pub/Sub publishers; domain-event-consumer consumes Pub/Sub and fans out into the in-process bus; API/bootstrap, outbox-relay, and domain-event-consumer register goat created/recheck/manual/verification and obligation shift/exit handlers. | `backend/cmd/outbox-relay/main.go`, `backend/cmd/domain-event-consumer/main.go`, `backend/internal/bootstrap/api.go`, `backend/internal/domainconsumer/*`, `backend/internal/outbox/*` |
| Publish -> existing-goat generation | Protocol publish now runs vaccination generation through a durable generation run row for vaccination versions. CLI generation also writes a run. | `backend/internal/bootstrap/api.go`, `backend/internal/protocol/app/publish.go`, `backend/internal/vaccination/app/generation.go`, `backend/migrations/postgres/000095_vaccination_generation_runs.sql`, `backend/cmd/generate-vaccination-obligations/main.go` |
| Config immutability | Published protocol versions are immutable at the DB boundary: repository commands publish only drafts, child rule/trigger edits require draft status, and published version config fields cannot be silently mutated. | `backend/internal/protocol/adapters/postgres/sqlc/commands.sql`, `backend/internal/protocol/ports/ports.go`, `backend/migrations/postgres/000093_protocol_published_version_immutability.sql` |
| New-goat generation | `goat.created` drives vaccination generation through the same idempotent generation path. Trusted procurement holding-park/imported vaccination history suppresses duplicates only when it is our supervised SOP/video/physical validation evidence. | `backend/internal/vaccination/app/generation_handler.go`, `backend/internal/vaccination/app/generation.go`, `backend/internal/vaccination/adapters/postgres/generation_integration_test.go` |
| Stage-change trigger | `POST /admin/goats/{goat_id}/stage` emits `goat.stage_changed`; `GoatRecheckHandler` listens to `goat.stage_changed` and `goat.location.changed` and re-runs goat-level generation. | `backend/internal/identity/app/goat_lifecycle.go`, `backend/internal/identity/adapters/http/handler.go`, `backend/internal/identity/adapters/postgres/goat_lifecycle.go`, `contracts/jsonschema/domain-event-envelope.schema.json` |
| Manual campaign trigger | `POST /vaccination/manual-campaigns` calls `GenerateManualCampaignForVersionWithRun`; `manual_campaign` schedule rows only fire through this deliberate path and are idempotent by campaign id plus `as_of`, with an HTTP `Idempotency-Key` command boundary. | `backend/internal/vaccination/adapters/http/handler.go`, `backend/internal/vaccination/app/generation.go`, `contracts/openapi/admin-api.yaml`, `apps/admin-web/lib/api/server.ts` |
| Shed/park drive clubbing | Sweeper groups generated per-animal obligations into compatible shed/park drives. Exact per-animal due dates are not drive boundaries: nearby compatible animals must be delayed within the authored safe window / one-time batching hold to maximize drive output. Micro-drives are valid only when no compatible work can be safely clubbed before the earliest selected animal's last safe date. | `backend/internal/obligation/app/sweeper.go`, `backend/internal/obligation/app/drive_planner.go`, `backend/tests/e2e/story_ak_drive_clubbing_test.go`, `backend/cmd/obligation-sweeper/main.go` |
| Goat shift | Move API emits `goat.location.changed`; shift handler re-scopes open unbatched obligations and now detaches/re-scopes planned batched obligations, decrementing old batch estimated count and marking stock reconciliation when reservation existed. | `backend/internal/obligation/app/shift.go`, `backend/internal/obligation/adapters/postgres/repository.go` |
| Goat exit | Exit API emits `goat.exited`; exit handler cancels open obligations. Procurement ineligible/excluded path also calls cancellation directly and DB guards block active vaccination obligations for excluded goats. | `backend/internal/obligation/app/cancel.go`, `backend/internal/procurement/app/service.go`, vaccination schema trigger |
| Sick/ICU/quarantine defer | Generation writes deferred status only when the published rule DSL includes `eligibility.defer_states` and goat lifecycle/state matches. | `backend/internal/vaccination/app/generation.go` |
| Stock block | FEFO lot pick ignores expired lots; reservation errors on missing/zero/insufficient stock; sweeper records batch `stock_block`; batch completion materialization requires lot and cold-chain; inventory movement plus balance adjustment is atomic. | `backend/internal/inventory/app/reserve.go`, `backend/internal/inventory/app/consume.go`, `backend/internal/inventory/adapters/postgres/repository.go`, `backend/internal/inventory/adapters/postgres/sqlc/query.sql`, `backend/internal/obligation/app/sweeper.go`, `backend/internal/vaccination/app/completion.go` |
| SOP/proof/verification | SOP submission bridge records vaccination completions; verification accept/reject handles completion/rework, consumes reserved stock before accepting SOP-recorded completions, and creates boosters only after accepted completion. | `backend/internal/sopbridge/vaccination_submission.go`, `backend/internal/sopbridge/verify_fanout.go`, `backend/internal/vaccination/app/completion.go`, `backend/internal/vaccination/app/booster.go` |
| Calendar/Action Center/Control views | Vaccination projections/read models feed Calendar, Action Center, Protocol Adherence, Workflow, Control Tower, and execution screens. | `backend/internal/calendar/*`, `backend/internal/vaccinationexecution/*`, `backend/internal/processintegrity/*`, `apps/admin-web/features/*` |
| Reminders/escalation waterfall | Reminder/escalation sweepers create durable notifications, apply SLA thresholds, route level 3 to Preventive Care (PC) Director and level 4 to CEO, and ack/resolve APIs close escalation state. | `backend/cmd/calendar-reminder-sweeper`, `backend/cmd/calendar-escalation-sweeper`, `backend/internal/calendar/*`, `backend/migrations/postgres/000089_*`, `000090_*` |
| Notification delivery | Dispatcher sends local-stub, generic webhook, Slack webhook, email webhook, FCM HTTP v1, and incident webhook channels with leases/retry/backoff. | `backend/cmd/notification-dispatcher/main.go`, `backend/internal/notification/adapters/gateway/gateway.go` |
| Incident adapter | Generic incident webhook adapter supports `incident`, `opsgenie`, and `pagerduty` channels by config. | `backend/internal/notification/adapters/gateway/gateway.go`, `gateway_test.go` |
| DLQ operations | Outbox DLQ list/replay/discard APIs, durable idempotent repair ledger, audit retry, generated client, and admin-web DLQ screen exist; kernel health endpoint reports dead-letter/failed/pending health. | `backend/internal/outbox/adapters/http/*`, `backend/internal/outbox/adapters/postgres/repository.go`, `backend/migrations/postgres/000094_outbox_discarded_status.sql`, `apps/admin-web/app/(admin)/operations/dlq/*`, `apps/admin-web/features/operations-dlq/*`, `contracts/openapi/admin-api.yaml` |
| Infra wiring | Dev Terraform includes Pub/Sub, Cloud Run jobs, Cloud Scheduler, Cloud Tasks, services/IAM for kernel workers. | `infra/envs/dev/*.tf` |

## Trigger Matrix

| Trigger type | Runtime behavior |
| --- | --- |
| `birth_age` | SM-1 generation computes due date from DOB; goat without DOB is skipped as no due date. |
| `post_arrival` | SM-1 generation computes due date from entry date; goat without entry date is skipped as no due date. |
| `calendar` | SM-1 generation computes due from schedule calendar/offset basis. |
| `after_previous_completion` | Booster/follow-up only after accepted verification. It is not fired by initial SM-1 generation. |
| `manual_campaign` | Fires only through `POST /vaccination/manual-campaigns` or the `vaccination.manual_campaign.requested` event handler. |

## CEO Message Accuracy

| CEO note statement | Code-backed status |
| --- | --- |
| Approved config creates due list. | True for vaccination publish path, through durable retryable generation run and idempotent obligations. |
| Existing goats get obligations. | True through publish-triggered generation and CLI/backfill generation. |
| New goats get obligations automatically. | True when outbox relay/domain-event-consumer is running. |
| Due goats are clubbed into output-maximizing drives inside buffer. | True through generation -> obligation sweeper/batching -> Calendar projection; guarded by `make vaccination-drive-clubbing-guard`. |
| Operator follows SOP, uploads proof, verifier checks. | True through SOP submission bridge, recorded completions, verification queue, accept/reject/rework. |
| Approved completion updates stock and prepares booster. | True for accepted verification with batch stock gates/reservation/consume and `after_previous_completion` booster rules. |
| Rejected proof goes for correction/rework. | True through verification reject/rework behavior. |
| Draft/not approved rule creates no work. | True: only published versions are listed/generated. |
| Trusted vaccination history suppresses duplicate work. | True for trusted/imported evidence matching generation suppression rules. |
| Sick/ICU/quarantine is held with reason. | True only when the published rule includes defer states; otherwise the rule author did not request defer behavior. |
| Goat shift moves pending work. | True for open/unbatched and planned batched work. In-progress/completed drive migration remains explicit exception policy. |
| Goat dead/sold/exited cancels pending work. | True for admin exit events and procurement exclusion path. |
| Date passed shows overdue/missed. | True through calendar/process projections when workers/projectors run. |
| Stock missing/expired blocks work. | True for core drive reservation and batch-backed completion gates; stock ledger/balance updates are atomic. Override workflow is not implemented. |
| Booster only after verified previous dose. | True. |
| Missed/late work alerts/escalates. | True in kernel rows/sweepers/dispatchers; target channels require config/secrets/deployment. |

## Remaining Caveats

- Dev infra code must still be applied and smoke-tested in each target Google
  project. Code-backed wiring is not the same as verified prod/stg runtime.
- Pub/Sub DLQ redrive/import into the admin DLQ screen remains ops integration
  work. Outbox DLQ UI and idempotent repair actions are implemented.
- Incident integration is generic webhook-based. Vendor-specific two-way
  acknowledgement/resolution sync is not implemented.
- Full automatic migration of already in-progress or completed shed drives after
  goat shift is intentionally not silent. It needs explicit rework/exception
  policy because proof and stock may already be in-flight.
- Defer/recovery completeness depends on health/lifecycle producers emitting
  recovery events. The generation code honors defer config; producer coverage is
  the boundary to verify per workflow.
- Open old-version obligations after a new config/SOP publish need explicit
  continue/cancel/supersede/regenerate decisions. Completed history must never
  be silently rewritten.
- PPR/FMD/HS/BQ should not be claimed schedule-backed until approved source
  schedules are published for those protocols.
- Adverse reaction capture exists only where SOP/proof fields record it; an
  automatic Health follow-up/treatment obligation is still separate work unless
  a health vertical rule is added.

## Verification Run For This Ledger

Latest local full/guard pass on 2026-06-27:

```text
bash -lc 'MESHA_RTK_NOISY_COMMANDS=0 go test ./...'
```

Frontend guards passed:

```text
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run build
```

Contract/schema gates passed:

```text
make sqlc-generate
bash tools/agent-hooks/check-boundaries.sh
bash tools/agent-hooks/check-contract-drift.sh
make api-client-check
```
