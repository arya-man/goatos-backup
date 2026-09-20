# Procurement is SOP-driven end to end (maintainer decision 2026-09-20)

Status: accepted; built on the SOP studio (`docs/decisions/sop-studio.md`), the SOP-driven herd
operations (`docs/decisions/sop-driven-herd-operations.md`) and the sale
(`docs/decisions/sales-sop.md`), which this follows step for step. Owner: tasks + sop +
animalpurchase + procurement + toxin + adminui + admin-web + Android.

## What was asked

"We need procurement like the sale. We have animal purchases like that; we need procurement for
all types -- what questions, what to render, what type of details we need, feed purchase,
anything, mapping it to toxin testing, everything end to end should be SOP driven. List and flow
chart both should be there."

Four choices were confirmed the same day: every procurement flow is in scope; the toxin test keeps
its own round state machine and gets its STEPS authored; recording a purchase (animal or feed)
OPENS a workflow the way recording a sale does; and the supply register gets its own vendor
document (`procurement.vendor`) rather than sharing the sales desk's.

## What was hard-coded before this

| Surface | Before |
|---|---|
| Animal purchase inspection + load form | already authored (`procurement.animal_purchase`, 2026-09-14) |
| What the desk and the farm DO with a load | nothing at all -- no step, no owner, no proof the animals arrived |
| Feed purchase entry form | hard-coded three ways (Go write, web drawer, phone) |
| Feed purchase steps (in transit -> reached -> payment) | status flips with no work attached |
| Toxin test | 7 steps, three wait gates and every instruction in `toxin/domain/task.go` |
| Supplier register questions | the sales desk's `sales.vendor` form |

## Decision 1: the supply register has its own form -- `procurement.vendor`

One register, two documents. Both halves of `procurement_vendors` rendered `sales.vendor`, so a
question the buying desk needed of a feed supplier stood in front of the sales desk asking a
butcher. The SIDE decides the document, and the side comes from the register's OWN data
(`procurement_vendor_catalog.register_side` for the row's record type), never from anything a
client sends. A caller that names no side keeps the pre-split document, so a legacy row still
reads the form it was written against. Both documents are validated by the same contract -- they
write one table, so a supply form that drops an identity question breaks the same row a sales one
would. Migration `000370` seeds the supply document from the SAME seed file the sales form ships,
so nothing moves until somebody edits it.

## Decision 2: opening a purchase load opens ONE workflow

`procurement.animal_purchase_intake` ("Animal purchase intake") is a **module** SOP
(`kind = module`, `module_key = procurement`) authored on **Procurement › Procurement SOP**, with
the same **List (default) | Flow** editor the herd operations, the sale and this page's other
documents use. `animalpurchase.Repository.CreateLoad` emits
`procurement.animal_purchase.load_recorded` INSIDE the insert transaction;
`tasks/app.AnimalPurchaseLoadRecordedWorkflowHandler` opens
`OpenSubjectWorkflow(template animal_purchase_intake, subject_ref_id = the load, park = the load's
own park)`. No animal: the workflow is keyed on the load exactly as the sale's is keyed on the
deal. A redelivered event opens nothing.

**Steps and forms are SEPARATE DOCUMENTS**, deliberately: `procurement.animal_purchase` keeps the
inspection and load forms, the intake SOP keeps the steps. That is the shape the sale already has
(`sales.deal` beside `sales.vendor`), and it is what lets each be edited and published without
touching the other.

The seeded v1 is the intake the farm runs, including the two things it had no record of at all --
that the animals were received, and in what condition:

| step | task type | owner | proof |
|---|---|---|---|
| Record the animals in this load | Do & confirm | Procurement Director | -- |
| Photo of the animals loaded | Take photo | Procurement Director | 1 photo |
| Decision on every animal | `animal_purchase_decision` (engine hook) | CEO / CXO | -- |
| Record the animals arriving | Record video | Park Head | 1 video |
| Unload and place the animals in their pen | Do & confirm | Park Head | -- |
| Was any animal hurt or sick on arrival? | Question (yes / no) | Park Head | -- |
| Tell the health team which animals arrived unwell | Do & confirm, **only if** the answer is Yes | Park Head | -- |

Pinned byte-for-byte to `tasks/domain/sopseed/procurement_animal_purchase_intake.json` by
`TestMigrationEmbedsTheProcurementSeed`.

## Decision 3: the decision step is the ENGINE's, and it waits for the last animal

The office decides animals ONE AT A TIME on its own screen. The step records that the work is
finished rather than asking anyone to say so twice, so `animal_purchase_decision` is a Task Type
Registry hook completed by `tasks/app.AnimalPurchaseDecidedWorkflowHandler` consuming
`procurement.animal_purchase.decided` -- and only when the decision just taken was the LAST one
owed.

The waiting count is the PRODUCER's, computed inside the decision transaction and already carried
on the payload (`load_pending`). The consumer never re-counts the load, so the step cannot
disagree with the row that moved. Two refusals fall out of it and both are pinned: a load still
holding a waiting animal leaves the step open, and a load where nothing has been decided at all
(every count zero) completes nothing -- a step that closed there would report a decision on an
empty load.

## Reads and routes

- `GET /app/workflows?module=procurement` -- the day's purchase-load work, for anyone holding an
  animal-purchase permission or `counts.write` (the park head who receives the animals).
- `GET /app/workflows/subject?template_key=animal_purchase_intake&subject_ref_id=<load>` -- the
  load's own workflow.
- The write gate is narrower than the read gate, as it is for the sale: read-only procurement
  access does not answer a step.

## Pinned by

- `tasks/domain`: `TestMigrationEmbedsTheProcurementSeed` (seed embedded verbatim, seven steps,
  the decision hook, every step owned, the arrival branch, the proof counts).
- `tasks/app`: `TestPurchaseLoadOpensAnAnimallessWorkflowKeyedOnTheLoad`,
  `TestDecisionStepWaitsForTheLastAnimal`.
- `procurement/domain` + `procurement/app`: `TestVendorFormRoutesToTheRegisterSidesOwnDocument`
  (mutation-tested), `TestVendorSideComesFromTheCatalogNotTheCaller`,
  `TestMigrationEmbedsTheSeededProcurementVendorForm`.

## Not here (yet)

The feed-purchase form and its workflow, the toxin test's authored steps, and the source-entry
load / landed-cost questions are the rest of this programme and land in the same document as they
ship.
