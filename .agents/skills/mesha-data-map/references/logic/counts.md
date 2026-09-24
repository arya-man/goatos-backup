Index: C1 total live animals · C2 kids/adults · C3 stage summary cards (Fattening, Bucks, Breeding, ICU, K0-K4) · C4 per-pen census table · C5 breed/stage/sex/pen charts · C6 purchased loads (purchased / on farm / male / female) · C7 census corrections + stage reclassifications · C8 pen reconciliation (wrong-pen cards) · C9 milk preparation (kids, litres, citric acid, verification)

# Counts > Breakdown + Milk preparation: logic cards (verified 24/09/2026, goatos-stg)

Screen: admin-web `/counts/breakdown` (`features/counts/counts-breakdown.tsx`, `counts-summary-cards.ts`, `counts-breakdown-loads.tsx`).
Endpoint: `GET /counts/breakdown?group_by=pen` (`backend/internal/counts/adapters/http/handler.go:50`) -> `GetCountsBreakdown`
(`backend/internal/counts/adapters/postgres/repository.go:3360`). A single batch of 4 SQLs: page (pens `:2800`), charts `:2876`,
facets `:2980`, loads `:3197`. All four share the census CTE `countsBreakdownGroupedCTE` (`:2669`).

Population used everywhere below (unless stated): `public.goats g` with `merged_into_goat_id IS NULL AND lifecycle_status='alive'`.
The frontend never sends `lifecycle_status`, so the repo default is `alive` (`repository.go:3383`). No species column in the screen: sheep sit
inside the same counts (breed `Anantapur Sheep` = sheep). ceo_ai equivalent: `ceo_ai.animal_current_scope WHERE lifecycle_status='alive'`
(same 1,562; the view does not drop merged goats, 0 merged today).

Screen filters (URL -> API -> SQL, OR within a filter, AND across filters):
- Park (top bar) or `bd_farm` -> `park_id` -> `g.park_id = ANY(...)` (the "Farm" column is really `goats.park_id`; `goats.farm_id` is empty).
- `bd_shed` (repeatable, `shed_id` or `shed_id|partition`) -> `pen=<shed>#<part>` -> `g.shed_id = ...` AND normalized partition key
  (`partitionKeyExpr`, `:2667`: `regexp_replace(lower(btrim(coalesce(gsp.partition_label,'whole'))),'^part[[:space:]]+','')`, so 'Part 3' = '3';
  a whole-shed entry takes every pen of that shed).
- `bd_stage` (repeatable) -> `management_stage` -> `coalesce(g.management_stage,'') = ANY(...)`; the single "Fattening" option expands to
  F2 / F2-Male / F2-Female (herdstage.ExpandFilter).
- `bd_breed` (repeatable) -> `coalesce(g.breed,'') = ANY(...)`. `bd_sex` -> `g.sex = ...`.

## C1 Total live animals ("kitne janwar hai", "total bakre", "herd size", "total animals")
- Card "Total animals" + KPI "Matching" = `total_count` = sum of `animal_count` over the census CTE (window SUM, not the page).
- Sex line under it = sum of the stage_sex chart (female / male / other).
- SQL: `SELECT count(*), count(*) FILTER (WHERE sex='female') f, count(*) FILTER (WHERE sex='male') m FROM goats WHERE merged_into_goat_id IS NULL AND lifecycle_status='alive';`
- 24/09/2026: **1,562** (984 female, 578 male, 0 other). CBE 850, CPT 712. Species: CBE 511 goats + 339 sheep, CPT 182 goats + 530 sheep
  (693 goats, 869 sheep). Answer "goats" as all animals with the species split (SKILL rule).
- Trap: Herd Register's "Total records" (1,741) includes sold/dead/inactive: not the headcount.

## C2 Kids vs adults ("kitne bachche", "kids kitne", "adult kitne")
- KPI "Kids · Adults" + % of total. Kid = `coalesce(herd_register_is_kid(age_band, management_stage), false)`; adult = the rest, so they always sum to C1.
  `herd_register_is_kid`: `lower(age_band)='kid'` OR (age_band not kid/adult AND `upper(management_stage) ~ '^K[0-9]'`).
- 24/09/2026: **761 kids, 801 adults** (49% / 51%). By park: CBE 408/442, CPT 353/359.
- Trap: "kid" is age_band, NOT stage. Only 59 animals carry a K-stage (K2 10, K3 32, ICU-Kid 17); 747 kids are age_band 'kid' in F2/Non-Pregnant etc.
  Say "kids by age band"; "K-stage kids" is a different, much smaller number.

## C3 Stage summary cards ("fattening mein kitne", "bucks kitne", "breeding stock", "ICU mein kitne", "K1/K2/K3 kitne")
- Built in the browser from charts.stage_sex (raw `management_stage` x sex) by `counts-summary-cards.ts:45` alias lists
  (stage compared after lowercasing and dropping non-alphanumerics):
  Fattening = F2, F2-Male, F2-Female, Fattening, Fattening male/female · Bucks = Buck, Bucks ·
  Breeding = Mother(s), Milking, M0, **Warmup**, Pregnant, Non-Pregnant, Breeding female, Breeding stock · ICU = any stage containing "icu" ·
  K0..K4 = K0..K4. Anything else is in no card (cards need not sum to the total).
- SQL:
```sql
SELECT CASE WHEN management_stage ILIKE '%icu%' THEN 'ICU'
  WHEN management_stage IN ('F2','F2-Male','F2-Female','Fattening','Fattening male','Fattening female') THEN 'Fattening'
  WHEN management_stage IN ('Buck','Bucks') THEN 'Bucks'
  WHEN management_stage IN ('Mother','Mothers','Milking','M0','Warmup','Pregnant','Non-Pregnant','Non Pregnant','Breeding female','Breeding stock') THEN 'Breeding'
  WHEN upper(replace(management_stage,' ','')) ~ '^K[0-4]$' THEN upper(replace(management_stage,' ','')) ELSE 'unbucketed' END card,
  count(*), count(*) FILTER (WHERE sex='female') f, count(*) FILTER (WHERE sex='male') m
FROM goats WHERE merged_into_goat_id IS NULL AND lifecycle_status='alive' GROUP BY 1 ORDER BY 1;
```
- 24/09/2026: Fattening **644** (192 F, 452 M) · Bucks **38** (M) · Breeding **821** (763 F, 58 M) · ICU **17** (9 F, 8 M) · K0 0 · K1 0 · K2 **10** · K3 **32** · K4 0. Sum 1,562.
- Traps: Breeding includes 58 **Warmup males** (young bucks being warmed up), so "breeding females" = 763, not 821. Raw stages seen today:
  Buck, F2-Female, F2-Male, ICU-Kid, K2, K3, Mother, Non-Pregnant, Warmup. Stage is raw text (no CHECK); near-duplicates would show separately.

## C4 Per-pen census table ("Godel 1 Part 3 mein kitne", "which pen has most", "pen-wise count")
- `countsBreakdownPensSQL` (`:2800`): census CTE grouped by (park_id, shed_id, partition_key) with animal/kid/adult counts and nested
  stage x breed x sex rows; ordered by animals desc; paged 10/page (totals are whole-result). `total_rows` = number of pens with live animals.
- Pen = `goats.shed_id` + `goat_shed_partitions.partition_label` (1:0..1 per goat; no row = 'whole'). Labels: park = `location_code` (CBE/CPT),
  shed = `locations.name`.
- 24/09/2026: **100 pens** hold live animals (CBE 59, CPT 41).
- For Ask Mesha, answer pen questions with `references/pens.sql` (same goats.shed_id + partition, gives pen codes like G1P3); never use
  `goats.current_location_id` (stale for 18+, NULL for most). Pen names repeat across parks: always name the park.

## C5 Charts (breed / stage / stage x sex / sex / per pen)
- `countsBreakdownChartsSQL` (`:2876`), same CTE, whole filtered set, every pen (safety cap 500). Stage chart uses raw stage + `animal_stage_lookup.name` label.
- Breed 24/09/2026: Anantapur Sheep 869, Beetal 399, Sojat 189, Sirohi 62, Beetal x Sojat 14, Osmanabadi 7, Malai x Sojat 5, Beetal x Malai 5,
  Malai 5, Boer x Sojat 2, Boer 2, Boer x Beetal 1, Boer x Malai 1, Boer x Sirohi 1.
  `SELECT breed, count(*) FROM goats WHERE merged_into_goat_id IS NULL AND lifecycle_status='alive' GROUP BY 1 ORDER BY 2 DESC;`
- Filter facets (dropdown counts) ignore the current selection of their own dimension; the lifecycle facet counts every status
  (alive 1,562 · sold 160 · inactive 13 · dead 6).

## C6 Purchased loads section ("load 136 mein kitne bache hai", "load se kitne abhi farm pe")
- `countsBreakdownLoadsSQL` (`:3197`). Member = `procurement_load_goats` with `current_state='accepted_herd_intake'`, DISTINCT ON goat_id
  (latest `intake_accepted_at`, then `created_at`, `load_goat_id`) so an animal belongs to one load.
- **Purchased** = `procurement_loads.expected_count` when > 0, else tracked members + `procurement_load_prior_outcomes.animal_count`
  (same rule as Sales > Purchase and Born). Not filtered by the page filters.
- **On farm / male / female** = members that pass the page filters (default alive). Newest 100 loads by `purchase_date`.
- SQL:
```sql
WITH member AS (SELECT DISTINCT ON (goat_id) goat_id, load_id FROM procurement_load_goats WHERE current_state='accepted_herd_intake'
   ORDER BY goat_id, intake_accepted_at DESC NULLS LAST, created_at DESC, load_goat_id DESC),
 tracked AS (SELECT m.load_id, count(*) tracked FROM member m JOIN goats g ON g.goat_id=m.goat_id GROUP BY 1),
 prior AS (SELECT load_id, sum(animal_count) prior FROM procurement_load_prior_outcomes GROUP BY 1),
 live AS (SELECT m.load_id, count(*) on_farm, count(*) FILTER (WHERE g.sex='male') male, count(*) FILTER (WHERE g.sex='female') female
   FROM member m JOIN goats g ON g.goat_id=m.goat_id WHERE g.merged_into_goat_id IS NULL AND g.lifecycle_status='alive' GROUP BY 1)
SELECT pl.context->>'load_ref' load, pl.purchase_date,
  CASE WHEN pl.expected_count>0 THEN pl.expected_count ELSE coalesce(t.tracked,0)+coalesce(p.prior,0) END purchased,
  coalesce(l.on_farm,0) on_farm, l.male, l.female
FROM procurement_loads pl LEFT JOIN tracked t USING (load_id) LEFT JOIN prior p USING (load_id) LEFT JOIN live l USING (load_id)
WHERE t.load_id IS NOT NULL OR p.load_id IS NOT NULL ORDER BY pl.purchase_date DESC NULLS LAST, pl.created_at DESC LIMIT 100;
```
- 24/09/2026 (purchased / on farm, all male): 136 (16/09/2026) 58/58 · 131 63/63 · 130 77/76 · 129 78/77 · 128 70/63 · 126 67/50 ·
  113 100/0 · 100 76/1 · 101 70/3.
- Traps: 113/100/101 are pre-GoatOS loads (prior outcomes); "on farm" is live-only under the current filters; the pen bracket drops animals with no shed.

## C7 Census corrections / stage reclassification ("who changed the stage", "census correction kab hua")
- Writes, not reads: `POST /admin/goats/census-slice/preview|commit` (edits breed/sex/stage of one census row) and
  `POST /admin/goats/shed-stage/preview|commit` (retag a pen's stage). Census scope SQL at `census_correction.go:32`
  (`backend/internal/identity/adapters/postgres/census_correction.go:32`): same shed + normalized partition + stage + breed + sex, alive, not exited.
- History = `audit_log.action` `'counts_census_slice_correction'` (`census_correction.go:335`) / `'counts_shed_reclassification'`;
  resource_type 'shed', actor_id -> workforce_members.user_id. Per-goat trail: `goat_identity_events` event_type `goat.stage_changed`.
- `SELECT action, (created_at AT TIME ZONE 'Asia/Kolkata')::date d, count(*) FROM audit_log WHERE action IN ('counts_census_slice_correction','counts_shed_reclassification') GROUP BY 1,2 ORDER BY 2 DESC;`
- 24/09/2026: 0 census-slice corrections ever; 25 shed reclassifications (latest 23/09/2026; 6 on 18/09).

## C8 Pen reconciliation ("galat pen mein kitne mile", "wrong pen goats")
- Not on the Breakdown screen: app queue `GET /app/counts/pen-reconciliation/cards` (`counts/adapters/http/pen_reconciliation_handler.go:58`).
  Card raised when a weighing scan resolves to a live animal whose registered pen differs from the weighed bucket pen
  (`counts/adapters/postgres/pen_reconciliation.go:39`); one non-completed card per goat. Summary `:206`: open / pending_verification / rework / completed.
- `SELECT p.location_code park, c.status, count(*) FROM pen_reconciliation_cards c LEFT JOIN locations p ON p.location_id=c.park_id GROUP BY 1,2;`
- 24/09/2026: CBE open 3, pending_verification 22; CPT open 49. Total 74, none completed.
- Trap: a card is "found in wrong pen during weighing", not a confirmed move; the register pen stays until someone shifts the animal.

## C9 Milk preparation (`/counts/milk-preparation`: "aaj kitna doodh banana hai", "milk kids kitne", "citric acid kitna")
- `GET /counts/milk-preparation` (`handler.go:53`) -> `milkPreparationPageSQL` (`counts/adapters/postgres/milk_preparation.go:140`).
  Day = today's IST business day (`:294`). Population: alive, unmerged, effective band (`milkCohortExpr` `:51`) = own stage if K1/K2/K3
  else `goats.milk_cohort` (clinical kids). K3 only inside its 7-day window: `k3_milk_started_on <= day <= k3_milk_started_on+6`
  (`:68`; NULL start = no milk). Park filter only.
- Per head/day: K1 200 ml x 4 = 800 ml, K2 300 x 4 = 1,200 ml, K3 200 x 2 = 400 ml. Citric acid = litres x 5.5 g, 1 decimal.
  KPIs: pens (sheds), kids (head_count), milk litres, citric acid g; verification chips per park from `milk_preparation_completions`
  (`shed_id IS NULL`, `preparation_date = day`, status <> 'retired'; none = not_submitted).
- SQL:
```sql
WITH k AS (SELECT p.location_code park, CASE WHEN g.management_stage IN ('K1','K2','K3') THEN g.management_stage ELSE g.milk_cohort END band
  FROM goats g LEFT JOIN locations p ON p.location_id=g.park_id
  WHERE g.merged_into_goat_id IS NULL AND g.lifecycle_status='alive' AND (g.management_stage IN ('K1','K2','K3') OR g.milk_cohort IS NOT NULL)
   AND ((CASE WHEN g.management_stage IN ('K1','K2','K3') THEN g.management_stage ELSE g.milk_cohort END) <> 'K3'
     OR (g.k3_milk_started_on IS NOT NULL AND (now() AT TIME ZONE 'Asia/Kolkata')::date BETWEEN g.k3_milk_started_on AND g.k3_milk_started_on+6)))
SELECT park, band, count(*) heads, sum(CASE band WHEN 'K1' THEN 800 WHEN 'K2' THEN 1200 WHEN 'K3' THEN 400 END) ml,
  round(sum(CASE band WHEN 'K1' THEN 800 WHEN 'K2' THEN 1200 WHEN 'K3' THEN 400 END)/1000.0*5.5,1) citric_g FROM k GROUP BY ROLLUP(1,2);
```
- 24/09/2026: **15 kids, 14.0 L, 77.0 g citric acid**. CBE 5 K2 (6 L); CPT 5 K2 + 5 K3 (8 L). Verification: 2 parks pending_verification.
- Traps: 22 K3 animals started 19/08 and 5 started 13/09 are out of their window (0 milk); 17 ICU-Kid animals have no milk_cohort, so they get
  no milk here. Kid milk FEEDING (4 sessions/day) is a different table: `milk_feeding_tasks` (feeding_date = preparation date + 1).
