# Sales SOP: the sale is a workflow, authored on the web (maintainer instruction 2026-09-19)

Status: accepted; built on the SOP studio (`docs/decisions/sop-studio.md`) and the SOP-driven
herd operations (`docs/decisions/sop-driven-herd-operations.md`). Owner: tasks + sop + sales +
adminui + admin-web + Android. Migration `000362_sales_sop.sql`.

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
The engine stamps it on the action row (`workflow_actions.owner_role`, 000362) and serves it with
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

`sale_tag_animals` is a new Task Type Registry row (seeded for every tenant by 000362 from
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
