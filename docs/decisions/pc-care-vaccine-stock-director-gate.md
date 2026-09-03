# Vaccine Stock: Operators Record, the PC Director Approves

Maintainer decision 2026-09-02, SUPERSEDING two halves of the 2026-08-27 per-vaccine stock
task shape: WHO records the fridge proof, and WHO judges it. The task grain (one task per
park × vaccine × drive-date−7, kernel-created from `vaccination_drive_assignments`) and the
proof set (one fridge photo + one fridge video, task-level) are unchanged.

## The decision

The PC Director cannot be in both farms. So:

1. **The park's own vaccination operators record the stock check.** The kernel reconciler
   (`backend/internal/pccare/adapters/postgres/inventory_vaccine_reconciler.go`) assigns each
   park's task to that park's vaccination operator pool — the same position/duty definition the
   drive planner's candidate query uses (active non-director positions scoped to the park whose
   duties execute `preventive_care`/`vaccination`), without the per-day absence/week-off
   filters, because the task is multi-assignee and lives for days. A park with NO such operator
   gets NO task and is reported loudly (`ParksMissingOperators`; the kernel stage logs an
   error on every pass) — a fallback assignee is never invented, per the vaccination
   no-operator-fallback rule.
2. **The PC Director sees every stock card read-only and judges the submitted proof.** His
   phone renders the approver face: the stock list across both parks (monitor read), cards not
   tappable for capture, and — once a task is submitted — the operators' photo/video with
   **Approve** and **Send back (with a mandatory reason)**. Approve completes the task on both
   state columns and emits `pc_care.task.completed`; a reject flips it to `rework` and the
   operators' card reopens.
3. **The tenant verifier never sees stock work.** This is the toxin-module approval-gate
   shape, NOT a Verification category: `inventory_vaccine` is no longer registered in the
   verifier queue registry (`verificationbridge.RegisterCategories` iterates
   `domain.VerifierReviewedCategories`), the pending-verification consumer AND the bridge both
   skip director-approved categories, and migration `000241` withdrew the already-pending
   verifier items at cutover. The verdict-exclusivity lock on `verification.verdict` is
   untouched.

## Authority

`pc_care.stock_approve`, granted to `pc_director` ONLY — not the CEO (he watches through the
monitor read, the toxin precedent one seat down), and never an operator: the person who filmed
the fridge must not accept their own evidence. It rides its own route,
`POST /app/pc-care/tasks/{task_id}/stock-verdict` (`appRecordPCCareStockVerdict`).

Both halves of the capability-gated lock are enforced:

- **Contract**: the `pc_care_stock_approve` bootstrap flag flips the stock tab to the
  approver face on Android (`Routes.PC_VACCINE_STOCK` renders the monitor list; the task
  drill's verdict bar renders only for the approve-capable viewer on a `pending_verification`
  task). Operators with the vaccination module get a new **Stock** capture tab
  (`vaccine_stock` contribution, `excludedPermission: pc_care.stock_approve` so the director —
  whose tab #1 is already swapped to Stock — never carries it twice).
- **Endpoint**: the stock-verdict route requires `pc_care.stock_approve`; the capture writes
  (`RegisterTaskProof`, `SubmitTask`) still require task-assignee membership, and the director
  is no longer an assignee (the reconciler strips director assignees off unfinished
  per-vaccine tasks, and migration `000241` did the one-time cutover).

## Semantics worth pinning

- A reject REQUIRES a reason (`422 reason_required`); it becomes `rework_reason`, rendered
  verbatim on the operators' card. An approve never carries one.
- The verdict is state-guarded and idempotent: repeating the verdict a task already carries
  echoes the task; a conflicting or premature verdict is `409 verdict_not_pending`. The
  approve path reuses `ApplyVerifiedTask` and the reject path `BounceTaskForRework` — one
  implementation for verifier-reviewed and director-approved categories alike, so there is no
  second apply path.
- `pc_care.task.pending_verification` still fires on submit (it is the durable record of the
  transition); only the verifier-item enqueue is skipped for director-approved categories.

## Pinned by

- `TestStockVerdictIsDirectorOnly`, `TestStockVerdictApproveCompletesTheTaskAsTheDirector`,
  `TestStockVerdictRejectRequiresAReasonAndBouncesToRework`,
  `TestStockVerdictIsIdempotentOnAReplayAndConflictsOtherwise`,
  `TestStockVerdictRefusesANonStockTask` (backend service);
- `TestRegisterCategoriesExcludesInventoryVaccine`,
  `TestEnqueueInventoryVaccineNeverCreatesAVerifierItem`,
  `TestPendingVerificationHandlerSkipsInventoryVaccine` (the three layers of the
  never-a-verifier-item rule);
- `TestReconcileInventoryVaccineTasksAssignsParkOperators` and its date/status matrix
  wrappers (Postgres, opt-in);
- Android `PcCareStockVerdictTest` (verdict bar offered only to the approver on a submitted
  task; reject requires a typed reason; an operator's raw event sends nothing).

## Known follow-up

No push notifies the director the moment an operator submits; he finds submitted cards on his
Stock tab. A `pc_care`-worded FCM to the PC Director on stock submit is a candidate follow-up
and must go through the notification-specificity contract when built.
