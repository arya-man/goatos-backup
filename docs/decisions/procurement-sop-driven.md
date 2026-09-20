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

## Decision 4: buying a feed load opens its own workflow -- and the toxin test is mapped to it

`procurement.feed_purchase_intake` ("Feed purchase") is the same shape one document over.
`procurement.Repository.CreateFeedPurchase` emits `procurement.feed_purchase.recorded` inside the
insert transaction and `tasks/app.FeedPurchaseRecordedWorkflowHandler` opens a workflow keyed on
the `feed_purchases` row.

`.recorded` is deliberately a SECOND event beside the existing `.reached` rather than a reuse of
it: reached is the load ARRIVING, whose consequence is stock and the aflatoxin test; recorded is
the load being BOUGHT, whose consequence is the work owed on it. A load bought today and reaching
on Friday emits both, three days apart, and each consumer reads the one it means.

| step | task type | owner | proof |
|---|---|---|---|
| Photo of the weighbridge slip | Take photo | Procurement Director | 1 photo |
| Mark the load as reached | `feed_purchase_reached` (engine hook) | Procurement Director | -- |
| Photo of the feed in the store | Take photo | Park Head | 1 photo |
| Aflatoxin test signed off | `toxin_test_accepted` (engine hook) | -- (the engine's) | -- |
| Has the vendor been paid in full? | Question (yes / no) | Procurement Director | -- |
| Settle the balance and record the payment | Do & confirm, **only if** the answer is No | Procurement Director | -- |

**TWO of its steps are the engine's, both for the same reason:** the fact each records already has
an owner elsewhere, and a tap would let the two disagree. The arrival step is completed by the
LEDGER's own delivery write (`procurement.feed_purchase.reached`); the aflatoxin step by an
ACCEPTED toxin round.

### The toxin mapping, one event wide

The toxin module announced NOTHING before this: a round began from a feed load reaching the farm
and ended on the CEO's screen, and no other part of the farm could tell whether a load had been
screened. `toxin.Repository.RecordVerdict` now emits `procurement.toxin_test.accepted` from inside
the verdict transaction, and that is the entire mapping the maintainer asked for.

**Only the ACCEPT is announced, and that is the point.** A rejected or Invalid round cancels itself
and mints a retest in the same transaction, so the load is still owed a test and its step must stay
open; announcing a reject would invite a consumer to read "we looked at it" as "it is done". The
strip OUTCOME rides along, because an accepted POSITIVE is a real result -- v1 flags the load and
does not block feeding -- so a reader tells a clean load from a flagged one by reading the outcome
rather than inferring it from the acceptance.

The toxin module keeps everything else: its round state machine, its server-clock wait gates, its
retest minting and its CEO/CXO-only verdict (maintainer choice 2026-09-20, "steps authored, toxin
engine keeps state").

## Decision 5: the toxin PROCEDURE is authored, and the engine still owns the round

How many steps the aflatoxin test has, what each one tells the tester to do, whether it is filmed
or photographed, how long the extract sits and which step each wait gates were SEVEN GO CONSTANTS
in `toxin/domain/task.go`. A kit change, a farm that centrifuges, or one word of a wrong
instruction meant a backend release. They are now `form_dsl.toxin` of the published
`procurement.toxin_test` SOP (`procedure.go`), authored on the same page as the rest.

**What is NOT authored, deliberately:** the round's state machine, the retest minting, the
CEO/CXO-only verdict, the reading vocabulary (Negative / Positive / Invalid) and the SERVER-CLOCK
enforcement of every gate. The document says what the procedure IS; the engine still decides what
happens when a strip comes back void, and no published version can change that. That separation is
what made it safe to open a medically-gated flow at all, and it is the maintainer's recorded choice
between the two ways of doing this.

**A round runs the procedure it was OPENED on.** `toxin_test_tasks.sop_version` is stamped at
creation (resolved IN SQL inside the writing transaction, so a version published a moment later
cannot be stamped) and never changes: publishing a shorter test this morning must not invalidate a
round opened yesterday, and a round that ran the old seven steps stays readable as the test it
actually was. A RETEST is new work and is minted on whatever is published then, so a corrected
instruction reaches the next attempt. Under the row lock the repository insists the procedure the
service resolved is still the round's (`ErrProcedureChanged`), so a publish between the read and
the lock is refused rather than quietly applied.

**Day one is the seven steps, exactly.** `TestSeededProcedureCompilesToTheLegacySteps` pins the
seeded document to `Steps()` step for step and gate for gate; `Steps()` stays in `task.go` as that
oracle and nothing in the write path calls it any more. A tenant that authors nothing runs it, and
so does a round whose version the library no longer carries -- an unreadable document falls back
rather than leaving a test nobody can finish.

**The validator is the medical safety.** `ValidateToxin` refuses at PUBLISH: steps not numbered
1..N in order (the phone, the completions table and every gate key on the number), a document
without exactly one reading step, a reading step that is not last (it is what ends the round), a
gate pointing forward or at a waiting row (it could never open), a waiting row with no duration,
and a step with no instruction. Each refusal is a way to strand a round mid-test, and each is
found by a publish rather than by a tester standing over a strip.

## Decision 6: the feed purchase FORM is authored too -- one engine, two profiles

What the Record feed purchase screens ask was hard-coded three ways (the Go write, the web drawer,
the phone), exactly as the vendor form was before September. It is now
`form_dsl.feed_purchase_form` of the published `procurement.feed_purchase_form` SOP.

**It reuses the vendor form's ENGINE rather than copying it.** Parsing, validation, compilation,
answer checking and the locked-id rule are one implementation, told by an `EntryFormProfile` which
section it lives in, which ids the module reads into typed columns, which ids must stay present
and compulsory, and which catalogs its choices may come from. A second copy of those rules would be
a second place for them to drift, and this file already carries five documents.

**The typed questions are the ledger's own columns** -- `purchase_date`, `farm_label`,
`feed_item_label`, `quantity_kg`, `vendor`, the four-way cost split, the payment pair, the arrival
pair. Their id and kind are LOCKED because the stock cards, the landed-rate arithmetic and the
aflatoxin task all read them; five of them must stay compulsory because every downstream read is
keyed on or divided by one. Their wording, hint, order and page are the author's, and any question
the farm adds lands in `feed_purchases.sop_answers` with the version it was answered on.

**The choices come from the ledger's own vocabularies**, resolved per request: the farms it buys
for, the ACTIVE feed catalog, and the two payment states the sheet has always carried. A feed
retired this morning stops being offered this afternoon, and a document can never carry a list
someone typed into it.

**Additive for older clients.** A write with no answers -- an APK from before the form existed --
is accepted on its typed fields exactly as it was. A write WITH answers must name the version it
rendered: it is judged against that exact version, and one the library never published is refused
rather than judged against a form nobody filled in.

## Not here (yet)

The source-entry load / landed-cost questions, the web drawer and phone rendering of the feed
purchase form's authored extras, and the phone's entry points into the two purchase workflows are
the rest of this programme.
