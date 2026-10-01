# Procurement journey: data model, events, hooks, routes, permissions

Migration numbers are placeholders from `000464` (origin/main's last is `000463`) and will be
renumbered at landing. Every table carries `tenant_id`, timestamps and `row_version`; every
write carries an idempotency key and fingerprint in the same transaction as its side effects.

## New tables

### `procurement_requests` — the CXO's ask

| column | notes |
|---|---|
| `request_id` uuid PK, `request_no` int (per-tenant running "#12") | |
| `raised_by` user, `raised_at` | the CXO |
| `species`, `sex`, `purpose` | configured codes |
| `quantity` int | |
| `weight_min_kg`, `weight_max_kg` numeric | |
| `price_unit` (`per_kg_live` \| `per_animal`), `price_min`, `price_max` | ₹ |
| `destination_park_id` | locations |
| `needed_by` date | |
| `breed`, `notes` | |
| `sop_answers` jsonb, `questionnaire_version` | the authored request form, stamped (existing shape) |
| `status` | `open` \| `sourcing` \| `fulfilled` \| `closed_short` \| `cancelled` |
| `fulfilled_count` int | maintained by the request_fulfilled hook |

### `procurement_vendor_quotes` — the call sheet

| column | notes |
|---|---|
| `quote_id`, `request_id` | |
| `vendor_id` (nullable until a new vendor is saved), `vendor_name_typed` | |
| `offered_count`, `price_unit`, `price`, `avg_weight_kg`, `available_from` date | |
| `warmup_at_vendor` bool, `notes`, `voice_note_proof_ref` | |
| `shortlisted` bool, `recorded_by`, `recorded_at` | |

### `procurement_journeys` — the subject of stages 2–9

| column | notes |
|---|---|
| `journey_id` PK, `journey_no` int | "J-27" |
| `request_id`, `quote_id`, `vendor_id` | |
| `animal_purchase_load_id` | 1:1 with the existing `animal_purchase_loads` row the phone inspection uses |
| `purpose`, `profile_version_id`, `profile_snapshot` jsonb | decision 3 |
| `agreed_count`, `buffer_count`, `price_unit`, `agreed_rate`, `weight_min_kg`, `weight_max_kg` | from the fix screen |
| `terms_answers` jsonb, `terms_version` | NDA/terms checklist, authored |
| `planned_dispatch_on` date, `departed_at`, `arrived_at` | |
| `destination_park_id`, `destination_shed_id`, `destination_partition_label` | pen chosen at selection (operational-location rules apply: both halves, canonical composer) |
| `sight_removed_count`, `weighing_removed_count`, `health_rejected_count`, `accepted_count`, `loaded_count`, `arrived_count`, `lost_count` | the funnel, each written by its hook |
| `agreed_value` numeric | recomputed at selection and departure |
| `status` | `sourced` \| `verifying` \| `selecting` \| `approval_pending` \| `warming_up` \| `ready_to_load` \| `in_transit` \| `arrived` \| `closed` \| `rejected` \| `cancelled` |
| `transit_manager_user_id` | written by the pick step |
| `rejected_reason` | |

Status is a **projection of stage completion** written by the hooks, never by a client; the
card's chips read it.

### `procurement_journey_payment_milestones`

`milestone_id, journey_id, seq, label, percent, due_after_stage, amount_rupees (computed at
unlock), due_on, status (locked|due|paid), workflow_action_id`.

### `procurement_journey_payments` — the ledger

`payment_id, journey_id, milestone_id (nullable), paid_on, amount_rupees, mode (cash|bank|upi),
reference, note, receipt_proof_ref, recorded_by, recorded_at`. Mirrors `feed_purchase_payments`.

### `procurement_journey_vehicles`

`journey_id, transporter_vendor_id, vehicle_no, driver_name, driver_phone, capacity_animals,
agreed_charge_rupees, route_plan text, checklist_answers jsonb`.

### `procurement_journey_losses`

`loss_id, journey_id, candidate_id, goat_id (nullable), stage (weighing|warmup|loading|transit|arrival),
reason code (configured register: `removed_on_sight`, `underweight`, `sick`, `died`, …), note,
proof_ref, recorded_by, recorded_at`.

### `procurement_journey_profiles` (+ `_versions`)

The profile per purpose with the keys in `configuration.md`; versioned draft → published → retired.

### `animal_purchase_candidates` — extended, not replaced

Add `journey_id`, `stage` (`weighed` \| `inspected` \| `decided`), `verified_weight_kg`,
`weighed_at`, `goat_id` (written at tagging), `rfid` (written at tagging). `field_verdict` gains
`removed_at_weighing`. The existing inspection, media and decision columns are untouched.

### `workflow_actions` — engine extensions (see `engine-gaps.md`)

`owner_roles text[]` (replaces the single `owner_role`, backfilled), `owner_from_action_key`,
`assignee_user_id`, `anchor_key`, `series_until_action_key`.

## Events (all through the outbox, aggregate `procurement_journey` / `procurement_request`)

| event | emitted by | consumers |
|---|---|---|
| `procurement.request.raised` | request write | opener: request + sourcing tracks; push to Procurement Director/Manager |
| `procurement.journey.opened` | fix-vendor write | opener: stock verification + payments; `vendor_fixed` hook; creates the `animal_purchase_loads` row; push to CXO |
| `procurement.journey.stage_completed{stage}` | `OnWorkflowCompleted` for every journey template | next-stage opener(s); journey status projection |
| `procurement.journey.load_approved` / `.load_rejected` | approval write | `load_approval` hook; opens warm-up + transport prep / closes journey; push |
| `procurement.journey.departed` | departure step | `departure_recorded` hook, transit opener, status |
| `procurement.journey.arrived` | arrival step | `arrival_recorded` hook, ends transit series, arrival opener, push |
| `procurement.journey.animals_tagged` | tagging confirm | `journey_tag_animals` hook; per-animal `goat.created` (existing identity event) |
| `procurement.journey.animals_placed` | place_in_pen | per-animal `goat.location.changed` (existing), landed-cost lines |
| `procurement.journey.payment_recorded` | ledger write | `payment_milestone` hook |
| `procurement.journey.closed` | journey_closed | `request_fulfilled` hook; `procurement_pc_handoffs` + the existing PC handoff event |
| `procurement.transit_check_missed` | kernel lateness sweep over series actions | push |

Each is registered in `context/architecture/domain-event-registry.json` with producer, consumer,
replay/DLQ and E2E proof (the registration checklist in the memory file
`new-domain-event-registration-checklist`: envelope enums, outbox validator branch, registry,
wiring, test).

## Task types and engine hooks (new registry rows, `sopseed/task_types_procurement_journey.json`)

| key | answer_kind | engine_hook | completed by |
|---|---|---|---|
| `record_quotes` | none | — | hand, refused while the request has no quote |
| `fix_vendor` | none | `vendor_fixed` | `procurement.journey.opened` |
| `weigh_stock` | none | — | hand, refused while no candidate is weighed |
| `mark_removed` | none | — | hand, opens the removal pick |
| `inspect_candidates` | none | — | hand, refused while a weighed animal has neither inspection nor removal |
| `load_approval` | none | `load_approval` | `load_approved` / `load_rejected` |
| `arrange_vehicle` | none | — | hand, refused until the vehicle row exists |
| `pick_person` | `person` (new answer kind) | — | hand |
| `journey_tag_animals` | none | `journey_tag_animals` | tagging confirm |
| `departure_recorded` | none | `departure_recorded` | the step's own complete writes `departed_at` (hook + tap in one transaction) |
| `arrival_recorded` | none | `arrival_recorded` | same shape |
| `arrival_reconcile` | none | `arrival_reconcile` | engine, when loaded = arrived + losses |
| `place_in_pen` | none | `place_in_pen` | the step's complete, which moves the goats |
| `pc_handoff` | none | `pc_handoff` | engine |
| `payment_milestone` | none | `payment_milestone` | ledger reaching the amount |
| `request_fulfilled` | none | `request_fulfilled` | journeys closing |

Each hook follows the seven-point registration the sale established (constant, refusal case,
sentinel error, registry row + migration, eventbus handler in `tasks/app/procurement_journey_workflow.go`,
service + repository method in `subject_hook_reconciliation.go`, `eventwiring` registration,
`requiredEngineHookSteps` entry, template key in `TemplateKeyToSOP` / `SubjectKeyedTemplate` /
`TemplateLabel`).

## Routes

| route | permission | notes |
|---|---|---|
| `GET/POST /procurement/requests`, `GET /procurement/requests/{id}`, `POST .../close` | `procurement.request.read` / `.raise` | web + phone (`/app/...` mirror) |
| `GET/POST /procurement/requests/{id}/quotes`, `PUT .../quotes/{id}` | `procurement.sourcing.write` | |
| `POST /procurement/requests/{id}/fix-vendor` | `procurement.sourcing.fix` | creates the journey |
| `GET /procurement/journeys`, `GET /procurement/journeys/{id}` | `procurement.journey.read` | board + detail, keyset, chips from status |
| `POST /app/procurement/journeys/{id}/stock-weights` | `procurement.journey.execute` | free-flow rows |
| `POST /app/procurement/journeys/{id}/removals` | same | |
| `POST /procurement/journeys/{id}/approval` | `procurement.journey.approve` | approve / reject |
| `POST /procurement/journeys/{id}/vehicle` | `procurement.journey.execute` | |
| `POST /app/procurement/journeys/{id}/tagging` | same | RFID per candidate, confirm |
| `POST /procurement/journeys/{id}/payments`, `GET .../money` | `procurement.journey.pay` | ledger |
| `GET /procurement/journey-profiles`, `PUT .../{purpose}`, `POST .../{purpose}/publish` | `procurement.profile.write` | Configuration register |
| `GET /app/workflows?module=procurement` and `/subject?template_key=…` | existing | the stage cards |
| `GET /procurement/journeys/{id}/timeline` | `procurement.journey.read` | the web detail's stage timeline (one read, not per-stage fan-out) |

## Permissions (new constants, `permissions.go`)

| permission | procurement_manager | procurement_director | health_director | park_head | ceo_internal |
|---|---|---|---|---|---|
| `procurement.request.read` | ✓ | ✓ | | | ✓ |
| `procurement.request.raise` | | | | | ✓ |
| `procurement.sourcing.write` | ✓ | ✓ | | | ✓ |
| `procurement.sourcing.fix` | | ✓ | | | ✓ |
| `procurement.journey.read` | ✓ | ✓ | ✓ | ✓ (own park) | ✓ |
| `procurement.journey.execute` | ✓ | ✓ | | ✓ (own park) + the picked transit manager per journey | ✓ |
| `procurement.journey.approve` | | ✓ | | | ✓ |
| `procurement.journey.pay` | | ✓ | | | ✓ |
| `procurement.animal_purchase.decide` (existing) | | | **✓ (new)** | | ✓ |
| `procurement.profile.write` | | | | | ✓ |

The transit manager is a **per-journey grant**: `assignee_user_id` on the step row admits that
person to that journey's execute routes (`JourneyAssignee` check in the handler, inside the same
read as the owner gate), so an AM needs no standing procurement permission. Per-person module
ticks: `procurement` module gains levels View (read) / Do (execute + sourcing write) / Oversee
(approve + pay + fix); `animal_purchases` Oversee moves to the Health Director's default ticks
(migration backfill, the 000245 pattern), the CEO keeps it.

## Scale shape

- Every list is keyset-paged by `(status, updated_at, id)`; the board reads one summary row per
  journey from `procurement_journeys` (the funnel counts are columns, not computed).
- The transit series is bounded: `count ≤ 100` at mint, and the arrival hook cancels the
  unminted remainder in one `UPDATE … WHERE series_key = …`.
- Stage opening is one transaction per stage: compile, insert actions, emit; the opener is
  idempotent on `(template_key, subject_ref_id)` as today.
- The lateness sweep over `workflow_actions` (new, generic) is keyset-chunked `FOR UPDATE SKIP
  LOCKED` on `(due_at, action_id)` with `status = 'pending'`, the sweeper shape the obligation
  sweeper uses; it is the first consumer of workflow lateness and belongs to the kernel, not to
  procurement.
