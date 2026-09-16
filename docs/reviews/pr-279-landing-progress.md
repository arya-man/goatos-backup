# PR 279 Landing Progress

## Scope

- PR: https://github.com/vgoats/goatos/pull/279
- Branch: `feat/feed-sop-driven`
- Goal: land the feed SOP card authoring/runtime change to `main` after review and exact local landing proof.

## Done

- Reviewed core backend feed SOP pinning, card validation, migration shape, transport materialization, admin-web feed editor publish path, and Android outbox dispatch mapping.
- No high-confidence review findings were found in the inspected paths.
- Focused backend proof passed:
  - `go test ./internal/feeddirection/domain ./internal/feeddirection/app ./internal/feedsop/adapters/postgres ./internal/sop/authored`
- Focused admin-web feed model proof passed:
  - `node --test --experimental-strip-types apps/admin-web/features/sops/feed-model.test.mjs`

## Known Failures / Notes

- Android focused unit proof did not run locally. After setting `ANDROID_HOME=/Users/raviteja/Library/Android/sdk`, `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.FeedDistributionCompleteViewModelTest' --stacktrace` failed during Gradle task creation before test compilation:
  - `Could not create task ':app:transformStgDebugUnitTestClassesWithAsm'`
  - `NoClassDefFoundError: Build_gradle$16$1` at `build.gradle.kts:598`
- Earlier broad admin-web `npm test -- --runTestsByPath ...` was not a focused signal because the script ignored the Jest-style flag and ran the whole Node test suite; failures were unrelated missing local `typescript` imports in existing scripts.

## Pending

- Run `make land-main` from the clean isolated worktree after this progress note is committed.
- Record final SHA, receipt state, and local/remote `origin/main` match.

## Current State

- Review status: no findings from inspected paths.
- Merge/deploy/push status: not yet landed; final promotion is gated on `make land-main`.
