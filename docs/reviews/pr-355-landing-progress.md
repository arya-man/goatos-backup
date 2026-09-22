# PR 355 landing progress

Date: 2026-09-22

Scope:
- Review and land PR 355 to `main`.
- Preserve the dirty primary checkout at `/Users/raviteja/mesha/goatos`.
- Use isolated worktree `/Users/raviteja/mesha/goatos-pr355-review`.

Current state:
- PR head reviewed: `d7c2dd2684ef3783d0a15352862287402306862a`.
- Merge-ref candidate reviewed: `fa4285d66bf4d24d19044372777c3ae88b48fe56`.
- Base before landing attempt: `origin/main` at `ebcd1155a50e7a6fb79011138ae38470cbcbeb5d`.
- Linearized and rebased candidate: `8ddd24074` after resolving Farm Born origin semantics with the Fattening stage fold.
- Staging deploy: not run.

Review / test evidence before landing:
- Backend focused tests passed from `backend`: `go test ./internal/pccare/... ./internal/pccaresop/... ./internal/platform/herdstage/... ./internal/procurement/... ./internal/counts/...`.
- Admin-web targeted SOP tests passed from `apps/admin-web`: `node --test --experimental-strip-types features/sops/pc-care-model.test.mjs features/sops/module-page.test.mjs features/sops/publish-closes-editor.test.mjs`.
- Android targeted PC Care tests passed from `apps/goatos-android`: `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew :app:testStgDebugUnitTest --tests '*PcCare*'`.
- Query-plan guard passed with the local OCI tunnel DSN: `tools/ci/run-local-ci.sh query-plans`.
- Post-rebase focused backend tests passed from `backend`: `go test ./internal/procurement/... ./internal/platform/herdstage/...`.

Pending:
- Run exact landing receipt with `make land-main` after this progress note update is committed.
- Verify local `HEAD`, `origin/main`, and remote `main` match the landed SHA.
