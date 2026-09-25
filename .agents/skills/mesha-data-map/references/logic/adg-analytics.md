# ADG Analytics (admin-web Weighing > ADG Analytics, /weighing/analytics)

Index: 0 page scope · 1 General (KPI strip, gain cards, pens table) · 2 Breed-wise · 3 Birth-wise · 4 Pen-wise (by pen type) · 5 Weight-wise (bands + feed by band) · 6 Time-wise (overall, breed, pen, load grids) · 7 Comparison (loads) · FCR tab -> fcr.md

All values: goatos-stg, verified 24/09/2026, params unless stated: all parks (CBE, CPT, PARIGI), 2026-08-03 .. 2026-09-22 IST, sex=male (page default), origin=all, weighing=all. Every SQL below is SELECT-only and returned exactly the app's own repository output for those params.

## 0. Page scope (applies to every tab)
- UI: apps/admin-web/features/weighing/weights-analytics.tsx. Tab param `tab` = general|breed|birth|shed|weight|time|load|fcr (:124). Reads per tab: :259-360.
- Filters -> API params: Park `park` -> park_id (empty = every park the viewer may monitor, service.go resolveMonitorParkScope); Period `wt_from`/`wt_to` -> from/to (inclusive IST business dates); Weighing `weighing` = all|individual_animal|per_shed_partition -> weighing_category (all -> ''); Sex `sex`: ABSENT = **male** (:195), `sex=all` -> '' (both), female; Origin `origin` farm_born|purchased, absent = both.
- Default window (landing-window.ts:51, landing-window-constants.ts): explicit wt_from/wt_to (future end clamped to today) else from = SOP copy `weights.window.*` (fallback 2026-08-03, floor 2026-07-05) through `latest_weighing_date` from GET /weighing/weighing-dates under the same park/sex/origin/weighing filters (400-day lookback; falls back to today). On stg today that is 2026-08-03 .. 2026-09-22 for males (09-23 only has female weighs).
- Backend window: [from 00:00 IST, to+1 00:00 IST) on accepted_at (app/service.go resolveWeighingWindow ~:2190; growth ~:1877). API default when from/to omitted: 15 days ending today (never hit from this page).
- Shared rules (all ADG figures): rejected scans (`weighing_observations.verification_status='rejected'`) and withdrawn/rejected whole-pen weighs (`weighing_shed_observations.withdrawn_at IS NOT NULL` or rejected) are excluded; PENDING is included; canceled buckets (`weighing_campaign_sheds.status='canceled'`) excluded. There is no separate "rework" filter in these reads: a reworked scan is a normal row unless rejected, and history rows are de-duplicated by latest-per-animal (shed_weights.go ind :197).
- Identity merge: identity_scope.go:146 — a goat with two ACTIVE RFIDs (animal_identifier_1/2) keys under one canonical tag (identifier_1 first). growth/shed reads resolve it over [from-400d, to+1); demographics over [from-90d, to+1).
- Sex scope (sex_scope.go:201): scanned tag counts when the goat behind its newest goat_identifiers row (any status) has that sex (tags weighed in [from-90d, to+1)); a whole-pen bucket counts only when EVERY live resident of the pen (pen, else parent shed + partition, scrubbed "Part N" key) has that sex (sexed_buckets :276). Mixed pens drop out of any sex filter. Origin scope (origin_scope.go): purchased = goat in procurement_load_goats; farm_born = not; pens agree-or-neither (origin_buckets :318).
- Two ADG engines on this page (TRAP — they differ slightly by design):
  - **growth.go** (General gain cards, pens-table gain, Time-wise overall): scanned pairs = CONSECUTIVE weighs per animal key (every weigh, ordered by accepted_at), pair kept if IST-day gap > 0.
  - **weight_demographics.go** (Breed, Birth, Pen-wise, Weight bands, Time-wise breed/pen/load): LAST weigh per animal per IST day, then consecutive days.
  - Both: per animal g = Σ(Δkg×1000)/Σ(days) (not a median of pair rates); whole pen (location+partition) weighed on >=2 IST days: (latest avg − first avg)×1000/days, counted once PER ANIMAL of its latest head count; blend = animal-weighted mean Σ(n·g)/Σn. Pen weighed once in window -> no gain (never 0).

## 1. General tab
Reads: GET /weighing/shed-weights (park_id, sex, origin, weighing_category, from, to, sale_threshold_kg=35, sale_lower_kg=30, include_loads=false, include_dates=false) + GET /weighing/leadership/growth sections=headline,shed_leaderboard,by_park + GET /weighing/weight-demographics sections=composition (breed column only).

### 1a. KPI strip (shed_weights.go:359-470 summary_individual / summary_lump / summary_rollup)
- "Kids weighed" = individual · whole-pen, sub = total. Individual = animal keys with a qualifying pair (prior weigh on an earlier IST day) whose later weigh is IN the window (both weighs in window since 2026-09-09, lookback 0). Whole-pen = Σ latest head count of pens weighed on >=2 IST days in window. SAME population as the gain card.
- Total weight (kg, 0 dp) = Σ latest qualifying scan weight + Σ latest pen `weight_kg` (pen total). Average (1 dp) = total / animals (null -> "No data").
- ">=30 kg" / ">=35 kg" (sale_ready_lower_kg / sale_ready_threshold_kg from growth_assumptions; tolerance 0): scanned by own latest weight; a pen counts WHOLE if its latest average >= line, else none. Basis = same animals.
- Value (verified): **185 · 335 / 520 kids, 16,554 kg, 31.8 kg, 419 >=30 kg, 97 >=35 kg**.
- Trap: KPI animals ≠ "animals weighed in the window" (single-weigh animals are excluded). For "how many weighed", use pens table sums or ceo_ai views.

### 1b. Daily gain cards: herd + per park (growth.go:618 growthHeadlineStats; :1444 growthParkGainsQuery)
- Headline = Σ(scanned animal g) + Σ(pen n × pen g) / (scanned animals + Σ pen n); sub = that denominator. Per-park cards only when no park is selected (by_park, same statistic; a scanned animal's pairs are attributed to the park of the pair). UI Math.round, "No data" when null.
- Value: **All 161 g/day (520)**, CBE 152 (292), CPT 171 (228), PARIGI no data.
- Weighing filter: individual_animal keeps only scanned pairs; per_shed_partition keeps only pens.

### 1c. Pens table (shed_weights.go:197-355 + growth.go:1044 leaderboard; pens-table.tsx; composition weight_demographics.go:969-1000)
- One row per (park, location, partition) = newest bucket WITH data in window (latest_bucket :347). Rows with 0 animals hidden; UI-only filter w_op/w_kg (gt|gte|eq|lte|lt|neq, eq = ±0.05 kg) on the row average; paging limit/offset.
- Animals/avg/total: scanned bucket = latest weigh per animal key in that bucket (any single weigh counts here, unlike 1a); whole-pen bucket = its live shed observation (animal_count, average_weight_kg, weight_kg). Last weighed = IST date.
- Gain column: whole pen = first→latest pen average span in window (shed_span :290); scanned pen = **MEDIAN** of the pen's pair rates (shed_leaderboard percentile_cont, not the mean) when it has pairs; else "—".
- Breed column: whole pen = live cohort breeds; scanned pen = breeds of tags scanned on the pen's latest scan day (unless a later whole-pen weigh); one named breed -> name, else "Mixed"/"Unknown".
- Value: 31 rows; e.g. CBE Castro 1 whole-pen 54 @ 39.1 kg, 2,110 kg, 169 g/day, 21/09; CPT Mandela 1 - Part 3 scanned 10 @ 30.5 kg, 213 g/day (median), Anantapur Sheep; CBE Yashoda 4 = Mixed.

### General base SQL (used by 1a, 1b, 1c and 6a)
```sql
WITH p AS (SELECT date '2026-08-03' f, date '2026-09-22' t, 'male'::text sexf, ''::text cat),
b AS (SELECT (f::timestamp AT TIME ZONE 'Asia/Kolkata') s, ((t+1)::timestamp AT TIME ZONE 'Asia/Kolkata') e, sexf, cat FROM p),
parks AS (SELECT location_id, tenant_id FROM locations WHERE location_type='park' AND status='active' AND retired_at IS NULL),
scoped AS (SELECT cs.campaign_shed_id, cs.campaign_id, cs.location_id, COALESCE(cs.partition_label,'') part, cs.weighing_category, c.park_id, l.name loc_name, l.parent_location_id
  FROM weighing_campaign_sheds cs JOIN weighing_campaigns c USING (campaign_id) LEFT JOIN locations l ON l.location_id=cs.location_id
  WHERE c.park_id IN (SELECT location_id FROM parks) AND cs.status<>'canceled'),
-- identity_scope.go: two active RFIDs of one goat -> one key (window start-400d)
id_weighed AS (SELECT DISTINCT lower(btrim(o.scanned_identifier)) tag FROM weighing_observations o, b
  WHERE o.campaign_shed_id IN (SELECT campaign_shed_id FROM scoped) AND o.accepted_at >= b.s - interval '400 days' AND o.accepted_at < b.e
    AND o.verification_status<>'rejected' AND btrim(o.scanned_identifier)<>''),
id_ident AS (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id, identifier_type FROM goat_identifiers
  WHERE status='active' AND identifier_type IN ('animal_identifier_1','animal_identifier_2') AND btrim(identifier_value)<>'' ORDER BY lower(btrim(identifier_value)), created_at DESC),
akmap AS (SELECT tag, canonical_tag FROM (SELECT i.tag, first_value(i.tag) OVER (PARTITION BY goat_id ORDER BY (identifier_type='animal_identifier_1') DESC, i.tag) canonical_tag,
  count(*) OVER (PARTITION BY goat_id) n FROM id_ident i WHERE goat_id IN (SELECT goat_id FROM id_ident JOIN id_weighed USING (tag))) x WHERE n>1),
-- sex_scope.go: scanned tags whose goat is that sex (90d lookback); whole-pen buckets whose live cohort is ONLY that sex
ident AS (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id FROM goat_identifiers ORDER BY lower(btrim(identifier_value)), created_at DESC),
sex_tags AS (SELECT DISTINCT lower(btrim(o.scanned_identifier)) tag FROM weighing_observations o JOIN ident i ON i.tag=lower(btrim(o.scanned_identifier)) JOIN goats g ON g.goat_id=i.goat_id, b
  WHERE o.campaign_shed_id IN (SELECT campaign_shed_id FROM scoped) AND o.accepted_at >= b.s - interval '90 days' AND o.accepted_at < b.e
    AND o.verification_status<>'rejected' AND lower(btrim(g.sex))=b.sexf),
occupied AS (SELECT DISTINCT shed_id FROM goats WHERE lifecycle_status='alive' AND shed_id IS NOT NULL),
parent_sheds AS (SELECT DISTINCT ON (parent_location_id, name) location_id, parent_location_id, name FROM locations WHERE location_type='shed' ORDER BY parent_location_id, name, location_id),
targets AS (SELECT DISTINCT s.location_id, s.part, COALESCE(CASE WHEN occ.shed_id IS NOT NULL THEN s.location_id END, ps.location_id) rid,
    COALESCE(NULLIF(s.part,''), NULLIF((regexp_match(s.loc_name,'\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1],''),'') rpart
  FROM scoped s LEFT JOIN occupied occ ON occ.shed_id=s.location_id
  LEFT JOIN parent_sheds ps ON ps.parent_location_id=s.parent_location_id AND ps.name=regexp_replace(s.loc_name,'\s*(-\s*)?(Part\s*)?[0-9]+$','')
  WHERE s.weighing_category='per_shed_partition'),
sex_buckets AS (SELECT t.location_id, t.part FROM targets t JOIN goats g ON g.shed_id=t.rid AND g.lifecycle_status='alive'
  LEFT JOIN goat_shed_partitions gsp ON gsp.goat_id=g.goat_id, b
  WHERE t.rpart='' OR regexp_replace(lower(btrim(gsp.partition_label)),'^(part|pt)[\s.-]*','')=regexp_replace(lower(btrim(t.rpart)),'^(part|pt)[\s.-]*','')
  GROUP BY t.location_id, t.part, b.sexf HAVING count(DISTINCT lower(btrim(g.sex)))=1 AND min(lower(btrim(g.sex)))=b.sexf),
-- scanned arm (growth.go growthPairsCTE): consecutive weighs per animal key, same-IST-day pairs dropped
base AS (SELECT o.observation_id, o.weight_kg::float8 w, o.accepted_at, s.park_id, COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))) k
  FROM weighing_observations o JOIN scoped s USING (campaign_shed_id) LEFT JOIN akmap ak ON ak.tag=lower(btrim(o.scanned_identifier)), b
  WHERE o.verification_status<>'rejected' AND o.accepted_at>=b.s AND o.accepted_at<b.e
    AND (b.sexf='' OR lower(btrim(o.scanned_identifier)) IN (SELECT tag FROM sex_tags)) AND (b.cat='' OR s.weighing_category=b.cat)),
pairs AS (SELECT park_id, k, w, observation_id, accepted_at, w - lag(w) OVER a dkg,
  (accepted_at AT TIME ZONE 'Asia/Kolkata')::date - (lag(accepted_at) OVER a AT TIME ZONE 'Asia/Kolkata')::date days FROM base WINDOW a AS (PARTITION BY k ORDER BY accepted_at, observation_id)),
q AS (SELECT * FROM pairs WHERE days>0),
-- 1b gain cards + 1a KPI strip:
animal_gain AS (SELECT k, (array_agg(park_id ORDER BY accepted_at DESC))[1] park_id, sum(dkg*1000.0)/sum(days) g FROM q GROUP BY k),
ind_latest AS (SELECT DISTINCT ON (k) k, w FROM q ORDER BY k, accepted_at DESC, observation_id DESC),
pen_obs AS (SELECT s.park_id, s.location_id, s.part, so.shed_observation_id id, so.accepted_at, so.average_weight_kg::float8 avg, so.animal_count n,
    so.weight_kg::float8 tot, (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d
  FROM weighing_shed_observations so JOIN scoped s USING (campaign_shed_id), b
  WHERE s.weighing_category='per_shed_partition' AND so.withdrawn_at IS NULL AND so.verification_status<>'rejected'
    AND so.accepted_at>=b.s AND so.accepted_at<b.e AND (b.sexf='' OR (s.location_id, s.part) IN (SELECT location_id, part FROM sex_buckets))),
pl AS (SELECT DISTINCT ON (location_id, part) * FROM pen_obs ORDER BY location_id, part, accepted_at DESC, id DESC),
pf AS (SELECT DISTINCT ON (location_id, part) * FROM pen_obs ORDER BY location_id, part, accepted_at, id),
pen_span AS (SELECT pl.park_id, pl.n, pl.avg, pl.tot, (pl.avg-pf.avg)*1000.0/(pl.d-pf.d) g FROM pl JOIN pf USING (location_id, part) WHERE pl.d>pf.d),
gain AS (SELECT park_id, 1.0 n, g FROM animal_gain UNION ALL SELECT park_id, n, g FROM pen_span)
SELECT 'ALL', round(sum(n*g)/sum(n)), sum(n) FROM gain
UNION ALL SELECT pk.location_code, round(sum(n*g)/sum(n)), sum(n) FROM gain JOIN locations pk ON pk.location_id=gain.park_id GROUP BY 1;
-- KPI: individual = count(ind_latest), total kg = sum(ind_latest.w)+sum(pen_span.tot), >=30 = count(w>=30)+sum(n) FILTER (avg>=30), same for 35.
```
Pens-table tail (after `q`): per bucket scanned = DISTINCT ON (campaign_shed_id, animal key) latest weigh; whole-pen = live shed obs; DISTINCT ON (park, location, partition) ORDER BY last_weighed DESC NULLS LAST, period_start_date DESC; gain = pen span (as pen_span, no head-count filter) else percentile_cont(0.5) of q's dkg*1000/days grouped by the later weigh's (location, partition).

CEO questions: "What is our daily weight gain right now?" / "Abhi daily gain kitna hai?"; "CBE vs CPT growth?" / "CBE aur CPT mein kaun zyada badh raha hai?"; "How many males are over 30/35 kg?" / "30/35 kilo ke upar kitne male hain?"; "Total live weight on farm?" / "Farm pe total kitne kilo ka maal hai?"; "Which pen is growing slowest?" / "Sabse slow kaunsa pen badh raha hai?".

## 2. Breed-wise (gain + average weight per breed)
GET /weighing/weight-demographics sections=dimensions -> gain_by_breed (bar = Math.round, "N animals") + by_breed (avg kg toFixed(1)). **Use references/adg-by-breed.sql** (verified against the dashboard; params from_date/to_date/park_code/sex/origin/weighing). Gain and weight are different populations (weight = every animal weighed once; gain = >=2 weigh days + pens that moved). Whole pens only count when all live residents are one breed (and the filtered sex). CEO: "Which breed grows fastest?" / "Kaunsi breed sabse tez badhti hai?".

## 3. Birth-wise (farm-born vs purchased per breed)
- GET weight-demographics sections=origin -> gain_by_breed_origin (weight_demographics.go:1147). Field is named median_gain_g_per_day but IS the animal-weighted mean. UI Math.round; a breed with one origin shows one bar.
- Formula: demographics gain (scanned per-day-last pairs + claimed pens). Scanned animal's origin = its tag in farm_born / purchased tag list (goat in procurement_load_goats = purchased); pen origin = all live residents agree; a pen must also be single-breed (and single-sex under a Sex filter). Unknown-origin animals are in neither bar, so bars need not add to Breed-wise.
- Page Origin filter still narrows the population first.
- Value: Anantapur Sheep farm-born **199 g (85)** / purchased **161 g (339)**; Beetal farm-born 127 (38); Sojat 135 (22); Malai 99 (11); Osmanabadi 81 (7); Sirohi 114 (4); Beetal x Sojat 149 (6); Malai x Sojat 115 (3); Beetal x Malai 110 (2); Boer x Beetal 218 (1); Boer x Malai 200 (1).
- SQL: adg-by-breed.sql CTEs `w` .. `claim` (drop its final SELECT; in `latest` also select s.location_id, s.partition_label; in `resolved_gain` also select ag.tag and LEFT JOIN latest l ON l.tag = ag.tag for l.location_id, l.partition_label), then:
```sql
tag_origin AS (SELECT wd.tag, CASE WHEN EXISTS (SELECT 1 FROM bought x WHERE x.goat_id=i.goat_id) THEN 'purchased' ELSE 'farm_born' END origin
  FROM id_weighed wd JOIN ident i ON i.tag=wd.tag),
all_targets AS (/* o_targets without the origin_on gate */ SELECT DISTINCT s.location_id, s.partition_label,
    COALESCE(CASE WHEN occ.shed_id IS NOT NULL THEN s.location_id END, phys.location_id) resolved_id,
    COALESCE(NULLIF(s.partition_label,''), NULLIF((regexp_match(loc.name,'\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1],''),'') rpart
  FROM scoped s LEFT JOIN locations loc ON loc.location_id=s.location_id LEFT JOIN o_occupied occ ON occ.shed_id=s.location_id
  LEFT JOIN o_parent_sheds phys ON phys.parent_location_id=loc.parent_location_id AND phys.name=regexp_replace(loc.name,'\s*(-\s*)?(Part\s*)?[0-9]+$','')
  WHERE s.weighing_category='per_shed_partition'),
bucket_origin AS (SELECT src.location_id, src.partition_label,
    CASE WHEN bool_and(EXISTS (SELECT 1 FROM bought x WHERE x.goat_id=g.goat_id)) THEN 'purchased'
         WHEN NOT bool_or(EXISTS (SELECT 1 FROM bought x WHERE x.goat_id=g.goat_id)) THEN 'farm_born' END origin
  FROM all_targets src JOIN goats g ON g.shed_id=src.resolved_id AND g.lifecycle_status='alive'
  LEFT JOIN goat_shed_partitions gsp ON gsp.goat_id=g.goat_id
  WHERE src.rpart='' OR regexp_replace(lower(btrim(gsp.partition_label)),'^(part|pt)[\s.-]*','')=regexp_replace(lower(btrim(src.rpart)),'^(part|pt)[\s.-]*','')
  GROUP BY 1,2)
SELECT breed, origin, sum(n) animals, round(sum(gs)/sum(n)) g_per_day FROM (
  SELECT rg.breed, t.origin, count(*) n, sum(rg.g) gs FROM resolved_gain rg JOIN tag_origin t ON t.tag=rg.tag WHERE rg.breed IS NOT NULL GROUP BY 1,2
  UNION ALL
  SELECT c.breed, bo.origin, sum(ls.animals), sum(ls.animals*ls.g_per_day) FROM lump_span ls JOIN claim c USING (location_id, partition_label)
    JOIN bucket_origin bo USING (location_id, partition_label) WHERE bo.origin IS NOT NULL GROUP BY 1,2) x GROUP BY 1,2 ORDER BY 1,2;
```
- Traps: stg purchased = only Anantapur Sheep (the farm buys sheep, breeds goats). Scanned origin match is on the canonical tag vs raw-tag lists (a double-tagged animal scanned on its secondary tag may fall out; rare).
- CEO: "Do our own-born kids grow faster than bought ones?" / "Ghar ke paida bachhe kharide hue se zyada tez badhte hain kya?"

## 4. Pen-wise (daily gain per breed, one bar per pen type)
- GET weight-demographics sections=shed_type -> gain_by_breed_shed_type (:1182, average_gain_g_per_day, Math.round) + shed_type_members (hover list of pens per bar, :1230).
- Pen type = `shed_partitions.shed_type`, a code from the farm's own Pen types register (`pen_types`, migration 000437, Configuration > Items and settings > Pen types; seeded elevated | non_elevated, the farm may add more). Names and order come from `pen_types` (the screen reads them from the page contract's `pen_types` option group; weighing itself never reads that table). Resolution: alias row (`alias_location_id` = weighing location) first, else physical shed (`shed_id` = resolved shed) + `normalized_label` = scrubbed partition (shed_type CTE :1002). Scanned animal takes the type of the pen of its LATEST weigh in window; pens must be single-breed (and single-sex if filtered). Pens with no active shed_partitions row are dropped. Weighing filter: individual counts only scanned, per_shed_partition only pens.
- Value: Anantapur Sheep elevated **173 g (176)** / non-elevated **166 g (249)**; every goat breed elevated only (Beetal 127/38, Sojat 135/22 …).
- SQL: same prefix as §3, then:
```sql
stype AS (SELECT st.location_id, st.partition_label, COALESCE(
    (SELECT sp.shed_type FROM shed_partitions sp WHERE sp.alias_location_id=st.location_id AND sp.status='active'),
    (SELECT sp.shed_type FROM shed_partitions sp WHERE sp.shed_id=st.resolved_id AND sp.status='active'
       AND sp.normalized_label=regexp_replace(lower(btrim(COALESCE(NULLIF(st.partition_label,''),st.resolved_partition_label))),'^(part|pt)[\s.-]*',''))) shed_type
  FROM shed_targets st)
SELECT breed, shed_type, sum(n), round(sum(gs)/sum(n)) FROM (
  SELECT rg.breed, st.shed_type, count(*) n, sum(rg.g) gs FROM resolved_gain rg JOIN stype st USING (location_id, partition_label)
   WHERE rg.breed IS NOT NULL AND st.shed_type IS NOT NULL GROUP BY 1,2
  UNION ALL
  SELECT c.breed, st.shed_type, sum(ls.animals), sum(ls.animals*ls.g_per_day) FROM lump_span ls JOIN claim c USING (location_id, partition_label)
    JOIN stype st USING (location_id, partition_label) WHERE st.shed_type IS NOT NULL GROUP BY 1,2) x GROUP BY 1,2 ORDER BY 1,2;
```
- CEO: "Is the elevated shed worth it?" / "Elevated shed mein growth zyada hai kya?"

## 5. Weight-wise
### 5a. Weight brackets: head count + daily gain per band (weight_demographics.go:1286)
- GET weight-demographics sections=weight_bands&band_edges_kg=15,20,25,30,35 (growth_assumptions.weight_band_edges_kg). Band = width_bucket(kg, edges): <15, 15–20, 20–25, 25–30, 30–35, 35+ (lower edge inclusive).
- Animals = scanned animals by their LATEST weigh in window (single weigh is enough) + pens by latest average with ALL their latest head count (pen must be all-that-sex under a Sex filter). Gain = only scanned animals with a demographics gain + pens weighed on >=2 days; gain_animals shown as "N animals"; band with no gain shows "no gain", not 0. Bands with 0 animals hidden.
- Value: <15: 12 animals, 70 g (5); 15–20: 85, 43 g (31); 20–25: 32, 105 g (31); 25–30: 34, 156 g (34); 30–35: 322, 172 g (322); 35+: 97, 183 g (97).
- SQL (prefix as §3):
```sql
claim_sex AS (SELECT sc.location_id, sc.partition_label FROM shed_cohort sc, b WHERE b.sexf='' OR (sc.sexes=1 AND lower(btrim(sc.sex))=b.sexf))
SELECT band, sum(n) animals, sum(gn) gain_animals, round(sum(gs)/NULLIF(sum(gn),0)) g FROM (
  SELECT width_bucket(r.weight_kg,'{15,20,25,30,35}'::numeric[]) band, count(*) n, count(ag.g) gn, COALESCE(sum(ag.g),0) gs
    FROM resolved r LEFT JOIN animal_gain ag ON ag.tag=r.tag GROUP BY 1   -- resolved needs l.tag added
  UNION ALL
  SELECT width_bucket(lu.average_weight_kg,'{15,20,25,30,35}'::numeric[]), sum(lu.animal_count), COALESCE(sum(ls.animals),0), COALESCE(sum(ls.animals*ls.g_per_day),0)
    FROM lump lu JOIN claim_sex USING (location_id, partition_label) LEFT JOIN lump_span ls USING (location_id, partition_label) GROUP BY 1) x
GROUP BY 1 ORDER BY 1;
```
- Trap: band head count (582 here) ≠ KPI strip 520 (bands count single-weigh scanned animals and pens weighed once).
- CEO: "How many animals are in each weight bracket, and which bracket grows best?" / "Kis weight group mein kitne jaanwar hain, aur kaunsa group tez badh raha hai?"

### 5b. Feed by weight band card (feed-weight-band-card.tsx / -table / -exits-drawer)
- GET /growth-director/feed-by-weight-band?park_id&sex&origin&weighing_category&from&to (growthdirector handler.go:47 -> app/feed_weight_band.go:28 -> postgres/feed_weight_band.go:122-310 rows, :333-392 exits). fb_view/fb_type/fb_src/fb_band/fb_pen/fb_group/fb_q/fb_limit/fb_offset are client-side; fb_animals flips on-farm (n, avg) vs All (n_all, avg_all incl. exited) client-side.
- Feed side: latest amended|locked `feed_direction_issues` per (park, workflow); rows quantity_kg > 0; duplicates SUM(quantity_kg), MAX(grams_per_head) per (pen, cohort, item); rollup (park, pen, shed tag, ration group, arm, breed, workflow): kg/day 1 dp, "item Ng/head". Not windowed by the period (sheet date can be after the window).
- Weight side: scanned = animal with a prior weigh on an earlier IST day (400-day lookback) and a weigh in window, banded on latest weigh, attributed to that weigh's pen; sex/origin via sex_scope/origin_scope tag lists; exited = goats.exited_at set (sold/died/other split). Whole-pen = pen weighed on >=2 IST dates (pen label folded), banded on latest average with latest head count, sex rule = all residents; a pen with a whole-pen average drops its per-animal rows.
- Tiles: Matched rows (n>0), Not shown, Rows, Pens, Lump/Per-animal rows, Animals (Σn dedup by park/pen/band/source; sub "X weighed" = individual + lump), Exited in period (goats.exited_at in window, merged_into_goat_id IS NULL, sex/origin).
- Value (male, 03/08–22/09, verified 2026-09-25): sheet 25/09/2026 (173 rollups); matched **45** rows on farm / 56 All (8 lump, 37 per-animal); **21** pens; Animals **463** ("520 weighed" = 185 individual + 335 lump); exited **76** (66 sold, 5 died, 5 other; 59 weighed). Rows: CBE Castro 2 30–35 73 @ 34.2; CBE Castro 3 30–35 59 @ 31.9; CBE Castro 1 35+ 54 @ 39.1. Full SQL: adg-analytics-charts.md C3/C4. Numbers move with every new locked sheet.
- SQL sketch (canonical-tag map omitted; rows then differ only for double-tagged animals):
```sql
-- latest sheet per (park, workflow):
SELECT DISTINCT ON (park_id, workflow) * FROM feed_direction_issues WHERE state IN ('amended','locked') ORDER BY park_id, workflow, feed_day DESC, COALESCE(locked_at, amended_at, issued_at) DESC;
-- per-animal band rows: tags with a pair ending in window (as ind_latest in the General base, 400-day prior allowed),
--   width_bucket(latest kg,'{15,20,25,30,35}'), n = count FILTER (WHERE goats.exited_at IS NULL), n_all = count(*);
-- pen rows: pen_span from the General base, width_bucket(pl.avg, edges), n = pl.n.
```
- Traps: "X weighed" can exceed the KPI strip (it was 551 vs 520 on the 24/09 sheet) because this read allows the prior weigh up to 400 days before the window and folds pen labels; on the 25/09 sheet it equals 520. A pen whose weighed animals all left shows under Not shown on farm.
- CEO: "How much feed does each weight group get?" / "Har weight band ko kitna feed ja raha hai?"; "Which pens are fed but not weighed?" / "Kaunse pen ko feed mil raha hai par weighing nahi hui?"; "How many males left this period?" / "Is period mein kitne male bahar gaye?"

## 6. Time-wise (tw_bucket = week (default) | month; tw_pen = location::partition narrows all four sections)
Week = Monday-start calendar week of the IST date (date_trunc('week')). Month = rolling 30-day block counted back from the window's LAST day: start = to − 29 − 30×((to − d)/30) (gain_bucket_sql.go:14-60) — TRAP: the first block's label can predate the window (07/25 for 03/08–22/09) and only holds in-window data. A bucket with nobody weighed is absent, never 0. Scanned animal: one figure per animal per bucket = Σgrams/Σdays of pairs whose LATER weigh is in the bucket. Pen: CONSECUTIVE pen weighs (not first-vs-latest), grouped by the later weigh's bucket, Σgrams/Σleg-days, head count at the pen's latest weigh in the bucket.

### 6a. Overall weekly gain (growth.go:908 growthWeeklyGainTemplate; GET /weighing/leadership/growth sections=weekly_gain&bucket&pen_location_id&pen_partition_label)
- Value (week): 03/08 **153 g (140)**, 10/08 141 (500), 17/08 194 (472), 24/08 198 (495), 31/08 88 (483), 07/09 222 (459), 14/09 169 (459), 21/09 147 (462). Month: 25/07 block 164 (561), 24/08 block 156 (502).
- SQL: General base + 
```sql
wk AS (SELECT (date_trunc('week', accepted_at AT TIME ZONE 'Asia/Kolkata'))::date ws, k, sum(dkg*1000.0)/sum(days) g FROM q GROUP BY 1,2),
pen_obs AS (/* as General base pen_obs */),
legs AS (SELECT location_id, part, n, d, (avg-lag(avg) OVER w)*1000.0 gg, d-lag(d) OVER w ld FROM pen_obs WINDOW w AS (PARTITION BY location_id, part ORDER BY d)),
pw AS (SELECT (date_trunc('week', d::timestamp))::date ws, location_id, part, (array_agg(n ORDER BY d DESC))[1]::float8 n, sum(gg)/sum(ld) g
  FROM legs WHERE ld>0 GROUP BY 1,2,3)
SELECT ws, round(sum(n*g)/sum(n)), sum(n) FROM (SELECT ws, 1.0 n, g FROM wk UNION ALL SELECT ws, n, g FROM pw) x GROUP BY ws ORDER BY ws;
```

### 6b. Breed × week (weight_demographics.go:1334 gain_by_breed_week; sections=weekly_gain)
- Demographics scanned engine (last weigh per IST day); pens only when single-breed (and single-sex if filtered), so rows need not add to 6a (mixed pens count in 6a only).
- Value: Anantapur Sheep 03/08 153 (140), 10/08 146 (433), 17/08 206 (405), 24/08 216 (411), 31/08 78 (395), 07/09 240 (392), 14/09 169 (385), 21/09 156 (389); Beetal 10/08 116 (22) … 14/09 209 (37), 21/09 97 (38).

### 6c. Pen × week grid (:1354 gain_by_pen_week; pen-week-gain-table.tsx, Math.round g)
- Scanned animals are placed in the pen of their LATEST weigh in the window (not the pen of each week); pens need no single-breed claim (mixed pens listed); under a Sex filter a pen needs all-that-sex residents. 146 cells.
- Value: CBE Castro 1: 10/08 60 (63), 17/08 212, 24/08 272, 31/08 60, 07/09 212, 14/09 259 (60), 21/09 153 (54). CBE Castro 2 07/09 320 (73).

### 6d. Load × week grid (:1385 gain_by_load_week; load-week-gain-table.tsx)
- A load = its pens in `weighing_shed_load_tags`; a pen tagged to >1 load counts for none (HAVING count(*)=1 :1412). Load figure = Σ(n·g)/Σn over its pens' 6c cells.
- Value: load 126 Ramesh Reddy = CBE Castro 1 row above; load 131 Krishnamorrthy 03/08 182 (63), 10/08 102 (95), 17/08 280 … 21/09 113 (63).
- SQL for 6b–6d (prefix as §3, sex filter via b.sexf):
```sql
agw AS (SELECT tag, (date_trunc('week', accepted_at AT TIME ZONE 'Asia/Kolkata'))::date ws, sum((weight_kg-prev_w)*1000.0)/sum(d-prev_d) g
  FROM paired WHERE prev_d IS NOT NULL AND d>prev_d GROUP BY 1,2),
agw_r AS (SELECT a.*, l.location_id, l.partition_label, g.breed FROM agw a JOIN latest l USING (tag) LEFT JOIN ident i USING (tag)
  LEFT JOIN goats g ON g.goat_id=i.goat_id, b WHERE b.sexf='' OR lower(btrim(g.sex))=b.sexf),
pen_legs AS (SELECT location_id, partition_label, animal_count n, d, (average_weight_kg-lag(average_weight_kg) OVER w)*1000.0 gg, d-lag(d) OVER w ld
  FROM pen_obs WINDOW w AS (PARTITION BY location_id, partition_label ORDER BY d)),
pw AS (SELECT (date_trunc('week', d::timestamp))::date ws, location_id, partition_label, (array_agg(n ORDER BY d DESC))[1]::float8 n, sum(gg)/sum(ld) g
  FROM pen_legs WHERE ld>0 GROUP BY 1,2,3),
pw_sex AS (SELECT pw.* FROM pw JOIN shed_cohort sc USING (location_id, partition_label), b WHERE b.sexf='' OR (sc.sexes=1 AND lower(btrim(sc.sex))=b.sexf)),
per_pen AS (SELECT location_id, partition_label, ws, sum(n) n, sum(n*g) gs FROM (
  SELECT location_id, partition_label, ws, 1.0 n, g FROM agw_r UNION ALL SELECT location_id, partition_label, ws, n, g FROM pw_sex) x GROUP BY 1,2,3),
one_load AS (SELECT location_id, min(load_ref) load_ref, min(owner_name) owner_name FROM weighing_shed_load_tags GROUP BY 1 HAVING count(*)=1)
-- 6b: breed rows = agw_r (breed not null) UNION ALL pw JOIN claim, Σ(n·g)/Σn by breed, ws
-- 6c: SELECT location_id, partition_label, ws, n, round(gs/n) FROM per_pen
-- 6d: SELECT load_ref, owner_name, ws, sum(n), round(sum(gs)/sum(n)) FROM per_pen JOIN one_load USING (location_id) GROUP BY 1,2,3
```
- Month bucket: replace date_trunc with the rolling expression above (anchor = to date). tw_pen: add `location_id = :pen AND partition_label = :part` to `scoped` (demographics) and to pairs/pen_obs (growth).
- CEO: "Was growth better this week than last?" / "Is hafte growth pichhle hafte se achhi thi?"; "How did Castro 1 do week by week?" / "Castro 1 ka hafte-wise gain?"; "Which purchase load is growing best each week?" / "Kaunsa load har hafte sabse achha badh raha hai?"

## 7. Comparison tab (tab=load; purchase vs latest weight per load)
- Ignores period/sex/origin/weighing; Park only. Reads (weights-analytics.tsx:297-360): GET /weighing/shed-weights?park_id&from=2024-01-01&to=today&include_loads=true (by_load, load_weights.go:49-245), GET /procurement/loadwise-weights (loadwise_handler.go:297 -> loadwise_repository.go:63-351, max 60 loads), GET /procurement/loadwise-sales (only when control load_value_chart enabled; SalesRead), GET /growth-director/sale-prices.
- Loads shown: only loads with a latest weighing (load-comparison-tab.tsx:141). A pen tagged to two loads is dropped (load_weights.go:188), so both loads can vanish (stg: 100/101 share Mandela 1 - Part 1; 113/136 untagged) -> 5 of 9 loads.
- Purchased avg = procurement_loads.purchase_weight_kg ÷ purchased (expected_count if >0 else attributed accepted procurement_load_goats + prior sold/died) (domain/loadwise.go:305-329). 1 dp. Park filter: load's farm = upper(location_code) only when every accepted animal names one park.
- Latest avg = per pen, latest IST day's Σ(avg·n)/Σn over whole-pen weighs (non-withdrawn, non-rejected) + scanned animals (last per animal per day, canonical key, non-rejected); load = Σ(avg·n)/Σn over its tagged pens' latest days. Pens cell = park code + pen · animals. Multiple = latest ÷ purchased, toFixed(1) on unrounded values.
- Value chart: purchase ₹ = animal + transport + other cost; stock ₹ = Σ remaining animals (alive/sick/under_treatment/quarantine/icu, not sold/dead/exited) × latest avg × sale price (species/stage/sex override else species; stg 425 ₹/kg); gain ₹ = stock − purchase.
- Value (all parks): 126 Ramesh Reddy 67 bought, 23.5 -> 39.1 kg (CBE Castro 1 · 54) 1.7×, stock ₹8,30,323 vs ₹7,08,000; 128 Krishnamorrthy 19.3 -> 31.9 (CBE Castro 3 · 59) 1.7×; 129 Krishnamorrthy 20.1 -> 33.2 (CPT Godel 2 P1/P2) 1.7×, gain ₹3,82,581; 130 Green Fresh Farm 21.5 -> 34.2 (CBE Castro 2 · 73) 1.6×; 131 Krishnamorrthy 19.4 -> 31.4 (CPT Castro 1/2) 1.6×.
- SQL: purchase side = references/load-wise-sales.sql. Latest side:
```sql
WITH scoped AS (SELECT cs.campaign_shed_id, cs.location_id, COALESCE(cs.partition_label,'') pl, cs.weighing_category cat
  FROM weighing_campaign_sheds cs JOIN weighing_campaigns c USING (campaign_id) WHERE cs.status<>'canceled'),
lump AS (SELECT s.location_id, s.pl, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, o.average_weight_kg a, o.animal_count n
  FROM scoped s JOIN weighing_shed_observations o USING (campaign_shed_id)
  WHERE s.cat='per_shed_partition' AND o.withdrawn_at IS NULL AND o.verification_status<>'rejected'
    AND o.accepted_at >= '2024-01-01 00:00+05:30' AND o.accepted_at < ((now() AT TIME ZONE 'Asia/Kolkata')::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata'),
ind AS (SELECT location_id, pl, d, avg(w) a, count(*) n FROM (SELECT DISTINCT ON (s.location_id, s.pl, lower(btrim(o.scanned_identifier)), (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date)
    s.location_id, s.pl, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, o.weight_kg w
  FROM scoped s JOIN weighing_observations o USING (campaign_shed_id) WHERE s.cat='individual_animal' AND o.verification_status<>'rejected'
  ORDER BY s.location_id, s.pl, lower(btrim(o.scanned_identifier)), (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date, o.accepted_at DESC) x GROUP BY 1,2,3),
daily AS (SELECT location_id, pl, d, sum(a*n)/NULLIF(sum(n),0) a, sum(n) n FROM (SELECT * FROM lump UNION ALL SELECT * FROM ind) u GROUP BY 1,2,3),
latest AS (SELECT DISTINCT ON (location_id, pl) * FROM daily ORDER BY location_id, pl, d DESC),
tag AS (SELECT location_id, min(load_ref) load_ref FROM weighing_shed_load_tags GROUP BY 1 HAVING count(*)=1)
SELECT t.load_ref, round((sum(l.a*l.n)/sum(l.n))::numeric,1) latest_avg, sum(l.n) animals_in_pens FROM tag t JOIN latest l USING (location_id) GROUP BY 1 ORDER BY 1;
```
- Traps: latest avg counts every animal standing in the tagged pen, whichever load it came from; stock value multiplies REMAINING head count (e.g. 77 for load 129) by the pen average (76 weighed).
- CEO: "How much has each load grown since purchase?" / "Har load kharidne ke baad kitna bada?"; "Which vendor's animals grew best?" / "Kis vendor ke jaanwar sabse achhe bade?"; "What is load 129 worth now vs what we paid?" / "Load 129 ki abhi ki value vs kharid?"

## Not on this page
- gain-threshold-bars.tsx (sections=gain_thresholds, <=180/180–200/200–250/>250 g per breed) renders on /weighing/weights, not on ADG Analytics.
- Shortcut trap: weight-demographics with weighing=per_shed_partition, sex=all, no origin and only dimensions/origin/weight_bands sections runs getShedPartitionWeightDemographics (weight_demographics.go:1855), a separate pens-only query; the page's default (sex=male) never takes it.
