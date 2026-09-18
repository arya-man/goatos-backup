# PR 301 Question Proof Progress

## Scope

- Review fix for PR 301: prevent admins from creating per-question proof requirements before the compatible Android APK is adopted.

## Done

- Added a fail-closed admin catalog gate for per-question proof authoring.
- By default, `/admin/pen-routines/catalog` advertises only `none` for `question_proof_kinds`, so the drawer cannot create new per-question proof rules for older APK users.
- Added explicit rollout switch `GOATOS_PEN_ROUTINE_QUESTION_PROOF_AUTHORING=true` to expose Photo, Video, and Photo or video after mobile rollout is verified.
- Existing backend/domain/mobile support for saved per-question proof rules remains intact.
- Added focused transport tests for both default hidden and rollout-enabled catalog behavior.

## Pending

- Push the updated PR head after focused guards pass.
- Do not enable `GOATOS_PEN_ROUTINE_QUESTION_PROOF_AUTHORING` in staging/main until the compatible APK is distributed/adopted or a force-update/min-version gate is active.
- No main merge, landing, or staging deploy has been performed.

## Tests / E2E Performed

- Passed: `cd backend && go test ./internal/penroutines/...`.
- Passed: `node --test --experimental-strip-types apps/admin-web/features/pen-routines/pen-routines.test.mjs`.
- Passed: `git diff --check`.
- First Android attempt failed before tests because the isolated worktree had no SDK location configured.
- Passed after setting the SDK path in the environment without writing `local.properties`: `cd apps/goatos-android && ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew :app:testStgReleaseUnitTest --tests 'sg.mesha.goatos.viewmodel.PenRoutineDetailViewModelTest' --stacktrace`.

## Known Failures

- None yet in this review-fix cycle.

## Metrics

- No latency or product performance metrics changed; this is a rollout safety gate.

## Judge Status

- Review finding patched in source; focused backend, admin-web, Android, and diff checks passed.

## Current SHA

- Starting PR head: `6adbae27f774d01476425a88511fbc1c9aa37321`.

## Deployment State

- Local PR worktree only. Not merged, not deployed.
