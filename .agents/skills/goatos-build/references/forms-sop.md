# Forms And SOP Reference

Load this when working on SOP forms, form builder/editor, native mobile runner,
task lifecycle, weekly assignment, verification, or Slack-form replacement.

Canonical docs:

- `context/forms/final-forms-sop-engine.md`
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

