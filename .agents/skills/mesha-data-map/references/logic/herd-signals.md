Index: KPIs (tags seen, mapped/unmapped, moving now, active 1m, moving 15m, quiet, weak signal, missing signal, low battery) · live table columns (movement/pattern/signal/battery/risk, baseline, 24h motion) · gateway health (status, tags recent, weak, unmapped, 15m window stats) · 12 insight cards · tag timeline/history chart · tag activity feed · mapping tab counts.

# Herd Signals (RFID / BLE smart tags) logic card

Screen: `apps/admin-web/app/(admin)/herd-signals` -> `features/herd-signals/*`; client `apps/admin-web/lib/api/herd-signals.ts`.
Backend: `backend/internal/herdsignals` (routes `adapters/http/handler.go:135-148`, service `app/service.go`, SQL `adapters/postgres/repository.go` = `repo.go` below).
Source tables (public schema, no `ceo_ai.*` view yet): `herd_signal_tag_latest` (1 row per tag; the base of every KPI), `herd_signal_activity_windows` (pre-bucketed 60/300/3600s tiers), `herd_signal_packets` (raw), `herd_signal_gateways`, `goat_identifiers`, `goats`, `locations`.
Verified on goatos-stg 2026-09-24 ~23:47 IST (single tenant `00000000-0000-4000-8000-000000000001`, 19 tags, 1 gateway).

## Global rules (apply to every entry)
- Everything is "now()"-relative, rolling, UTC timestamps. No IST day boundary anywhere on this screen. For "aaj" questions use `date_trunc('day', now() at time zone 'Asia/Kolkata') at time zone 'Asia/Kolkata'`.
- Stale = `now() - last_seen_at > 30 min` (`repo.go:714` `staleAfterInterval`). Read-time override: movement_state -> `'stale'`, pattern_state -> `'missing'` (`repo.go:716-717`). Always apply these CASEs; stored states are ingest-time and go stale.
- movement_state (stored at ingest, 15-min motion_count delta, `domain/motion.go:51`): `>=100 moving`, `10-99 low`, `1-9 quiet`, `0 not_moving`. Note "low" has NO KPI card.
- Tag -> goat mapping: `goat_identifiers` where `status='active' AND smart_tag_capable` and `normalized_value IN (UPPER(BTRIM(tag_id)), UPPER(BTRIM(tag_mac)))`, LIMIT 1 (`repo.go:725-741`). Location = `goats.shed_id` -> `locations`. The tag table stores no shed; a moved goat moves its tag.
- `tl.mapping_state` ('mapped'/'unmapped'/'conflict') is written at ingest; the goat join is resolved at read time. They can disagree after a replace/unmap until next packet.
- Monitoring boundary: `tl.animal_monitoring_since` (set on bind). Baselines, correlated insights and the activity feed ignore anything before it; NULL = no animal => no baseline.
- Packet dedupe: `ON CONFLICT (tenant_id, tag_id, device_seen_at, motion_count, received_date) DO NOTHING` (`repo.go:196`). Don't count raw packets for "reads"; use activity_windows.packet_count.
- Gap delta: a delta after a >30 min reception hole is flagged `gap_delta=true` and excluded from baseline, 24h sums, spike and risk.
- Pens = shed + partition label (see `../pens.sql`). Gateway/location display uses `oploc.OperationalLocation{ShedName, PartitionLabel}`.
- Motion units are an opaque accelerometer counter delta, NOT steps/distance. Never call it steps.

## KPI strip (`herd-signals-kpis.tsx:74-155`) - GET /herd-signals/live `summary`
SQL: `repo.go:883-915` `computeSummary` (same WHERE as the table, minus movement_state/live_state/cursor). If `risk_state` filter is set, summary is computed in Go instead (`service.go:988 summaryFromItems`, MappedAnimals = distinct goats there).

| KPI (label) | CEO asks | Formula | stg value |
|---|---|---|---|
| Tags seen | "kitne tag dikh rahe", "total tags" | count(*) of tag_latest rows (ever seen, incl. stale) | 19 |
| detail: mapped animals / unmapped | "kitne janwar pe tag laga" | `mapping_state='mapped'` / `'unmapped'` (counts TAGS, not distinct goats) | 19 / 0 (19 distinct goats) |
| Moving now | "abhi kaun chal raha" | `now()-last_seen_at<=30s AND last_packet_motion_delta>0` | 1 |
| Active 1m | "kitne janwar active" (last minute) | `now()-last_seen_at<=90s AND motion_delta_60s>0` | 3 |
| Moving last 15m | "kitne chal phir rahe" | effective movement_state='moving' (delta>=100 in 15m) | 2 |
| Quiet tags | "kaun shaant hai" | effective movement_state='quiet' (1-9) | 2 (plus not_moving 7, low 8 not on a card) |
| Weak signal | "signal kamzor" | `signal_state='weak'` (ingest: RSSI <= -75 dBm or avg <= -80) | 0 |
| Missing signal | "kaunsa tag band hai", "tag ka signal nahi aa raha" | effective movement_state='stale' (>30 min no packet) | 0 |
| Low battery | "battery kam" | `battery_state IN ('low','critical')` stored at ingest (<2800 mV low, <2600 critical) | 0 |
| (summary only) sensor_abnormal | "sensor kharab" | temperature_sensor_ok IS FALSE OR accelerometer_sensor_ok IS FALSE | 0 |

```sql
SELECT count(*) tags_seen,
 count(*) FILTER (WHERE mapping_state='mapped') mapped, count(*) FILTER (WHERE mapping_state='unmapped') unmapped,
 count(*) FILTER (WHERE now()-last_seen_at<=interval '30 seconds' AND COALESCE(last_packet_motion_delta,0)>0) moving_now,
 count(*) FILTER (WHERE now()-last_seen_at<=interval '90 seconds' AND COALESCE(motion_delta_60s,0)>0) active_1m,
 count(*) FILTER (WHERE (CASE WHEN now()-last_seen_at>interval '30 minutes' THEN 'stale' ELSE movement_state END)='moving') moving_15m,
 count(*) FILTER (WHERE (CASE WHEN now()-last_seen_at>interval '30 minutes' THEN 'stale' ELSE movement_state END)='quiet') quiet,
 count(*) FILTER (WHERE (CASE WHEN now()-last_seen_at>interval '30 minutes' THEN 'stale' ELSE movement_state END)='stale') missing_signal,
 count(*) FILTER (WHERE signal_state='weak') weak,
 count(*) FILTER (WHERE battery_state IN ('low','critical')) low_battery
FROM public.herd_signal_tag_latest tl;   -- 19|19|0|1|3|2|2|0|0|0
```
Traps: moving_now/active_1m flicker second-to-second (screen and your query will differ by +-1-2). "Tags seen" includes dead tags. Battery shown on the table can be escalated to watch/critical by trend (Go, `BatteryStateWithTrend`) but the KPI uses only stored state.

## Filters (GET /herd-signals/live query params -> SQL, `repo.go:751-843`)
- `park_id` / `shed_id` -> `g.park_id` / `g.shed_id` via the goat join (unmapped tags drop out when filtered by pen).
- `movement_state` -> effective movement CASE = value (table only, not summary).
- `live_state=moving_now|active_1m` -> the 30s / 90s predicates above.
- `mapping_state` -> `tl.mapping_state` (Mapping tab uses this: mapped/unmapped/conflict).
- `pattern` -> effective pattern CASE = value; `pattern=not_normal` (Alerts tab) = pattern NOT IN ('normal','no_movement') OR weak OR battery low/critical OR accelerometer false OR mapping conflict.
- `risk_state=low|watch|high|attention` -> Go-only (below).
- `q` -> ILIKE on tag_id, tag_mac, gateway_id, goat display_id, shed name, breed, sex, animal_identifier_1/2.

## Live table columns (`herd-signals-animals-table.tsx`, `service.go:634-841`)
- Animal/pen: goat join above; pen label = shed name + goat's partition label (fallback shed partition).
- Last seen, RSSI, battery mV, temp, motion deltas (30s/60s/5m/15m/1h): straight from tag_latest columns.
- Pattern: effective pattern_state (Go at ingest, `motion.go:197`): missing (>30m) > spike (15m delta > p75_baseline*3*2.5, non-gap) > recovered (was watched, now delta>=10) > inactive (>180 min consecutive windows <10) > quiet_watch (>90 min) > no_movement (delta 0).
- Baseline (`repo.go:1312`): p75 (percentile_disc) of 300s-bucket motion_delta, last 24h, packet_count>0, non-gap, after monitoring_since.
- 24h motion (`repo.go:1360`): SUM(motion_delta) 300s tier, last 24h, packet_count>0, non-gap. stg top: A00031 = 10148, A0002D = 9019, A00041 = 7384.
- Risk (Go only, `service.go:900-970`): score +2 if 15m delta <= -70% vs own baseline (scaled by window/300), +1 if >= +150%; +1 if <= -70% vs pen median; +1 if tag temp >= pen median +1.5 C; +2 pattern inactive/missing, +1 quiet_watch/spike; +1 sensor abnormal. score 1 low, 2 watch, >=3 high. Pen medians are over the whole filtered cohort. No SQL equivalent; approximate with the pieces above.

## Gateways tab (`herd-signals-gateways.tsx`) - GET /herd-signals/gateways (`service.go:444`)
| Field | CEO asks | Formula | stg |
|---|---|---|---|
| Status online/offline | "gateway online hai?" | Go `service.go:1068`: offline if last_seen_at NULL or > 30 min old (ignores stored status text) | f130d402dcb4 online |
| Tags seen recently | "gateway kitne tag pakad raha" | `repo.go:1398`: tag_latest by current gateway_id, last_seen_at within 30 min | 19 |
| Weak / Unmapped tags | | same grouping, signal_state='weak' / mapping_state='unmapped' | 0 / 0 |
| 15m tags / moving tags / packets | "last 15 min kitne packet aaye" | `repo.go:1430`: activity_windows 300s tier, bucket_start >= now()-15m, by window gateway_id: count(DISTINCT tag_id), count(DISTINCT tag_id where motion_delta>0), sum(packet_count) | 19 / 10 / 1275 |
```sql
SELECT gateway_id, CASE WHEN last_seen_at IS NULL OR now()-last_seen_at>interval '30 minutes' THEN 'offline' ELSE 'online' END FROM public.herd_signal_gateways;
SELECT gateway_id, count(DISTINCT tag_id), count(DISTINCT tag_id) FILTER (WHERE motion_delta>0), sum(packet_count)
FROM public.herd_signal_activity_windows WHERE bucket_seconds=300 AND bucket_start>=now()-interval '15 minutes' AND gateway_id IS NOT NULL GROUP BY 1;
```
Traps: "recent" counts use the tag's CURRENT gateway; the 15m stats use the gateway that actually heard each window (they can differ when tags roam). 300s buckets touching the 15m edge mean it's really 15-20 min. stg gateway has NO shed_id/label -> location blank.

## Insights tab (`herd-signals-insights.tsx`) - GET /herd-signals/insights (`service.go:549`, SQL `repo.go:1480-1628`)
| Card | CEO asks | Formula | stg |
|---|---|---|---|
| Tags Live Now | "abhi kitne tag zinda" | effective movement_state <> 'stale' (i.e. seen <=30 min; card copy says "5 min" - wrong, SQL is 30) | 19 |
| Missing Signal | "kaunse tag band" | effective pattern='missing' (all tags, not only mapped despite copy) | 0 |
| Low Movement Watch | "kaun kam hil raha" | effective pattern IN (quiet_watch, inactive) | 0 |
| High Movement Spike | "kaun zyada bhaag raha" | effective pattern='spike' | 0 |
| Pen Signal Coverage `x/y` | "kitne pen covered" | x = distinct goats.shed_id of non-stale mapped tags; y = count(DISTINCT shed_id) FROM herd_signal_gateways | 1/0 (gateway not bound to a shed) |
| Weak Signal Tags | | signal_state='weak' | 0 |
| Battery Attention | | battery_state='low' ONLY (critical excluded - differs from KPI) | 0 |
| Post-Vaccination Movement Watch | "tike ke baad janwar sust?" | distinct goats with accepted vaccination_completions in last 24h, after monitoring_since, tag pattern IN (quiet_watch,inactive,missing) | 0 |
| Health Case Activity Trend | | distinct goats with health_cases.status='active', monitored tag pattern IN (quiet_watch,inactive) | 0 |
| Feed x Activity | | distinct feed_direction_completions.shed_id, status recorded/accepted, fed_at last 4h, shed has a monitored mapped tag | 0 |
| Weight x Activity | | distinct weighing_observations.scanned_identifier (raw, no goat resolve) matching tag_id/mac, accepted last 24h, after monitoring_since | 0 |
| Unmapped Smart Tags | "kaunsa tag kisi janwar pe nahi" | mapping_state='unmapped' | 0 |
SQL sketch: reuse the KPI query with `CASE ... THEN 'missing' ELSE pattern_state END` for pattern cards; correlated cards copy `repo.go:1525-1624` verbatim (join tag via `UPPER(BTRIM(tag_id|tag_mac)) = gi.normalized_value`). All returned 0 on stg; x/y coverage returned 1 and 0.

## Tag history chart - GET /herd-signals/tags/{tag_id}/timeline?from&to&bucket_seconds (`service.go:352`, `repo.go:917`)
- Rows from herd_signal_activity_windows for one tag, tier auto: span <=1h -> 60s, <=24h -> 300s, else 3600s (`motion.go:326`). Missing buckets are zero-filled in Go (packet_count 0 = no data, not "no movement"). Bucket value = motion_delta; gap_delta buckets are flagged.
- Trap: buckets are UTC-aligned; 3600s buckets start at :30 IST.
```sql
SELECT sum(motion_delta), sum(packet_count), count(*) FROM public.herd_signal_activity_windows
WHERE tag_id='A00031' AND bucket_seconds=3600
  AND bucket_start >= (date_trunc('day', now() at time zone 'Asia/Kolkata') at time zone 'Asia/Kolkata');  -- full IST day 24/09/2026: 9610 | 7378 | 24 (today's hourly buckets are empty just after midnight)
```

## Drawer activity feed - GET /herd-signals/tags/{tag_id}/activity?from&to (`app/activity.go:41`, `adapters/postgres/activity.go:74`)
- Needs tag mapped to a goat and monitoring_since; else returns a reason (tag_not_mapped / boundary unknown / window before monitoring). from is clamped to monitoring_since; max range limited, events capped (truncated flag).
- Events: vaccinations (vaccination_completions status='accepted', administered_at), treatments (health_treatment_sessions.completed_at), shed moves (goat location history), plus pen-grain feed events; each gets a before/after motion correlation in Go from activity_windows. Correlation only, never a diagnosis.
- SQL approx: `SELECT administered_at FROM vaccination_completions WHERE goat_id=<goat> AND status='accepted' AND administered_at BETWEEN greatest(<from>, monitoring_since) AND <to>` (repeat per kind).

## Mapping tab (`herd-signals-mapping-table.tsx`) - GET /herd-signals/live?mapping_state=... ; writes POST /herd-signals/tag-mappings(/replace|/unmap)
- Counts = summary.tags_seen for the mapping_state filter: mapped 19, unmapped 0, conflict 0 on stg (19 distinct goats).
- Traps: a goat can carry >1 active smart_tag_capable identifier (replace flow blocks it in UI, not in DB); replaced/unmapped identifiers go non-'active' and vanish from the join; tag_latest.mapping_state only refreshes on next ingest; unmapped tags have no pen, no baseline, no activity feed.

## Not reproducible purely in SQL
Risk state/score and pen-group % (Go, cohort medians), composed battery trend state, zero-filled timeline buckets, activity-event correlations, gateway online/offline (trivial CASE given above). Pattern/movement states are Go-computed at ingest but stored, so SQL reads them directly.
