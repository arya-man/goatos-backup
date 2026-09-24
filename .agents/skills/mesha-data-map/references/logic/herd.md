Index: H1 total records · H2 active (live) · H3 adults / kids · H4 untagged kids · H5 dead / sold / culled · H6 herd list rows (search, filters, weight) · H7 admin goat writes (register, move, exit, stage) · H8 Analytics "Herd composition right now" (live, kids·adults, breed, stage, age, sex, park)

# Counts > Herd register + Analytics composition: logic cards (verified 24/09/2026, goatos-stg)

Screen: admin-web `/counts/herd` (`features/counts/herd-register.tsx`, `herd-filters-modal*.tsx`, `herd-actions.ts`).
Two reads in parallel (`herd-register.tsx:172-173`):
- KPI cards: `GET /herd-register/summary?park_id&breed&sex` (`backend/internal/counts/adapters/http/handler.go:49`) ->
  `GetHerdRegisterSummary` (`backend/internal/counts/adapters/postgres/repository.go:2562`). No lifecycle filter sent.
- Row list: `GET /goats/search?limit&cursor&q&breed&sex&park_id&status=alive` (`backend/internal/identity/adapters/http/handler.go:37`) ->
  `SearchGoats` (`backend/internal/identity/adapters/postgres/repository.go:109`).
Base for both: `public.goats`, `merged_into_goat_id IS NULL`. ceo_ai equivalent: `ceo_ai.animal_current_scope` (1,741 rows, 1,562 alive).

Filters: top-bar park -> `g.park_id = $`; filter modal Sex chip -> `g.sex = $`; Breed chip -> `g.breed = $` (exact text); Search box `q` (list only,
see H6). KPIs honour park/breed/sex but NOT q. No species, stage or pen filter on this screen (use Counts > Breakdown for those).

## H1 Total records ("register mein kitne", "total entries ever")
- `count(*)` of every unmerged goat, ANY lifecycle (alive + sold + dead + inactive + culled). Sub-label "seeded goat rows".
- 24/09/2026: **1,741** (CBE 995, CPT 746). Trap: NOT the headcount; never answer "kitne janwar hai" with this.

## H2 Active ("kitne zinda", "live herd", "total bakre abhi")
- `count(*) FILTER (WHERE lifecycle_status='alive')`. Same number as Breakdown C1 and Analytics H8.
- 24/09/2026: **1,562** (CBE 850, CPT 712; 693 goats, 869 sheep).

## H3 Adults / Kids ("bachche kitne", "adult kitne")
- Alive AND `herd_register_is_kid(age_band, management_stage)` (kid) / `NOT ...` (adult). Function: `lower(age_band)='kid'` OR
  (age_band not kid/adult AND `upper(management_stage) ~ '^K[0-9]'`). No COALESCE here (Breakdown has one); identical today because age_band is never NULL.
- 24/09/2026: adults **801**, kids **761** (CBE 442/408, CPT 359/353).

## H4 Untagged kids ("bina tag ke bachche")
- Alive kid with NO active `goat_identifiers` row of type `animal_identifier_1` (the RFID).
- 24/09/2026: **0**.
- Trap: 329 alive animals carry a placeholder RFID `TEMP-<park>-<shed>-NNN` as their active animal_identifier_1, so they count as tagged.
  "Animals without a real RFID" = `identifier_value ILIKE 'TEMP-%'` (329 alive). See identity.md I3.

## H5 Dead / Sold / Culled ("kitne mare", "kitne bike", "culled")
- `count(*) FILTER (WHERE lifecycle_status IN 'dead'|'sold'|'culled')`, all-time (no date window on this screen).
- 24/09/2026: dead **6** (CBE 5, CPT 1), sold **160** (CBE 140, CPT 20), culled **0**. Not shown as a card: inactive 13 (CPT).
- For a period (this month etc.) use SKILL "Animals sold" / "Mortality rate" (IST `exited_at`), not these totals.

```sql
SELECT p.location_code park, count(*) total, count(*) FILTER (WHERE g.lifecycle_status='alive') active,
  count(*) FILTER (WHERE g.lifecycle_status='alive' AND NOT herd_register_is_kid(g.age_band,g.management_stage)) adults,
  count(*) FILTER (WHERE g.lifecycle_status='alive' AND herd_register_is_kid(g.age_band,g.management_stage)) kids,
  count(*) FILTER (WHERE g.lifecycle_status='alive' AND herd_register_is_kid(g.age_band,g.management_stage) AND NOT EXISTS (
    SELECT 1 FROM goat_identifiers gi WHERE gi.goat_id=g.goat_id AND gi.identifier_type='animal_identifier_1' AND gi.status='active')) untagged_kids,
  count(*) FILTER (WHERE g.lifecycle_status='dead') dead, count(*) FILTER (WHERE g.lifecycle_status='sold') sold,
  count(*) FILTER (WHERE g.lifecycle_status='culled') culled
FROM goats g LEFT JOIN locations p ON p.location_id=g.park_id WHERE g.merged_into_goat_id IS NULL GROUP BY ROLLUP(1);
```

## H6 Herd list rows ("show me the goats in CBE", "G-000100 kaunsa hai", "find tag X")
- Always `status=alive` (`herd-register.tsx:158`), keyset paged by `display_id` ascending (cursor = last display_id), limit from page-size options.
- Columns: display_id, Tag 1 = active `animal_identifier_1` (RFID), Tag 2 = active `animal_identifier_2` (ear tag), park, shed (+partition),
  breed, sex, weight, lifecycle, health, reproductive.
- `q` matches `g.display_id = q` exactly OR an active identifier with `normalized_value = q` (trimmed, NOT case-folded in this path).
- Weight column = latest `goat_identity_events.payload->>'weight_kg'` (identity events, not weighing): stale; for weights use weighing
  (SKILL `herd-avg-weight.sql`), not this column.
- Traps: `normalized_value` is lower-case for alphanumeric tags (e.g. ear tag `SA2328392` is stored `sa2328392`), so typing an upper-case ear
  tag in the search box can miss; for Ask Mesha always match `lower(btrim(identifier_value))` (see identity.md I2). 173 goats (143 alive,
  30 sold) have TWO active animal_identifier_2 rows (e.g. a 15-digit second RFID + `CBE-1797` ear tag, from double-tagging-csv): the list's
  LEFT JOIN on animal_identifier_2 then repeats that goat, one row per Tag 2.
- Location filter `location_id` uses vestigial `current_location_id`; ignore it, use shed_id + partition (`references/pens.sql`).

## H7 Admin writes behind the screen actions (for "who registered/moved/exited")
- `POST /admin/goats` (register), `/bulk-preview|bulk-commit` (CSV import), `/{id}/move`, `/{id}/exit`, `/{id}/critical-death-exit`,
  `/{id}/stage`, `/{id}/health`, `/{id}/reproductive`, `/{id}/identity` (`identity/adapters/http/handler.go:42-57`).
- Each approved write = one `identity_decisions` row (decision_type create_goat / move_goat / exit_goat / stage_goat / health_goat, state approved)
  plus one `goat_identity_events` row per goat. 24/09/2026 decisions: exit_goat 149, create_goat 61, health_goat 62, move_goat 9, stage_goat 9.
- Trap: create_goat 61 ≠ herd size; most animals came from imports (`goat.created` events 1,741).

## H8 Counts > Analytics "Herd composition right now" (`/counts/analytics`, `features/counts/herd-analytics.tsx`)
- `GET /counts/herd-analytics?park_id&from&to` (`counts/adapters/http/handler.go:51`) -> `herdAnalyticsCompositionSQL`
  (`backend/internal/counts/adapters/postgres/herd_analytics.go:34`). Composition ignores from/to (always NOW); only park filters it.
- Population: alive, unmerged, optional park. KPIs: **Live animals** = count; **Kids · Adults** = `coalesce(herd_register_is_kid(...), false)` split.
  Charts: breed (`btrim(breed)`), stage (raw `btrim(management_stage)`, no fattening fold), age band (kid/adult), sex, park (`location_code`).
- 24/09/2026: live **1,562**; kids **761** · adults **801**; park CBE 850 / CPT 712; sex female 984 / male 578;
  stage Non-Pregnant 758, F2-Male 452, F2-Female 192, Warmup 58, Buck 38, K3 32, ICU-Kid 17, K2 10, Mother 5; breed as Breakdown C5
  (Anantapur Sheep 869, Beetal 399, Sojat 189, Sirohi 62, ...).
```sql
SELECT 'stage' dim, coalesce(btrim(management_stage),'') k, count(*) FROM goats WHERE merged_into_goat_id IS NULL AND lifecycle_status='alive' GROUP BY 2
UNION ALL SELECT 'sex', coalesce(sex,''), count(*) FROM goats WHERE merged_into_goat_id IS NULL AND lifecycle_status='alive' GROUP BY 2
UNION ALL SELECT 'age', CASE WHEN coalesce(herd_register_is_kid(age_band,management_stage),false) THEN 'kid' ELSE 'adult' END, count(*)
  FROM goats WHERE merged_into_goat_id IS NULL AND lifecycle_status='alive' GROUP BY 2 ORDER BY 1,3 DESC;
```
- Trap: Analytics, Breakdown and Herd register "Active" are the same population by design; if they disagree, it's a filter (park/breed/sex) difference.
  Births/deaths/movements on the same page are a different card set (monthly flow), not covered here.

## H9 Counts > Analytics "Herd movement" KPIs + monthly flow chart (births / deaths / sold / other exits / net / movements)
- Screen: `features/counts/herd-analytics.tsx:263-313` (KPIs Births, Deaths, Sold, Net change) and `:315-328` (flow line chart, series births/deaths/sold/other_exits over `months[].label`).
  Date picker `herd-analytics-date-filter.tsx` -> `from`/`to` (IST dates). Park top bar -> `park_id`.
- Endpoint: `GET /counts/herd-analytics?park_id&from&to` (`counts/adapters/http/handler.go:51`) -> `GetHerdAnalytics`
  (`backend/internal/counts/adapters/postgres/herd_analytics.go:167`), flow SQL `herdAnalyticsFlowSQL` (`:90-163`). Response `totals.{births,deaths,sold,other_exits,net_change,movements,animals_moved}` and `months[]`.
- Window: default = 1st of month 11 months back .. today IST, FLOORED at **2026-08-01** (`domain/herd_analytics.go:132,144,167`); max 36 months; a month spine makes quiet months 0.
- Formulas (all unmerged goats, optional `park_id`):
  - Births = `origin_type='birth'` with `COALESCE(dob, entry_date, created_at IST date)` in window, bucketed by that month.
  - Exits = `lifecycle_status IN (dead,sold,culled,transferred,lost)` with `COALESCE(exited_at IST, updated_at IST)` date in window. Deaths = `exit_reason='died'` (or NULL reason and status dead); Sold = `exit_reason='sold'` (or status sold); Other = culled/transferred/lost. Buckets are disjoint.
  - Net change (Go `:258,271`) = births - (deaths + sold + other_exits). Movements (not a KPI tile, in payload) = count of applied `shifting_events` by IST `completed_at` month, park matched on source OR destination; animals_moved = sum `shifting_event_impacts.head_count`.
```sql
WITH b AS (SELECT date '2026-08-01' f,(now() AT TIME ZONE 'Asia/Kolkata')::date t)
SELECT 'births' k, count(*) FROM goats g,b WHERE merged_into_goat_id IS NULL AND origin_type='birth'
  AND COALESCE(dob,entry_date,(created_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN b.f AND b.t
UNION ALL SELECT coalesce(exit_reason,lifecycle_status), count(*) FROM goats g,b WHERE merged_into_goat_id IS NULL
  AND lifecycle_status IN ('dead','sold','culled','transferred','lost')
  AND COALESCE((exited_at AT TIME ZONE 'Asia/Kolkata')::date,(updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN b.f AND b.t GROUP BY 1;
```
- Verified 24/09/2026 (default window 01/08-24/09): births **1** (CPT, Sep), deaths **6** (CBE 5, CPT 1; Aug 3 / Sep 3), sold **160** (CBE 140, CPT 20; Aug 31 / Sep 129), other exits 0,
  net change **-165**; movements 25 events (Aug 7 / Sep 18), animals_moved 25.
- Traps: floor date hides the April-May 2026 import-era births (404 in May) - "births this year" here is 1, not 468. Sold here is by exit date on `goats`, not sales invoices (see sales.md).
  Deaths match mortality.md / health.md (6). Exit with NULL `exited_at` falls back to `updated_at` (any later edit moves its month).
- CEO questions: "How many were born / died / sold since August?" - "August se kitne paida hue, kitne mare, kitne bike?"; "Is the herd growing or shrinking?" - "Herd badh raha hai ya ghat raha hai?";
  "How many pen movements this month?" - "Is mahine kitni shifting hui?"
