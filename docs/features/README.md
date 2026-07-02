# Feature PRD/TRD Index

This folder holds feature-level PRD/TRD documents for Goat OS work that crosses
phase boundaries or needs its own build-ready contract before implementation.

Phase docs still live in `docs/phases/`. Use this folder when a cross-cutting
feature needs its own product semantics, data ownership, scale model, API
contract, parity plan, and rollout gates before code starts.

Current feature docs:

- `docs/features/cutover-contract.md`
- `docs/features/critical-animal-action-guardrails.md`
- `docs/features/locations/AGENT-TASK.md`
- `docs/features/locations/PRD.md`
- `docs/features/locations/TRD.md`

Old counts, mortality, counter-family, and operator-management feature specs
were deleted from the active tree. Do not rebuild their old dashboard/admin
tracks unless product scope is explicitly reopened.

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
- Critical guardrails are generic kernel capability, not per-feature hacks.
  Follow `context/architecture/operational-kernel-system-design.md` for the
  shared guardrail engine and
  `docs/features/critical-animal-action-guardrails.md` for animal-operation
  policy packs such as quarantine, ICU, death, movement, birth, health,
  procurement, feed, and sale/allocation. Do not implement high-risk actions as
  ordinary CRUD, generic form submission, or generic shifting without reason
  validation, proof, authority, obligations, escalation, audit, and read-model
  visibility. When replacing a legacy workflow, preserve the useful legacy
  controls as capability parity, but close known legacy gaps in the same
  production scope. Use that doc's vertical/module ownership map before placing
  guardrail work under Preventive Care (PC), Counts, Procurement, Feed, Sales, or a shared kernel
  module.
- Permission namespaces follow product ownership: dashboard/report permissions
  use `analytics.<feature>.*`, while master-data modules such as Locations use
  module namespaces such as `locations.*`. New features should state their
  namespace explicitly instead of guessing from nearby specs.
