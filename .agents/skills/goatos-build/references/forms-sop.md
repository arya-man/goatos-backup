# Forms And SOP Reference

Load this when working on SOP forms, form builder/editor, native mobile runner,
task lifecycle, weekly assignment, verification, or Slack-form replacement.

Canonical docs:

- `context/forms/final-forms-sop-engine.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/protocol-engine/state-machines.md`
- `docs/preventive-care-vaccination/PRD.md`
- `docs/preventive-care-vaccination/TRD.md`
- `context/source-findings/drive-docs-findings.md`
- `context/execution/next-contracts.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/execution/sop-vaccination-backend-handoff.md`
- `context/execution/vaccination-process-integrity-backend-handoff.md`
- `context/source-findings/feed-direction-legacy-system-reference.md`
- `docs/feed-direction/SOP-MOBILE-CUTOVER-PRD.md`
- `docs/feed-direction/SOP-MOBILE-CUTOVER-TRD.md`
- `docs/decisions/task-timing-alerting-violations-and-appeals.md`

Rules:

- Goat OS owns the form DSL and runtime contract.
- Conditions are declarative and deterministic, not arbitrary JavaScript.
- Admins create forms; operators execute assigned tasks on Android.
- Native runner evaluates the same DSL offline.
- Every procedure step declares whether its clock is availability, planned,
  flexible, hard operational, clinical-safe, or not applicable. Clients never
  infer a deadline from a session label or schedule. A hard breach is only
  evidence; violation attribution, appeal, decision, and HR action remain
  separate governed workflows, and the task/SOP engine never edits payroll.
- Server revalidates every submission against form version, permissions, live
  state, idempotency, and workflow gates.
- `repeat_for_each_goat` is a first-class batch semantic, not a normal field.
- Slack forms are legacy discovery/migration input only.
- Feed Direction Slack prompts are captured in the canonical Feed legacy
  reference. Packing, transport, distribution/consumption, water, wastage,
  verification, rework, and emergency bridge must render as versioned Android
  SOP tasks on the same kernel. Default and custom-composition sheds share one
  execution path.
- Legacy Slack schemas are now captured canonically: death, shifting, birth/
  abortion, health diagnosis/follow-up, not-eating, proof policy, correction
  and rectification behavior.
- Old operator-management feature specs were deleted from the active tree. Use
  backend workforce/RBAC modules and current protocol/Preventive Care (PC) docs for the active
  execution model.
- Health symptom field groups/options are captured in the forms doc and source
  findings. Do not rebuild them from memory.
- Legacy delete-row behavior maps to void/reversal/correction events with audit,
  original media, rectified media, verifier, and corrected answer where needed.
- Dynamic pickers come from backend reference data/offline caches, not hardcoded
  Slack dropdowns.
- The current walking slice is vaccination: SOP/proof policy is configured by
  the active scoped `vaccination.matrix` version and executed through
  Preventive Care (PC) / vaccination work.
  Vaccination execution context (park/shed/stage/defer/blocker/owner context)
  renders inside /vaccination, not as a separate Parks module.
  `/sops` is reopened only as the Admin/Data Ops SOP Library for the vaccination
  slice; it must not display non-vaccination SOP inventory or revive old `/tasks`/
  generic SOP product behavior.
- The project-level target is Android SOP runner -> Goat OS app API -> backend
  validation/idempotency/proof/audit -> module-owned canonical event -> Postgres
  projections. BQ/Sheets are removable only per feature/grain after coverage,
  cross-source dedup, and shadow parity gates pass.
- Keep three gates separate: vaccination execution acceptance, legacy SOP
  execution retirement, and dashboard BQ/Sheets retirement. Do not let one
  vaccination proof path imply full SOP closeout.
- Android SOP execution depends on Operator Management for active profile,
  verified login, scope grants, capabilities, device/session state, app
  bootstrap, and dynamic task/SOP visibility.
- Before calling SOP replacement closed, cross-check the module that owns the
  workflow, the proof policy, the Android runner, and the backend verification
  path. Vaccination is only one vertical using the shared SOP/proof engine.
- For the current vaccination process-integrity slice, SOP is not complete until
  it feeds the backend process-integrity model used by Protocol Adherence,
  Action Center, Control Tower, vaccination execution context, and workflow drilldowns.
