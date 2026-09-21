# Docs-Only Local CI Scope

## Scope

- Fix local CI scope classification so pure Markdown docs under `docs/` use the
  existing `docs-only` fast lane instead of the heavier `common` job.

## Done

- Broadened `tools/ci/component-paths.json` `docsOnly.prefixes` to include all
  `docs/` Markdown files.
- Added a `ci-scope.mjs` self-test for
  `docs/preventive-care-vaccination/vaccination-rules.md`.
- Updated the screenshot remediation self-test's stale diagnostic assertion so
  the CI-tooling lane remains self-consistent while landing this change.
- Added the screenshot remediation self-test to the CI common-only classifier
  list so this tooling assertion edit does not fan out to backend/admin/android.

## Pending

- Commit and land through the required main gate.

## Tests / Evidence

- `node tools/ci/ci-scope.mjs --self-test`: PASS.
- Direct classifier proof for
  `docs/preventive-care-vaccination/vaccination-rules.md`: selected jobs are
  `docs-only`.
- First `make land-main`: failed in the CI-tooling self-test suite at
  `screenshot remediation guard self-test` because case (d) still expected the
  older diagnostic phrase `not re-record the gap`; the guard now reports the
  same failure as `STILL blocked`.
- Second `make land-main`: interrupted after scope widened to
  `common,backend,admin-web,android`; root cause was the touched
  `tools/ci/check-screenshot-remediation.test.sh` missing from `ciCommonOnly`.

## Deployment State

- Not deployed. CI tooling/documentation-only main landing requested.

## Current SHA

- Base: `395faac17` from `origin/main` at worktree creation.
