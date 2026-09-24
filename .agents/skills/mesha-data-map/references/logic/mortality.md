# Logic card: Counts > Mortality (GET /counts/mortality)
Index: deaths tile · mortality rate · kid deaths+rate · adult deaths+rate · died within 7 days · cause established · deaths by month (kids/adults) · rate tables (stage, kid/adult, breed, species, sex, farm, pen, load, vendor) · count series (age at death, cause, season, days since arrival, days since vaccination) · cross tabs (season×stage, load×cause, vendor×cause, breed×cause) · deaths list

Common to every entry
- Screen: `apps/admin-web/app/(admin)/counts/mortality` -> `features/counts/mortality.tsx` (+ `mortality-tables.tsx`); client `lib/api/server.ts:1076`.
- Route: `backend/internal/counts/adapters/http/handler.go:52` GetMortality -> `adapters/postgres/mortality.go:426` GetMortality (3 SQLs in one batch: population :43, deaths :198, recent list :363).
- DEATH predicate (identical to Herd Analytics deaths): `goats.merged_into_goat_id IS NULL AND (exit_reason='died' OR (exit_reason IS NULL AND lifecycle_status='dead'))`, death day = `COALESCE(exited_at, updated_at) AT TIME ZONE 'Asia/Kolkata'`::date, window inclusive.
- Only APPROVED deaths exist here: a death report (`POST /app/counts/death-events`) is a pending `counts_approval_requests` row; `goats` flips to dead/died only when approved (`approval_repository.go:986` ExitGoatInTx). Pending/rejected reports are never counted.
- Window filter (`from`,`to`, IST days): default = first of month 11 back, floored at 2026-08-01 -> today (`domain/herd_analytics.go:167`), so today = 2026-08-01..2026-09-24. Max 1150 days; only one of the pair = 400.
- Park filter: top-bar scope -> `park_id` -> `g.park_id = :park` on both deaths and live animals. No species/breed filter on this screen (they are series).
- Rate = `deaths / LIVE animals in that section TODAY * 100`, 1 decimal, half-up (`domain/mortality.go:382`); nil (shows "No live animals in this section") when live = 0. Denominator is NOT an at-risk/average population (decision 2026-09-18). Live = `lifecycle_status='alive'`, not merged.
- ceo_ai: `ceo_ai.mortality_base` (per park per day: deaths, kid_deaths, adult_deaths, first_week_deaths, cause_established, active_population) AGREES on STG for all tiles (6/5/1/0/2). Differences: its kid rule is `stage ILIKE 'k%' OR age<365d` (screen: `herd_register_is_kid(age_band, stage)`), its population is "not in dead/sold/culled/.../inactive" (not strictly alive). `ceo_ai.animals_base exit_reason='died'` = 6 too.
- Traps: death date is the APPROVAL day (exited_at is stamped at approval), not the day it was reported (G-003210 reported 17/08, counted 19/08). 2 of 6 deaths (G-003420, G-003284) have no death approval at all (health/legacy path). If exited_at is NULL the screen falls back to updated_at (SKILL.md's sketch uses exited_at only: same result today, 0 NULLs). 0 merged goats today.

## Deaths (tile "Deaths")
- Q: how many died / kitne mare / maut kitni hui / deaths this month / mortality count Coimbatore.
- Formula: count of death-predicate goats in window (`mortality.go:98` total branch). Value 24/09/2026 default window: **6** (CBE 5, CPT 1). Sep 2026 only: 3 (all CBE). Aug: 3.
```sql
SELECT count(*) FROM goats g WHERE g.merged_into_goat_id IS NULL
 AND (g.exit_reason='died' OR (g.exit_reason IS NULL AND g.lifecycle_status='dead'))
 AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,(g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN '2026-08-01' AND '2026-09-24';  -- 6
```

## Mortality rate (tile) + every RATE table
- Q: mortality rate / maut ka percentage / death rate by breed, pen, load, vendor, farm, sex, species, stage.
- Formula: `mortality.go:57-179` pop CTE = live-today OR died-in-window rows, each flagged; per bucket `deaths=count(died)`, `animals=count(live)`; Go rounds (`mortality.go` ~:497). Buckets: stage = btrim(management_stage) ('' -> "No tag"); breed = btrim(breed); sex; species = lower(species); park = locations.location_code; pen = shed_id + normalized partition_label (only pens WITH a death; label = shed name + partition, e.g. "Godel 1 Part 7"; animals = live in that pen today via goat_shed_partitions); load = latest `procurement_load_goats` row with current_state='accepted_herd_intake' (buckets `farm_born` if origin_type='birth', `no_load`; loads shown only with a death); vendor = that load's `source_party_id` -> parties.display_name (EVERY vendor shown, even 0%).
- Values 24/09 (default window): total **6/1562 = 0.4%**; park CBE 5/850=0.6, CPT 1/712=0.1; species goat 3/693=0.4, sheep 3/869=0.3; sex male 5/578=0.9, female 1/984=0.1; stage F2-Male 4/452=0.9, ICU-Kid 1/17=5.9, ICU 1/0=no rate; breed Anantapur Sheep 3/869=0.3, Beetal 2/399=0.5, Malai 1/5=20.0; load: farm-born 2, no_load 1 (G-002472 procured, no load row), load 128 (Krishnamorrthy) 2, load 126 (Ramesh Reddy) 1.
- Trap: a dead animal is NOT in the denominator (it isn't live) and the denominator is today's herd, so a small-section rate (Malai 1/5=20%) or a pen that has since emptied (ICU 1/0) looks extreme; read deaths beside animals.
```sql
WITH pop AS (SELECT g.*, (g.exit_reason='died' OR (g.exit_reason IS NULL AND g.lifecycle_status='dead'))
   AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,(g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN :from AND :to died,
   g.lifecycle_status='alive' live FROM goats g WHERE g.merged_into_goat_id IS NULL)
SELECT lower(species) k, count(*) FILTER (WHERE died) deaths, count(*) FILTER (WHERE live) animals,
  round(count(*) FILTER (WHERE died)*100.0/nullif(count(*) FILTER (WHERE live),0),1) rate_pct
FROM pop GROUP BY ROLLUP(1);   -- swap species for breed / sex / management_stage / park_id
```

## Kid deaths / Adult deaths (tiles, with rate) + "Kids and adults" table
- Q: kitne bachhe mare / kid mortality / adult deaths.
- Formula: kid = `COALESCE(herd_register_is_kid(age_band, management_stage), false)` (unknown band -> adult). Rate = kid deaths / live kids today.
- Values: kids **5 / 761 = 0.7%**, adults **1 / 801 = 0.1%**. (`mortality_base` kid_deaths 5, adult 1: agrees.)

## Died within 7 days of birth (tile)
- Q: new-born deaths / paida hote hi mare / neonatal mortality.
- Formula: deaths with `died_on - dob BETWEEN 0 AND 7` (`mortality.go:355`, age band d0_7). NULL dob or dob after death -> 'unknown', not counted.
- Value: **0**. Trap: 403 goats carry placeholder dob 09/05/2026 and 77 carry 21/07/2024, so age-based buckets for imported animals are approximate.

## Cause established (tile "x / deaths")
- Q: cause of death known? / kis wajah se mare / kitno ka reason pata hai.
- Formula: recorded (row in `health_death_causes`) + inferred (no recorded row, but a `health_cases` row with status IN ('closed_dead','held_death_review') open on the death day: start_date <= died_on AND (closed_at NULL OR closed_at IST >= died_on)). Frontend sums `cause_recorded + cause_inferred` (`mortality.tsx:456`).
- Value: **2 / 6** (both recorded: G-003284 fever, G-002472 ACIDOSIS; 0 inferred). `mortality_base.cause_established` = 2 (agrees).

## By cause (count series) + cross tabs load×cause, vendor×cause, breed×cause
- Q: deaths by disease / bimari se kitne mare / which vendor's animals die of what.
- Formula: column key = cause_key (recorded wins) else `inferred:<disease names joined ' · '>` else '' ("No cause recorded"); basis chip recorded/inferred/none (`mortality.go:268-275`). Cross tabs group the same death rows by (load_key|vendor_key|breed, cause).
- Values: fever 1 (recorded), ACIDOSIS 1 (recorded), no cause 4. Trap: cause_key casing is raw ('fever' vs 'ACIDOSIS'); death `health_cases` are nearly empty, so for most deaths say "cause not recorded", never guess.

## Deaths by month (chart, kids vs adults)
- Formula: group by `to_char(died_on,'YYYY-MM')`, deaths + kids; months with no death filled 0 in Go. Partial first/last months cover only in-window days.
- Values: Aug 2026 3 (3 kids), Sep 2026 3 (2 kids, 1 adult).

## Age at death (count series)
- Formula: `died_on - dob` bands d0_7, d8_30, d31_90, d91_180, d181_365, over_1y, unknown (`mortality.go:255`).
- Values: d31_90 1, d91_180 3, d181_365 1, over_1y 1.

## By season (count series) + Season × stage
- Formula: month of died_on: Mar-May summer, Jun-Sep monsoon, Oct-Nov post_monsoon, Dec-Feb winter.
- Values: monsoon 6 (F2-Male 4, ICU-Kid 1, ICU 1).

## Days on the farm before death (count series)
- Formula: procured only (origin_type='birth' -> unknown): `died_on - COALESCE(plg.arrived_at IST, procurement_loads.arrived_on, goats.entry_date)`; bands d0_7, d8_30, d31_90, over_90, unknown.
- Values: d31_90 1, over_90 2, unknown 3 (2 farm-born + G-002472 no load and no entry_date). Trap: origin_type is NULL for 405 goats (e.g. the 3 dead sheep) -> treated as purchased.

## Days since last vaccination (count series)
- Formula: `max(vaccination_completions.administered_at)` with status IN ('recorded','accepted') and <= death instant; `died_on - that IST day`; unknown = never vaccinated on record.
- Values: d8_30 2, d31_90 1, unknown 3.

## Deaths in this window (paged list)
- Formula: same death rows, ordered died_on DESC, goat_id; `recent_limit`/`recent_offset` page only the list (bad values -> first page), tiles don't change. Columns: tag, died on, breed, sex, stage, age days, park, pen (shed + partition), load ref, cause (+basis).
- Values: G-002472 23/09, G-003284 17/09, G-003664 05/09, G-003210 19/08, G-003420 16/08, G-002749 12/08.
