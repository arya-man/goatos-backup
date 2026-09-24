# PR 397 close retry fixes

Scope: fix completed-close retry rejection and two stale sale-line tests. PR branch only; no main merge or staging deployment.

Base SHA: 0980dab3a891d063eb5bcb9aa1d39d9ab14684be. Current candidate: uncommitted changes on this base.

Done:
- Read persisted deal status in the existing feed-demand query; skip stock validation only for already-closed deals.
- Cover successful close, depleted-stock retry, reopening and renewed shortage confirmation.
- Update frontend shapes and verify mixed-sale money, animals, feed kilograms and counted pieces.

Proof:
- Before: completed-close replay regression failed with 400 kg balance versus 600 kg demand; two frontend shape assertions failed.
- After: focused sales, feed adapter and adminui Go packages pass (default PostgreSQL tests skipped); sale-lines tests 4/4 pass.
- SQL bind-contract and aggregate-projection guards, including self-tests, pass.
- Explicit PostgreSQL tests pass on disposable OCI test databases: TestFeedDemandForDealSumsPerFeedNotPerLine, TestFeedDemandForADealWithNoFeedLineIsEmptyAndNotAnError, TestFeedCloseReplayUsesPersistedStatusAndDepletesOnlyOnce (107.723s including migrations).
- TestAPIWiresTheFeedStoreIntoSales and close/reopen service regressions pass.
- Query cost: same round trip and row grain; one additional status column, no new query. Latency not measured.

Pending: portability index build and recheck, final diff review, commit and PR branch push.
Judge status: self-review passed; no independent judge requested.
Known failures: initial ai-doctor failed because this fresh worktree lacks generated CRG/repowise indexes; rebuilding them. None in focused product checks. Full local CI and browser/device E2E not run for this follow-up; frontend production code is unchanged.
Deployment state: not started; not requested.
