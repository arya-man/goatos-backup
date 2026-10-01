# Procurement journey: what the tasks/SOP engine must learn

Inventory taken 2026-10-01 against origin/main `21895dc6a`.

**Engine:** G1, G2, G3 and G6 are closed by the `procurement_journey` Temporal workflow (see
`docs/decisions/procurement-journey-orchestration-engine.md`); their original task-engine designs below are kept for reference
only. G4, G5, G7, G8 and G9 are unchanged build work in `backend/internal/tasks` and the SOP
studio. G1–G3 block stage 2 onward; G4–G6 can land beside the stages that need them.

## G1 — Workflow chaining: a completed workflow opens the next (blocks everything after stage 1)

**Status:** closed by the `procurement_journey` Temporal workflow (see `docs/decisions/procurement-journey-orchestration-engine.md`).

**Today:** `sop_definitions.triggers jsonb` exists and nothing reads it; `WithCompletionHook` has
one registration (counts reconcile). Every opener is a domain event.

**Build:** a generic completion consumer. `OnWorkflowCompleted` for any template whose SOP
definition carries `triggers.on_complete[]` emits `<module>.workflow.stage_completed{template_key,
subject_ref_id, subject_kind}` through the outbox, and a generic opener consumes it:
`triggers.on_complete[i] = {open: <template_key>, when: all_of[<template_key>…]}`. `when` is the
"both warm-up and transport prep must be done" gate: the opener records each completed
prerequisite in `workflow_subject_hook_receipts` (the existing monotonic receipt table) and opens
the target when the set is complete, idempotent on `(template, subject)` as today.

**Authored, bounded:** the chain is declared on the SOP definition (seeded), shown read-only on the
Flow view as a "then opens …" footer, and NOT editable by the farm in v1 (configuration.md: order
is the engine's). Publish refuses a cycle.

**Pinned by:** `TestStageCompletionOpensTheNextStage`, `TestTwoPrerequisitesOpenOnceWhenBothDone`
(either order, redelivery), mutation-tested by dropping the receipt check.

## G2 — Day-unit and date-anchored schedules (blocks stages 4, 5, 8)

**Status:** closed by the `procurement_journey` Temporal workflow (see `docs/decisions/procurement-journey-orchestration-engine.md`).

**Today:** `after_event{offset_minutes}`, `at_fixed_time{day_offset, time}`,
`after_step{offset_minutes}`, `series.from_event{interval_minutes, count}`. Minutes only; the only
anchor is `EventAt` or a prior step.

**Build:**
- `offset_days` beside `offset_minutes` everywhere (the editor shows days for ≥ 1 day).
- A new schedule kind `before_anchor{anchor, day_offset, time}` / `after_anchor{…}` where
  `anchor` is a named date the SUBJECT supplies at open (`OpenSubjectWorkflowInput.Anchors
  map[string]time.Time`): `planned_dispatch_on`, `load_approved_at`. `workflow_actions.anchor_key`
  records which; a subject that cannot supply a named anchor refuses the open
  (`anchor_missing`), never silently due-now.
- `series.daily{days: <int or {profile.key}>}` as sugar over `from_event` with `interval = 1440`.
- Template substitution: `{profile.*}`, `{journey.*}`, `{milestone.*}` in titles, details and
  schedule numbers, resolved from `OpenSubjectWorkflowInput.Vars`; publish validates every key
  against the module's declared var schema (`tasks/domain.VarSchema[module]`).

**Pinned by:** `TestBeforeAnchorDueDates`, `TestAnchorMissingRefusesOpen`,
`TestTemplateVarsResolveAtOpenAndRefuseUnknownAtPublish`.

## G3 — Series anchored on a step, ended by a step (blocks stage 7)

**Status:** closed by the `procurement_journey` Temporal workflow (see `docs/decisions/procurement-journey-orchestration-engine.md`).

**Today:** `series.from_event` runs a fixed `count` from `EventAt`; it cannot start at a step's
completion or stop on a condition.

**Build:** `series.from_step{step, interval_minutes, until_step, max_count ≤ 100}`. Rows are
minted when `step` completes (same transaction, `after_step` already sets due times there),
`until_step` completing cancels every unminted/undue row (`series_until_action_key` on the row,
one UPDATE). A late row is `pending` past `due_at + grace`; the kernel lateness sweep (G6) emits
the push. The phone sorts a series by `due_at` and shows "next check in 1h 40m" from the server
time it was served.

**Pinned by:** `TestSeriesStartsAtDepartureAndEndsAtArrival` (checks between are due at 3h
intervals, none after arrival), mutation-tested by dropping the cancel.

## G4 — Owners: a list, and a person picked in an earlier step (blocks stages 1, 3, 5, 6, 7)

**Today:** `owner` is one designation; the CEO floor; blank = anyone.

**Build:**
- `owner: [code, code]` (list; a single string still accepted). `StepOwnedBy` passes if the
  caller holds any. `workflow_actions.owner_roles text[]` with the old column backfilled.
- A `pick_person` task type with answer kind `person`: options are compiled at open from the
  people holding the designations in `options_from: {designations: [...], park: subject}` (reads
  `workforce_members` + active grants, the roster the leadership-tasks picker already uses). The
  answer is a `user_id`; the step's `assignee_user_id` and the subject's named column
  (`riding_am_user_id` or `transit_manager_user_id`, declared on the pick step as `writes_to`) are
  written in the answer's transaction. A document may carry several pick steps; each dependent
  step names which one it follows.
- `owner_from_step: <key>` on a step: at open the step carries `owner_from_action_key`; when the
  pick is answered, every dependent step gets `assignee_user_id` in the same transaction; until
  then those steps are blocked `awaiting_assignee`. The owner gate passes the assignee OR the
  listed designations OR the CEO floor.
- Publish rule (per-SOP, declared with the definition): `assignee_required` — a document must
  contain the named pick steps and every other step must name one of them. For
  `procurement.transit` that is two picks: the riding AM for field steps and the transit manager
  for monitoring steps. This is "every transit has a transit manager".
- A step minted by the lateness sweep (G6) for a late sibling (`on_late: {open: <key>, owner_from_step}`)
  so "check 3 is overdue, contact the AM" is a real step for the transit manager, not only a push.

**Pinned by:** `TestStepOwnedByAnyOfListedDesignations`, `TestPickedPersonOwnsDependentSteps`,
`TestTransitDocumentRefusedWithoutAPick` (mutation-tested).

## G5 — Repeatable ledgers as steps, and hand-complete refusals with a reason (stages 1, 2, 3, 5)

**Today:** a step completes by hand (any gate) or by engine. There is no "complete by hand, but
only once the ledger has rows" shape; `parameter_schema` on task types is stored and unread.

**Build:** a `completion_guard` on the task type (`parameter_schema` finally read): a named
predicate the repository evaluates under the row lock before a hand-complete —
`quotes_exist`, `stock_weighed`, `candidates_inspected_or_removed`, `vehicle_recorded`. A failed
guard returns `409 <guard>_pending` with backend copy ("Record at least one quote first"). The
phone renders the step with a deep link (`opens: <route-template>` on the task type, the
`opensSaleTagging` shape generalised to a route the backend composes) and the guard's sentence.

**Pinned by:** `TestGuardedStepRefusesHandCompleteUntilLedgerHasRows` per guard.

## G6 — Lateness is a kernel sweep with a push, not a label (stages 5, 7, 9)

**Status:** closed by the `procurement_journey` Temporal workflow (see `docs/decisions/procurement-journey-orchestration-engine.md`).

**Today:** an overdue `workflow_actions` row is only a "late" chip; no consumer reads lateness.

**Build:** a generic `workflow-lateness` kernel stage (`kernelstages`): keyset over
`workflow_actions WHERE status='pending' AND due_at + grace < now() AND late_notified_at IS
NULL`, emits `<module>.step_late{template, action, subject}` once per row, which the
notificationbridge maps through the audience catalog (`procurement.transit_check_missed`,
`procurement.milestone_overdue`, `procurement.prep_late`). This is the kernel's "owner + clock →
contact waterfall" item for workflow steps; the escalation ladder beyond the first push stays on
the kernel plan.

**Pinned by:** `TestLateStepNotifiesOnce` (idempotent across sweeps).

## G7 — Per-step verification for a non-birth template (stages 6, 7, 8; can follow v1)

**Today:** `ReviewedPerStep` is true only for birth. Procurement steps enqueue nothing.

**Build:** `review: per_step` on the SOP definition (not per step) so loading, transit and
arrival videos reach the verifier under a `procurement_evidence` category with its own audience
row; the birth per-step machinery (`HoldStepForReview`, `ApplyStepVerdict`, the item key shape)
is reused unchanged. v1 of the journey ships WITHOUT review (the office watches the journey card
itself); switching it on is a definition flag plus the catalog row.

## G8 — The Work Board procurement source (stage 2 onward)

**Today:** `ModuleProcurement` exists, visibility is gated, and the lane is hidden because no
source exists.

**Build:** `procurement/adapters/boardsource`: one row per journey in a non-terminal status,
"J-27 · Kumar Farms · 72 sheep" subtitled with the current stage and its next due step, href to
the journey detail. Visible on `procurement.journey.read` or `leadership_tasks.read`.

## G9 — SOP studio: author what the above adds

- Owner select becomes multi-select; a "Done by the person picked in …" option listing the
  document's pick steps.
- Schedule editor: days unit, `before/after <anchor>` with the module's anchor list, `daily for
  {profile.key}` and `from step … until step …` series.
- Flow view: a locked "then opens <stage>" footer; the pick step drawn as a person icon; series
  drawn as one repeated box with the interval on the line.
- Task type picker shows the deep-link task types with their guard sentence.
- `{profile.…}` autocomplete in title/detail fields from the module's var schema; unknown key is
  an inline error before the server refuses it.

All of G9 is proven at laptop 1440 and phone 390 with the responsive guard, per the admin-web
rule.

## What is NOT an engine change

- The stock weighing, tagging, fix-vendor, load approval, vehicle and ledger screens are
  procurement's own write paths with their own tables; the engine only learns (G5) to refuse a
  hand-complete until they have rows and to deep-link to them.
- The journey status column is a projection written by the `procurement_journey` workflow
  (`RecordJourneyEvent`); the task engine does not know what "in_transit" means.
- Every clock a person sees is a kernel `workflow_actions.due_at` / `available_at` and every push a
  catalog row; the Temporal workflow holds no timer that decides when a person sees work or is late.
