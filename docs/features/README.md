# Feature PRD/TRD Index

This folder holds feature-level PRD/TRD documents for Goat OS work that crosses
phase boundaries or needs its own build-ready contract before implementation.

Phase docs still live in `docs/phases/`. Use this folder when a feature, such
as Mortality, must define its own product semantics, data ownership, scale
model, API contract, parity plan, and rollout gates before code starts.

Current feature docs:

- `docs/features/counter-family/AGENT-RUNBOOK.md`
- `docs/features/counter-family/INTEGRATION-CHECKLIST.md`
- `docs/features/cutover-contract.md`
- `docs/features/counts/AGENT-TASK.md`
- `docs/features/counts/PRD.md`
- `docs/features/counts/TRD.md`
- `docs/features/locations/AGENT-TASK.md`
- `docs/features/locations/PRD.md`
- `docs/features/locations/TRD.md`
- `docs/features/mortality/AGENT-TASK.md`
- `docs/features/mortality/PRD.md`
- `docs/features/mortality/TRD.md`
- `docs/features/operator-management/AGENT-TASK-ADMIN.md`
- `docs/features/operator-management/AGENT-TASK-ANDROID.md`
- `docs/features/operator-management/PRD.md`
- `docs/features/operator-management/TRD.md`

Counts and Locations are separate feature specs but one delivery slice:
Locations owns canonical location tree, aliases, capacity, usage checks, and
review queues; Counts consumes those records for legacy parity and future
canonical projections.

The shared cutover contract owns the transition rule for any feature that is
temporarily fed by BigQuery/Sheets but will later be fed by Android/backend
canonical writes.

Operator Management is the cross-phase workforce/auth foundation for Android
SOP execution. It is not payroll HRMS and not legacy parity; it owns the active
operator roster, app bootstrap, scope/capability/device gates, and sanitized
legacy submitter mapping needed before Android SOPs replace Slack execution.

The counter-family agent runbook and task files are execution guides for running
Locations, Counts, and Mortality in parallel. They do not replace the PRD/TRD
contracts.

The Phase 2 Operator/SOP runbook and task files are execution guides for running
Operator Management admin/backend, Operator Android, and SOP Builder/Task Engine
in parallel. They do not replace the PRD/TRD contracts.

Rules:

- Do not use these docs to bypass the phase roadmap.
- Do not duplicate architecture decisions already owned by `context/` or
  `docs/decisions/`; link to the canonical decision instead.
- Any dashboard/report feature that slices by month, date, breed, farm, load,
  category, status, gender, source, or similar dimensions must follow
  `docs/decisions/high-scale-dashboard-projections.md`.
- Any feature using BQ/Sheets as a temporary upstream before Android/backend
  canonical writes must follow `docs/features/cutover-contract.md`.
- Feature completion means the full required legacy-visible/product scope is
  implemented, reconciled, tested, and scalable. Pending, migration-only, or
  source-unavailable required sections may exist in internal/dev review, but
  they block production completion.
- Permission namespaces follow product ownership: dashboard/report permissions
  use `analytics.<feature>.*`, while master-data modules such as Locations use
  module namespaces such as `locations.*`. New features should state their
  namespace explicitly instead of guessing from nearby specs.
