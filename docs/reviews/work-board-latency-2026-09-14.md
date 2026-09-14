# Work Board Latency PR Evidence - 2026-09-14

## Goal

Stop the admin Work Board from taking the staging API back into 8s+ tail latency by removing
the per-render backend request burst, while preserving one-park backend bounds, per-lane
pagination, module vocabulary, and partial-degradation behavior.

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
waiting behind Work Board bursts. Analytics/events are not fixed by this PR; they need an infra
lane split so event ingestion cannot compete with business reads.

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

## Events Backpressure

- `/app/analytics/events` still runs on the main API service in this PR, but it no longer has
  unlimited synchronous DB insert concurrency per API instance. Staging pins
  `GOATOS_ANALYTICS_MAX_IN_FLIGHT=2`; when both slots are busy the handler returns `429` with
  `Retry-After: 5` before touching Postgres.
- This protects business reads from analytics write pileups without scaling the main API wide.
  Android already treats normal analytics as best-effort fire-and-forget; critical analytics events
  use the durable queue and will retry after a retryable failure.
- The next infra step is still a separate capped event-ingest lane/service/queue. That would isolate
  event backlog and billing completely from Work Board, weighing, vaccination, and bootstrap.

## Validation

- Passed: `go test ./internal/workboard/... ./internal/permissions`
- Passed: `npm --prefix apps/admin-web run typecheck -- --pretty false`
- Passed: `node --test --experimental-strip-types apps/admin-web/features/work-board/work-board-board.test.mjs`
- Passed: `npm --prefix apps/admin-web run check:request-plan-fanout`
- Passed: `make api-client-generate`
- Passed: `go test ./internal/vaccinationexecution/adapters/http ./internal/vaccinationexecution/app`
- Passed: `go test ./internal/feeddirection/app`
- Passed: `go test ./internal/appanalytics/adapters/http`

## Local OCI E2E Status

The branch was run with an isolated backend on `127.0.0.1:18080` against the sanctioned OCI tunnel
`127.0.0.1:15432`.

Blocked for full HTTP Work Board E2E: the OCI database currently is not staging-equivalent for this
scenario. It has no tenants/parks and had empty org-role seed tables, so normal HTTP auth returned
403 until local seed repair was attempted; after role seed repair the requested tenant still did not
exist. Treat `make oci-stg-db-parity` or an equivalent tenant/grant/park fingerprint as a required
precondition before accepting future "OCI reproduces staging" claims for this route.

## Infra Plan

- Restore API Cloud Run max scale to the intended Terraform value and prevent deploy restore from
  leaving staging capped at 2 instances. This PR uses a cost-capped API max of 4, not 10; noisy
  event traffic should move to a separate capped lane instead of forcing the business API to scale
  wide.
- Align Cloud Run concurrency with the backend DB pool. Current shape allows many HTTP requests to
  pile into a much smaller DB connection pool, which creates queueing.
- Split analytics/events into a separate ingestion lane: separate service and/or queue, separate
  DB pool, and eventually BigQuery/PubSub style ingestion. Events can be captured more often, but
  they must not have the same priority as Work Board, weighing, vaccination, or bootstrap reads.
- Add alerts for Work Board p95/p99, admin-web backend fetch timeout rate, Cloud Run instance cap
  pressure, DB backends, and event-ingestion backlog.

## Billing Impact

- This PR itself should reduce waste: fewer admin-web backend requests and less repeated auth/HTTP
  overhead.
- Raising API max scale or lowering concurrency can increase Cloud Run instance time during bursts,
  but it buys lower latency. This PR caps the API at 4 instances to bound that spend; if event bursts
  are frequent, the cost-safe fix is a separate events service/queue, not a higher business API cap.
- Upgrading Cloud SQL has a direct monthly cost increase; do it only if metrics show DB CPU/IO or
  connection pressure remains after request-shape fixes.
- Splitting events adds small service/queue/storage cost, but it protects business APIs from noisy
  telemetry and makes cost/performance visible per lane.
