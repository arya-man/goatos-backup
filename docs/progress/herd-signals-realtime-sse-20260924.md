# Herd Signals realtime live monitor - 2026-09-24

## Scope

- Add realtime movement fields that distinguish fresh-but-still animals from missing signal.
- Add SSE transport for the Herd Signals live monitor so the browser no longer relies on blind page polling.
- Add operator-selectable movement/baseline controls and live columns in admin-web.
- Preserve existing 15-minute activity semantics as sustained activity.

## Done

- Created isolated branch/worktree from `origin/main`: `feat/herd-signals-realtime-sse-20260924`.
- Added migration `000299_herd_signals_realtime_motion.sql`.
- Added latest-row fields:
  - `last_packet_motion_delta`
  - `last_packet_window_seconds`
  - `motion_delta_30s`
  - `motion_delta_60s`
  - `last_moved_at`
- Threaded fields through backend domain, ingest upsert, read queries, service response, and summary counters.
- Added backend SSE endpoint: `GET /herd-signals/live/stream`.
- Added admin-web stream bridge, explicit movement-window controls, and live table columns.
- Fixed subagent review blockers:
  - Migration version moved to `000395`.
  - Pause now closes the EventSource.
  - SSE emits lightweight ticks instead of duplicating full `/live` reads.
  - Reconnect gap totals do not count as realtime movement.
  - `last_moved_at` is preserved across fresh still packets.
  - Same-batch equal timestamps use the last reading.
  - Low-battery summary includes critical consistently.
  - KPI keys are distinct for moving now, active 1m, and moving last 15m.
  - 5m movement window has a real backend field.
- Fixed Codex review blockers:
  - SSE heartbeat now uses a 15s cadence and the client enforces a 15s minimum between full `router.refresh()` calls.
  - Low-battery KPI row filtering now includes `critical`, matching the backend summary aggregate.
- Replaced the bounded route-refresh bridge with data-bearing SSE `snapshot` events fanned out through Postgres `LISTEN/NOTIFY`.
- Fixed latest review blockers:
  - Stream clients subscribe before the initial snapshot, so an ingest committed during the first read is queued instead of lost.
  - Admin-web visible copy now uses pen vocabulary for peer comparison labels.
  - The Next SSE proxy route is explicitly dynamic and pinned to the Node runtime, matching the existing streaming proxy pattern.
- Fixed parallel review blockers:
  - Mobile/card `data-l` labels now use `Pen peers`.
  - OpenAPI `HerdSignalsSummary` includes the realtime `moving_now` and `active_1m` counters returned by the backend.
  - OpenAPI ingest prose documents server-stamped `received_at` and replay dedupe by `device_seen_at`/motion/date bucket.
- Fixed second review blockers:
  - Stream/export query construction now maps `moving_15m` and `quiet` KPI filters to backend `movement_state`, matching the server-rendered table.
  - SSE heartbeats now update connection freshness so a healthy idle stream does not show stale just because no data-bearing ingest arrived.
  - Backend `event: error` snapshot failures remain visible and are not masked by later heartbeat ticks.
  - OpenAPI `/herd-signals/live` prose now states that movement/live-state filters narrow rows while summary keeps the broader scoped cohort and exposes bucket counters.
- Fixed final contract-review blockers:
  - OpenAPI `HerdSignalItem` now includes the realtime fields returned by the backend and consumed by admin-web: `last_packet_motion_delta`, `last_packet_window_seconds`, `motion_delta_30s`, `motion_delta_60s`, `motion_delta_5m`, and `last_moved_at`.
  - Regenerated `packages/api-client/src/generated/app-api.ts` so `/herd-signals/live/stream`, `live_state`, summary counters, export filters, and item motion fields are present in the client contract.
- Fixed final parallel-review blockers:
  - Stream/export query construction now normalizes the same default `sort=smart_tag`, `dir=asc`, and `limit=25` as the server-rendered table key, so default URL snapshots update visible KPIs and rows.
  - The initial live-tab server read now drops stale `movement_state` whenever a realtime `live_state` KPI is active, matching stream/export semantics.
  - Admin-web Herd Signals live DTOs now alias generated `AppApiComponents["schemas"]` types, removing battery-trend drift from the hand-written DTO.
  - Battery trend formatting now uses the current generated contract: `stable|falling`, `window_days`, and endpoint readings.
  - Mapping writes now emit the same tenant-scoped live notification as ingest after syncing `herd_signal_tag_latest`, so open SSE views update on bind/unmap/replace without waiting for a later packet.
  - The Postgres listener now broadcasts one catch-up snapshot to open streams after every successful `LISTEN`, covering notifications missed during listener reconnect gaps.
  - Added guards for SSE event names/payload shapes, mapping-write notifications, listener reconnect catch-up, default live stream key normalization, and DTO generated-schema use.
  - Stream/export query construction now computes effective filters first and serializes them once in canonical server-key order, preventing filtered views from writing snapshots under a different store key.
  - Alerts tab no longer opens an SSE snapshot stream because it does not consume the live snapshot store.
  - Server-side movement KPI clicks now clear stale `hs_move`; changing the visible Movement select clears `hs_kpi`, so URL state and visible filters cannot contradict each other.
  - Added the missing admin-web `/api/herd-signals/export.csv` proxy route for table exports.
  - Export now preserves current table `sort`/`dir` through the admin-web proxy, backend handler, and service export walk.
  - Mapping tab export now clears movement/pattern filters that the visible Mapping table does not consume.
  - Page-only KPI export disablement is scoped to the Live tab, so leftover KPI URL state cannot disable Alerts export.
  - Fixed the stream handler test recorder data race found by `go test -race`.
  - Sorted CSV export pagination now advances with the encoded live-sort cursor instead of a bare tag id, preventing page-2 skips/duplicates under `smart_tag`, temperature, motion, or delta sorts.
  - Mapping tab export now also clears stale `risk_state`, matching the visible Mapping table.

## Pending

- Browser screenshot in Chrome.
- Final post-parallel-review local CI receipt and clean review.
- Landing attempt on 2026-09-24 rebased PR head onto `origin/main` at `3bf7ac59f54db780b6d13e26d5d7173e65d0beb8`, but `make land-main` did not push because local CI was red. Failing gates: leadership assistant coverage, postgres bind contract, duplicate migration version, admin-web lint/typecheck/build, and Android config-cache guard. Focused fixes are in progress before any retry.

## Design Notes

- `motion_delta` remains the existing 15-minute sustained activity value.
- `last_packet_motion_delta`, `motion_delta_30s`, and `motion_delta_60s` are the realtime movement fields.
- `last_moved_at` separates a resting animal with fresh packets from a tag/gateway that stopped reporting.
- Peers mean same shed/pen at the backend grouping level, but the admin-web UI copy uses pen vocabulary to satisfy the current visible-language guard.
- Current SSE implementation writes an initial `snapshot`, then pushes subsequent `snapshot` events after committed ingest notifications from Postgres `LISTEN/NOTIFY`. `tick` events are heartbeat-only and do not refresh the route.
- Baseline controls are intentionally limited to values backed by the current API: self Off/Last 24h and shed Now. Broader self windows (1h/6h/7d) and shed windows (1h/today) need backend query parameters and aggregates in a follow-up.

## Proof

- `go test ./internal/herdsignals/...` from `backend` - PASS after reviewer fixes.
- `npm run typecheck` from `apps/admin-web` - PASS after `npm ci` in isolated worktree. Local Node warned v23.1.0 while package asks for 24.x.
- `node --test --experimental-strip-types features/herd-signals/*.test.mjs` from `apps/admin-web` - PASS after reviewer fixes.
- Codex review-fix retest:
  - `go test ./internal/herdsignals/...` from `backend` - PASS.
  - `node --test --experimental-strip-types features/herd-signals/*.test.mjs` from `apps/admin-web` - PASS (15 tests).
  - `npm run typecheck` from `apps/admin-web` - PASS after `npm ci`; local Node still warns v23.1.0 while package asks for 24.x.
- Local browser route attempt:
  - Started admin-web on `http://127.0.0.1:3077` with `GOATOS_LOCAL_DEV_AUTO_AUTH=0` to avoid DB mutation.
  - `/herd-signals?scope_mode=company&hs_live_window=30s&hs_own_base=off&hs_shed_base=now` redirected to `/login`.
  - `/api/auth/firebase-config` returned 503, so authenticated visual verification is blocked in this local stack until a valid local auth/backend setup is provided.
- Codex post-review fixes on 2026-09-24:
  - `go test ./internal/herdsignals/... ./internal/permissions/...` from `backend` - PASS after the local race/copy fixes.
  - `node --test --experimental-strip-types apps/admin-web/features/herd-signals/herd-signals-live-table-controls.test.mjs apps/admin-web/features/herd-signals/responsive-css.test.mjs apps/admin-web/features/counts/pen-vocabulary.test.mjs` from repo root - PASS after the local race/copy fixes.
  - `node tools/agent-hooks/check-herd-signals-language.mjs` - PASS after the OpenAPI/copy fixes.
  - `npm --prefix apps/admin-web run check:ui-contract` - PASS after the OpenAPI/copy fixes.
  - `npm --prefix tools/contract-validation run validate` - PASS after installing that package's local dependencies with `npm --prefix tools/contract-validation ci`.
  - `git diff --check` - PASS after the local race/copy fixes.
  - `npm --prefix apps/admin-web run typecheck` - PASS after installing admin-web dependencies with `npm --prefix apps/admin-web ci`; local Node v23.1.0 still warns because package asks for 24.x.
  - `npm --prefix apps/admin-web test` - PASS after installing Playwright Chromium with `npm --prefix apps/admin-web exec playwright install chromium`; 1078 tests passed.
  - Accidental full `npm --prefix apps/admin-web test -- ...` expanded the whole suite and failed; relevant failure was `features/counts/pen-vocabulary.test.mjs` on Herd Signals `shed` labels, plus unrelated missing `typescript` failures in other tests.
- Final contract/client fix:
  - `make api-client-generate` - PASS; regenerated `packages/api-client/src/generated/app-api.ts` from the updated OpenAPI contract.
  - `go test ./internal/herdsignals/... ./internal/permissions/...` from `backend` - PASS after the final contract/client fix.
  - `node --test --experimental-strip-types apps/admin-web/features/herd-signals/herd-signals-live-table-controls.test.mjs apps/admin-web/features/herd-signals/responsive-css.test.mjs apps/admin-web/features/counts/pen-vocabulary.test.mjs` - PASS after the final contract/client fix.
  - `npm --prefix tools/contract-validation run validate` - PASS after the final contract/client fix.
  - `git diff --check` - PASS after the final contract/client fix.
  - `npm --prefix apps/admin-web run typecheck` - PASS after the final contract/client fix.
  - `node tools/agent-hooks/check-herd-signals-language.mjs` - PASS after the final contract/client fix.
  - `npm --prefix apps/admin-web run check:ui-contract` - PASS after the final contract/client fix.
  - `npm --prefix apps/admin-web test` - PASS after the final contract/client fix; 1078 tests passed.
  - `make api-client-check` before commit regenerated cleanly but failed on the intentional uncommitted generated-client diff; rerun after commit is required to prove no generator drift.
- Final parallel-review fixes:
  - `node --test --experimental-strip-types apps/admin-web/features/herd-signals/herd-signals-live-table-controls.test.mjs apps/admin-web/features/herd-signals/responsive-css.test.mjs apps/admin-web/features/counts/pen-vocabulary.test.mjs` - PASS after default stream key, DTO, and formatter fixes.
  - `npm --prefix apps/admin-web run typecheck` - PASS after generated-schema DTO aliases.
  - `go test ./internal/herdsignals/... ./internal/permissions/...` from `backend` - PASS after mapping notify and listener catch-up fixes.
  - `node --test --experimental-strip-types apps/admin-web/features/herd-signals/herd-signals-live-table-controls.test.mjs apps/admin-web/features/herd-signals/responsive-css.test.mjs apps/admin-web/features/counts/pen-vocabulary.test.mjs` - PASS after canonical stream-key, Movement/KPI, Alerts stream-consumption, and export proxy fixes.
  - `npm --prefix apps/admin-web run typecheck` - PASS after adding the export proxy route.
  - `git diff --check` - PASS after the latest frontend fixes.
  - `node --test --experimental-strip-types apps/admin-web/features/herd-signals/herd-signals-live-table-controls.test.mjs apps/admin-web/features/herd-signals/responsive-css.test.mjs apps/admin-web/features/counts/pen-vocabulary.test.mjs` - PASS after export-sort/filter fixes.
  - `go test ./internal/herdsignals/... ./internal/permissions/...` from `backend` - PASS after export-sort and recorder-race fixes.
  - `go test -race ./internal/herdsignals/adapters/http ./internal/herdsignals/adapters/postgres ./internal/herdsignals/app` from `backend` - PASS after fixing the test-recorder race.
  - `npm --prefix apps/admin-web run typecheck` - PASS after export-sort/filter fixes.
  - `node --test --experimental-strip-types apps/admin-web/features/herd-signals/herd-signals-live-table-controls.test.mjs apps/admin-web/features/herd-signals/responsive-css.test.mjs apps/admin-web/features/counts/pen-vocabulary.test.mjs` - PASS after sorted export cursor and Mapping risk-filter fixes.
  - `go test ./internal/herdsignals/... ./internal/permissions/...` from `backend` - PASS after sorted export cursor and Mapping risk-filter fixes.
  - `go test -race ./internal/herdsignals/adapters/http ./internal/herdsignals/adapters/postgres ./internal/herdsignals/app` from `backend` - PASS after sorted export cursor and Mapping risk-filter fixes.
  - `npm --prefix apps/admin-web run typecheck` - PASS after sorted export cursor and Mapping risk-filter fixes.
- Landing guard repair on 2026-09-24:
  - `make land-main` at `9690e1b0c9f1ca6b2d41e3c1d0d50db5ce45bae9` - RED; it did not push main. Failing gates were `agent: aggregate-projection` and `admin-web unit tests`.
  - `make aggregate-projection-guard` - PASS after adding changed projection-review coverage evidence for existing Herd Signals OneToMany, MultiPage, ScopeHierarchy, and StatusMatrix integration tests.
  - `node --test --experimental-strip-types apps/admin-web/features/herd-signals/herd-signals-live-table-controls.test.mjs` - PASS after updating the SSE reconnect assertion for the memoized `streamHref` dependency.
