# SOP studio: List and Flow views, answer-driven branches, two kinds of SOP (maintainer decisions 2026-09-18)

Status: accepted; phase 2 of the Configuration programme (`docs/decisions/configuration-items-and-settings.md`
recorded the constraint; this document is the build). Owner: tasks + sop + adminui + admin-web + Android.

## What was asked

"Do this for the SOPs we already built -- herd operations, weighing, feed. The default view should be
List, and one more should be the chart. In weighing there are two types, lump sum and normal; if I
want to add something only for lump sum I need to do it like that. Do it properly for whatever we have
built." Three choices confirmed the same day: answer-driven branches (not static only), per-module
pages with a List / Flow toggle (not one studio screen), and the SOP kind shipped with a first general
SOP.

## Decision 1: every SOP editor has two views, List (default) and Flow

`/counts/sops`, `/weighing/sops` and `/feed/sops` keep their editors; each gets a **List | Flow**
toggle in the page head (`?view=flow` deep-links the chart; the server reads it so SSR and the client
agree). Both views edit the SAME rows -- switching never loses an edit, and a step added on the chart
is the step the List shows. Flow is drawn, never dragged: the order the chart shows is the order the
phone runs, so there is no second truth to reconcile.

- **Herd Operations** (`follow_up.tracks[].steps[]`): Start, the steps down a spine, a **decision**
  under every question whose answer gates later steps, one column per branch condition, an
  "Otherwise" line, Finish. `+` on a line inserts a step THERE with that path's condition; "Add
  branch" on a decision starts a new column on the next unused answer. The properties panel is the
  List view's own step card.
- **Weighing** (`weighing` section): the session is not a step list, so the chart is its shape --
  plan, the removal card, a **"Weighed as"** decision with a **Per animal** column and a **Whole pen**
  column, submit, verify. A capture or question added on the Whole pen side applies to whole-pen
  weighs only: that is the lump-sum ask. The fixed scan-and-submit steps are drawn locked.
- **Feed** (`feed` cards): the cards in chain order (distribution → leftover; packing; transport),
  each with its captures and questions, then the verifier's review.

Shared pieces: `features/sops/flow-canvas.tsx` (boxes, curves, insert `+`, fork labels, zoom/fit),
`flow-layout.ts` (the pure follow-up layout, unit-tested), `branch-field.tsx`.

## Decision 2: answer-driven branches are an ENGINE feature

A follow-up step may carry `when_answer: {step, op, value[]}` (`eq | ne | in | not_in | gt | gte |
lt | lte`; a pick-many answer matches when any picked value does). The engine:

1. **Validates at publish** (`tasks/domain.ValidateFollowUp`, run by `sop/app` on every version):
   the question must be an EARLIER step of the same track that records an answer; the operator
   must fit its answer kind; every value must be one the question can produce.
2. **Stamps the condition on the action row** at open (`workflow_actions.answer_gate`, migration
   000350), so the branch is pinned with the version the workflow started on.
3. **Blocks** a gated step until its question is answered (`blocked_reason = awaiting_answer`, with a
   backend-composed `branch_note` such as "Only if “Is the kid clean?” is No").
4. **Settles the branch in the answer's own transaction** (`ResolveAnswerBranches`): a gated step
   the answer does not satisfy becomes **`skipped`** -- a NEW action status, off every count and every
   gate, never the next step, never served to the phone -- and a step gated on a skipped question
   goes with it. The taken branch simply stays pending. There is no third path: a re-answer is not
   possible (a completed question refuses a second write), so a branch is settled exactly once.

Why `skipped` and not `canceled`: canceled steps still sit in the completion total (the whole workflow
was withdrawn); a skipped step must NOT hold a run open. `RecomputeCard` skips them entirely, so the
card's total shrinks to the taken path and the run completes when that path is done. Pinned by
`tasks/domain/sop_branches_test.go` (mutation-tested two ways) and, on real Postgres,
`TestGeneralSOPRunBranchesOnTheAnswer`.

Weighing and feed questions keep their own `only_if` (question, value) visibility rule inside a card;
that is a card-local display rule the phone already runs, not a workflow branch, and the chart labels
it on the line the same way.

## Decision 3: two kinds of SOP, and the first general one

`sop_definitions.kind` is `module` (owned and run by a module: birth by Herd Operations, a session by
Weighing, a card by Feed) or `general` (farm-wide, tied to no module, started by hand); `module_key`
names the owner of a module-level SOP (migration 000351 backfills both from the code prefix). The kind
is a column, not a naming convention -- but the page decides it: a module SOP page authors module-level
SOPs, **Configuration › Work instructions** (`/configuration/work-instructions`) authors general ones,
and the builder shows the kind it will write. A general SOP's code is `general.<slug>`; its builder
steps become the `main` follow-up track the phone runs (no capture form).

A general run: `POST /app/workflows/start {sop_code}` under an `Idempotency-Key` (the key IS the run:
a retried tap never opens two) → a workflow keyed on that run id with **no animal**
(`workflow_instances.subject_goat_id` is nullable from 000351; template key `general:<code>`, module
`general`), compiled by the same opener the herd operations use, driven through the same workflow
routes. `GET /app/sops/general` lists what can be started.

The seeded first general SOP is the **Gate visitor check** (five steps, one answer-driven branch:
footwear disinfection and overshoes only when the visitor has been on another livestock farm this
week), published v1 for every tenant by 000351 and pinned byte-for-byte to
`tasks/domain/sopseed/general_gate_visitor_check.json`.

## What the phone shows

A step on a branch carries `branch_note` and, until its question is answered,
`blocked_reason = awaiting_answer`; the phone renders the note verbatim in the step's footer
(`WorkflowBranchNoteTest`). Skipped steps are never served, so nothing on the phone needs to know the
status exists. The Work instructions phone module (start a general SOP, see today's runs) is the next
slice of this programme.

## Pinned by

- `tasks/domain/sop_branches_test.go`, `sopseed_migration_test.go` (`TestMigrationEmbedsTheGeneralSeed`),
  `tasks/adapters/postgres/general_sop_integration_test.go`.
- `admin-web/features/sops/flow-layout.test.mjs`, `followup-model.test.mjs` (seeded documents still
  round-trip byte-for-byte with the branch fields present).
- `adminui/app` page tests (the Work instructions page reaches the CEO; `sop.read` on the leaf).
- Browser proof 2026-09-18 on the throwaway stack (API :8102 → `goatos_cfgqa`, admin-web :3401):
  Herd Operations Flow (select, insert on a line, branch on "Is the kid clean? is No", Add branch,
  List parity, draft saved through the server validator), Weighing Flow (question added on the Whole
  pen side only, capture on Per animal, List parity, draft saved), Feed Flow (question on the leftover
  card, capture on a distribution line, List parity), Work instructions page listing and opening the
  seeded general SOP in Flow with its branch; the same run driven over HTTP: NO hides the branch and
  the card reads 3 of 3, YES keeps all five.
