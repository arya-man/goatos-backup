# PR 391 card review fixes — 2026-09-25

Scope: fix off-page task routing and page-independent dated card totals/status, then push the existing PR. No merge or deployment.

Source baseline: `8f1c23bdd556bfed0d7107d972b174b71a39c8b4`.
Current main inspected: `c0b37abf24787b2dc7cb4a3dae94fb0729e938d5` (ancestor of the PR).
Isolated branch: `codex/pr391-card-summary-fixes`; primary dirty checkout preserved.
The verified fix is the commit containing this document. Its exact final SHA and publication readback are recorded in the PR description and `/tmp/pr391-final-receipt.md` after pushing.

## Done

- Complete dated membership carries every source task. A mixed or unknown task identity, including an unrelated future representative, opens the existing combined-submit path.
- Backend per-pen/operator-day summaries count unique animals across assignments. Pending obligations win over completed siblings; vaccine doses remain a separate count. Internal animal identities never enter API JSON or cached public projections.
- Every source task must be submitted, or all obligations accepted, before a card becomes record-only.
- Operator-scoped overdue/deferred pending siblings survive row and membership filters even when legacy row counters look complete.
- Android membership controls card visibility/date; cards, header/progress, and adherence use the same dated totals. Future work cannot inflate today. Old cached membership keeps the complete roster accessible but labels unknown totals honestly.
- Go, OpenAPI, generated TypeScript and Kotlin contracts agree. The partition guard recognizes canonical `partitionLabel` with a regression fixture, without suppressions.

## Before / after

| Scenario | Before | Verified after |
|---|---|---|
| One assignment, two tasks, one loaded row | Route pinned to representative task | Taskless combined-submit route retains complete membership |
| Completed page-one assignment plus pending off-page assignment | 1 target / 1 done | 2 targets / 1 done / 1 due |
| Same animal in completed and pending assignments | Page-dependent count/status | 1 target / 0 done / 1 due |
| Accepted dose plus overdue pending sibling | Operator page can hide pen | Pen visible, writable and due |
| Submitted task plus unsent task, all doses ready | Representative can lock card | Card remains writable |
| Today 2 animals and future 50 in one pen | Representative rows can inflate header | Today remains 2 |

## Exact validation

- Android baseline: 24 ShedsViewModel tests, 6 expected regression failures before changing production ViewModel (`/tmp/pr391-red-ShedsViewModelTest.xml`).
- Final Android: 120 app tests passed after the last source edit: ScanViewModelTest 61, ShedsViewModelTest 24, ExecutionRouteIdentityTest 9, ShedsExecutionIdentityTest 26. ExecutionRepositoryPaginationTest: 26 passed. Total 146.
- Android commands: `:app:testProdDebugUnitTest --tests '*ScanViewModelTest' --tests '*ShedsViewModelTest' --tests '*ExecutionRouteIdentityTest' --tests '*ShedsExecutionIdentityTest'`; `:core:core-data:testDebugUnitTest --tests '*ExecutionRepositoryPaginationTest'`.
- Build environment: isolated `GRADLE_USER_HOME=/tmp/pr391-gradle-home`, JDK 21, local Android SDK, one worker, 2GiB Gradle heap and in-process Kotlin. Final app rerun took 1m52s.
- Go: `go test -p 2 ./internal/vaccination/... ./internal/obligation/... ./internal/protocol/app ./internal/vaccinationexecution/... ./internal/platform/vaccinepurpose ./internal/kernelstages -count=1` passed.
- Real PostgreSQL: `TestCardMembershipIncludesOffPageAssignments` and `TestDatedMembershipSummariesKeepDistinctAnimalsAndAllSourceTasks` passed against a dedicated disposable OCI database, not STG or the maintained clone. Final run 155.913s including 150.55s setup. Baseline SQL reproduced missing complete facts; removing scoped inclusion exemptions reproduced `operator page lost overdue pending sibling`.
- Guards: full mobile guard, operational read-model/location and PostgreSQL bind guards/self-tests passed. Full-tree Android media-egress guard passed (560 files).
- Generated client independently regenerated with openapi-typescript 7.13.0 and byte-compared with the tracked file; match. `git diff --check` passed.

Evidence: `/tmp/pr391-final-android-app-tests.log`, `/tmp/pr391-green-android-tests.log`, `/tmp/pr391-final-go-tests.log`, `/tmp/pr391-operator-day-final-all-green.log`, `/tmp/pr391-operator-day-scoped-red.log`, `/tmp/pr391-final-mobile-guard.log`, `/tmp/pr391-final-contract-guards.log`, `/tmp/pr391-final-media-guard.log`, `/tmp/pr391-api-client-check.log`.

## Latency / payload boundary

Fixture: 2 animals, 3 vaccine doses, 2 tasks, 1 paginated row. Summary payload: baseline 932B; final 1197B. Cold combined/separate reads: baseline 102.402/46.346ms; corrected run 131.796/57.874ms; final concurrent-build run 307.391/106.660ms. These are noisy small-fixture observations, not a performance improvement or scale certification. New animal-fact JSON is constructed only for summaries, not plain paginated reads.

## Prevention and ownership

Canonical owner: existing PostgreSQL vaccination execution read model. Android reads through the Room-backed ExecutionRepository; existing ScanViewModel/Room/outbox remains the submit/write owner. No schema migration, medical rule, event worker, tenant/grant policy, media download or deployment change.

Regression layers cover SQL cardinality/scoping, additive API serialization, cached response compatibility, complete route identity, combined-submit behavior and dated UI aggregates. Scale anti-pattern guidance and the partition guard record the prevention rule. Month-history/current-sync review included identity, source sync, merged execution and outbox routing.

Independent judge: separate `independent_judge` agent, inherited model/reasoning, reviewed baseline plus exact final source/guard diff. No remaining blocking finding. Conditional test requirement is now satisfied by the final green runs.

## Remaining boundaries / state

Known current focused-test failures: none. Earlier shared Gradle cache corruption was isolated; one initial daemon disappeared before tests, then the smaller-memory retry completed. One guard false positive was corrected and its full suite rerun.

Phone/browser/live E2E and full landing CI were not run. The existing PR records a release-signature mismatch for the installed phone app; this checkpoint does not replace that proof. No performance certification, main merge or deployment is claimed. Publication is limited to the existing PR branch; the final push receipt is recorded separately as described above.
