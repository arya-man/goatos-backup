# Forms And SOP Reference

Load this when working on SOP forms, form builder/editor, native mobile runner,
task lifecycle, weekly assignment, verification, or Slack-form replacement.

Canonical docs:

- `context/forms/final-forms-sop-engine.md`
- `docs/phases/phase-02-sop-task-engine/PRD.md`
- `docs/phases/phase-02-sop-task-engine/TRD.md`
- `docs/phases/phase-02-sop-task-engine/SOP-CLOSEOUT.md`
- `context/source-findings/drive-docs-findings.md`
- `context/execution/next-contracts.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`

Rules:

- Goat OS owns the form DSL and runtime contract.
- Conditions are declarative and deterministic, not arbitrary JavaScript.
- Admins create forms; operators execute assigned tasks on Android.
- Native runner evaluates the same DSL offline.
- Server revalidates every submission against form version, permissions, live
  state, idempotency, and workflow gates.
- `repeat_for_each_goat` is a first-class batch semantic, not a normal field.
- Slack forms are legacy discovery/migration input only.
- Legacy Slack schemas are now captured canonically: death, shifting, birth/
  abortion, health diagnosis/follow-up, not-eating, proof policy, correction
  and rectification behavior.
- Health symptom field groups/options are captured in the forms doc and source
  findings. Do not rebuild them from memory.
- Legacy delete-row behavior maps to void/reversal/correction events with audit,
  original media, rectified media, verifier, and corrected answer where needed.
- Dynamic pickers come from backend reference data/offline caches, not hardcoded
  Slack dropdowns.
- Phase 2 starts with the Shifting SOP as the walking skeleton for the reusable
  SOP/task engine. Vaccination, health, feed, death, birth/abortion,
  procurement, and other Slack-derived workflows must migrate onto the same DSL
  and runtime rather than creating one-off forms.
- The project-level target is Android SOP runner -> Goat OS app API -> backend
  validation/idempotency/proof/audit -> module-owned canonical event -> Postgres
  projections. BQ/Sheets are removable only per feature/grain after coverage,
  cross-source dedup, and shadow parity gates pass.
- Keep three gates separate: Phase 2 Shifting platform acceptance, legacy SOP
  execution retirement, and dashboard BQ/Sheets retirement. Do not let a
  Shifting demo or Android path imply full SOP closeout.
- Before calling SOP replacement closed, cross-check Counts, Locations, and
  Mortality dependencies in `SOP-CLOSEOUT.md`; especially count verification,
  weight, status/stage, death, birth/abortion, sale/exit, and proof/video flows
  that are not all covered by the Shifting walking skeleton.
