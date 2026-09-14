# Work Board Latency PR Evidence - 2026-09-14

## Goal

Stop the admin Work Board from taking the staging API back into 8s+ tail latency by removing
the per-render backend request burst, while preserving one-park backend bounds, per-lane
pagination, module vocabulary, and partial-degradation behavior.

## Hard Acceptance Screens

This PR is not acceptable if normal mobile/admin-web use can still show the 2026-09-14 failure
screens that triggered this work:

- `Admin-web contract unavailable` / `backend_down`
- `The board could not be loaded. Try again.`
- `Weights could not be loaded`

Judges and reviewers must treat those strings as explicit E2E failure markers. Browser evidence for
this PR must load the real Work Board and Weights pages and fail if any of those strings render.
API-only timing is not enough for final acceptance.

## Progress Checklist

Current PR: <https://github.com/vgoats/goatos/pull/259>  
Current head: `b58b5a97d9d97c714b7677d6ae99a472a56f99f9`

| Item | Status | Evidence / next action |
| --- | --- | --- |
| Find the user-visible outage screens | Done | Screenshots showed Work Board `backend_down`, board load failure, and Weights load failure. |
| Identify root cause class | Done | Work Board fanout and slow source reads were the main business API failure path; events were noisy shared-lane pressure. |
| Reproduce with OCI/local staging DB path | Done | OCI restored from staging refresh dump, measured with default tenant, 2 parks, 1,681 goats, 2,231 weighing observations. |
| Before API numbers | Done | Origin-main fanout: p50 9.5s, p95 11.5s, max 11.5s. First bundled attempt: p50 20.1s, p95 20.3s. |
| Backend Work Board fix | Done | Bundled `/work-board/page`, lane short-circuit from summary, feed empty-day no-degrade, vaccination precheck state-aware. |
| API after numbers | Done | Final all-parks Work Board: p50 270ms, p95 465ms, max 898ms cold first request; no degraded modules. |
| Frontend Work Board fix | Done | Admin-web reads one bundled page per active park and caps all-parks backend page concurrency at 2. |
| Weights failure guard | Done | Visual smoke covers `/weighing/weights` desktop/mobile and fails on `Weights could not be loaded`. |
| Exact outage string guards | Done | Visual smoke and sidebar latency checks fail on `Admin-web contract unavailable`, `backend_down`, board load failure, and Weights load failure. |
| Local browser E2E | Done | Production-build visual smoke passed for Work Board and Weights on laptop and mobile. |
| Local browser interaction timing | Done | Work Board sidebar: 519ms cold, then 414ms, 316ms, 262ms, 295ms. Weights warms to 632ms, 372ms, 389ms after cold route costs. |
| Lighthouse local score | Done | Work Board 84 performance / 100 accessibility; Weights 83 performance / 100 accessibility. |
| Events isolation | Done in PR, pending live proof | PR adds capped event lane (`min=0`, `max=1`, DB pool 2) and route tooling. Must verify after STG deploy. |
| Billing guard | Done in PR, pending live proof | Business API is kept at max 2; event traffic moves to the separate max 1 event lane instead of raising business API scale. |
| Slow API inventory | Done | Baseline table below lists every observed >1s API in the 14:40-15:00 IST window, not only Work Board. |
| `/work-board/rows` and `/work-board/summary` | Done | Replaced by bundled `/work-board/page`; local OCI p95 is 465ms for full all-parks Work Board after fixes. |
| `/admin-web/bootstrap` | Pending live proof | Not directly rewritten in this PR; expected to improve from less shared API/DB queueing and event split. Must verify from live logs after deploy. |
| `/feed-packing/worklist` | Inspected, follow-up needed | Worklist summary is an intentionally whole-filter draw. No risky default-shape change in this PR; needs explicit page-only/no-summary contract if still >500ms live. |
| `/weighing/leadership/growth` and `/weighing/shed-weights` | Guarded, pending live proof | Weights web E2E added and Work Board module reads improved. Dedicated endpoint tuning still requires live after metrics. |
| `/app/vaccination/execution` | Done | App route now opts out of expensive card summaries unless requested. |
| `/app/proofs/.../complete` | Pending investigation | Only 2 slow samples in baseline; not proven as repeated offender yet. Needs live query/log drilldown after deploy. |
| `/app/leadership-tasks` | Pending investigation | Only 3 slow samples in baseline; not proven as repeated offender yet. Needs live query/log drilldown after deploy. |
| `/auth/session-events` | Pending investigation | Small sample baseline. Need live post-deploy logs to separate cold/queueing/auth path cost. |
| Judge review | Done for current iteration | Backend/admin judges reviewed; findings were folded into latest fixes. |
| PR raised/pushed | Done | PR #259 is open, mergeable, not draft, at `b58b5a97...` before this billing-cap correction. |
| STG deploy | Pending | Official deploy path requires landed `origin/main` or explicit break-glass. Do not deploy this PR as normal STG until merge/landing decision. |
| Live STG verification | Pending | After deploy: Cloud Run logs, event route split, Work Board/Weights live E2E, public PageSpeed/Lighthouse. |
| Final completion | Pending | Requires STG deploy/live verification or explicit instruction to stop at PR-only. |

## What Was Happening

- Live staging screenshots at 14:47-14:50 IST showed `/work-board` failing and the shell reporting
  `backend_down` on vaccination data.
- Staging API logs for 14:40-15:00 IST showed several APIs over 1s. Work Board was the largest
  repeated admin-web offender: `/work-board/rows` averaged about 19.9s and hit about 48s max;
  `/work-board/summary` averaged about 5.5s and hit about 16s max.
- Other APIs were also slow in the same window: `/admin-web/bootstrap` averaged about 7.6s,
  `/feed-packing/worklist` about 4.9s, `/weighing/leadership/growth` about 2.2s,
  `/weighing/shed-weights` about 2.1s, and `/app/vaccination/execution` about 1.1s.
- The admin page opened "All parks" by firing 10 backend requests in parallel: two summaries and
  eight lane row reads. With a module filter it could add more summary reads for vocabulary.
- Mobile analytics/events were high volume and shared the same API/DB lane, but Work Board was the
  business API that actually timed out. Events are an amplifier/noise source, not the only root cause.

## Code Fix

- Added `GET /work-board/page`, a one-park bundled page read returning:
  - whole-filter summary,
  - optional unfiltered vocabulary summary,
  - one lane page per Work Board lane,
  - degraded modules, own-row flag, park and business-date metadata.
- Switched admin-web `/work-board` to call one bundled page per active park instead of summary plus
  one rows call per lane.
- Registered route permission parity with existing Work Board reads.
- Added OpenAPI schema/operation and regenerated the TypeScript API client.

## Expected API Latency Improvement

This PR primarily improves admin-web Work Board pressure:

- one park: 5-6 backend calls per render -> 1 backend call
- two parks: 10-12 backend calls per render -> 2 backend calls

That should reduce Work Board wall time and, more importantly, reduce burst pressure on the shared
API instance and DB pool. Weighing and vaccination should improve indirectly because they stop
waiting behind Work Board bursts. Analytics/events are split into a separate capped staging lane in
this PR so event ingestion cannot compete with business reads after the PR is deployed and the
event path routing script has been applied.

## Corrected Baseline Metrics

Log source: `goatos-api-stg`, `jsonPayload.msg="http_request"`, 2026-09-14 14:40-15:00 IST.

Percentiles use a conservative nearest-rank read for small samples; for 2-3 calls, p95 is effectively
the slowest observed call.

| API | Count | Avg | P95 | Max |
| --- | ---: | ---: | ---: | ---: |
| `/work-board/rows` | 21 | 19.9s | 48.0s | 48.0s |
| `/app/proofs/.../complete` | 2 | 18.1s | 36.2s | 36.2s |
| `/app/leadership-tasks` | 3 | 11.6s | 34.6s | 34.6s |
| `/admin-web/bootstrap` | 6 | 7.6s | 25.3s | 25.3s |
| `/work-board/summary` | 7 | 5.5s | 15.0s | 16.0s |
| `/feed-packing/worklist` | 12 | 4.9s | 29.1s | 29.1s |
| `/auth/session-events` | 4 | 4.0s | 8.1s | 8.1s |
| `/weighing/leadership/growth` | 11 | 2.2s | 11.8s | 11.8s |
| `/weighing/shed-weights` | 16 | 2.1s | 14.5s | 14.5s |
| `/app/vaccination/execution` | 6 | 1.1s | 1.4s | 1.5s |

## Feed/Vaccination Follow-up

- `/app/vaccination/execution` had a safe request-shape fix available: the service already supports
  skipping page-independent card summaries with `include_card_summaries=false`, but the app route
  left the tri-state unset and therefore inherited the legacy expensive default. The app route now
  sets the flag false unless a caller explicitly asks for card summaries.
- `/feed-packing/worklist` was inspected but not changed. Its summary is intentionally a whole
  filtered worklist store draw, not a page summary, and tests pin that invariant. A safe feed
  performance change should be an explicit fast response shape (for example an opt-in
  page-only/no-summary variant) plus an Android/admin contract update; changing the default summary
  would risk under-drawing feed for operators.

## Events Separation

- `/app/analytics/events` now has two protections in this PR:
  - the handler has a per-instance non-blocking insert cap via `GOATOS_ANALYTICS_MAX_IN_FLIGHT=2`;
  - staging has a separate `goatos-analytics-events-stg` Cloud Run service using
    `GOATOS_API_ROUTE_MODE=events`, `GOATOS_PG_MAX_CONNS=2`, min scale `0`, max scale `1`, and
    concurrency `20`.
- Event-only route mode registers health/version plus `POST /app/analytics/events`; it deliberately
  skips Work Board, weighing, vaccination, proof upload/download signed routes, auth session-events,
  admin UI, and the rest of the business API.
- The Cloud Deploy scripts now update and verify the event lane on every backend rollout, so it does
  not stay on a stale image after this PR lands. The deploy path uses `gcloud run deploy` for the
  events service, so the first rollout can create it if Terraform has not applied the Cloud Run
  service yet. It fails closed unless the dedicated `goatos-events-stg` service account exists,
  preserving the separate service identity, route mode, event cap, DB pool cap, min scale `0`, and
  max scale `1`.
- Remaining deployment requirement: live traffic must be routed to the events lane. The safe
  transparent production shape is a URL-map path rule sending `/app/analytics/events` to
  `goatos-analytics-events-stg` while all business API paths stay on `goatos-api-stg`. This PR adds
  `tools/deploy/stg-analytics-events-routing.sh` to create/use the serverless NEG/backend service
  and patch the existing non-Terraform URL map safely. Until that route is applied, the backpressure
  cap protects the main API but Android clients using the current API base URL still post events to
  the main API service.
- Live check while preparing the PR update: `goatos-stg-dashboard-map` had `api-host` defaulting to
  `goatos-api-stg-backend` with no path rules, and `goatos-analytics-events-stg` did not exist yet.
  Therefore a new staging deploy from this PR is required before claiming live event separation.

## Validation

- Passed: `go test ./internal/workboard/... ./internal/permissions`
- Passed: `npm --prefix apps/admin-web run typecheck -- --pretty false`
- Passed: `node --test --experimental-strip-types apps/admin-web/features/work-board/work-board-board.test.mjs`
- Passed: `npm --prefix apps/admin-web run check:request-plan-fanout`
- Passed: `make api-client-generate`
- Passed: `go test ./internal/vaccinationexecution/adapters/http ./internal/vaccinationexecution/app`
- Passed: `go test ./internal/feeddirection/app`
- Passed: `go test ./internal/appanalytics/adapters/http`
- Passed: `go test ./internal/feeddirection/adapters/boardsource ./internal/processintegrity/adapters/boardsource ./internal/workboard/adapters/http ./internal/workboard/... ./migrations/postgres ./internal/bootstrap ./internal/appanalytics/adapters/http`
- Passed: `node --test scripts/smoke-visual-route-coverage.test.mjs`
- Passed: `npm test -- --test-name-pattern='work-board|weights page|growth director|visual smoke|Lighthouse'`
- Passed: `bash -n tools/deploy/stg-clouddeploy-task.sh tools/deploy/stg-analytics-events-routing.sh tools/deploy/stg-clouddeploy-release.sh tools/deploy/stg-cloudbuild-release.sh`
- Passed: `node --test tools/deploy/stg-admin-web-traffic-order.test.mjs`
- Passed: `npm run build`
- Passed: `git diff --check`
- Passed after final latency edits: `go test ./internal/feeddirection/adapters/boardsource ./internal/processintegrity/adapters/boardsource ./internal/workboard/adapters/http ./internal/workboard/... ./migrations/postgres`
- Passed after final latency edits: `npm test -- --test-name-pattern='work-board|visual smoke|Lighthouse|weights page|growth director'`
- Passed after final latency edits: production-build visual smoke for Work Board and Weights on laptop and mobile.

## Local OCI E2E

The branch was run with an isolated backend on `127.0.0.1:18181` against the sanctioned OCI tunnel
`127.0.0.1:15432`. Before applying this PR's migrations, the OCI database was refreshed from
`local-data/goatos-stg-to-oci/backups/20260910-024950/stg-refresh.dump`; the previous OCI contents
were backed up at
`local-data/goatos-stg-to-oci/backups/20260914-162535-pre-pr259-refresh/oci-before-pr259-refresh.dump`.
The restored database had the default tenant, 2 parks, 1,681 goats, 2,231 weighing observations,
136 feed direction issues, and 238 weighing work items.

Business date used for Work Board reproduction: `2026-08-10`.

### Before/After API Numbers

| Scenario | Before | After |
| --- | ---: | ---: |
| Origin-main admin-web fanout, all parks, 10 calls/sample | p50 9.5s, p95 11.5s, max 11.5s | replaced by 2 bundled page calls/sample |
| Initial bundled page before lane/source fixes | p50 20.1s, p95 20.3s, max 20.3s | fixed |
| Final bundled Work Board page, all parks parallel | p50 270ms, p95 465ms, max 898ms cold first request | current PR |
| Feed module page, all parks parallel | p50 496ms, p95 842ms | current PR |
| Weighing module page, all parks parallel | p50 315ms, p95 585ms | current PR |
| Health module page, all parks parallel | p50 336ms, p95 534ms | current PR |
| Vaccination module page, all parks parallel | p50 239ms, p95 333ms | current PR |
| PC Care module page, all parks parallel | p50 248ms, p95 360ms | current PR |
| Verification module page, all parks parallel | p50 296ms, p95 510ms | current PR |
| Counts module page, all parks parallel | p50 249ms, p95 578ms | current PR |
| Milk module page, all parks parallel | p50 190ms, p95 444ms | current PR |

Plain-English read: the PR kills the 8-20s class, removes the request burst that was taking the
site down, and brings the measured full all-modules/all-parks Work Board read under 500ms p95 after
the initial cold request. The final run returned no degraded modules. The remaining outlier is the
first cold sample at 898ms, so staging should still be watched for cold-start/pool-warm behavior.

Correctness note: the vaccination fast precheck is state-aware. It excludes completed rows only when
the requested lane/state filter cannot include Done work; whole-board and Done reads still preserve
completed vaccination visibility.

### Browser E2E Numbers

Production build served locally on `127.0.0.1:3401` against the OCI-backed API:

| Route | Local interaction result |
| --- | ---: |
| `/work-board` sidebar navigation | 519ms cold, then 414ms, 316ms, 262ms, 295ms |
| `/weighing/weights` direct route fallback | 4,209ms cold, 2,884ms second, then 632ms, 372ms, 389ms |

The route guard now fails if these visible strings appear: `Admin-web contract unavailable`,
`backend_down`, `The board could not be loaded`, or `Weights could not be loaded`.

Passed against desktop and mobile viewports:

```text
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3401 \
GOATOS_API_BASE_URL=http://127.0.0.1:18181 \
GOATOS_SMOKE_ONLY_ROUTES=work-board,weighing-weights \
npm run smoke:visual:live
```

Screenshot proof was captured under
`.codex-goatos-render/admin-web-screenshots/2026-09-14T12-09-27-327Z`; the mobile Work Board and
Weights images were visually checked and showed loaded pages, not the failure screens above.

### Lighthouse

Local Lighthouse was run against the rebuilt production admin-web server. PageSpeed Insights was not
run locally because it needs a public URL; run it against STG after deployment for the public Google
score.

| Route | Performance | Accessibility | Best Practices | SEO |
| --- | ---: | ---: | ---: | ---: |
| `/work-board` | 84 | 100 | 96 | 100 |
| `/weighing/weights` | 83 | 100 | 96 | 100 |

## Infra Plan

- Keep API Cloud Run max scale at the intended staging billing cap of 2 instances. Noisy event
  traffic moves to a separate capped lane instead of forcing the business API to scale wider.
- Align Cloud Run concurrency with the backend DB pool. Current shape allows many HTTP requests to
  pile into a much smaller DB connection pool, which creates queueing.
- Route `/app/analytics/events` to the separate `goatos-analytics-events-stg` service. Events can be
  captured more often, but they must not have the same priority as Work Board, weighing,
  vaccination, or bootstrap reads.
- Add alerts for Work Board p95/p99, admin-web backend fetch timeout rate, Cloud Run instance cap
  pressure, DB backends, and event-ingestion backlog.

## Billing Impact

- This PR itself should reduce waste: fewer admin-web backend requests and less repeated auth/HTTP
  overhead.
- Raising API max scale or lowering concurrency can increase Cloud Run instance time during bursts.
  This PR keeps the business API capped at 2 and uses request-shape fixes plus event isolation for
  latency instead of buying performance by widening the main API.
- Upgrading Cloud SQL has a direct monthly cost increase; do it only if metrics show DB CPU/IO or
  connection pressure remains after request-shape fixes.
- Splitting events adds at most one small Cloud Run instance during event bursts in this PR
  (`min=0`, `max=1`) plus two database connections. It protects business APIs from noisy telemetry
  without raising the main API above the cost-capped max of 2.
