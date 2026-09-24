# PR 397 repeated review and fixes — 2026-09-25

Scope: fix monthly sales reporting and repeat independent agent reviews, correcting confirmed adjacent findings. PR branch push authorized; no main merge or staging deployment.
Base SHA: d948c5a9058fb22687d53d65f9746acde513458c. Candidate: commit containing this document.

Done:
- Monthly backend totals now include every sale and all custom animal products; serialized totals and feed/other quantities reach OpenAPI/generated clients and web chart helpers. Legacy response fallback and explicit-zero semantics retained.
- Regression: mixed 5,000 revenue / 4 animals fixture matches monthly and headline totals; custom animal, feed, counted goods, manure and sheep covered.
- Updated animal headline tooltip to include all animal products.

Review round 1:
- Monthly reviewer: no remaining monthly arithmetic/contract defect; found inaccurate animal tooltip, corrected.
- Client reviewer: confirmed non-feed kilogram quantities previewed as pieces; fixed with focused tests.
- Backend reviewer: confirmed legacy feed family balance lookup and combined-family demand warning defects; fixed.

Tests so far: sales/procurement default Go suites passed (DB tests opt in); focused web format/line/action tests passed. API client regenerated. Final guards passed.
Before/after: 1,000 feed/other/custom-animal sale used to produce zero chart revenue; new backend total is 1,000. Two custom animals now remain two in monthly totals.

Pending: commit and PR push/readback; fixes, repeated reviews and scoped validation complete.
Known limitations: broader fixture failures from previous progress notes; no full CI receipt, fresh browser/device E2E, or live HTTP latency proof. No production performance claim. No deployment started.

## Final implementation and review result
- Fixed editor totals: non-feed kg remain kg; number units alone contribute pieces. Mixed fixture has 180.5 non-feed kg, 25 feed kg and 200 pieces.
- Fixed legacy concentrate balance identity: both legacy labels resolve to successor balance across all five database states.
- Grouped sibling feed demand by shared stock identity: two 60 kg lines against 100 kg now warn once for 120 kg, instead of passing independently. Create and close paths share the helper; acknowledgement and completed replay semantics remain intact.
- Removed obsolete Sheep/Goat/Manure enums from deal and price-band response schemas; authored names are valid. Clarified live counts and counted-quantity weight semantics in schema descriptions.
- Three agents participated. Round 1 confirmed the editor units, family lookup/grouping, stale response enums and tooltip gaps; all corrected. Round 2 cross-review: client reviewer found no actionable family warning or monthly bugs; backend reviewer found no actionable monthly/schema/editor bugs; monthly reviewer found no remaining monthly bugs. This is scoped review confidence, not a guarantee that the entire repository has zero bugs.

## Final executed checks
- PASS: `go test ./internal/sales/... ./internal/procurement/... ./internal/adminui/app ./internal/feeddirection/adapters/postgres` (default DB tests opt in separately).
- PASS: `go test ./cmd/api -run '^$'` compilation.
- PASS: `go test ./internal/sales/adapters/http -run 'TestMonthlyWire|TestOverviewSummary' -count=1` after final counted-item fixture adjustment.
- PASS: `node --experimental-strip-types --test features/procurement/sales-format.test.mjs features/procurement/sale-lines.test.mjs features/procurement/sales-record-action.test.mjs` — 28 tests.
- PASS: admin-web `npx tsc --noEmit --pretty false`.
- PASS: real disposable PostgreSQL tests `TestSuccessorFeedSaleUsesLegacyFamilyStock` and `TestFeedCloseReplayUsesPersistedStatusAndDepletesOnlyOnce` with `GOATOS_RUN_POSTGRES_TESTS=1` and isolated socket DSN. No STG/shared application data mutated.
- PASS: postgres-bind-contract, aggregate-projection, operational-read-model-contract, mesha-data-map and git-identity guards. Data-map live column check skipped, source hashes in sync.
- PASS: api-client-check after staging regenerated output. Initial check reported the intentional unstaged generated changes.
- Small DB fixture timing: 3 purchases, 3 sale lines, 2 feed days; stock-items SQL p90 5.23 ms and stock-loads p90 8.75 ms. These are not HTTP, load-scale or before/after performance proof.

Known limits remain: no full local CI/landing receipt, authenticated live browser/device E2E, or HTTP latency proof. Historical broader fixture failures are not claimed fixed or green. No main merge, deployment or live smoke. Final candidate SHA is the commit containing this note; push/readback reported in task response.
