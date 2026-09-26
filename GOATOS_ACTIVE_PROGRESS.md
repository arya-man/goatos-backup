# Goat OS Active Progress

Last updated: 2026-09-21 03:16 IST

## Current Task: PR294 visual overhaul + API latency follow-up

- Scope: continue PR #294 toward the Minimal Dashboard visual direction while keeping Mesha brand
  colors, and keep the API latency gate from regressing.
- Baseline artifact SHA/API stamp: `7e4c0b23fca256383690bb88fbce7d3ed7e8a807`.
- Promotion state: PR branch only; no merge, no main push, no staging deploy.
- Current local API: `127.0.0.1:18086`; rebuild/restart it after committing this wave before
  claiming latency improvement.
- Baseline red samples:
  - `vaccination_operations`: p50 `484.9ms`, p90 `520.8ms`, p95 `522.2ms`,
    p99 `530.5ms`.
  - `vaccination_command_board`: p50 `275.9ms`, p90 `337.9ms`,
    p95 `360.3ms`, p99 `362.5ms`.
  - `vaccination_command_shed_dose_matrix`: p50 `524.8ms`, p90 `532.4ms`,
    p95 `544.4ms`, p99 `631.7ms`.
  - `work_board_page_cpt`: p50 `220.6ms`, p90 `302.1ms`, p95 `333.3ms`,
    p99 `363.2ms`.
- Planned safe edit: one backend SQL planner-fence patch in
  `backend/internal/vaccinationexecution/adapters/postgres/commandboard_sql.go`
  for the shed-dose matrix CTEs, matching the existing PR294 command-board
  latency pattern from nearby SQL. Validate with focused command-board Go tests
  plus `git diff --check`.
- Additional local edits now present:
  - vaccination command-board/cohort/shed-dose live cache keys use the explicit/live snapshot helper;
  - mobile tabs, Work Board lanes and Procurement Vendors mobile card layout were patched;
  - focused tests/typecheck/webview integrity are green, but latency rerun is still pending.

## Current State

- Current local PR SHA: `53781cb35573796fcab6ec73ce5cb47850b8f2ca`.
- Staging has not been deployed.
- `main` has not been pushed.
- PR has not been merged.
- Temporary diagnostic file is absent:
  `backend/internal/workboard/app/source_timing_oci_test.go`.
- Final local work is committed but not yet pushed at this checkpoint.

## Latest Local Evidence

- Expanded live browser route smoke passed on laptop and mobile for 25 nested
  routes across Action Center, Protocol, Approvals, Verify, Vaccination,
  Procurement Source Entry, Sales Buyer Analytics, Operations Audit, Operations
  DLQ, and Leave:
  `.codex-goatos-render/admin-web-screenshots/2026-09-15T18-58-10-887Z`.
- Current-SHA Work Board and Weighing browser E2E passed on laptop and mobile:
  `.codex-goatos-render/admin-web-screenshots/2026-09-15T19-03-46-696Z`.
  Covered:
  - `/work-board?scope_mode=company`
  - `/weighing/weights?scope_mode=company`
  - `/weighing/analytics?scope_mode=company&tab=general`
  - `/weighing/analytics?scope_mode=company&tab=breed`
  - `/weighing/analytics?scope_mode=company&tab=breed&wt_from=2026-08-03&wt_to=2026-09-15`
  - `/weighing/analytics?scope_mode=company&tab=birth`
  - `/weighing/analytics?scope_mode=company&tab=shed`
  - `/weighing/analytics?scope_mode=company&tab=weight`
  - `/weighing/analytics?scope_mode=company&tab=time`
  - `/weighing/analytics?scope_mode=company&tab=load`
- Browser checks still fail on these forbidden strings:
  `backend_down`, `Admin-web contract unavailable`, `The board could not be loaded`,
  and `Weights could not be loaded`.
- Local OCI-backed HTTP latency gate first run:
  `.codex-goatos-render/pr264-perf/gate-final-53781cb355-20260915T190150Z.json`.
  Result: failed because one dimensions-section sample reached `p99/max=606.1ms`.
- Immediate repeated local OCI-backed HTTP latency gate rerun:
  `.codex-goatos-render/pr264-perf/gate-dimensions-rerun-53781cb355-20260915T190249Z.json`.
  Result: passed. All hot-path p99 values were under `500ms`; worst p99 values:
  `weighing_dates=494.1ms`, `growth_weights=442.0ms`,
  `demographics_origin=321.0ms`, `work_board_cpt=227.0ms`,
  `work_board_cbe=117.6ms`.
- API evidence verifier passed:
  `API latency evidence: PASS @ 53781cb35573796fcab6ec73ce5cb47850b8f2ca`.
- Tests passed after latest fixes:
  - `npm --prefix apps/admin-web test -- lib/api/short-read-cache.test.mjs scripts/smoke-visual-route-coverage.test.mjs features/weighing/weights-window.test.mjs features/procurement/animal-purchases.test.mjs features/verification-review/toxin-review-render.test.mjs`
    (`693` tests passed).
  - `npm run typecheck --workspace apps/admin-web`.
  - `go test ./internal/vaccinationexecution/app ./internal/weighing/adapters/postgres -run 'TestCardSummaryPreservesVerificationPendingState|TestVaccinationExecutionPageCompletePageCardSummaryPreservesVerificationPendingAsInReview|TestWeightDemographicsSectionedReadsGateProducerCTEs|TestShedPartitionShortcutOnlyHandlesSectionsItPopulates|TestGrowthLosingAnimalsUsesLookbackBeforeFilteringLatestPeriod|TestWeightDemographicsWeeklyGainKeepsPenAndLoadProducers|TestWeightDemographicsWeeklyGainDoesNotUseBrokenCaseWrappedSubselects'`.
  - `git diff --check`.

## Pending Right Now

- Fresh judge review is running against final SHA `53781cb35573796fcab6ec73ce5cb47850b8f2ca`.
- If judges sign off, push branch `fix/pr264-api-latency-e2e-20260915` to PR #273.
- If any judge finds a blocker, fix it, rerun the relevant proof, amend, and
  rerun final judge review.

## Stop Rule

- No staging deployment, push to `main`, merge, or PR completion claim is authorized.
- Local proof and judge signoff must come first.
- Local fixture smoke is correctness proof only; it does not prove the real
  live 4s/6s/8s Weighing paths are below 500ms.

## Done Locally

- Production RCA for the 2026-09-15 Weighing Analytics crash is now tied to
  logs, deployed SHA, and git history:
  - Production dashboard rendered `Weights could not be loaded` because
    backend `/weighing/weight-demographics` returned HTTP 500.
  - Cloud Run backend log at `2026-09-15T17:26:38Z` had request id
    `91b7cd900f33399aebb8c39c5895e71e` with `SQLSTATE 42601`:
    `syntax error at or near ")"`.
  - Deployed services were on commit label `03ebe28194bc`.
  - The bad SQL shape was introduced in merged PR #270
    (`https://github.com/vgoats/goatos/pull/270`), commit `1662c93dcb`, by
    wrapping weekly gain JSON subqueries as
    `CASE WHEN $19::boolean THEN (SELECT ... ) ... ) ELSE '[]'::jsonb END`.
    The extra close-paren before `ELSE` is the syntax error.
- Added guard coverage for the real failure class, not a single hard-coded
  `wt_from`/`wt_to` URL:
  - backend source guard bans the broken PR #270 CASE-wrapped weekly subselect
    shape for `gbw/gpw/glw`;
  - backend integration tests execute both `dimensions` and `weekly_gain`
    section reads so SQL compilation is exercised;
  - browser smoke now includes a generated wide-window Breed tab route
    (`today-43d` through `today`) and requires Breed/Origin/Shed/Weight/Load
    tab product signals, while still failing on `Weights could not be loaded`.
- Fixed the procurement Animal Purchases attention marker regression by replacing
  generic global `dot l` usage with scoped `ap-attention-dot` styling and a
  source guard.
- Admin-web `/weighing/weight-demographics` reads now use the same short-read
  cache as the other Weighing route reads, with failed results evicted instead
  of poisoning retries.

- Weighing admin-web now requests narrowed Growth sections:
  - `/weighing/weights`: `headline,shed_leaderboard,losing_animals`
  - `/weighing/analytics` General: `headline,shed_leaderboard,by_park`
  - `/weighing/analytics` Time-wise: `weekly_gain`
  - Shed-wise: no Growth read.
- Backend Growth API now supports `headline` as a section.
- `headline` computes its own rejected and losing-animal counts truthfully when
  requested.
- The losing-animal list is returned only when `losing_animals` is requested, so
  `headline` does not widen the list payload.
- OpenAPI app/admin Growth section contracts include `headline`, and the guard
  checks the regex is attached to `sections`, not `park_id`.
- Admin-web aggregate guard no longer allows inline bypass comments.
- Added worktree `AGENTS.md` local-first performance/deploy rule.
- Added PR264 hot-path manifest covering the rendered route shapes for:
  - Weighing page reads
  - Weighing analytics tab reads
  - Growth Director sectioned read
  - Work Board bundled page reads
  - Mobile vaccination execution with truthful card summaries
- Added local PR264 evidence verifier for the manifest.
- Added before/after latency comparison guard. It refuses same-commit,
  different-dataset, different-manifest, different-route-shape, missing-route,
  percentile-regression, and material-payload-growth comparisons.
- Fixed judge-found OpenAPI bug: admin `/weighing/leadership/growth` now keeps
  `from`/`to` as dates and puts the section regex on `sections`.
- Tightened the OpenAPI test so `/weighing/leadership/growth` cannot be
  accidentally satisfied by `/app/weighing/leadership/growth`.
- Tightened PR264 evidence: API timing alone is no longer enough. Evidence must
  include browser proof for `/work-board`, `/weighing/weights`, and
  `/weighing/analytics`, and must prove the exact failure strings are absent.
- Tightened PR264 manifest assertions so Work Board/Vaccination/Weighing arrays
  cannot pass with empty data.
- Added Android repository guard proving mobile vaccination execution keeps
  `includeCardSummaries=true` at the API-call boundary.
- Patched Vaccination execution service fanout so page rows, carry summary,
  filter options, and card summaries run under one request context in parallel
  instead of serially stacking DB latency.
- Added a short read-cache key for
  `VaccinationExecutionCardSummaries`, matching the page read cache shape, so
  repeated mobile refreshes do not keep re-running the whole-filter aggregate.

## Local Checks Passed After Latest Fix

Updated: `2026-09-15 13:26 IST`

- `go test ./internal/weighing/app ./internal/weighing/adapters/postgres`
- `npm --prefix apps/admin-web test -- features/weighing/weights-window.test.mjs features/weighing/growth-director.contract.test.mjs features/weighing/gain-thresholds.test.mjs`
  - Result: `671` tests passed after sectioning `getWeightDemographics`.
- `node tools/contract-validation/validate-contracts.mjs`
- `make admin-web-sectioned-aggregate-reads-guard`
- `git diff --check`
- `make api-latency-policy-test`
  - Result: `32` tests passed, including PR264 manifest, browser-evidence, and
    before/after comparison guard tests.
- `./gradlew :core:core-data:testDebugUnitTest --tests 'sg.mesha.goatos.core.data.ExecutionRepositoryPaginationTest'`
  - Result: passed.
- `go test ./internal/vaccinationexecution/app ./internal/vaccinationexecution/adapters/http`
  - Result: passed after the Vaccination fanout/cache patch.
- `node tools/perf/api-latency-compare.test.mjs && node tools/perf/api-latency-policy.test.mjs`
  - Result: `17` tests passed after the latest local edits.
- `go test ./internal/weighing/app ./internal/weighing/adapters/postgres ./internal/weighing/adapters/http ./internal/vaccinationexecution/app ./internal/vaccinationexecution/adapters/http`
  - Result: passed after the Weighing demographics section patch and
    Vaccination fanout/cache patch.
- `gofmt -w internal/weighing/adapters/postgres/weight_demographics.go && go test ./internal/weighing/app ./internal/weighing/adapters/postgres ./internal/weighing/adapters/http`
  - Result: passed after SQL-side section gating in `GetWeightDemographics`.
- `gofmt -w internal/weighing/app/shed_weights_test.go && go test ./internal/weighing/app ./internal/weighing/adapters/postgres ./internal/weighing/adapters/http ./internal/vaccinationexecution/app ./internal/vaccinationexecution/adapters/http`
  - Result: passed after adding demographics section service guards.
- `node tools/contract-validation/validate-contracts.mjs`
  - Result: passed after removing duplicate `sex` query parameters from the
    admin `weight-demographics` OpenAPI contract.
- `node tools/ci/check-grafana-durability.mjs --self-test && node tools/ci/check-grafana-durability.mjs`
  - Result: passed after fixing GMP alert filter label/resource syntax and
    tightening the guard to Prometheus/GMP filters.
- `make api-latency-policy-test`
  - Result: `32` tests passed after making sectioned demographics hot paths
    mandatory in `tools/perf/hot-paths.pr264.json`.
- `make admin-web-sectioned-aggregate-reads-guard && npm --prefix apps/admin-web test -- features/weighing/weights-window.test.mjs features/weighing/gain-thresholds.test.mjs features/weighing/growth-director.contract.test.mjs`
  - Result: `671` tests passed.
- `rg -n "getWeightDemographics\\(" apps/admin-web -S`
  - Result: only the server wrapper plus intended `weights.tsx` and
    `weights-analytics.tsx` calls remain; both page calls pass explicit
    `sections`.
- `npm --prefix packages/api-client run generate`
  - Result: regenerated `packages/api-client/src/generated/app-api.ts` so the
    generated app client includes `/weighing/weight-demographics?sections=...`.
  - Note: `make api-client-check` still reports the generated diff against
    `HEAD` because this PR work is uncommitted; the generated file is now part
    of the local diff that must be included.
- `node tools/perf/api-latency-evidence.test.mjs && node tools/perf/api-latency-policy.test.mjs && node tools/perf/api-latency-compare.test.mjs`
  - Result: `29` tests passed after aligning the PR264 evidence verifier with
    the sectioned demographics manifest.
- `node tools/contract-validation/validate-contracts.mjs && git diff --check`
  - Result: contracts and whitespace checks passed after generated-client and
    Grafana alert-filter fixes.
- `node --check tools/deploy/smoke-stg-grafana-dashboards.mjs && node tools/ci/check-grafana-durability.mjs --self-test && node tools/ci/check-grafana-durability.mjs`
  - Result: passed after making Grafana smoke require representative live
    datasource queries, not only dashboard JSON/provisioning.
- `node --check tools/deploy/smoke-stg-grafana-dashboards.mjs && node tools/deploy/smoke-stg-grafana-dashboards.mjs --self-test && node tools/ci/check-grafana-durability.mjs --self-test && node tools/ci/check-grafana-durability.mjs`
  - Result: passed after adding feature-wise Grafana panel contracts and a
    local smoke self-test for empty/null Grafana frames.
- `make grafana-durability-guard`
  - Result: passed; the public guard target now runs smoke syntax check, smoke
    self-test, durability self-test, and dashboard/provisioning validation.
- `git diff --check -- Makefile infra/grafana/dashboards/00-feature-health.json docs/observability/GRAFANA_ACCESS.md tools/ci/check-grafana-durability.mjs tools/deploy/smoke-stg-grafana-dashboards.mjs`
  - Result: passed.

## Code Fixes In This Local Pass

- Vaccination execution page now runs independent reads in parallel instead of
  stacking page rows, carry summary, park options, and card summaries in one
  serial request.
- Vaccination card summaries now use the existing short read cache.
- Weighing demographics now accepts a bounded `sections` parameter through the
  HTTP/app/repository path.
- Weighing demographics SQL now gates result aggregate subqueries by requested
  section: tab-specific reads return empty JSON for unused sections instead of
  computing those output aggregates. Legacy omitted `sections` still requests
  every section.
- Admin Weights page requests only the demographics sections it renders.
- Admin Weights analytics tabs request only the demographics section for the
  active tab.
- The sectioned aggregate guard now fails if admin-web adds broad aggregate
  reads without explicit sections.
- Weighing service tests now prove invalid demographics sections fail before
  repository access, and valid sections reach the repo with resolved
  scope/window.
- PR264 latency manifest now measures the actual sectioned demographics route
  shapes used by admin analytics tabs: `dimensions`, `origin`, `shed_type`,
  `weight_bands`, and `weekly_gain`.
- Grafana durability guard now rejects stale product titles, stale GMP
  `generic_task` resource filters, and Grafana-style singular label selectors
  inside Cloud Monitoring alert filters.
- Grafana SLO alert filters now use the live GMP resource shape with Cloud
  Monitoring filter syntax: `resource.type="prometheus_target"` and
  `metric.labels.*`.
- PR264 evidence verifier now requires the five sectioned demographics hot
  paths in addition to the legacy full demographics route, matching
  `tools/perf/hot-paths.pr264.json`.
- Grafana deploy smoke now POSTs `/api/ds/query`, verifies representative
  Cloud Monitoring-native panels, and checks feature-wise Feature Health panels.
  For each feature slice, it first checks whether current GMP route telemetry
  exists in the last 24h; if it does, the committed request-rate and p95 panels
  must return data, otherwise the feature is logged as no-current-telemetry
  rather than claimed complete.
- Feature Health now explicitly labels the remaining APM gaps: pending
  Faro/RUM collector deployment, pending Cloud Trace dashboard proof from live
  browser spans, and pending end-to-end `traceparent` continuity through outbox
  relay/consumer boundaries.
- Replaced the remaining user-facing Grafana doc label `Goat OS stg — API / RED`
  with `Goat OS — API / RED`; concrete resource names like `goatos-stg` remain
  unchanged.

## Browser/Timing Proof Already Run

- A disposable local stack was run on:
  - Postgres `127.0.0.1:55432`
  - API `127.0.0.1:18080`
  - admin-web `127.0.0.1:13300`
- Weighing fixture:
  - tenant `11111111-1111-4111-8111-111111111111`
  - `animals=7`, `observations=4`, `scale_animals=5001`
- Focused browser smoke passed for `/weighing/weights` and `/weighing/analytics`
  tabs and did not show:
  - `backend_down`
  - `Admin-web contract unavailable`
  - `The board could not be loaded`
  - `Weights could not be loaded`
- Local fixture timing showed sub-200ms page renders and ~1ms Growth reads, but
  that dataset is too small to prove the live slow paths are fixed.

## Important Decision

- Do not flip Android vaccination execution to `include_card_summaries=false`
  in this PR. Mobile has an existing test proving page-independent card
  summaries are needed when rejected work lives beyond the visible page. Removing
  summaries would make the screen faster but less truthful.
- Correct Vaccination fix is backend-side/query-shape work: make the summary
  path cheaper or split it safely, without losing authoritative cross-page card
  status/counts.
- Judge found Weighing General still pays for headline rejected/losing counts.
  That may be product-correct, but it is not free. If live PR264 latency stays
  above budget, split the headline fields so pages can request only the headline
  numbers they actually render.
- Earlier Weighing judge found the bigger remaining anti-pattern: admin routes
  still used one monolithic `/weighing/weight-demographics` read.
- Current local diff now addresses that at the API contract and result-query
  level: admin routes pass explicit demographics sections, backend validates the
  bounded section set, cache keys include the section key, origin-only helper
  reads are skipped unless the `origin` section is requested, and result
  aggregate subqueries are gated by section booleans.
- This is still not final signoff. Fresh judge review and live-size timing/EXPLAIN
  proof are required before claiming the 4s/6s/8s weighing problem is fixed.

## Current Local/OCI Timing Blocker

- Update `2026-09-15 19:52 IST`: current SHA
  `3fff191f96251c81c881b331a57baf2eda56eb6d` has fresh local OCI-backed proof.
  The first targeted Vaccination execution request after restart was cold/slow
  (`3.249s`, service `3182ms`), but the following repeated warm HTTP requests
  were under target (`~0.060s..0.190s` except bounded small jitter), matching the
  acceptance rule that cold first hit can be slow while normal repeated requests
  must stay below budget.
- Formal PR264 API gate passed:
  `.codex-goatos-render/pr264-perf/gate-final-3fff191-execmode-20260915T141656Z.json`.
  It ran 20 warm measured samples per route after 5 warmups against local API
  `127.0.0.1:18082` and OCI clone data. Worst API result was
  `pr264_app_vaccination_execution_with_card_summaries` at `p99/max=401.1ms`;
  Work Board CBE/CPT were `126.7ms`/`101.0ms` max; Weighing and Growth routes
  were `132.7ms..158.0ms` max.
- Chrome E2E passed against the same local API/token:
  `.codex-goatos-render/admin-web-screenshots/2026-09-15T14-18-26-004Z/browser-evidence.json`.
  Laptop and mobile `/work-board?scope_mode=company`,
  `/weighing/weights?scope_mode=company`,
  `/weighing/analytics?scope_mode=company&tab=general`, and
  `/weighing/analytics?scope_mode=company&tab=time` loaded with route-specific
  product signals. The evidence records absence of `backend_down`,
  `Admin-web contract unavailable`, `The board could not be loaded`, and
  `Weights could not be loaded`.
- Combined evidence verifier passed:
  `node tools/perf/api-latency-evidence.mjs --report .codex-goatos-render/pr264-perf/gate-final-3fff191-execmode-20260915T141656Z.json --expected-sha $(git rev-parse HEAD) --browser-evidence .codex-goatos-render/admin-web-screenshots/2026-09-15T14-18-26-004Z/browser-evidence.json`.
- Local guard bundle passed after the latest Vaccination side-read
  `pgx.QueryExecModeExec` fix:
  focused backend Go tests, admin-web typecheck, admin weighing/visual coverage
  tests (`691` pass), contract validation, perf policy/evidence/request-path
  tests (`34` pass), `make grafana-durability-guard`, and `git diff --check`.
- Pending now: fresh judge agents against this exact SHA/evidence, PR body update
  with these final reports, and branch push after judge signoff. No staging
  deployment, merge, or push to `main` has been done.

- Update `2026-09-15 18:36 IST`: Ravi correctly rejected treating cache as the
  performance fix. Current acceptance remains real API latency, not UI masking.
  Cache/coalescing is only a guard against duplicate browser fanout.
- The latest real Work Board HTTP timing after the Vaccination Work Board source
  fix is now under target for both OCI-clone parks and no longer reports
  degraded Vaccination:
  - CPT park `/work-board/page`: `200`, `0.362s`, route timing `total=240ms`,
    `source_count_vaccination_vaccination_drive_pen=240ms`, no degraded field.
  - CBE park `/work-board/page`: `200`, `0.222s`, route timing `total=221ms`,
    `source_count_vaccination_vaccination_drive_pen=183ms`, no degraded field.
- Actual API fixes in this pass include a direct Vaccination Work Board
  `CountByWorkState` path, bounded no-work precheck/read budgets, Weighing
  sectioned aggregate SQL, removal of hidden losing-list reads from
  headline-only Growth analytics, and skipped origin/sex scope reads when those
  filters are absent.
- Pending now: rerun the formal PR264 local OCI API gate, Chrome E2E, focused
  tests, and judge agents. Do not sign off from the two manual curl timings.
- Update `2026-09-15 18:51 IST`: judge findings were fixed and current local
  gates rerun. The admin-web short read cache key now includes an auth-token
  fingerprint, so it cannot share grant-scoped Weighing reads across actors.
  The browser smoke now records route-specific product signals, and the PR264
  evidence verifier rejects browser proof without those signals. Grafana docs
  and APM dashboard copy no longer claim live Grafana/APM proof from local-only
  guard evidence.
- Fresh Chrome E2E with route signals:
  `.codex-goatos-render/admin-web-screenshots/2026-09-15T13-17-30-936Z`.
  The browser evidence records:
  - Work Board: lane counters present, `healthy_empty_state=true`, no degraded
    banner/failure strings.
  - Weighing Weights: `has_losing_weight_table=true`.
  - Weighing Analytics General: `has_weighing_kpis=true`.
  - Weighing Analytics Time-wise: `has_weekly_growth=true`.
- Final local OCI API gate after the guard/doc/cache-key fixes:
  `.codex-goatos-render/pr264-perf/gate-final-local-20260915185000.json`.
  Evidence validator passed against the route-signal browser proof.
- Final timing highlights:
  - Weighing shed weights: `p95=118.4ms`, `max=118.4ms`.
  - Weighing weight demographics: `p95=128.8ms`, `max=128.8ms`.
  - Sectioned demographics: `p95=116.6ms..130.5ms`.
  - Sectioned Growth: `p95=119.1ms..126.8ms`.
  - Growth Director weights sections: `p95=119.3ms`.
  - Vaccination execution with card summaries: `p95=264.8ms`.
  - Work Board formal gate is cache-warm (`p95=1.0ms..1.3ms`); use the
    separate cache-busted series above for backend compute proof.
- Final local guard bundle passed after these changes:
  - Backend focused Go packages.
  - Admin-web focused/related tests, `673` tests passed.
  - Contract validation.
  - Perf policy/evidence/compare/request-path tests, `34` tests passed.
  - `make grafana-durability-guard`.
  - `git diff --check`.

## 2026-09-15 19:07 IST Reality Check

- Ravi rejected cache-centered framing. Correct acceptance is real local
  OCI-backed API latency plus Chrome E2E; cache/coalescing is only a duplicate
  fanout guard, not the primary fix.
- Actual code/query fixes now under review:
  - Work Board route timing and bounded fanout/concurrency.
  - Vaccination Work Board direct count path and bounded precheck/read budgets.
  - Weighing sectioned demographics/Growth reads, including skipping hidden
    losing-list and unused origin/sex scope work.
  - Vaccination execution parallelized page reads while preserving
    `include_card_summaries=true`.
  - Auth timing and concurrent grant/person lookup without stale per-person
    access caching.
- Fresh browser E2E against local admin-web `127.0.0.1:13300` and local
  OCI-backed API `127.0.0.1:18082` passed for:
  - `/work-board?scope_mode=company`
  - `/weighing/weights?scope_mode=company`
  - `/weighing/analytics?scope_mode=company&tab=general`
  - `/weighing/analytics?scope_mode=company&tab=time`
- Fresh browser evidence path:
  `.codex-goatos-render/admin-web-screenshots/2026-09-15T13-34-15-737Z/browser-evidence.json`.
  It records route-specific product signals and absence of:
  `backend_down`, `Admin-web contract unavailable`,
  `The board could not be loaded`, `Weights could not be loaded`.
- Fresh cache-busted Work Board HTTP timing after restarting admin-web with the
  current token:
  `.codex-goatos-render/pr264-perf/workboard-cachebusted-20260915T133557Z.txt`.
  All 8 samples returned `200`, total HTTP `0.106s..0.257s`, route compute
  `70ms..157ms`, `degraded=[]`.
- Pending immediately after this note: rerun the formal PR264 API latency gate
  so its worktree diff hash includes this progress document update, then rerun
  focused local guards and ask judge agents to re-review current evidence.
- Regression judge found the service-level lane source cap was not enough by
  itself: `/work-board/page` could still launch one lane service call per open
  lane. Added route-level lane service concurrency cap
  `maxPageLaneServiceConcurrency=2` plus
  `TestPageBoundsLaneServiceConcurrency`, so one request cannot multiply lane
  source fanout beyond the default DB pool budget.
- Pending: rerun current judge agents after the fixes.
- Formal repeated local OCI API gate passed after the Work Board Vaccination
  source fix:
  `.codex-goatos-render/pr264-perf/gate-after-workboard-vaxfix-20260915183743.json`.
  It measured `12` samples per hot path after `5` warmups, same tenant/user/date
  shape, and all routes passed the PR264 thresholds.
- Important interpretation: the formal gate's Work Board route hit the normal
  short route cache (`p95=1.4ms`/`1.2ms`) after warmup. Separate cache-busted
  Work Board HTTP timings were run to prove backend compute is also under
  target: CPT park samples were `0.305s`, `0.155s`, `0.109s`, `0.160s`,
  `0.091s`, `0.105s`, `0.159s`, `0.071s`; route timing totals were
  `214ms`, `153ms`, `107ms`, `158ms`, `89ms`, `104ms`, `156ms`, `68ms`, with
  no degraded modules.
- Current gate timing highlights:
  - Weighing shed weights: `p95=123.6ms`, `max=123.6ms`.
  - Weighing weight demographics full/page shape: `p95=126.5ms`,
    `max=126.5ms`.
  - Weighing sectioned demographics hot paths: `p95=117.0ms..132.3ms`.
  - Weighing Growth sectioned hot paths: `p95=117.4ms..125.3ms`.
  - Vaccination execution with card summaries: `p95=120.4ms`, `max=120.4ms`.
- Chrome E2E passed against the same local API/admin-web:
  `.codex-goatos-render/admin-web-screenshots/2026-09-15T13-08-41-366Z`.
  Captured laptop and mobile for `/work-board?scope_mode=company`,
  `/weighing/weights?scope_mode=company`,
  `/weighing/analytics?scope_mode=company&tab=general`, and
  `/weighing/analytics?scope_mode=company&tab=time`.
  Visual inspection confirmed Work Board has no degraded Vaccination banner and
  Weighing pages show real data instead of failure screens.
- Evidence validator passed:
  `node tools/perf/api-latency-evidence.mjs --report .codex-goatos-render/pr264-perf/gate-after-workboard-vaxfix-20260915183743.json --expected-sha $(git rev-parse HEAD) --browser-evidence .codex-goatos-render/admin-web-screenshots/2026-09-15T13-08-41-366Z/browser-evidence.json`
  returned `API latency evidence: PASS @ 401bbb0209dd3104ce4dcd9b7cbc8e1347149f21 (23 hot paths)`.
- Focused local guards passed after the latest edits:
  - Backend Go focused packages for postgres/httpmiddleware/permissions,
    workboard, weighing, vaccinationexecution, pccare, and processintegrity.
  - Admin-web focused route/contract tests, `672` tests passed.
  - `node tools/contract-validation/validate-contracts.mjs`.
  - `node --test tools/perf/api-latency-policy.test.mjs tools/perf/api-latency-evidence.test.mjs tools/perf/api-latency-compare.test.mjs tools/perf/request-path-evidence.test.mjs`,
    `33` tests passed.
  - `make grafana-durability-guard`.
  - `git diff --check`.
- Temporary diagnostic file check passed:
  `backend/internal/workboard/app/source_timing_oci_test.go` is absent.
- Pending: current judge agents must finish and sign off before any PR-ready
  claim. No staging deploy was run, so Grafana/APM remains local guard proof
  only, not live staging proof.

- Update `2026-09-15 18:16 IST`: still **not ready** for judge signoff,
  merge, deploy, or PR completion.
- The repeated local OCI API gate passed after adding the auth person-scope
  cache and starting the API with warm pool settings:
  `GOATOS_PG_MAX_CONNS=10`, `GOATOS_PG_MIN_CONNS=4`.
- Evidence file:
  `.codex-goatos-render/pr264-perf/gate-after-minconns-20260915175746.json`.
  The repeated warm API timings were under 500ms for the PR264 Weighing,
  Vaccination, and Work Board manifest routes.
- This does **not** close the task. The Chrome/admin-web E2E is still red:
  `/work-board` rendered, but `/weighing/analytics` hung/failed under
  browser-style concurrent SSR reads against the local OCI-backed API, with
  backend logs showing `/weighing/leadership/growth` and sometimes
  `/weighing/shed-weights` hitting the 3s backend query timeout.
- Current next step: instrument the exact SSR backend request shapes and fix
  the concurrent Weighing analytics path. Sequential in-process or warmed API
  timing alone is not acceptable proof.

- Update `2026-09-15 14:03 IST`: still **not ready** for judge signoff, merge,
  or deploy.
- Fixed one confirmed anti-pattern after the latest failure:
  `/weighing/weights` no longer requests hidden demographics sections. It now
  asks only for `composition,dimensions,gain_thresholds`; the PR264 perf
  manifest and policy test now enforce that page-shaped request.
- Local focused checks passed after this edit:
  - `node --test tools/perf/api-latency-policy.test.mjs apps/admin-web/features/weighing/weights-window.test.mjs`
  - `go test ./internal/weighing/adapters/postgres ./internal/weighing/app ./internal/weighing/adapters/http`
- Added a backend micro-optimization: `GetShedWeights` skips the RFID
  same-animal map when `weighing_category=per_shed_partition`, because that
  path does not read scanned individual-animal rows. Focused backend tests
  still pass.
- Row-returning local OCI timing after API restart, tenant
  `00000000-0000-4000-8000-000000000001`, dates `2026-09-01..2026-09-09`:
  - `/weighing/shed-weights?...weighing_category=per_shed_partition`
    returned `10` rows / `336` animals, but still measured cold `3.217s`,
    then `0.928s`, `0.705s`, `0.760s`, `0.563s`.
  - `/weighing/weight-demographics?...weighing_category=per_shed_partition&sections=composition,dimensions,gain_thresholds`
    returned non-empty demographics but still measured cold `0.875s`, then
    `0.399s`, `0.496s`, `0.583s`, `0.496s`.
- Conclusion: the Weights page is much safer than the original 8s/timeout path,
  and the over-fetch is now guarded, but the local row-returning gate is still
  above the requested 500ms target, especially `shed-weights`. Continue with
  `shed_weights.go` query/follow-up profiling before any browser proof or judge
  signoff.

- Resolved stale tunnel blocker: the OCI clone DB now answers `select 1`, and
  the local API on `127.0.0.1:18080` answers `/readyz`.
- Local clone migrations were applied through the current backend family before
  the API was restarted.
- The first OCI-backed PR264 latency gate is still **red**, so the PR is not
  ready for signoff, merge, or deploy.
- Red endpoints from `.codex-goatos-render/pr264-perf/20260915T074212Z`:
  - `pr264_weighing_dates`: p50 `353.9ms`, p90/p95/p99 `509.6ms`.
  - `pr264_weighing_shed_weights`: one timeout/failure, p99 `3368.2ms`.
  - `pr264_weighing_weight_demographics`: all attempts failed/timed out.
  - `pr264_weighing_weight_demographics_origin_section`: all attempts failed.
  - `pr264_weighing_weight_demographics_shed_type_section`: all attempts failed.
  - `pr264_weighing_weight_demographics_weekly_gain_section`: all attempts
    failed.
  - `pr264_weighing_weight_demographics_dimensions_section`: returned data but
    p90 exceeded the `300ms` route budget.
  - `pr264_weighing_weight_demographics_weight_bands_section`: returned data but
    p90/p99 exceeded budget.
  - `pr264_work_board_page_cbe` and `pr264_work_board_page_cpt`: failed local
    manifest assertion/timing proof; still needs split between data-window
    assertion issue and real code latency.
  - `pr264_app_vaccination_execution_with_card_summaries`: one failure and
    roughly `2.3s-2.5s` samples.
- The current Weighing demographics anti-pattern is still present structurally:
  section flags exist, but the repository still runs one huge SQL statement with
  all CTEs in the planner scope. Fast sections need true split/fast paths or an
  equivalent change that prevents unused heavy branches from planning/executing.
- Active parallel agents:
  - Weighing SQL split/fast-path worker.
  - Work Board/Vaccination latency triage worker.
  - Grafana/APM durability worker: local guard work is green; live smoke still
    needs an actual staging Grafana run before promotion.

### Work Board / Vaccination Triage Patch - 2026-09-15

- Work Board PR264 failures were bad manifest fixture IDs, not measured slow
  endpoint samples. The saved gate report had zero samples because both
  `pr264_work_board_page_cbe` and `pr264_work_board_page_cpt` failed the
  `lanes.todo.rows` assertion during warmup.
- Local OCI DB read through the running API process config showed the shared
  CBE/CPT park IDs present:
  - `00000000-0000-4000-8000-000000003001 | Coimbatore`
  - `00000000-0000-4000-8000-000000003002 | Channapatna`
  The manifest's old `30000000-...0001/0002` IDs returned no location rows.
- Patched `tools/perf/hot-paths.pr264.json` to use the shared CBE/CPT park IDs
  for Work Board.
- Vaccination execution with card summaries is a real slow path in the saved
  gate: one 500 and successful samples around `1957.5ms-2499.8ms`.
- Patched `VaccinationExecutionPage` so `include_card_summaries=true` keeps
  truthful summaries but avoids the whole-filter card summary SQL when the
  first page already covers the full filtered result. Multi-page responses still
  use `VaccinationExecutionCardSummaries`.
- Added/updated app-layer guards:
  - complete first page builds default summaries without the SQL aggregate;
  - multi-page response still uses the full-filter aggregate.
- Focused green tests after patch:
  - `go test ./internal/vaccinationexecution/app`
  - `go test ./internal/vaccinationexecution/adapters/http`
  - `go test ./internal/vaccinationexecution/adapters/postgres`
  - `go test ./internal/workboard/...`
  - `node --test --experimental-strip-types apps/admin-web/features/work-board/work-board-board.test.mjs`
  - `node --test tools/perf/api-latency-policy.test.mjs tools/perf/api-latency-evidence.test.mjs tools/perf/api-latency-compare.test.mjs`
- Not rerun yet: the actual PR264 HTTP latency gate. The running API requires a
  bearer token, and this shell does not have the earlier `GOATOS_BEARER_TOKEN`
  or local HS256 auth secret available without reauth/restarting the stack.

## Pending

- Run the PR264 repeatable before/after performance harness against the same
  staging-equivalent data for:
  - `/weighing/weights`
  - `/weighing/analytics` tabs
  - `/weighing/shed-weights`
  - `/weighing/leadership/growth`
  - `/work-board/page` versus old summary/rows fanout
  - `/app/vaccination/execution`
- Re-run browser proof after any further code changes.
- Re-run judge review after any further code changes.
- Only after proof and judge signoff: ask maintainer before push/merge/deploy.

### Continuation Update - 2026-09-15 17:19 IST

- Local-only. No staging deploy, merge, or push to main.
- OCI tunnel on `127.0.0.1:15432` was alive; local API was restarted on
  `127.0.0.1:18082` against the OCI clone.
- Minted a fresh local bearer token for tenant
  `00000000-0000-4000-8000-000000000001` using active tenant-wide user
  `69462d72-d1ed-5558-9ffd-88173eba3451`.
- Fixed auth request-path latency by:
  - adding a combined per-person access snapshot read;
  - making the middleware run active-grant lookup and per-person access lookup
    concurrently instead of serially stacking OCI round trips;
  - preserving the existing no-cache active-grant guard.
- Fixed Growth `losing_animals` section bug introduced during optimization:
  the rewritten query now types the old `$3` lookback parameter so Postgres
  does not fail with SQLSTATE `42P18`.
- Narrowed Growth `losing_animals` to latest-two individual observations inside
  the selected period instead of the broad shared growth-pairs CTE.
- Focused checks passed:
  - `go test ./internal/platform/httpmiddleware ./internal/workforce/adapters/postgres ./internal/permissions/adapters/postgres`
  - `go test ./internal/weighing/adapters/postgres ./internal/weighing/app ./internal/weighing/adapters/http`
  - `git diff --check`
- Repeated local OCI HTTP probe after auth parallelization:
  - `/weighing/shed-weights?...per_shed_partition`: cold `3.324s`, then
    `0.742s, 0.748s, 0.336s, 0.324s, 0.401s, 0.318s, 0.426s`.
  - `/weighing/weight-demographics?...sections=composition,dimensions,gain_thresholds`:
    cold `1.573s`, then `0.575s, 0.400s, 0.515s, 0.324s, 0.419s, 0.645s, 0.322s`.
  - `/app/vaccination/execution?limit=20&include_filter_options=true&include_card_summaries=true`:
    cold `4.643s`, then `0.301s, 0.492s, 0.507s, 0.520s, 0.305s, 0.330s, 0.380s`.
- Growth offender probe after losing-query typing fix:
  - `/weighing/leadership/growth?...sections=headline,shed_leaderboard,losing_animals`:
    cold `4.055s`, then `0.561s, 0.334s, 0.189s, 0.492s, 0.270s, 0.237s, 0.466s`.
- Full PR264 latency gate was attempted but stopped because it hit repeated
  `/weighing/leadership/growth` cold-section timeouts; not certified yet.
- Current blocker:
  - Normal warm requests are much closer and often under 500ms, but the full
    manifest is still not green because cold/section-cache misses in Growth can
    hit the 3s local query timeout.
  - Browser E2E and final judge signoff are still pending.

### Local PR264 API Gate Rerun - 2026-09-15 14:50 IST

- Local-only. No push, merge, staging deploy, or mobile deploy.
- Local API was restarted from this worktree on `127.0.0.1:18080` with
  `GOATOS_ENV=local` against the OCI staging-equivalent DB clone.
- Minted a local bearer token for tenant
  `00000000-0000-4000-8000-000000000001` using a tenant-level
  `growth_director` subject with active grants.
- Fixed the section-pruning 500 in `weight_demographics.go` by anchoring typed
  SQL parameters for section flags and need flags.
- Fixed the no-filter weighing date/shed path by skipping sex/origin scope
  resolver queries when no sex/origin filter is selected.
- Fixed the per-shed origin bucket timeout by rewriting the origin bucket
  resolver location lookup so Postgres does not execute the location-name
  subquery roughly 1,000 times.
- Corrected `tools/perf/hot-paths.pr264.json` to use the row-returning weighing
  window `2026-09-01..2026-09-09` and the correct sectioned Growth Director
  assertion `road_to_sale.total_animals`.
- Focused green checks:
  - `go test ./internal/weighing/adapters/postgres ./internal/weighing/app ./internal/weighing/adapters/http`
  - `node --test tools/perf/api-latency-policy.test.mjs tools/perf/api-latency-evidence.test.mjs tools/perf/api-latency-compare.test.mjs`
- Latest local PR264 report:
  `.codex-goatos-render/pr264-perf/latest-after.json`.
- Passing in latest report:
  - `pr264_weighing_shed_weights`: p90/p95/p99 `282.7ms`.
  - `pr264_weighing_weight_demographics`: p90/p95/p99 `172.9ms`.
  - `pr264_weighing_weight_demographics_dimensions_section`: `187.1ms`.
  - `pr264_weighing_weight_demographics_origin_section`: `181.8ms`.
  - `pr264_weighing_weight_demographics_shed_type_section`: `182.6ms`.
  - `pr264_weighing_weight_demographics_weight_bands_section`: `171.9ms`.
  - `pr264_weighing_weight_demographics_weekly_gain_section`: `181.4ms`.
  - `pr264_weighing_growth_weights_sections`: `262.3ms`.
  - `pr264_weighing_growth_general_sections`: `268.6ms`.
  - `pr264_weighing_growth_time_sections`: `255.2ms`.
  - `pr264_growth_director_weights_sections`: `183.2ms`.
  - `pr264_app_vaccination_execution_with_card_summaries`: `265.6ms`.
- Still red in latest report:
  - `pr264_weighing_dates`: p90/p95/p99 `323.0ms`, slightly above the strict
    300ms gate but still below 500ms.
  - `pr264_work_board_page_cbe`: p90/p95/p99 `888.5ms`.
  - `pr264_work_board_page_cpt`: p90/p95/p99 `7354.9ms`.
- Active sidecar agent:
  - `01a0a463-deba-7fb3-97a2-a1ec01af0371` Work Board latency worker.
- Next local-only actions:
  - Investigate/fix Work Board `page` endpoint bottleneck, especially CPT.
  - Re-run PR264 API latency gate.
  - Only after API gate is green, run browser E2E proof for `/work-board`,
    `/weighing/weights`, and `/weighing/analytics` with failure-string guards.
  - Then run final judge review. No merge/deploy without maintainer confirmation.

## Handover Update - 2026-09-15 15:33 IST

### Stop State

- Ravi explicitly stopped this session and asked for handover.
- Do **not** deploy staging.
- Do **not** merge or push main.
- Local API was stopped with Ctrl+C.
- OCI tunnel session may still be running from earlier; verify before reusing.
- Temporary diagnostic file still exists and must not be shipped as-is:
  - `backend/internal/workboard/app/source_timing_oci_test.go`

### Fresh Local Edits Made After Last Progress Entry

- `backend/internal/feeddirection/adapters/boardsource/source.go`
  - Changed Feed Work Board `CountByState` from four serial activity reads to four concurrent bounded activity reads.
  - Reason: Feed count was one of the slow healthy Work Board summary sources.

- `backend/internal/processintegrity/adapters/boardsource/source.go`
  - Reduced Work Board vaccination canonical read budget from 250ms to 150ms.
  - Added 75ms vaccination due-work precheck budget and fail-open behavior on precheck timeout.
  - Reason: precheck/canonical vaccination count was holding Work Board summary and causing board route to sit near/over 500ms.
  - Caveat: this makes vaccination more likely to degrade in Work Board summary under slow PI reads; it should not hide valid rows because lane pruning fix below keeps degraded modules eligible.

- `backend/internal/workboard/adapters/http/handler.go`
  - `modulesWithLaneRows` now treats degraded summary modules as unknown and keeps them in lane reads instead of pruning them as zero.
  - Reason: if vaccination summary degrades, the page must not silently hide valid vaccination rows.

- `backend/internal/workboard/adapters/http/handler_test.go`
  - Added regression coverage for degraded summary modules not being pruned from lane reads.

### Focused Tests Passed After Those Edits

From `/Users/raviteja/mesha/.integrated-prs-20260914183121`:

```sh
gofmt -w backend/internal/processintegrity/adapters/boardsource/source.go \
  backend/internal/workboard/adapters/http/handler.go \
  backend/internal/workboard/adapters/http/handler_test.go \
  backend/internal/feeddirection/adapters/boardsource/source.go
cd backend && go test ./internal/feeddirection/adapters/boardsource ./internal/workboard/... ./internal/processintegrity/adapters/boardsource
```

Result: passed.

### Actual Route Timing Evidence - Not Good Enough

Fresh patched local API was restarted on `127.0.0.1:18081` against OCI clone DB and measured with existing token `/tmp/goatos-pr264-token.txt`.

Route measured:

```text
GET /work-board/page?park=<park>&business_date=2026-08-10&limit=50
```

CBE park `00000000-0000-4000-8000-000000003001`:

```text
15.964s cold, 6.208s, then repeated roughly 1.10s-2.63s
```

CPT park `00000000-0000-4000-8000-000000003002`:

```text
2.690s first, then repeated roughly 1.06s-1.36s
```

Smaller per-lane limits did not fix it:

- `limit=10`: mostly ~1.06s-1.38s
- `limit=20`: mostly ~0.92s-1.25s
- `limit=30`: mostly ~1.04s-2.54s

Conclusion: Work Board page is still **not under 500ms for normal repeated requests**. Do not claim fixed.

### Important Interpretation

- Warmup/cold hit being slow is acceptable only if steady-state repeated requests are below target.
- Current repeated Work Board route is still around 1s+, so this is not a warmup-only issue.
- The earlier in-process diagnostic timings were misleadingly optimistic; real HTTP path remained slow.

### One Bad Measurement Attempt

A direct `/work-board/rows` timing attempt used malformed query parameter `state=...` instead of the route's expected state parameter shape, and then hit auth/grant errors after an `unexpected EOF`. Do not use that failed `/rows` measurement as performance evidence.

### Likely Next Investigation

- Stop relying on the temporary in-process timing test alone.
- Add/request real route-level phase timings inside `/work-board/page` handler:
  - auth/grant/person scope lookup
  - summary service call
  - each lane service call
  - JSON encode/write size
- The route likely spends time outside the individual source SQL timings, possibly permission/grant scope lookup, DB pool/tunnel behavior, or handler fanout/JSON encode.
- Use one request id from API logs and correlate all route phases.
- Once route-level phases show the real bottleneck, fix that path and rerun repeated HTTP measurements.

### Grafana / Deploy / Merge State

- Grafana dashboard work exists locally, but live data/APM is not fully proven.
- No staging deployment should be started from this state.
- No merge/main push should happen from this state.

## Continuation Update - 2026-09-15 16:10 IST

### Local-Only Work Done

- Added request-gated `/work-board/page` route timing:
  - enable with `debug_timing=1` or `X-GoatOS-Debug-Timing: 1`;
  - logs `work_board_page_timing` with request/trace/tenant/actor IDs;
  - response header `X-GoatOS-Route-Timing` records query/scope, visibility/lane
    parsing, summary, lane service, JSON encode, write, bytes, and total.
- Added a Work Board app timing hook so the same debug request records source
  timings for each `CountByState` and `ListRows` call.
- Removed temporary diagnostic test file:
  - `backend/internal/workboard/app/source_timing_oci_test.go`
- Added Weighing Work Board fast list paths for single-lane completed and
  verification-pending reads. These avoid the rework subqueries that cannot
  affect closed/submitted board states.

### Focused Checks Passed

```sh
gofmt -w backend/internal/workboard/adapters/http/handler.go \
  backend/internal/workboard/app/service.go \
  backend/internal/workboard/app/timing.go
cd backend && go test ./internal/workboard/...

gofmt -w backend/internal/weighing/adapters/boardsource/source.go
cd backend && go test ./internal/weighing/adapters/boardsource ./internal/workboard/...
```

Result: passed.

### Route Timing Findings

- Local API ran on `127.0.0.1:18082` against the OCI clone through the local SSH
  tunnel on `127.0.0.1:15432`.
- Token used: existing `/tmp/goatos-pr264-token.txt` for tenant
  `00000000-0000-4000-8000-000000000001`.
- Parks/date:
  - CBE `00000000-0000-4000-8000-000000003001`
  - CPT `00000000-0000-4000-8000-000000003002`
  - business date `2026-08-10`

Before the Weighing fast-path edit, source-level timing showed:

- CPT `page_lane=done`: curl `7.630s`, handler `6.298s`;
  `weighing.ListRows(done)` `2.750s`, `verification.ListRows(done)` `0.461s`.
- CPT `page_lane=in_review`: curl `3.076s-4.842s`, handler
  `2.190s-2.907s`; `weighing.ListRows(in_review)` `0.554s-1.373s`,
  `verification.ListRows(in_review)` `0.276s-0.476s`.
- Full page still had 3s source timeouts before the fast path.

After the Weighing fast-path edit:

- CPT `page_lane=done`: curl `2.747s-4.843s`, handler `1.976s-2.337s`;
  `weighing.ListRows(done)` `0.507s-0.616s`, `verification.ListRows(done)`
  `0.308s-0.463s`.
- CPT `page_lane=in_review`: curl `3.076s-4.842s`, handler
  `2.190s-2.907s`; `weighing.ListRows(in_review)` `0.554s-1.373s`,
  `verification.ListRows(in_review)` `0.276s-0.476s`.
- Full page remained red:
  - CBE: curl `4.639s`, handler `3.899s`, summary `1.306s`, lane wait
    `2.592s`, response `101408` bytes.
  - CPT: curl `4.624s`, handler `3.633s`, summary `1.394s`, lane wait
    `2.238s`, response `55471` bytes.

### Current Conclusion

- This section is retained as historical context for the earlier red run.
- See the latest-state section below for the current rebased evidence.

## Latest Rebased State - 2026-09-15 19:27 IST

This section is superseded by `Final Current-SHA State - 2026-09-15 19:55 IST`
below. It is retained as chronology from the first rebase pass.

- Local-only. No staging deploy, merge, or push to `main` was run.
- Current branch: `fix/pr264-api-latency-e2e-20260915`.
- Current HEAD: `b8ab93282a2bbb76323a8c674702e3a95664afe6`.
- Temporary diagnostic file check: `backend/internal/workboard/app/source_timing_oci_test.go`
  is absent.
- Local API was restarted against the OCI clone on `127.0.0.1:18082`.
- Local admin-web was restarted on `127.0.0.1:13300`.
- Rebase conflict with current `main` was resolved by preserving main's
  Weighing Time-wise load/pen weekly grids and reapplying sectioned API reads.

### Latest Rebased Timing Evidence

- Formal repeated local OCI API gate passed:
  `.codex-goatos-render/pr264-perf/gate-final-rebased-20260915T135310Z.json`.
- Evidence validator passed against current HEAD and browser proof:
  `API latency evidence: PASS @ b8ab93282a2bbb76323a8c674702e3a95664afe6`.
- All 15 PR264 hot paths passed with 20 samples and zero failures.
- Worst formal max was `160.2ms`
  (`pr264_growth_director_weights_sections`).
- Endpoint highlights:
  - Weighing dates: p95 `141.7ms`, max `147.8ms`.
  - Weighing shed weights: p95 `143.4ms`, max `145.6ms`.
  - Weighing weight demographics: p95 `151.6ms`, max `153.5ms`.
  - Sectioned demographics: p95 `137.9ms..150.1ms`, max `148.5ms..156.9ms`.
  - Sectioned Growth: p95 `143.4ms..147.2ms`, max `147.7ms..150.7ms`.
  - Growth Director weights sections: p95 `140.0ms`, max `160.2ms`.
  - Work Board CBE: p95 `131.6ms`, max `142.0ms`.
  - Work Board CPT: p95 `102.7ms`, max `130.4ms`.
  - Vaccination execution with card summaries: p95 `147.3ms`, max `149.1ms`.
- Fresh cache-busted Work Board HTTP proof also passed:
  `.codex-goatos-render/pr264-perf/workboard-cachebusted-20260915T135614Z.txt`.
  It ran 8 unique-query samples, all `200`, HTTP total `0.100s..0.293s`,
  route compute `59ms..229ms`, `degraded=[]`.

### Latest Rebased Browser/E2E Evidence

- Chrome E2E passed against the same local API/admin-web:
  `.codex-goatos-render/admin-web-screenshots/2026-09-15T13-53-58-655Z/browser-evidence.json`.
- Captured laptop and mobile for:
  - `/work-board?scope_mode=company`
  - `/weighing/weights?scope_mode=company`
  - `/weighing/analytics?scope_mode=company&tab=general`
  - `/weighing/analytics?scope_mode=company&tab=time`
- Browser evidence records route-specific product signals and absence of:
  `backend_down`, `Admin-web contract unavailable`,
  `The board could not be loaded`, and `Weights could not be loaded`.

### Latest Rebased Local Guards

- Final focused guard bundle passed after the rebase:
  - Backend Go focused packages for platform/postgres/httpmiddleware,
    permissions, Work Board, Weighing, Vaccination execution, PC Care board
    source, and Process Integrity board source.
  - Admin-web focused/related tests: `691` passed.
  - `node tools/contract-validation/validate-contracts.mjs`.
  - Perf policy/evidence/compare/request-path tests: `34` passed.
  - `make grafana-durability-guard`.
  - `git diff --check`.

### Pending Before PR-Ready Claim

- Fresh rebased judge agents are running for API latency, browser E2E, Grafana
  claim scope, and regression/security/billing review.
- Branch still needs a `--force-with-lease` push after judge signoff because the
  PR branch was rebased.
- No staging Grafana live/APM proof exists in this local-only pass; Grafana is
  locally guarded only until a staging deploy/readback is explicitly authorized.

## Final Current-SHA State - 2026-09-15 19:55 IST

- Local-only. No staging deploy, merge, or push to `main` was run.
- Current branch: `fix/pr264-api-latency-e2e-20260915`.
- Current HEAD: `3fff191f96251c81c881b331a57baf2eda56eb6d`.
- Temporary diagnostic file `backend/internal/workboard/app/source_timing_oci_test.go`
  is absent.
- Current final API evidence:
  `.codex-goatos-render/pr264-perf/gate-final-3fff191-execmode-20260915T141656Z.json`.
- Current final Chrome evidence:
  `.codex-goatos-render/admin-web-screenshots/2026-09-15T14-18-26-004Z/browser-evidence.json`.
- API latency judge: PASS.
- Browser E2E judge: PASS.
- Grafana/observability wording judge: wording fix applied after initial
  current-SHA review; final boundary is:
  `make grafana-durability-guard` proves committed Grafana dashboard/provisioning/smoke
  wiring only. No staging deploy, live Grafana dashboard readback, or live APM
  verification was performed in this pass.
- Regression/security/billing judge: PASS; no blocking regression, security, or
  billing findings found at current HEAD.
- Branch still needs a `--force-with-lease` push after all fresh judges sign off.

## Final Current-SHA State - 2026-09-15 23:36 IST

- Local-only. No staging deploy, merge, or push to `main` was run.
- Current branch: `fix/pr264-api-latency-e2e-20260915`.
- Current HEAD: `75ad1abd05e4dcfa83ea47f13bcc062fbdcb266a`.
- Temporary diagnostic file `backend/internal/workboard/app/source_timing_oci_test.go`
  is absent.
- Production Weighing crash RCA:
  - Deployed production commit label: `03ebe28194bc`.
  - Failing backend request: `/weighing/weight-demographics`.
  - Backend error: `SQLSTATE 42601`, `syntax error at or near ")"`.
  - Cause: merged PR #270 commit `1662c93dcb` introduced invalid SQL in
    `weight_demographics.go` weekly gain CASE subselects.
  - User-visible result: admin-web rendered `Weights could not be loaded`.
- Current clean repeated local OCI API evidence:
  `.codex-goatos-render/pr264-perf/gate-final-75ad1abd05-20260915T180528Z.json`.
  - `passed=true`, `worktree_dirty=false`, 20 samples per endpoint, zero failures.
  - Worst p99: `286.5ms` (`pr264_weighing_dates`), under the 500ms policy.
  - Work Board CBE p99: `108.4ms`; Work Board CPT p99: `117.1ms`.
  - Weighing demographics p99: `152.8ms`; weekly gain p99: `220.2ms`.
  - Weighing Growth weights sections p99: `151.9ms`.
  - Vaccination execution with card summaries p99: `137.0ms`.
- Current Chrome E2E evidence:
  `.codex-goatos-render/admin-web-screenshots/2026-09-15T18-03-03-540Z/browser-evidence.json`.
  Captured laptop and mobile for Work Board, Weights, and every Weighing
  Analytics tab, including the dynamic wide Breed-wise range
  `/weighing/analytics?scope_mode=company&tab=breed&wt_from=2026-08-03&wt_to=2026-09-15`.
  The E2E gate fails on `backend_down`, `Admin-web contract unavailable`,
  `The board could not be loaded`, and `Weights could not be loaded`.
- Orange-dot visual proof:
  `.codex-goatos-render/ui-fixtures/orange-dot-fixed.png`.
  This is a local browser-rendered fixture because the OCI clone has zero animal
  purchase rows; the PR also includes source guards that reject the old generic
  `dot l` class.
- Current local guards:
  - Focused backend Go packages passed.
  - Admin-web focused/related suite passed: `691` tests.
  - `npm run typecheck --workspace apps/admin-web` passed.
  - Perf evidence/policy/compare tests passed.
  - `git diff --check` passed.
- Fresh current-SHA judges are running:
  - Backend/RCA/perf judge: pending.
  - Browser/Grafana/UI judge: pending.
- Branch still needs a `--force-with-lease` push after fresh judge signoff.

## PR273 independent repair — 2026-09-16

Current baseline: `73c50f282ef335cd6d92203bf88252c0e9a30399`.
Scope: independent review, correctness fixes, matched OCI HTTP before/after,
Chrome laptop/mobile proof, judges, local CI, then PR-branch push only.

Confirmed defects and changes in progress:
- Degraded summary zero counts suppressed recoverable lane reads.
- A 30-second Work Board response cache retained completed tasks in Todo and
  hid compute time in fixed-URL latency tests. Removed the response cache.
- Vaccination presentation-row summaries lost dose labels and changed status
  precedence. Restored canonical summaries alongside independent page reads.
- Growth losing-animal shortcut disagreed with canonical same-day pair rules.
  Restored the canonical qualifying-pair read; integration proof in progress.
- CPT vaccination source replayed its raw computation per shed: 125 loops,
  about 229,500 index scans, 11.48s query execution for 11 completed rows.
  Query-plan repair and semantic proof in progress.
- Evidence guards now reject degraded responses, preserve row counts and
  require actual API build identity. Matched Work Board harness compares
  legacy five-request fanout and bundled page with row identities.

Local proof so far: 693 admin-web tests; web production build; mock fidelity;
route-coverage tests; focused Go suites and Work Board/auth race tests pass.
Failing-before tests reproduced stale tasks, lost degraded-lane rows and
vaccination summary loss. Initial common CI failed assistant coverage and
AI tooling readiness; tooling rebuilt, coverage documented, final CI pending.

Baseline fixed-URL gate was green but warmed the removed response cache.
Stricter CBE uncached page: p95 267.8ms, p99 277.0ms, 101246 bytes.
CPT baseline failed every sample with missing vaccination data; those partial
responses are not valid speed evidence. Full fresh metrics remain pending.

Judge status: source/SQL/evidence reviews active. No promotion performed.
Detailed raw evidence and ongoing progress: `.codex-goatos-render/pr273-proof/`.

## PR273 resumed final validation — 2026-09-16

Scope remains PR branch only; no merge or deployment. Baseline 73c50f282. Further CPT canonical SQL optimization reduces the measured SQL plan from 209ms to 82ms while preserving all 11 full rows in the fixed-scope parity check. This is SQL evidence, not yet an HTTP under-500ms verdict. Deterministic evidence timestamp ties added.

First full local CI completed RED at 740f57129: ai-doctor stale index, grpc vulnerability, aggregate review/test markers, inline SQL scale guards, and MATERIALIZED CTE parsing guard. Source/guard fixes are prepared; govulncheck now reports zero reachable vulnerabilities. Final full CI rerun required after commit. New growth multiplicity/status/date tests are running in isolated PostgreSQL.

Browser proof now requires populated historical Work Board desktop/mobile, actual card selectors, no degraded warnings, runtime API SHA, and a live local-server launch receipt. Earlier 22-route Chrome run passed but was on an older build and empty current-day board; final rerun remains pending. Final baseline/after uncached HTTP measurements, judges, and PR push remain pending. No deployment performed.

### Final judge corrections
Vaccination summary now uses a live canonical aggregate so cached zero lane counts cannot suppress newly moved rows. Feed card metrics likewise read current state per request. Real isolated PostgreSQL mutation regressions cover both; PI10-case suite passes103.947s, weighing matrix/date suite passes91.785s, feed completion/reopen passes85.328s. Timestamp ties now use stable task/submission/completion IDs. Browser fixture uses the actual procurement answer function and stylesheet with explicitly synthetic props and passes desktop/mobile.

Baseline repeat20 HTTP: CBE page p95=303.72ms/p99=353.11ms; CPT page p95=1210.26ms/p99=1297.51ms with20/20 degraded responses, so these are failed-response timings rather than healthy baseline performance. CPT legacy fanout p95=860.34ms/p99=860.78ms also degraded20/20. Final rebuilt HTTP/Chrome/full CI verification remains pending in the local proof ledger. Latest upstream rebase includes prototype-only main967c3683b. No push or deployment yet.

### Request-sharing validation and populated Chrome findings
909e1f66e bundled HTTP20-sample run: CBEp95=321.92ms/p99=383.58ms; CPTp95=424.35ms/p99=462.53ms. Both tails meet500ms, but CPTp90=399.96ms still exceeds300ms and one legacy fanout response degraded: not a green gate. Request memo is fresh per HTTP page, shares typed successful reads only, evicts errors, and isolates scope. PI complete-page counts match real canonical SQL counts (isolated PostgreSQL85.920s); overflow101/205 uses full aggregate and all11states/owner scope tested.
Raw canonical materialization further reduces query planning55ms→23ms and execution83ms→78ms, with exact11-row equality. Populated Chrome exposed intended8px avatar overlap and real milk-tag contrast failure. Guard now narrowly verifies reachable stacked targets; milk tag contrast improves3.82:1→5.23:1. No layout redesign or accessibility waiver. Final rebuilt API gate, Chrome sweep, all local CI and judge receipts remain pending; local proof ledger retains failures and final outputs.

### Final critical-path and evidence corrections
The two-lane/four-source to four-lane/two-source change retained healthy p95/p99 below500ms, but CPT p90 remained372.7ms at2a1e0ded6. Prepared-query and candidate-history-filter experiments were rejected rather than shipped without reliable sufficient benefit. PI now carries the existing canonical workforce user ID internally (not API JSON), removing a measured20.6–41.5ms trailing identity lookup with exact8-read/four-scope identity parity.
Main page summary workers now prefetch their own populated first-page lane rows sequentially within the existing per-summary9-worker bound. This overlaps fast-source rows with slow-source counts; normal sorted lane assembly and cursors reuse exact request-scoped query results. Empty, failed, out-of-scope and cursor intents skip; failed rows retry normally. Independent judge found no blocking correctness findings; focused race and scope/order/cursor/freshness/concurrency tests pass. Optional vocabulary retains its existing separate summary bound. Final HTTP acceptance remains pending.
Populated Chrome dev proof passes93cards on laptop/mobile (12/4/67/318 lane counts). Narrow avatar reachability/clipping tests pass. Final production24-route sweep still required. Evidence certification now rejects dirty build sources and dirty latency reports and requires final matching API identity. Node perf49tests pass. Full clean-commit local CI, production Chrome, final metrics and PR push remain pending. No merge/deploy performed.

### Production screenshot review correction
Full local CI passed clean8be3122f (allreceipt), and finalmatched HTTP gate passed: CBEpagep95/p99=235.12/237.79ms, CPT=287.89/292.35ms; no finaldegraded samples. Backend/SQLjudges approved. Screenshot judge found procurement had redirected the PCdirector to Approvals, so the24-route run is not accepted as complete procurement proof. Added strict origin/path/query preservation and real Animal purchases heading guards. An existing OCI CEO user has all required stored page grants; no grant or business-data changes were made. Browser proof will bind its actor/tenant to the local frontend runtime. Final guard commit requires refreshed exact-SHA local CI/API build/productionChrome beforepush. Orange-dot actual-component syntheticfixture passeddesktop/mobile; liveOCIprocurement has no rows.


## PR273 independent review follow-up — 2026-09-16

Scope: fix the default-Weights benchmark mismatch and cross-instance cache
freshness regression; push to the PR branch only. Baseline SHA
`e9e0a4c7c525f12b2a1b8c45caac1a70690dbd25`.

Done:
- Restored analytics cache TTL from 120s to 30s. Two repository instances
  reproduce the stale sibling-cache failure before the fix and expiry after it.
- Benchmark now uses Male and both weighing modes. It measures the same
  400-day date lookup as the landing pages, then derives August 3 through the
  observed latest weighing date for aggregate requests. The verifier rejects
  changed filters, sections, dates, and individual sample URLs.
- Added guards tying benchmark defaults to both product pages and an executable
  synthetic-HTTP test of the 16-endpoint benchmark workflow.

Validation: both targeted regressions failed before the fix;
`make api-latency-policy-test` passed 57/57;
`go test -race ./internal/weighing/adapters/postgres -count=1` passed (live
PostgreSQL cases remain opt-in and were not run); `git diff --check` passed.
AI Doctor was attempted: executables passed, isolated-checkout CRG/Repowise
indexes are missing. Source inspection was used; no graph evidence is claimed.
Self-review complete; no independent judge was requested for this follow-up.

Performance: this fixes workload validity and bounds freshness; no new real-DB
latency improvement is claimed. Full exact-SHA CI, real HTTP latency and browser
certification for the overall PR remain pending. No merge/main push/deployment.
Local continuation details: `.codex-goatos-render/pr273-independent-review/`.


## PR273 actor-binding review fix — 2026-09-16

Scope: fix and push the independent-review actor-binding finding to PR273 first,
per the maintainer's latest priority. PR274 integration/rebase, main landing and
staging/mobile deployment are pending and have not started.

Baseline/current committed SHA: `9f570d4cc0f83133690f23d63a7e25978ede263a`.
Done: reproduced comparator accepting different users with zero failures;
confirmed the API benchmark report does not record its authenticated actor.
Implementation complete: bind server-observed `/app/me` user identity to benchmark
start/end, before/after comparison, and browser proof. JWT tenant/header mismatch
and endpoint identity-header overrides fail closed. Cookie-only certification is
explicitly unsupported because it cannot establish the backend tenant binding.
No credentials or full profile responses are retained.
Tests: actor regressions failed before implementation; final
`make api-latency-policy-test` passed 64/64 and `git diff --check` passed.
Baseline focused Go suites and 147 Node tests also passed.
Known failures: the reported actor-binding defect is fixed; overall PR performance
certification remains pending as recorded below.
Before/after metrics: correctness-only evidence repair; no latency improvement
claimed. Final real-DB latency and production browser certification remain pending.
Judge: independent actor-fix review approved; no blocking findings. Reviewer
independently reran the 64 tests and diff check.
Deployment: none; no main push, merge, or deployment authorized before green proof.


## Combined PR273 + PR274 landing — 2026-09-16

Scope: combine PR273 actor fix `1a2bfbe12` (pushed and remote-verified) with
PR274 `bd8bc9e94`, rebase onto current main, repeat independent review/fixes,
then exact-head local CI and real product proof before main/staging/mobile.
Main baseline: `967c3683b`; active integration HEAD before merge: `1a2bfbe12`.
Done: fetched exact PR274; main rebase reports up to date; resolved SQL section
selection vs old weekly-grid switch, retained person-resolved weighing grants,
and kept both assistant coverage records. Regenerated OpenAPI clients.
Known failures discovered: legacy bool test call after merge (fixed); authored
window benchmark mismatch (agent fixing); required numeric null answer (fixed,
failing-before regression); Android non-finite numeric submission (agent fixing).
Tests: initial Go run failed legacy bool compile; corrected signature and focused
weighing/weighingsop/verification/adminui suite passed. Admin-web typecheck passed.
OCI dedicated integration tests for SOP/fasting/demographics/media are running;
no shared OCI data reset or staging mutation. No new performance numbers yet.
Judges: backend pass completed with numeric-null fix; frontend/perf and Android
reviews/fixes active. All final combined reviews, live browser/phone checks,
latency comparisons, screenshot CI receipt and landing remain pending.
Deployment: unchanged; neither PR merged, no staging/mobile build started.

### Combined review fixes and focused proof
Actor-binding fix was pushed to PR273 as `1a2bfbe12`. Integration includes
PR274 `bd8bc9e94` on main `967c3683b`. Fixed authored Weights benchmark policy
and stale latest-date parity; required JSON null numeric answers; Android invalid,
nonfinite and out-of-range supplied numbers (including optional `1,5`, `.`, `1.`),
with inline question feedback; and unsupported pick-many Other explanation authoring.
Frontend and backend reject unsupported Other configuration; pick-one is preserved.
Focused proof: performance 70 tests; frontend 63 plus 10 SOP model tests; Android
85 VM and 4 dispatch tests, then 10 final numeric/inline regressions; mobile guards;
Go weighing/weighingsop/verification/adminui suites; weighing and verification
Postgres integration (215.9s/171.2s). Dedicated full rules-source DB suite pending
(the earlier filtered command selected no tests in that package).
Judges: numeric/backend and policy integration independently passed; pick-many
restriction independently passed. No remaining confirmed scoped code defect.
Full exact-head CI, fresh before/after HTTP and production browser/phone proof
remain pending. An isolated database copy is being prepared without mutating the
shared source database. No new performance claim, merge, or deployment.

### Last review-loop edge cases and test-host recovery
Fixed optional pick-one Other without explanation (12 Android tests green, mobile
guard green, independent judge passed) and optional numeric null rendering as zero
(normalizer/verification renderer share numeric absence semantics; regression
failed before, domain tests pass). Final independent backend judge is reviewing.
Fresh full-source database copy exceeded test-host disk capacity and interrupted
the rules-source DB test. Removed only this task's failed clone and failed test
template; PostgreSQL recovered and shared source read-back returned 1681 goats.
Created a separate 567 MB copy of the existing disposable PR274 capture snapshot
instead; it contains 1681 goats/66 weighing campaigns. This is staging-like
snapshot proof, not current STG data. A historical migration302 checksum differs
in that existing test snapshot; migration runner's local-only allowance is used
only on this new disposable copy. No production migration checksum was changed.
Pending: rerun rules-source DB test, exact committed CI/screenshots, production
browser and physical phone evidence, matched performance results. Main still
967c3683b; deployment remains not started.

### Full-CI first pass corrections
First complete CI pass exposed a stale FastingCardSOPVersions test-double signature
(fixed), two internal SQL partition-key expressions misidentified as display
formatting (narrow reviewed annotations, no SQL behavior change), missing local
Playwright browser installation (installed), stale Repowise (rebuilding), and a
Gradle self-test classloader from a deleted temporary fixture. Unique disposable
script source identity fixes that cache collision; two consecutive adversarial
red/green runs passed without changing configuration-cache checks. Focused wiring
and weighing package tests and operational-location guard pass. Diagnostic real
SOP editor E2E passed desktop/mobile; all four screenshots visually checked.
The final-SHA CI/browser/API/phone receipts still require reruns. Continued live
status is recorded outside the source tree in tmp/pr273-274-release-progress.md.
No merge/deploy has occurred.
