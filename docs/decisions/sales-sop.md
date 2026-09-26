# Sales SOP: the sale is a workflow, authored on the web (maintainer instruction 2026-09-19)

Status: accepted; built on the SOP studio (`docs/decisions/sop-studio.md`) and the SOP-driven
herd operations (`docs/decisions/sop-driven-herd-operations.md`). Owner: tasks + sop + sales +
adminui + admin-web + Android. Migration `000369_sales_sop.sql`.

## What was asked

"We don't have a sales SOP. The sales SOP is hard-coded -- what steps we need to do, who will
have access, everything is hard-coded in mobile. Make everything SOP-driven, like #308: list and
flowchart, what questions, what tasks; if any task is not there, add it there."

What the phone ran before this: a fixed three-page Record sale form, a fixed Tag animals flow
(pick → weigh → confirm), receipts and a status word on the sale detail -- and NOTHING after the
sale was recorded that asked anyone to load the animals, photograph the gate pass or chase the
balance. Who could do what came only from the `/people` ticks.

## Decision 1: recording a sale opens ONE workflow from the `sales.deal` SOP

`sales.deal` ("Sale") is a **module** SOP (`kind = module`, `module_key = sales`) authored on
**Sales › Sales SOP** (`/sales/sops`), a module-surface route in the exact shape of `/counts/sops`
(recorded in the IA guard's allowlist by this decision). Its `follow_up.sales_deal` track is what
happens AFTER a sale is recorded. The same **List (default) | Flow** editor the herd operations use
edits it -- answer-driven branches, proof counts, schedules, insert-on-the-chart -- so a step the
farm is missing is added on the web, never in an APK.

The engine side is the existing opener, not a new one. `sales.Repository.CreateDeal` emits
`sales.deal.recorded` INSIDE the transaction that inserts the deal; `tasks/app.
SaleRecordedWorkflowHandler` opens `OpenSubjectWorkflow(template sales_deal, subject_ref_id =
the deal, park = the farm's park)`, compiled from the tenant's PUBLISHED version (the seeded
document when never authored), pinned on the workflow. No animal: the workflow is keyed on the
deal like a general run is keyed on its run id (`workflow_instances.subject_goat_id` NULL, template
key `sales_deal`, module `sales`). A redelivered event opens nothing.

The seeded v1 mirrors the old phone flow and adds the two tasks it never had:

| step | task type | owner | proof |
|---|---|---|---|
| Tag the animals sold | `sale_tag_animals` (engine hook) | Park Head | -- |
| Record the animals being loaded | Record video | Park Head | 1 video |
| Photo of the gate pass | Take photo | Park Head | 1 photo |
| Has the buyer paid in full? | Question (yes / no) | Procurement Director | -- |
| Collect the balance and record the receipt | Do & confirm, **only if** the answer is No | Procurement Director | -- |

Pinned byte-for-byte to `tasks/domain/sopseed/sales_deal.json` by `TestMigrationEmbedsTheSalesSeed`.

## Decision 2: WHO does a step is authored -- `owner` on every step

"Who will have access" is a property of the step, not of the APK. A follow-up step may carry
`owner: <designation_code>` from the farm's **designation catalog** (`designation_catalog`, the
same job titles `/people` pre-fills from: `park_head`, `operator`, `procurement_director`, ...).
The engine stamps it on the action row (`workflow_actions.owner_role`, 000366) and serves it with
the catalog's label (`owner_role` / `owner_label` on the step DTO).

- A caller who does not hold that designation (its active grant roles) sees the step **read-only**:
  `blocked = true, blocked_reason = for_other_role`, and the phone renders "For the Park Head".
- The write path refuses them: `403 step_for_other_role` on answer and complete
  (`domain.StepOwnedBy`, checked inside the same locked transaction as every other gate).
- Blank owner = anyone who can open the workflow. The **CEO/CXO floor** is never narrowed by an
  authored owner (`ceo_internal` passes every step), matching the request-path floor. An engine or
  CLI caller (nil roles) is never refused.
- At **publish**, an owner outside the active catalog is refused by path
  (`unknown_designation`); free text would lock a step for everyone. The catalog is read live
  (`sop/app.DesignationSource`), never a constant list, and the editor's "Done by" select is the
  `sop_step_owners` option group compiled from the same rows.

This is the reason the sale's routes are ORed with the sales permissions rather than a new one:
the sales desk (`sales.read` / `sales.write`) and the park head who tags (`sales.allocate_animals`)
each own steps of the SAME workflow, and the handler narrows per module
(`canReadWorkflowModule`) so a caller admitted on a sales permission alone cannot read a birth
card.

## Decision 3: the tag step is engine-completed, never a tap

`sale_tag_animals` is a new Task Type Registry row (seeded for every tenant by 000366 from
`sopseed/task_types_sales.json`; `task_types.json` is pinned to 000308 and is never edited) whose
engine hook the sale SOP must keep (`engine_step_removed` at publish, the `tag_kid` shape). The
phone deep-links the step to the existing sale-tagging screen (`/sale-tagging/sale/{deal}`, from
the card's `subject_ref_id`); the **allocation confirm** completes it --
`tasks/app.SaleAllocatedWorkflowHandler` consumes `goat.sale_allocated` (the event identity
already emits per confirm) and runs `CompleteSaleTagStep`, idempotent on the step. A completion by
hand is refused `409 sale_tagging_pending`, so a sale can never read "animals tagged" with no
animal on it. A confirm for a sale whose workflow is not open yet is a no-op; the opener stamps the
step and the next confirm (or a replay) completes it.

Both consumers are registered in `eventwiring.RegisterWorkflowConsumers`, so the API in-process
bus, `kernelstages`, `domain-event-consumer` and `outbox-relay` all run them. Registry entries:
`context/architecture/domain-event-registry.json` (`sales.deal.recorded`, and the second consumer
on `goat.sale_allocated`).

## What did NOT change

- The Record sale capture form (sale / buyer / money) and the tagging screen's fields stay
  canonical: they are the sale's DATA ENTRY, validated by the sales module, not steps. Adding
  SOP-authored extra questions to the capture form is the same phase-2 item the herd operations
  carried (`docs/decisions/sop-driven-herd-operations.md` → capture-form parity).
- Receipts and the status word are still edited on the sale detail; the SOP's payment question
  asks the desk to confirm the money, it does not move it.
- Who can OPEN Sales at all is still `/people` (the per-person page-access model). The SOP decides
  who does each step inside a sale; `/people` decides who reaches sales at all. Both halves are
  needed and neither replaces the other.
- Weighing stays isolated; nothing here reads a weighing table. The tasks card's `sales_deals`
  join is a 1:0..1 display enrichment on the deal's primary key (buyer, animal count, farm),
  exactly like the `goats` join beside it; the engine reads no sale to decide anything.

## Reads and routes

- `GET /app/workflows?module=sales` -- the day's sale workflows (chips, keyset), for anyone holding
  a sales permission or counts.write.
- `GET /app/workflows/subject?template_key=sales_deal&subject_ref_id=<deal>` -- the sale's own
  workflow, what the sale detail screen shows; `404 workflow_not_found` until the recorded event has
  opened it (the phone says "Steps will appear once the sale is synced").
- Cards carry `subject_ref_id` and `subject_label` ("Kumar Traders · 12 animals · CBE"); the detail's
  facts add Buyer and Animals.

## Pinned by

- `tasks/domain`: `TestMigrationEmbedsTheSalesSeed`, `TestStepOwnedByHonoursTheAuthoredDesignation`,
  `TestCompileTrackStampsTheOwner`, `TestTaskTypeRegistryCoversEveryHookAndAnswerKind` (the new hook).
- `sop/app`: `TestSalesSOPContract` (seed saves; tag step cannot be removed; unknown designation
  refused by path; blank owner is anyone).
- `tasks/adapters/postgres`: `TestSaleWorkflowRunsTheSalesSOP` on real Postgres -- goat-less open
  keyed on the deal, idempotent redelivery, owners stamped with labels, park head refused on the
  tag tap, operator refused on the park head's step, engine completion + replay + no-workflow
  no-op, CEO floor, desk-only money question, YES skips the balance step, module=sales list and
  subject read. Mutation-tested: dropping the owner gate on the complete path turns it red.

## Addendum 2026-09-19: the market survey stays on Sales Config; "Reported by" joins it there

Asked and answered the same day: the market survey (cities, questions with units, call time) is
Sales Config's, not the Sales SOP page's -- "keep it there only". What was added is the
**Reported by** card beside it: WHO makes the morning calls (the people holding the
`market_survey` phone module), given or taken back in place. `GET /market/reporters`,
`PUT /market/reporters/{person_id}` on `sales.market.config.write`; the toggle is a
read-modify-write through the person-access service `/people` uses (one write path, audited,
row_version-fenced); a person with no park yet is refused with "set this person's park on People
first". The Sales SOP page is the sale's steps and nothing else.

## Addendum 2026-09-19: the vendor form is authored -- the `sales.vendor` SOP

Maintainer instruction, same day: "in future I want to add any vendor, any data, any optional,
anything -- rendered from SOP, reflecting on mobile on the spot". Confirmed missing: the Add / Edit
vendor form was hard-coded three ways (`VendorWrite` in Go, the phone's `VendorField` wizard, the
web drawer's field list). It is now DATA.

**What the form is.** `form_dsl.vendor_form` of the published `sales.vendor` SOP: pages of
questions (`choice` / `multi` / `text` / `number`, each with title, hint, compulsory flag, choices,
min/max/unit, `only_if` on an earlier pick-one). Authored on `/sales/sops` through the same pages
editor the procurement inspection uses (profile `vendor_form`: no load form, no media). Migration
`000368` seeds v1 mirroring the phone's three steps -- pinned byte for byte by
`TestMigrationEmbedsTheSeededVendorForm`.

**Typed questions are the register's own columns.** `business_name`, `record_type`, `state`,
`status`, `city`, `phone_number`, `price_per_goat`, ... keep a LOCKED id and kind
(`domain.lockedVendorQuestions`); the catalog-backed ones (`record_type`, `state`, `status`,
`capacity_unit`, `supply_frequency`, `feed`, `breed`) carry `catalog: <kind>` and get their
choices from the vendor catalog at compile time -- record types narrowed by register side, exactly
as `/procurement/vendor-catalog` does. Four identity questions (`business_name`, `record_type`,
`state`, `status`) must stay present AND compulsory; publish refuses a document without them
(`VendorFormSOPContract`, registered on sop/app). Everything else -- wording, hint, order, page,
optional/compulsory, and any NEW question -- is the author's.

**Where answers live.** `GET /procurement/vendor-form?side=` serves the compiled form with its
`version`. A form-driven client sends EVERY asked answer keyed by question id (blank included --
the write is a REPLACE) plus `questionnaire_version`; the service loads THAT version
(`VendorFormSource.VendorFormVersion`, published or retired), checks the answers against it
(`ValidateVendorAnswers`: compulsory, offered choices, "other" text, number range, unknown id,
questions hidden by `only_if` not owed), maps the typed ones onto the columns
(`ApplyVendorAnswers`) and stores the rest in `procurement_vendors.sop_answers` with the version.
A version the library no longer serves is refused `409 vendor_form_changed` -- reopen the form. A
typed-only client (an older APK, the importer) sends no answers, and the stored extras are
PRESERVED on its replace, the way finance is preserved for a caller who cannot read it. The
single-vendor reads carry `answer_rows` labelled by the form the vendor was answered on; an
answer whose question was since removed lists under its id rather than vanishing.

**Both screens render the form.** The web drawer (`vendor-form-fields.tsx`) draws one section per
page and one control per question, conditions live, and carries typed columns the form does not
ask as hidden inputs so the replace cannot blank them. The phone wizard is one step per page
(`FormPageStep`), same rules, voice-note slot on the last page, refreshed on every open so a
publish on the web is what the next Add vendor asks; the cached copy keeps it usable offline.
Publish v2 in Chrome, open Add vendor on the web and the phone: both asked the new questions on
the spot.

Pinned by `procurement/domain.TestValidateVendorFormRefusesWhatTheRegisterCannotRun`,
`TestValidateVendorAnswersRefusesEachBadShape`, `TestApplyVendorAnswersSplitsTypedFromExtras`,
`procurement/app` `vendor_form_service_test.go` (typed/extras split, missing required extra names
its field, stale version refused, typed-only update preserves, side narrowing) and the OCI Postgres
round trip `TestVendorFormAnswersRoundTripAndSurviveATypedOnlyUpdate`; web
`vendor-form-model.test.mjs`; Android `VendorFormAnswersTest`.

## A sale without animals (maintainer decision 2026-09-25)

Every recorded sale opened the `sales_deal` workflow with "Tag the animals sold" on it, but the
tagging confirm refuses a deal with no animal count -- so a manure, feed or other-item sale, or an
animal sale recorded with a blank head count, carried a step nobody could finish and a card that
stayed open and overdue forever.

- New step condition **`sale_has_animals`** (`tasks/domain.StepWhenSaleHasAnimals`, compiled from
  `CompileOptions.SaleHasAnimals`, the `kid_pen_unresolved` shape). It is refused outside a sale
  track, and a sale track must keep at least one step without it, so a sale with no animals still
  opens with its money steps.
- The seeded document puts `"when": "sale_has_animals"` on **tag_animals, loading_video and
  dispatch_note** (the gate pass). The tag step stays a REQUIRED engine step: a conditional tag
  step is still present, so the document publishes (`engine_step_removed` still fires if it is
  deleted). Migration `000432` adds the condition IN PLACE to each tenant's published version
  (`000369` is untouched; checksummed on STG).
- **Who decides:** the producer. `sales.deal.recorded` carries `has_live_animals`, computed from the
  deal's LINES inside the recording transaction (`sales/domain.DealWrite.HasLiveAnimals`): an
  animal-kind line whose head counts add up to one whole animal -- the same whole-animal test the
  tagging gate applies. Never the deal-level `animal_count`: legacy manure deals carry `1` there.
  An event written before the key existed opens every step, as before.
- A kept step that waits on a dropped step (`requires`, an `after_step` schedule) is released
  rather than left waiting forever; a branch off a dropped question is dropped with it.
- On `/sales/sops` the step editor offers the condition as **"Only when the sale has animals"**
  (option group `sop_step_conditions_sales`, backend copy).
- Migration `000433` repairs workflows already open: the unfinished tag / loading / gate-pass steps
  of a sale with no live animals are skipped and the card recomputed.

**Not done: a sale EDITED after it opened.** There is no edit path for a deal's lines today (a sale
is recorded, then only its status and receipts change), so nothing can gain or lose animals after
the workflow opens. When a line edit is built, it must emit an event the tasks module consumes to
skip the three steps (animals lost) or append them (animals gained) -- the engine has no
append-steps-from-SOP call yet (only the birth capture re-shoot appends), so that half is its own
piece of work.

## A failed sale (maintainer decisions 2026-09-25)

"Deal Failed" only wrote an audit row; the sale's workflow stayed open and its tagged animals stayed
sold. Three decisions, the same day:

1. **A failed sale gives its animals back.** Every animal tagged to the deal goes back to alive in
   the pen it was in.
2. (No staging audit was needed.)
3. **Deal Failed is final.** A failed deal cannot be moved to any other status; to sell those
   animals the desk records a NEW sale.

How it runs:

- `sales.Repository.SetDealStatus` refuses a move out of Deal Failed under the row lock (`409
  sale_deal_failed_is_final`, farm copy "This sale is marked failed, and a failed sale stays failed.
  To sell these animals, record a new sale."). The service refuses before asking the feed store
  anything. The deal payload carries backend-decided `status_options` -- every status for a live
  deal, `[]` for a failed one -- and the web drawer and the phone sale detail offer exactly that
  list, hiding the status editor when it is empty.
- On an actual change it emits **`sales.deal.status_changed`** (with `actor_id`) in the same
  transaction. Two consumers, both registered in every bus process:
  - **tasks** (`SaleStatusChangedWorkflowHandler`): cancels the open sale workflow -- unfinished
    steps cancelled, finished steps kept, card closed; a write to a cancelled step is `409
    action_canceled`.
  - **identity** (`SaleFailedReleaseHandler` -> `Repository.ReleaseSaleAllocations`, registered by
    `eventwiring.RegisterSaleReleaseConsumers`): in ONE transaction, under the same per-sale lock the
    tagging confirm takes, every `goat_sale_allocations` row still `tagged` becomes `released`
    (released_at / released_by = who failed it, else who tagged / release_reason; never deleted),
    and each animal still `sold` goes back to `alive` with `exit_reason` and `exited_at` cleared.
    That is ALL the tagging changed: the canonical exit never moved the animal, so it is in the pen
    and partition it was sold from. Each animal gets its own identity decision (`identity_goat` /
    `goat_reinstated`), identity event, audit row and **`goat.reinstated`** outbox event.
    Idempotent: a replay finds nothing tagged.
- The event is durable and the release idempotent, so there is no window in which a deal is
  failed and its animals are sold without a delivery that will release them.
- The tagging confirm locks the same deal row and refuses a failed deal (`409 sale_deal_failed`).

What each module does when the animals come back:

- **Counts, herd register, feed projection:** read live `goats` (the herd register through its own
  trigger), so the animals are back in their pen's head count the moment the release commits.
- **Sold-animal reads** (Sold page counts and weight bands, Load wise, Farm born, the sale's own
  tagged list, the Feed Director's 07:00 reduce reminder): all read `status = 'tagged'` only, so the
  released animals drop out.
- **Vaccination:** `GoatReinstatedHandler` runs the ordinary per-goat generation with
  `returnedToHerd`. ONLY in that run does an obligation the exit cancelled (`ineligible_after_exit`)
  mint a successor, so the kernel re-owes the work the sale's exit cancelled; every other run keeps
  the exit cancellation terminal.
- **Feed Director:** one message, below ("A failed sale tells the Feed Director").

Not done: a sale failed BEFORE this change keeps its animals sold (the release runs on the status
event). If any exist, re-emitting the event for those deals is a one-off repair.

Pinned by `tasks/domain.TestSaleWithoutAnimalsOpensOnlyThePaymentSteps` and siblings,
`tasks/app.TestSaleRecordedCarriesWhetherTheSaleHasAnimals`, `TestDealFailedCancelsTheSaleWorkflow`,
`sales/domain.TestHasLiveAnimalsReadsTheAnimalLinesHeadCount`, and on real Postgres
`TestSaleWithoutAnimalsOpensWithoutAnUnfinishableTagStep` (runs 000432's own SQL),
`TestDealFailedCancelsItsSaleWorkflow`, `TestRepair000433UnsticksExistingSaleWorkflows`,
`identity/adapters/postgres.TestAFailedSaleReleasesItsTaggedAnimalsBackIntoTheirPens` and
`TestADealFailedIsFinalAndTakesNoAnimals`, `vaccination/app.TestReturnedToHerdRunReOwesTheWorkItsExitCancelled`,
`sales/app.TestDealFailedIsFinal`, `identity/app.TestOnlyAFailedDealReleasesItsAnimals` -- each
mutation-tested when written.

## Three more sale rules (maintainer decisions 2026-09-25)

### 1. The stock question is asked only at the close

An OPEN feed sale (In Discussion, Advance Paid) takes nothing off the feed store until it closes,
so recording one no longer asks "the store is short, confirm?". The question is asked once, when
the sale closes: at record for a sale recorded as Deal Closed (or with no status, which records
Deal Closed), or at the status change to Deal Closed. `sales/app.SalesService.confirmFeedStock`;
pinned by `TestRecordingAnOpenFeedSaleNeverAsksTheStore`.

### 2. A closed sale uses the close date

Closing an open deal stamps TODAY's business date (Asia/Kolkata, the server's clock -- never the
client's) as its `sale_date`, in the same statement as the status change, so its revenue and the
feed store's depletion (`feed_sale_depletions.feed_day` follows `sale_date`) land on the day the
sale actually happened. The date it was recorded for is kept in `sales_deals.planned_sale_date`
(migration 000434), written on the FIRST close only, served on the deal payload as
`planned_sale_date`; the audit row carries both dates. A re-close changes nothing. A sale recorded
already Deal Closed keeps its typed date -- that is its sale date -- and has no planned date.

The record-time date rule is now held on the server too: a sale recorded as closed cannot be dated
after today, and no sale of any status beyond the 60-day window (`domain.MaxSaleDateDaysAhead`) the
web drawer already offered. Pinned by `TestClosingASaleStampsTheCloseDateAndKeepsThePlannedOne`
(Postgres) and `TestSaleDateWindowAtRecord`.

### 3. A failed sale tells the Feed Director

When a failed sale releases animals back into their pens, the release transaction emits ONE
`goat.sale_released` per deal with the pens, and `notificationbridge.SaleFeedReduceNotifier` (the
same notifier as the sale notice, stored-audience wired in every durable bus) sends ONE push:

> Sale to Kumar Traders failed: 5 animals back in their pens
> The sale to Kumar Traders failed on 25/09/2026. 5 animals are back in CBE Castro 1 (3) and CBE
> Mandela 1 - Part 2 (2). Feed these pens as before from 27/09/2026.

Pens are park-qualified operational names (oploc); dates DD/MM/YYYY; the feed day is
`feeddirection/domain.SaleFeedReductionDay` on the pens' park correction clock at the release
instant -- the same clock the sale notice reads. Audience: its own catalog key
`feed.sale_failed_return` (default: the Feed Director), editable on People / HRMS ->
Notifications. Event key `feed.sale_failed_return:<deal>`; notification type
`feed_sale_failed_return` (migration 000435 widens the closed type list). A failed sale with no
tagged animals releases nothing and sends nothing. Pinned by
`TestFailedSaleTellsTheFeedDirectorWhichPensFeedAsBefore` (mutation-tested on the audience key)
and the Postgres release test.

## A planned sale's work is due on its sale day (2026-09-26)

Every `sales.deal` step is authored "immediately", and immediately counted from the RECORDING
instant, so an In Discussion / Advance Paid sale planned for a later day read "Tag the animals sold
· Overdue" the moment it was saved. This is a clock anchor, not an authoring change: the SOP still
says "immediately", and the sale workflow now counts it from the sale's own business day
(`tasks/domain.SaleClockAnchor`, Asia/Kolkata days, never hours). A sale dated after the day it is
recorded anchors at 00:00 IST of its sale date; a sale dated that day or earlier anchors on the
recording exactly as before. The anchor is stored in `workflow_instances.clock_anchor_at` (migration
000443, NULL = event_at, which is every other workflow); the card's `event_at` stays the recording
moment, so it still lists on the day it was recorded.

When the sale CLOSES, its date is restamped to the close day (rule 2 above), and
`sales.deal.status_changed` now carries that `sale_date`. A workflow anchored on the planned day
moves every unfinished, clock-timed step (and the card's next due) by the amount its anchor moves,
so a sale closed early is due now; closed on its recording day it returns to the recording instant.
A workflow anchored on its recording is never moved, and a redelivered close finds the anchor
already where it belongs. (There is no path that edits an open sale's date; if one is added it must re-anchor the same way.)
Pinned by `TestPlannedSaleStepsAreDueOnTheSaleDateNotAtRecording` (Postgres),
`TestPlannedSaleClockAnchorsOnTheSaleDate` and `TestSaleClockAnchorIsTheSaleDayOnlyWhenItIsAhead`.
