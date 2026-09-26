# SOP-Driven Herd Operations and Sales Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Herd Operations Are SOP-DRIVEN, and Every New Operational Feature Must Be (maintainer lock, 2026-09-13)

Read `docs/decisions/sop-driven-herd-operations.md` first. The operator's steps for a birth, a death,
a shifting completion and a reconcile card -- WHICH questions, WHICH proof (video x n, photo x n),
WHEN each step is due -- are compiled at workflow open from the PUBLISHED SOP's `follow_up` section
(`/counts/sops` -> Edit operator steps) and PINNED on the workflow. Publishing a new version changes
the NEXT workflow opened; a workflow already open keeps the steps it started with. `Z1+Z3`-style
rules of thumb apply here too: a question, a proof count or a time frame typed into Go or Kotlin for
one of these flows is a defect, not a shortcut. `tasks/domain/templates.go` is the golden ORACLE for
the seeded documents and nothing else; `make sop-driven-herd-operations-guard` blocks a production
call to it, a seed that drifted from migration 000304, and a step title hardcoded on the phone.

The framework (`goatOS_Config_Framework.pdf` v2) is DATA: the Category Registry (`sop_categories`)
and the Task Type Registry (`sop_task_types`, with `answer_kind` + `engine_hook` + `parameter_schema`)
are tenant rows served to the builder as option groups, never constants in contract code. A step
the SERVER must act on names a task type with an `engine_hook` (`weigh_kg`, `tag_kid`, `record_pen`,
`colostrum_feed`, `death_evidence`, `return_to_pen`); the hook is matched on the step KEY so a relabel
never detaches it, and the web editor keeps those steps' key and type fixed.

**Rule for every new operational feature from now on (Claude AND Codex):** the questions, proof
requirements and timing an operator sees are authored on the web and rendered by the phone; a new
module plugs into the same engine (a `follow_up` track with a template key, a completion hook for
its module, a verdict route for rework) rather than shipping its own hardcoded step list. If a
feature genuinely cannot yet run on the engine, say so in its decision doc and record the phase-2
item; do not hardcode quietly. Proof of a change to any of these flows is BOTH a web publish and a
phone run that shows the next workflow on the new version and an open one unchanged.

Capture-form parity (2026-09-16, done): Add birth / Add death take a SOP-authored capture card
(photos, videos, questions) and send it as `sop_capture`; with no card published they behave exactly
as before. Birth report proofs are verified ONE ITEM PER SLOT (`counts-birth-capture:<birth>:<slot>:<ref>`,
a reject re-shoots only that slot); death stays one bundle, now carrying the report media first and
EVERY authored step's proofs -- "death is exactly two videos" is retired, and a fixed pair on the
backend or phone is blocked by `sop-driven-herd-operations-guard`. Details and the deploy-day
differences: `docs/decisions/sop-driven-herd-operations.md` → "Capture-form parity".

Phase 2 (recorded, not done): shifting completion on the engine, a capture card on Raise shifting,
the `/config` editor for the two registries, cross-category `triggers`.

## The Sale Is A Workflow, Authored On /sales/sops (maintainer instruction 2026-09-19)

What happens AFTER a sale is recorded -- tag the animals, load them, photograph the gate pass,
settle the money -- and WHO does each step are no longer hard-coded on the phone. They are the
published `sales.deal` SOP (kind `module`, module `sales`), authored on **Sales › Sales SOP**
with the same List | Flow editor the herd operations use, and RUN by the tasks engine:
`sales.Repository.CreateDeal` emits `sales.deal.recorded` inside its own transaction and
`tasks/app.SaleRecordedWorkflowHandler` opens ONE workflow keyed on the deal (`subject_ref_id`,
no animal, template key `sales_deal`), pinned to the version in force. Publishing a new version
changes the next sale; a sale already recorded keeps the steps it started with.

Three rules bind future changes:

1. **WHO does a step is a property of the step.** `FollowUpStep.owner` is a designation code
   from `designation_catalog`, stamped on `workflow_actions.owner_role`; a caller who does not
   hold it sees the step read-only (`blocked_reason=for_other_role`) and the write path refuses
   them (`403 step_for_other_role`, `domain.StepOwnedBy`). The CEO floor is never narrowed; an
   engine caller (nil roles) is never refused; publish refuses an owner outside the catalog.
   `/people` still decides who reaches Sales at all -- the SOP decides who does each step inside
   a sale. Do not collapse the two.
2. **The tag-animals step is engine-completed, never a tap.** `sale_tag_animals` (task type +
   hook, seeded by 000366) is completed by `goat.sale_allocated` via
   `tasks/app.SaleAllocatedWorkflowHandler`; a by-hand completion is refused
   `409 sale_tagging_pending`, and a `sales.deal` version that drops the step cannot be published.
   The phone deep-links it to the tagging screen on the card's `subject_ref_id`.
3. **The Record sale form and the tagging screen's fields stay canonical data entry**, validated
   by the sales module; SOP-authored capture questions are the same phase-2 item the herd
   operations carry. The card's `sales_deals` join is a 1:0..1 display enrichment (buyer, animal
   count, farm); the engine reads no sale to decide anything.

Canonical prose: `docs/decisions/sales-sop.md`. Pinned by `TestMigrationEmbedsTheSalesSeed`,
`TestStepOwnedByHonoursTheAuthoredDesignation`, `TestSalesSOPContract` and, on real Postgres,
`TestSaleWorkflowRunsTheSalesSOP` (mutation-tested).

## Park Heads Tag Animals To A Sale, And Nothing Else Of Sales (maintainer decision 2026-09-11)

The SOP gives the `tag_animals` step to `park_head`; this is how a park head reaches it. The
capability module `sale_allocation` (`sales.allocate_animals` alone) is on BOTH surfaces; on the
phone it is its own module -- "Sales", one bar item "Tag animals" at `/sale-tagging` -- offered to
a person who holds the permission WITHOUT `sales.read`, and it follows the `/people` tick (ticking
it adds the module to anyone, clearing it removes it). The queue `/admin/goats/sale-tagging`
carries NO buyer and NO money. Every allocation route is clamped to the caller's park, for the
animals AND for the sale's own farm (`403 park_out_of_scope`). The phone tags by Bluetooth reader
or typed tag, takes each animal's weight (required) and NO price, and submits only when the sale is filled
exactly.
Migration `000443` also writes the tick for every park head already on /people and as the
`park_head` job default. Do not "simplify" by granting the park head `sales.read` so the queue
can show the buyer: that lights up the whole Sales module. Canonical prose:
`docs/decisions/sale-tagging-park-head.md`.
