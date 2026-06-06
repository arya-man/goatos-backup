# Operator Mobile Agent Context

Read first:

- `../../context/frontend/final-frontend-mobile-backend-architecture.md`
- `../../context/forms/final-forms-sop-engine.md`
- `../../context/product/goat-os-feature-phases.md`

Purpose:

- Android field app for operators: task list, SOP form runner, proof capture,
  offline queue, sync, scan/device adapters, and entry logs.

Do:

- Build task-first Android flows.
- Use generated OpenAPI clients.
- Keep offline submissions idempotent.
- Use direct signed media upload.
- Keep UI field-friendly: large touch targets, icon-first, Telugu/Hindi-ready.

Do not:

- Do not let operators create SOP forms.
- Do not write directly to Firestore, GCS, Sheets, BigQuery, or databases.
- Do not use Slack as canonical execution.
