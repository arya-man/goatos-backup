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

## Pending

- Browser screenshot in Chrome.
- Commit, push, PR.

## Design Notes

- `motion_delta` remains the existing 15-minute sustained activity value.
- `last_packet_motion_delta`, `motion_delta_30s`, and `motion_delta_60s` are the realtime movement fields.
- `last_moved_at` separates a resting animal with fresh packets from a tag/gateway that stopped reporting.
- Peers mean same shed/pen. Existing risk comparison already groups by shed; UI copy should call it shed peers, not pen group.
- Current SSE implementation emits lightweight refresh ticks on a short server ticker. It removes browser interval polling and avoids duplicate full live reads. A follow-up hardening should replace the stream ticker with Postgres `LISTEN/NOTIFY` or another cross-instance fanout so Cloud Run instances push only on ingest updates.
- Baseline controls are intentionally limited to values backed by the current API: self Off/Last 24h and shed Now. Broader self windows (1h/6h/7d) and shed windows (1h/today) need backend query parameters and aggregates in a follow-up.

## Proof

- `go test ./internal/herdsignals/...` from `backend` - PASS after reviewer fixes.
- `npm run typecheck` from `apps/admin-web` - PASS after `npm ci` in isolated worktree. Local Node warned v23.1.0 while package asks for 24.x.
- `node --test --experimental-strip-types features/herd-signals/*.test.mjs` from `apps/admin-web` - PASS after reviewer fixes.
- Local browser route attempt:
  - Started admin-web on `http://127.0.0.1:3077` with `GOATOS_LOCAL_DEV_AUTO_AUTH=0` to avoid DB mutation.
  - `/herd-signals?scope_mode=company&hs_live_window=30s&hs_own_base=off&hs_shed_base=now` redirected to `/login`.
  - `/api/auth/firebase-config` returned 503, so authenticated visual verification is blocked in this local stack until a valid local auth/backend setup is provided.
