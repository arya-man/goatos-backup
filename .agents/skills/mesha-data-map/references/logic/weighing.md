# Weighing — admin-web /weighing/weights (logic map)

Index: W0 scope+filters · W1 kids weighed split · W2 total kg · W3 avg kg · W4 over-30 / over-35 · W5 park daily gain (ADG) · W6 shed table · W7 shed gain chart · W8 breed/sex/stage charts · W9 gain-threshold table · W10 load chart+placements · W11 losing kids · W12 corrections/verification/rejected rules · W13 screens that do not exist in admin-web.

Screen: `apps/admin-web/features/weighing/weights.tsx` (route `app/(admin)/weighing/weights/page.tsx`). One page render = 4 reads, all with the SAME scope (park_id, sex, origin, weighing_category, from, to):
- `GET /weighing/shed-weights` → `backend/internal/weighing/adapters/postgres/shed_weights.go:71` (W1–W4, W6, W7 lump bars, W10)
- `GET /weighing/leadership/growth?sections=headline,shed_leaderboard,losing_animals` → `growth.go:285` (W5, W7 individual bars, W11)
- `GET /weighing/weight-demographics?sections=composition,dimensions,gain_thresholds` → `weight_demographics.go:340` (W8, W9, composition chips)
- `GET /growth-director/...road_to_sale,fair_fight` — Growth Director, documented elsewhere.
Handlers: `adapters/http/handler.go:132-141`.

## W0 Scope, window, filters (read this first)
- Window: `wt_from`/`wt_to` IST business days, half-open `[from 00:00 IST, to+1 00:00 IST)` on `accepted_at`. Default = `2026-08-03` (`landing-window-constants.ts:11`, or SOP rolling setting) → latest weighing day (`GET /weighing/weighing-dates` over 400-day lookback). Future dates clamp to today.
- Sex: **absent = MALE** (page default). `sex=all` = everyone. Individual rows kept if tag ∈ sex-scope tags (`sex_scope.go`, goats.sex via goat_identifiers); a whole-pen weigh counts only if the pen's live cohort is 100 % that sex (mixed pens counted by neither side).
- Origin: absent = all; `farm_born`/`purchased` decided per PEN from `weighing_shed_load_tags` (`origin_scope.go`). Sex+origin = intersection.
- Weighing: `all` | `individual_animal` | `per_shed_partition` → `weighing_campaign_sheds.weighing_category`.
- Park: `c.park_id = ANY(...)` via `weighing_campaigns`.
- Always: bucket `status <> 'canceled'`; observations `verification_status <> 'rejected'` (pending + rework + verified all COUNT); whole-pen rows `withdrawn_at IS NULL`.
- Identity merge: one animal = canonical tag (`identity_scope.go`: tags sharing a goat_id in goat_identifiers collapse to one key); else `lower(btrim(scanned_identifier))`.
- Sale lines: tenant assumptions `sale_ready_lower_kg` (30) / `sale_ready_threshold_kg` (35), `domain/shed_weights.go:307`.

## W1 "Kids weighed" = individual · whole-pen (sub: total)
`summary_individual` + `summary_lump`, `shed_weights.go:~370-455`. **Only kids weighed TWICE inside the window** (pairs, lookback $12 = 0).
- Individual: per animal key, pairs (LAG by accepted_at) with IST-day gap > 0 and latest point ≥ from → count animals.
- Whole pen: per (location, partition), latest vs first non-withdrawn weigh in window, latest day > first day → sum latest `animal_count`.
- Trap: shed table (W6) counts kids weighed ONCE; KPI is always ≤ table sum. 
- Verified 2026-09-24 (sex=all, 03/08–24/09, all parks): **419 · 335 (754 total)**.

## W2 Total weight (kg, 0 dp)
Same population as W1: Σ latest weight of each paired individual + Σ latest `weight_kg` (pen total) of each paired pen. Verified: **22,201 kg**.

## W3 Average weight (kg, 1 dp)
`shed_weights.go:~620` = W2 / W1 total (herd-weighted, not mean of pens). Null → "No data". Verified: **29.4 kg**.

## W4 Over 30 kg / Over 35 kg (sub: threshold basis = W1 total)
Individual: latest weight ≥ line. Whole pen: pen's latest AVERAGE ≥ line → ALL its head count, else none (never split). Verified: **≥30 = 458, ≥35 = 108** (basis 754).

## W5 "<Park|All parks> daily gain" (g/day, rounded)
`growthHeadlineStats`, `growth.go:618`; pairs CTE `growth.go:204-284`.
- Per animal g = Σ(w−prev)·1000 / Σ IST-days over its in-window pairs (days > 0; every capture kept, no per-day dedup).
- Per pen g = (latest avg − first avg)·1000 / days, weighted by latest head count.
- Headline = (Σ animal g + Σ pen g·head) / (animals + pen heads). Sub = headline animals (= W1 total). `insufficient_data` → "No data", never 0.
- Verified: **139 g/day** over 754.

## W6 Shed table (park, pen + composition chips, kids, avg kg, total kg, last weighed, status)
`scoped → ind/lump → per_bucket → latest_bucket`, `shed_weights.go:~200-360`.
- Individual bucket: DISTINCT ON animal key, latest in-window weigh → count, avg, sum, max IST day.
- Whole-pen bucket: the live shed observation's animal_count / average_weight_kg / weight_kg.
- One row per (park, location, partition): newest bucket WITH data (`last_weighed DESC NULLS LAST`, then period_start_date). Status = bucket status. Capped `MaxShedWeightsRows`.
- Header line "Sheds weighed N / M": M = all non-canceled pen rows in scope (sex/origin do NOT shrink M), N = rows with kids > 0.
- Verified: **45 / 47** sheds; table kid sum 767 (once-weighed, raw-tag sketch).
- Composition chips: `weight_demographics.go:969-1000` (breed·sex·count of latest-weighed tags; whole pen from live goats in shed).

## W7 Shed daily-gain chart/table
- Individual pens: `growthShedLeaderboard` `growth.go:1044` → **median** pair ADG per pen (`percentile_cont(0.5)`), shown only if pair_count > 0.
- Whole pens: `shed_span` `shed_weights.go:~290` → (latest avg − first avg)·1000/days inside window; null if one weigh day.
- Trap: individual = median of pairs, whole pen = mean movement — different statistics on one chart (tagged Individual / Whole pen).

## W8 Breed / Sex / Stage charts (weight or gain toggle)
`weight_demographics.go:66-150`.
- Weight: individual latest weight per animal key → goats (breed/sex/management_stage via newest goat_identifiers row); whole pen joins only if pen cohort is single breed (breed chart) / single sex / single stage; bar = Σkg / Σheads (head-weighted).
- Gain: per animal Σgain/Σdays **after collapsing to last weigh per IST day** (`obs` CTE) + pen spans weighted by heads; bar = mean (field name says `median_gain` — it is a weighted MEAN). Unknown breed/sex/stage rows are dropped from these bars.
- Trap: dedup-per-day means these gains can differ slightly from W5.

## W9 Gain thresholds by breed (>250 · 200–250 · 180–200 · ≤180 g/day)
`weight_demographics.go:281-305`. Per animal g (W8 method) bucketed; whole pens (single-breed cohort only) put all heads in the band of the pen's g. % = count / breed animals (`sharesOfWhole`, sums to 100).

## W10 Load chart + load placements table
`load_weights.go:49` (included by default, `include_loads != false`).
- Pen → load via `weighing_shed_load_tags`; a pen tagged with 2+ loads is dropped (counted in `load_unattributed_sheds`).
- avg kg = Σ(pen avg·heads)/Σheads over weighed tagged pens; gain = same weighting over pens weighed twice only (label shows span days). Individual pens: per tag latest per IST day, then pen daily avg.
- Placements: pens per load with head counts, ordered by park then heads.

## W11 Losing kids table
`growthLosingAnimals` `growth.go:1361`: each animal's LATEST in-window pair; keep ADG < 0; order most negative first; LIMIT 200; paged client-side. Columns: tag, pen, previous kg, latest kg, change, days between, latest date. Verified: **53** kids (sex=all).

## W12 Corrections, verification, rework, rejected, withdrawn
- Correction (`POST /app/weighing/observations/{id}/weight-correction`, `weight_correction.go:80-100`) OVERWRITES `weight_kg` (whole pen also `animal_count`, `average_weight_kg`); first original kept in `operator_weight_kg`/`operator_animal_count`. Every screen number uses the corrected value. Stg: 500 animal + 8 pen corrections.
- `verification_status`: rejected excluded everywhere; pending (278) and rework (16) INCLUDED. Growth headline also returns `unverified_observation_count` (pending endpoints).
- Whole-pen reopen/resubmit: older rows get `withdrawn_at` → excluded (2 on stg).

## W13 Not admin-web screens
Rounds/campaigns, progress, fasting, per-pen verification exist only as app/mobile APIs (`/app/weighing/campaigns…`, `/app/weighing/fasting`, `/weighing/process-state`); verification queue is the generic `/verification` route. No admin-web weighing number comes from them. For "last weighing of pen X" use `../pen-weighing-latest.sql`; ADG by breed `../adg-by-breed.sql`; herd avg `../herd-avg-weight.sql`.

## SQL sketch (KPIs W1–W5, W11) — verified 2026-09-24, sex=all, all parks, 03/08–24/09/2026
Result: `ind 419 | pen 335 | 22201 kg | 29.4 kg | ≥30 458 | ≥35 108 | 139 g | losing 53`.
```sql
WITH w AS (SELECT '2026-08-03'::date AT TIME ZONE 'Asia/Kolkata' s, '2026-09-25'::date AT TIME ZONE 'Asia/Kolkata' e),
idm AS (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id FROM goat_identifiers ORDER BY 1, created_at DESC),
canon AS (SELECT tag, min(tag) OVER (PARTITION BY goat_id) ct FROM idm),
sc AS (SELECT * FROM weighing_campaign_sheds WHERE status<>'canceled'),
o AS (SELECT o.observation_id, o.weight_kg::float8 wt, o.accepted_at, coalesce(c.ct, lower(btrim(o.scanned_identifier))) k
  FROM weighing_observations o JOIN sc ON sc.campaign_shed_id=o.campaign_shed_id AND sc.weighing_category='individual_animal'
  LEFT JOIN canon c ON c.tag=lower(btrim(o.scanned_identifier)), w
  WHERE o.verification_status<>'rejected' AND o.accepted_at>=w.s AND o.accepted_at<w.e),
p AS (SELECT *, lag(wt) OVER x pw, lag(accepted_at) OVER x pa FROM o WINDOW x AS (PARTITION BY k ORDER BY accepted_at, observation_id)),
qi AS (SELECT *, (accepted_at AT TIME ZONE 'Asia/Kolkata')::date-(pa AT TIME ZONE 'Asia/Kolkata')::date dd FROM p
       WHERE pw IS NOT NULL AND (accepted_at AT TIME ZONE 'Asia/Kolkata')::date>(pa AT TIME ZONE 'Asia/Kolkata')::date),
ind AS (SELECT DISTINCT ON (k) k, wt FROM qi ORDER BY k, accepted_at DESC, observation_id DESC),
ag AS (SELECT k, sum((wt-pw)*1000)/sum(dd) g FROM qi GROUP BY k),
lp AS (SELECT cs.location_id, coalesce(cs.partition_label,'') pl, sh.animal_count n, sh.weight_kg tw, sh.average_weight_kg a,
         (sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, sh.accepted_at
  FROM weighing_shed_observations sh JOIN sc cs ON cs.campaign_shed_id=sh.campaign_shed_id AND cs.weighing_category='per_shed_partition', w
  WHERE sh.withdrawn_at IS NULL AND sh.verification_status<>'rejected' AND sh.accepted_at>=w.s AND sh.accepted_at<w.e),
ll AS (SELECT DISTINCT ON (location_id,pl) * FROM lp ORDER BY location_id,pl,accepted_at DESC),
lf AS (SELECT DISTINCT ON (location_id,pl) * FROM lp ORDER BY location_id,pl,accepted_at),
ls AS (SELECT l.n, l.tw, l.a, (l.a-f.a)*1000/(l.d-f.d) g FROM ll l JOIN lf f USING (location_id,pl) WHERE l.d>f.d)
SELECT (SELECT count(*) FROM ind) ind_kids, (SELECT sum(n) FROM ls) pen_kids,
  round(((SELECT sum(wt) FROM ind)+(SELECT sum(tw) FROM ls))::numeric) total_kg,
  round((((SELECT sum(wt) FROM ind)+(SELECT sum(tw) FROM ls))/((SELECT count(*) FROM ind)+(SELECT sum(n) FROM ls)))::numeric,1) avg_kg,
  (SELECT count(*) FILTER (WHERE wt>=30) FROM ind)+(SELECT coalesce(sum(n) FILTER (WHERE a>=30),0) FROM ls) ge30,
  (SELECT count(*) FILTER (WHERE wt>=35) FROM ind)+(SELECT coalesce(sum(n) FILTER (WHERE a>=35),0) FROM ls) ge35,
  round((((SELECT sum(g) FROM ag)+(SELECT sum(n*g) FROM ls))/((SELECT count(*) FROM ag)+(SELECT sum(n) FROM ls)))::numeric) adg_g,
  (SELECT count(*) FROM (SELECT DISTINCT ON (k) * FROM qi ORDER BY k, accepted_at DESC) z WHERE wt<pw) losing;
```
Add filters: park → `JOIN weighing_campaigns c USING (campaign_id) WHERE c.park_id=…` in `sc`; weighing mode → drop the ind or ls half; sex → restrict `k` to tags whose goats.sex matches and ls to single-sex pens (app default is MALE, so app numbers differ from this sex=all sketch).

Shed-count sketch (W6 header), verified **47 in scope / 45 weighed**: per non-canceled bucket compute in-window kids, keep `DISTINCT ON (park_id, location_id, coalesce(partition_label,''))` ordered by `last_day IS NULL, last_day DESC, period_start_date DESC, created_at DESC`, then `count(*)`, `count(*) FILTER (WHERE kids>0)`.

## CEO questions
- How many kids were weighed twice / what's the herd average weight? — "Kitne bacche do baar tole gaye, average weight kitna hai?" → W1, W3
- Total live weight on the farm? — "Farm pe total kitna kilo maal hai?" → W2
- How many kids are over 30 / 35 kg (sale ready)? — "30/35 kilo se upar kitne bacche hain, bechne layak kitne?" → W4
- What's our daily gain (ADG) for CBE / all parks? — "Roz ka gain kitna aa raha hai, CBE ka ADG kya hai?" → W5
- Which pens were weighed, when, and what do they weigh? — "Kaunse shed tole gaye, kab, aur average kitna?" → W6
- Which pen is growing fastest/slowest? — "Kaunsa shed sabse tez/dheere badh raha hai?" → W7
- Which breed / sex / stage is heaviest or gaining most? — "Kaunsi breed sabse zyada gain kar rahi hai? Male vs female?" → W8
- How many kids of each breed gain over 250 g or under 180 g a day? — "Har breed mein 250 gram se upar kitne, 180 se neeche kitne?" → W9
- How is each purchase load doing, and where were they placed? — "Har load ka weight aur gain kaisa hai, kis shed mein rakha?" → W10
- Which kids are losing weight? — "Kaunse bacche weight kho rahe hain?" → W11
- Were weights corrected / are any unverified? — "Kitne weight correct kiye gaye, kitne verify baaki hain?" → W12

## W14 Shed table rows + shed gain chart (W6/W7 with SQL) — added 2026-09-25
- Endpoints: `GET /weighing/shed-weights` (handler.go:230 -> shed_weights.go:71 `scoped/ind/lump/shed_span/per_bucket/latest_bucket` CTEs) rows[].animals_weighed / average_weight_kg / total_weight_kg / last_weighed_date / bucket_status / shed_average_gain_g_per_day; `GET /weighing/leadership/growth sections=shed_leaderboard` (handler.go:188 -> growth.go:1044 `growthShedLeaderboard`) shed_leaderboard[].median_adg_g_per_day, adg_pair_count.
- UI: weights.tsx table (section.sheds.title, columns park, shed + chips, animals_weighed, average_weight, total_weight, last_weighed, workflow ~:1240-1300); shed chart `ShedMetricChart` weights.tsx:1028 (gain view: per-animal bars from shed_leaderboard where adg_pair_count>0 + whole-pen bars from shed_average_gain_g_per_day; weight view: row averages). Chart table columns park, shed, breed, sex, count, basis, value. Order: park (CBE, CPT) then natural pen name, not ranked.
- Formula: one row per (park, location, partition) = newest non-canceled bucket WITH data in window. Scanned bucket: latest non-rejected weigh per same-animal key (single weigh counts) -> count, avg, sum, max IST day. Whole-pen bucket: its live (withdrawn_at NULL, non-rejected) shed observation. Gain: whole pen = (latest avg − first avg)·1000/days over live pen weighs in window (null when one weigh day); scanned pen = MEDIAN of consecutive-weigh pair rates (IST-day gap>0) filed under the later weigh's pen.
- Filters -> SQL: park `c.park_id=…` in `scoped`; weighing `cs.weighing_category=…`; sex/origin restrict scanned tags (sex_scope.go tag list) and pens (all live residents one sex/origin) — see W0. Status column = weighing_campaign_sheds.status of the winning bucket (completed/closed/…).
- Verified 2026-09-25, sex=all, all parks, 2026-08-03..09-24: 47 rows, 45 with kids>0 (= "Sheds weighed 45 / 47"); CBE Castro 1 whole pen 54 @ 39.1 kg, 2,110 kg, 21/09, 169 g; CPT Mandela 1 - Part 3 scanned 10 @ 30.5 kg, median 213 g; CBE Godel 2 - Part 3 −87 g (whole pen); CPT Yashoda 2 22 kids, no pairs -> no gain bar.
```sql
WITH prm AS (SELECT '2026-08-03'::date fd, '2026-09-24'::date td),
w AS (SELECT (fd::timestamp AT TIME ZONE 'Asia/Kolkata') s, ((td+1)::timestamp AT TIME ZONE 'Asia/Kolkata') e FROM prm),
idm AS (SELECT tag, first_value(tag) OVER (PARTITION BY goat_id ORDER BY (identifier_type='animal_identifier_1') DESC, tag) ct
  FROM (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id, identifier_type FROM goat_identifiers
        WHERE status='active' AND identifier_type IN ('animal_identifier_1','animal_identifier_2') AND btrim(identifier_value)<>'' ORDER BY 1, created_at DESC) i),
scoped AS (SELECT cs.campaign_shed_id, cs.location_id, coalesce(cs.partition_label,'') pl, cs.weighing_category cat, cs.status, c.park_id, c.period_start_date, cs.created_at
  FROM weighing_campaign_sheds cs JOIN weighing_campaigns c USING (campaign_id) WHERE cs.status<>'canceled'),
ind AS (SELECT s.campaign_shed_id, count(*) n, avg(wt) a, sum(wt) t, max(d) d FROM scoped s JOIN LATERAL (
   SELECT DISTINCT ON (coalesce(nullif(coalesce(m.ct, lower(btrim(o.scanned_identifier))),''), o.observation_id::text)) o.weight_kg wt, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d
   FROM weighing_observations o LEFT JOIN idm m ON m.tag=lower(btrim(o.scanned_identifier)), w
   WHERE o.campaign_shed_id=s.campaign_shed_id AND o.accepted_at>=w.s AND o.accepted_at<w.e AND o.verification_status<>'rejected'
   ORDER BY coalesce(nullif(coalesce(m.ct, lower(btrim(o.scanned_identifier))),''), o.observation_id::text), o.accepted_at DESC, o.observation_id DESC) x ON true
  WHERE s.cat='individual_animal' GROUP BY 1),
lump AS (SELECT s.campaign_shed_id, sh.animal_count n, sh.average_weight_kg a, sh.weight_kg t, (sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d
  FROM scoped s JOIN weighing_shed_observations sh USING (campaign_shed_id), w
  WHERE s.cat='per_shed_partition' AND sh.withdrawn_at IS NULL AND sh.verification_status<>'rejected' AND sh.accepted_at>=w.s AND sh.accepted_at<w.e),
pb AS (SELECT s.*, coalesce(i.n,l.n,0) n, coalesce(i.a,l.a) a, coalesce(i.t,l.t) t, coalesce(i.d,l.d) d FROM scoped s LEFT JOIN ind i USING (campaign_shed_id) LEFT JOIN lump l USING (campaign_shed_id)),
lb AS (SELECT DISTINCT ON (park_id, location_id, pl) * FROM pb ORDER BY park_id, location_id, pl, (d IS NULL), d DESC, period_start_date DESC, created_at DESC, campaign_shed_id DESC),
-- W7 whole-pen span: first vs latest live pen weigh in window
po AS (SELECT cs.location_id, coalesce(cs.partition_label,'') pl, o.average_weight_kg a, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, o.accepted_at
  FROM weighing_shed_observations o JOIN weighing_campaign_sheds cs USING (campaign_shed_id), w
  WHERE o.withdrawn_at IS NULL AND o.verification_status<>'rejected' AND o.accepted_at>=w.s AND o.accepted_at<w.e),
span AS (SELECT l.location_id, l.pl, (l.a-f.a)*1000.0/(l.d-f.d) g FROM (SELECT DISTINCT ON (location_id,pl) * FROM po ORDER BY location_id,pl,accepted_at DESC) l
  JOIN (SELECT DISTINCT ON (location_id,pl) * FROM po ORDER BY location_id,pl,accepted_at) f USING (location_id,pl) WHERE l.d>f.d),
-- W7 scanned median: consecutive weighs per animal key (growthPairsCTE), pair filed under the LATER weigh's pen
b AS (SELECT o.observation_id, o.weight_kg::float8 wt, o.accepted_at, cs.location_id, coalesce(cs.partition_label,'') pl, coalesce(m.ct, lower(btrim(o.scanned_identifier))) k
  FROM weighing_observations o JOIN weighing_campaign_sheds cs USING (campaign_shed_id) LEFT JOIN idm m ON m.tag=lower(btrim(o.scanned_identifier)), w
  WHERE o.verification_status<>'rejected' AND o.accepted_at>=w.s AND o.accepted_at<w.e),
pr AS (SELECT *, (wt-lag(wt) OVER x)*1000.0/nullif((accepted_at AT TIME ZONE 'Asia/Kolkata')::date-(lag(accepted_at) OVER x AT TIME ZONE 'Asia/Kolkata')::date,0) g,
  (accepted_at AT TIME ZONE 'Asia/Kolkata')::date-(lag(accepted_at) OVER x AT TIME ZONE 'Asia/Kolkata')::date dd FROM b WINDOW x AS (PARTITION BY k ORDER BY accepted_at, observation_id)),
med AS (SELECT location_id, pl, percentile_cont(0.5) WITHIN GROUP (ORDER BY g) g, count(*) pairs FROM pr WHERE dd>0 GROUP BY 1,2)
SELECT coalesce(pk.location_code,pk.name) park, l.name pen, lb.pl part, lb.cat, lb.n kids, round(lb.a::numeric,1) avg_kg, round(lb.t::numeric) total_kg, lb.d last_weighed, lb.status,
  round(coalesce(sp.g, md.g)::numeric) gain_g
FROM lb JOIN locations l ON l.location_id=lb.location_id JOIN locations pk ON pk.location_id=lb.park_id
LEFT JOIN span sp ON sp.location_id=lb.location_id AND sp.pl=lb.pl
LEFT JOIN med md ON md.location_id=lb.location_id AND md.pl=lb.pl AND lb.cat='individual_animal'
ORDER BY 1,2,3;
```
- Traps: whole-pen gain = mean movement, scanned = median of pairs (two statistics on one axis). A pen with one weigh day is absent from the gain chart (not 0). The table's kid counts include single-weigh kids, so its sum > KPI "Kids weighed".
- CEO: "Which pens were weighed and what do they weigh?" / "Kaunse pen tole gaye, unka average kitna hai?"; "Which pen is gaining slowest?" / "Kaunsa pen sabse dheere badh raha hai?"

## W15 Composition chips + Breed column (shed_composition)
- Endpoint: `GET /weighing/weight-demographics sections=composition` (handler.go:270 -> weight_demographics.go:870-1000 `pen_scan/pen_lump_day/pen_latest_scan/individual_composition/lump_composition`) shed_composition[].chips{breed,sex,stage,animals}, source.
- UI: weights.tsx table chips (shedCohorts), weights-analytics.tsx:703 Breed column (one named breed -> name, else "Mixed", none -> "Unknown").
- Formula: scanned pen = tags weighed on the pen's LATEST scan day in window (unless a later whole-pen weigh exists), goat breed/sex/stage via newest goat_identifiers row; whole pen = LIVE residents of the pen (pen, else parent shed + scrubbed "Part N"). Under a Sex filter scanned tags must be that sex and whole pens all-that-sex.
- SQL: prefix = references/adg-by-breed.sql from `WITH w AS (` through `lump_span` (params from_date/to_date/sex as needed), then:
```sql
pen_scan AS (SELECT s.location_id, s.partition_label, COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))) tag,
    (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, o.accepted_at, o.observation_id, g.breed, g.sex, g.management_stage
  FROM weighing_observations o JOIN scoped s USING (campaign_shed_id) LEFT JOIN akmap ak ON ak.tag=lower(btrim(o.scanned_identifier))
  LEFT JOIN ident i ON i.tag=COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))) LEFT JOIN goats g ON g.goat_id=i.goat_id, b
  WHERE o.accepted_at>=b.s AND o.accepted_at<b.e AND o.verification_status<>'rejected' AND btrim(o.scanned_identifier)<>'' AND b.cat IN ('','individual_animal')
    AND (b.sexf='' OR lower(btrim(g.sex))=b.sexf)),
pen_lump_day AS (SELECT location_id, partition_label, max(d) d FROM pen_obs po, b WHERE b.sexf='' OR EXISTS (SELECT 1 FROM shed_cohort sc
  WHERE sc.location_id=po.location_id AND sc.partition_label=po.partition_label AND sc.sexes=1 AND lower(btrim(sc.sex))=b.sexf) GROUP BY 1,2),
pls AS (SELECT DISTINCT ON (ps.location_id, ps.partition_label, ps.tag) ps.* FROM pen_scan ps
  JOIN (SELECT location_id, partition_label, max(d) d FROM pen_scan GROUP BY 1,2) pd USING (location_id, partition_label, d)
  LEFT JOIN pen_lump_day pld ON pld.location_id=ps.location_id AND pld.partition_label=ps.partition_label
  WHERE pld.d IS NULL OR ps.d>pld.d ORDER BY ps.location_id, ps.partition_label, ps.tag, ps.accepted_at DESC, ps.observation_id DESC),
chips AS (
  SELECT location_id, partition_label, 'scanned' src, COALESCE(NULLIF(breed,''),'Unknown breed') breed, COALESCE(NULLIF(sex,''),'unknown sex') sex, COALESCE(NULLIF(management_stage,''),'Unknown stage') stage, count(*) n
  FROM pls GROUP BY 1,2,3,4,5,6
  UNION ALL
  SELECT st.location_id, st.partition_label, 'live_cohort', COALESCE(NULLIF(g.breed,''),'Unknown breed'), COALESCE(NULLIF(g.sex,''),'unknown sex'), COALESCE(NULLIF(g.management_stage,''),'Unknown stage'), count(*)
  FROM shed_targets st JOIN (SELECT DISTINCT location_id, partition_label FROM lump) l USING (location_id, partition_label)
  JOIN goats g ON g.shed_id=st.resolved_id AND g.lifecycle_status='alive' LEFT JOIN goat_shed_partitions gsp ON gsp.goat_id=g.goat_id
  WHERE (st.resolved_partition_label='' OR regexp_replace(lower(btrim(gsp.partition_label)),'^(part|pt)[\s.-]*','')=regexp_replace(lower(btrim(st.resolved_partition_label)),'^(part|pt)[\s.-]*',''))
    AND NOT EXISTS (SELECT 1 FROM pls WHERE pls.location_id=st.location_id AND pls.partition_label=st.partition_label)
  GROUP BY 1,2,3,4,5,6)
SELECT pk.location_code park, l.name pen, c.partition_label part, c.src,
  CASE WHEN count(DISTINCT c.breed) FILTER (WHERE c.breed<>'Unknown breed')=1 AND bool_and(c.breed<>'Unknown breed') THEN min(c.breed)
       WHEN count(*) FILTER (WHERE c.breed<>'Unknown breed')=0 THEN 'Unknown' ELSE 'Mixed' END breed_column,
  string_agg(c.breed||' · '||c.sex||' · '||c.stage||' · '||c.n, ' | ' ORDER BY c.n DESC) chips
FROM chips c JOIN locations l ON l.location_id=c.location_id JOIN locations pk ON pk.location_id=l.parent_location_id GROUP BY 1,2,3,4 ORDER BY 1,2,3;
```
- Verified 2026-09-25 (male, 2026-08-03..09-22): 31 pens (= ADG Analytics pens table rows); CBE Castro 1 live cohort Anantapur Sheep · male · F2-Male · 49 (table shows 54 weighed); CBE Yashoda 4 Mixed (Beetal 5, Sojat 4, Beetal x Sojat 2, Boer x Beetal 1); CPT Mandela 1 - Part 3 Anantapur Sheep 10; CPT Yashoda 2 Mixed (Beetal K3 7, Anantapur Sheep K3 3).
- Trap: a whole pen's chips are TODAY's live residents, not the animals weighed (Castro 1: 49 vs 54).
- CEO: "What breed is in Castro 1?" / "Castro 1 mein kaunsi nasl hai?"

## W16 Sex and Stage charts (W8 with SQL; Breed chart = references/adg-by-breed.sql)
- Endpoint: weight-demographics sections=dimensions -> by_sex / by_stage (average_weight_kg, animals) and gain_by_sex / gain_by_stage (median_gain_g_per_day = weighted MEAN), weight_demographics.go:1040-1130; domain weight_demographics.go:280-292.
- UI: weights.tsx:984 (sex) / :1004 (stage) MetricChart, weight toFixed(1), gain Math.round; toggle weight|gain (URL metric params).
- Formula: weight = scanned animals' latest weigh (single weigh enough) + pens' latest whole-pen weigh (avg × head count) claimed only when the live cohort is single-sex (sex bar) / single-stage and, under a Sex filter, single-sex of that sex (stage bar); bar = Σkg/Σheads. Gain = per-animal Σgain/Σdays after keeping the LAST weigh per IST day + pens weighed on >=2 days (span × heads), same claim rules. Animals with no goat record have no sex/stage and drop out.
- SQL: prefix = adg-by-breed.sql `WITH w AS (` .. `lump_span`, with three edits: `resolved` and `resolved_gain` also select `g.sex, g.management_stage` (gt.* in resolved_gain); `shed_cohort` also selects `min(g.management_stage) AS stage, count(DISTINCT g.management_stage) AS stages`. Then:
```sql
dim AS (
  SELECT 'sex' dim, sex val, count(*)::float8 wn, sum(weight_kg)::float8 wt, 0::float8 gn, 0::float8 gs FROM resolved WHERE sex IS NOT NULL GROUP BY sex
  UNION ALL SELECT 'sex', sc.sex, sum(l.animal_count), sum(l.animal_count*l.average_weight_kg), 0, 0 FROM lump l JOIN shed_cohort sc USING (location_id, partition_label), b
    WHERE sc.sexes=1 AND (b.sexf='' OR lower(btrim(sc.sex))=b.sexf) GROUP BY sc.sex
  UNION ALL SELECT 'sex', sex, 0, 0, count(*), sum(g) FROM resolved_gain WHERE sex IS NOT NULL GROUP BY sex
  UNION ALL SELECT 'sex', sc.sex, 0, 0, sum(ls.animals), sum(ls.animals*ls.g_per_day) FROM lump_span ls JOIN shed_cohort sc USING (location_id, partition_label), b
    WHERE sc.sexes=1 AND (b.sexf='' OR lower(btrim(sc.sex))=b.sexf) GROUP BY sc.sex
  UNION ALL SELECT 'stage', management_stage, count(*), sum(weight_kg), 0, 0 FROM resolved WHERE management_stage IS NOT NULL GROUP BY 2
  UNION ALL SELECT 'stage', sc.stage, sum(l.animal_count), sum(l.animal_count*l.average_weight_kg), 0, 0 FROM lump l JOIN shed_cohort sc USING (location_id, partition_label), b
    WHERE sc.stages=1 AND (b.sexf='' OR (sc.sexes=1 AND lower(btrim(sc.sex))=b.sexf)) GROUP BY sc.stage
  UNION ALL SELECT 'stage', management_stage, 0, 0, count(*), sum(g) FROM resolved_gain WHERE management_stage IS NOT NULL GROUP BY 2
  UNION ALL SELECT 'stage', sc.stage, 0, 0, sum(ls.animals), sum(ls.animals*ls.g_per_day) FROM lump_span ls JOIN shed_cohort sc USING (location_id, partition_label), b
    WHERE sc.stages=1 AND (b.sexf='' OR (sc.sexes=1 AND lower(btrim(sc.sex))=b.sexf)) GROUP BY sc.stage)
SELECT dim, val, sum(wn) weight_animals, round((sum(wt)/nullif(sum(wn),0))::numeric,1) avg_kg, sum(gn) gain_animals, round((sum(gs)/nullif(sum(gn),0))::numeric) gain_g
FROM dim GROUP BY 1,2 ORDER BY 1, 3 DESC;
```
- Verified 2026-09-25 (male, 2026-08-03..09-22, all parks): sex male 30.4 kg (582) / 161 g (520 — equals the headline gain card); stage F2-Male 32.0 kg (511) / 165 g (508), Warmup 19.5 kg (58) / −87 g (10), K3 16.1 kg (12) / 175 g (2), ICU-Kid 18.0 kg (1) / no gain.
- Traps: weight and gain are different populations; under the default male filter the sex chart is one bar; use sex=all for male vs female.
- CEO: "Male vs female weight and gain?" / "Nar aur mada ka wajan aur gain?"; "Which stage grows best?" / "Kis stage mein sabse achha gain?"

## W17 Gain-threshold table / bars by breed (W9 with SQL)
- Endpoint: weight-demographics sections=gain_thresholds -> gain_thresholds[] {breed, n, b180, b1820, b2025, a250} (weight_demographics.go:280-305 and :1432). Table contract `gain-thresholds` (adminui service.go weightsGainThresholdTable: breed, gain_animals, above_250, band_200_250, band_180_200, upto_180); UI weights.tsx:1059 table/chart toggle + gain-threshold-bars.tsx (% of breed, sharesOfWhole sums to 100).
- Formula: each animal counted ONCE in its band by its demographics gain (last weigh per IST day pairs); bands: >250, 200–250 (>200, ≤250), 180–200 (>180, ≤200), ≤180 g/day. Whole pens only if single-breed (+ single-sex under Sex filter), ALL heads in the band of the pen's span gain.
- SQL: adg-by-breed.sql prefix through `lump_span` + `claim`, then:
```sql
t AS (SELECT breed, count(*) n, count(*) FILTER (WHERE g<=180) b180, count(*) FILTER (WHERE g>180 AND g<=200) b1820,
         count(*) FILTER (WHERE g>200 AND g<=250) b2025, count(*) FILTER (WHERE g>250) a250 FROM resolved_gain WHERE breed IS NOT NULL GROUP BY breed
  UNION ALL
  SELECT c.breed, sum(ls.animals), coalesce(sum(ls.animals) FILTER (WHERE ls.g_per_day<=180),0), coalesce(sum(ls.animals) FILTER (WHERE ls.g_per_day>180 AND ls.g_per_day<=200),0),
         coalesce(sum(ls.animals) FILTER (WHERE ls.g_per_day>200 AND ls.g_per_day<=250),0), coalesce(sum(ls.animals) FILTER (WHERE ls.g_per_day>250),0)
  FROM lump_span ls JOIN claim c USING (location_id, partition_label) GROUP BY c.breed)
SELECT breed, sum(n) animals, sum(a250) over_250, sum(b2025) g200_250, sum(b1820) g180_200, sum(b180) le_180 FROM t GROUP BY breed ORDER BY 2 DESC, 1;
```
- Verified 2026-09-25 (male, 2026-08-03..09-22): Anantapur Sheep 425 = 14 / 25 / 54 / 332; Beetal 38 = 1 / 1 / 4 / 32; Sojat 22 = 0 / 4 / 1 / 17; Malai 11 = 1 / 0 / 0 / 10; Osmanabadi 7 all ≤180; Beetal x Sojat 6 = 0/1/1/4; Boer x Beetal 1 in 200–250; Boer x Malai 1 in 180–200.
- Trap: a whole pen is all-or-nothing in one band (e.g. 38 sheep of CPT Godel 2 Part 1 at 179 g all land in ≤180).
- CEO: "How many kids gain over 250 g a day?" / "250 gram se zyada roz badhne wale kitne hain?"

## W18 Load chart + "Where each load sits" table (W10 with SQL)
- Endpoint: shed-weights include_loads=true -> by_load[] {load_ref, owner_name, sheds, animals, average_weight_kg, average_gain_g_per_day, span_days, placements[]} + load_unattributed_sheds (load_weights.go:40-245). UI weights.tsx:1126 chart (gain view drops loads with no second weigh; label carries span days), :1147 note "N pens not mapped to a load", :1160 table (load, park, sheds, animals).
- Formula: per pen, daily point = head-weighted avg of whole-pen weighs + scanned animals (last weigh per animal per IST day); pen latest = last day (must be ≥ from); pen gain = (latest − first daily avg)·1000/days. Load = pens in weighing_shed_load_tags (pens tagged to 2+ loads dropped); avg = Σ(avg·n)/Σn; gain = same weighting over pens with a gain. Ordered by gain desc.
```sql
WITH w AS (SELECT ('2026-08-03'::date::timestamp AT TIME ZONE 'Asia/Kolkata') s, ('2026-09-25'::date::timestamp AT TIME ZONE 'Asia/Kolkata') e, '2026-08-03'::date fd),
idm AS (SELECT tag, first_value(tag) OVER (PARTITION BY goat_id ORDER BY (identifier_type='animal_identifier_1') DESC, tag) ct
  FROM (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id, identifier_type FROM goat_identifiers
        WHERE status='active' AND identifier_type IN ('animal_identifier_1','animal_identifier_2') AND btrim(identifier_value)<>'' ORDER BY 1, created_at DESC) i),
scoped AS (SELECT cs.campaign_shed_id, cs.location_id, c.park_id, coalesce(cs.partition_label,'') pl, cs.weighing_category cat
  FROM weighing_campaign_sheds cs JOIN weighing_campaigns c USING (campaign_id) WHERE cs.status<>'canceled'),
lump_daily AS (SELECT s.location_id, s.pl, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, o.average_weight_kg a, o.animal_count n
  FROM scoped s JOIN weighing_shed_observations o USING (campaign_shed_id), w
  WHERE s.cat='per_shed_partition' AND o.withdrawn_at IS NULL AND o.verification_status<>'rejected' AND o.accepted_at>=w.s AND o.accepted_at<w.e),
ind_daily AS (SELECT s.location_id, s.pl, x.d, avg(x.wt) a, count(*) n FROM scoped s JOIN (
    SELECT DISTINCT ON (o.campaign_shed_id, coalesce(nullif(coalesce(m.ct, lower(btrim(o.scanned_identifier))),''), o.observation_id::text), (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date)
      o.campaign_shed_id, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, o.weight_kg wt
    FROM weighing_observations o LEFT JOIN idm m ON m.tag=lower(btrim(o.scanned_identifier)), w
    WHERE o.accepted_at>=w.s AND o.accepted_at<w.e AND o.verification_status<>'rejected'
    ORDER BY o.campaign_shed_id, coalesce(nullif(coalesce(m.ct, lower(btrim(o.scanned_identifier))),''), o.observation_id::text), (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date, o.accepted_at DESC, o.observation_id DESC) x
  USING (campaign_shed_id) WHERE s.cat='individual_animal' GROUP BY 1,2,3),
daily AS (SELECT location_id, pl, d, sum(a*n)/nullif(sum(n),0) a, sum(n) n FROM (SELECT * FROM lump_daily UNION ALL SELECT * FROM ind_daily) u GROUP BY 1,2,3),
r AS (SELECT *, row_number() OVER (PARTITION BY location_id, pl ORDER BY d DESC) z, row_number() OVER (PARTITION BY location_id, pl ORDER BY d) f FROM daily),
sl AS (SELECT r.* FROM r, w WHERE z=1 AND d>=w.fd),
sg AS (SELECT l.location_id, l.pl, (l.a-f.a)*1000.0/(l.d-f.d) g, l.d-f.d span FROM r l JOIN r f ON f.location_id=l.location_id AND f.pl=l.pl AND f.f=1 WHERE l.z=1 AND l.d>f.d),
tag AS (SELECT location_id, min(load_ref) load_ref, min(owner_name) owner_name FROM weighing_shed_load_tags GROUP BY 1 HAVING count(*)=1)
SELECT t.load_ref, t.owner_name, count(*) pens, sum(sl.n) animals, round((sum(sl.a*sl.n)/sum(sl.n))::numeric,1) avg_kg,
  round((sum(sg.g*sl.n) FILTER (WHERE sg.g IS NOT NULL)/nullif(sum(sl.n) FILTER (WHERE sg.g IS NOT NULL),0))::numeric) gain_g, max(sg.span) span_days,
  string_agg(coalesce(pk.location_code,pk.name)||' '||sh.name||CASE WHEN sl.pl<>'' THEN ' '||sl.pl ELSE '' END||' · '||sl.n, ', ' ORDER BY pk.location_code, sh.name, sl.pl) placements,
  (SELECT count(*) FROM sl s2 WHERE NOT EXISTS (SELECT 1 FROM tag t2 WHERE t2.location_id=s2.location_id)) unattributed_pens
FROM tag t JOIN sl USING (location_id) LEFT JOIN sg USING (location_id, pl)
LEFT JOIN locations sh ON sh.location_id=t.location_id
LEFT JOIN LATERAL (SELECT min(park_id::text)::uuid park_id FROM scoped s2 WHERE s2.location_id=t.location_id) sp ON true LEFT JOIN locations pk ON pk.location_id=sp.park_id
GROUP BY t.load_ref, t.owner_name ORDER BY 6 DESC NULLS LAST, 1;
```
- Verified 2026-09-25 sex=all, all parks, 2026-08-03..09-24: 129 Krishnamorrthy 76 @ 33.2 kg, 181 g (CPT Godel 2 - Part 1 · 38, Part 2 · 38); 126 Ramesh Reddy 54 @ 39.1, 169 g; 130 Green Fresh Farm 73 @ 34.2, 167 g; 131 Krishnamorrthy 63 @ 31.4, 166 g; 128 Krishnamorrthy 59 @ 31.9, 162 g; 38 weighed pens not mapped to a single load. Sex filter: scanned tags restricted to that sex, pens to single-sex pens (W0).
- Trap: a load's figure is every animal standing in its pens, whichever load they came from.
- CEO: "Which supplier's load is growing best?" / "Kis vendor ka load sabse achha badh raha hai?"

## W19 Losing-kids table rows (W11 with SQL)
- Endpoint: leadership/growth sections=losing_animals -> losing_animals[] (growth.go:1361 growthLosingAnimals; contract losing-kids: tag, shed, previous, latest, change, days_apart, last_weighed). Paged client-side.
```sql
WITH w AS (SELECT ('2026-08-03'::date::timestamp AT TIME ZONE 'Asia/Kolkata') s, ('2026-09-25'::date::timestamp AT TIME ZONE 'Asia/Kolkata') e),
idm AS (SELECT tag, first_value(tag) OVER (PARTITION BY goat_id ORDER BY (identifier_type='animal_identifier_1') DESC, tag) ct
  FROM (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id, identifier_type FROM goat_identifiers
        WHERE status='active' AND identifier_type IN ('animal_identifier_1','animal_identifier_2') AND btrim(identifier_value)<>'' ORDER BY 1, created_at DESC) i),
b AS (SELECT o.observation_id, o.scanned_identifier, o.weight_kg::float8 wt, o.accepted_at, cs.display_name shed, coalesce(pk.location_code,pk.name) park,
    coalesce(m.ct, lower(btrim(o.scanned_identifier))) k
  FROM weighing_observations o JOIN weighing_campaign_sheds cs USING (campaign_shed_id) JOIN weighing_campaigns c ON c.campaign_id=cs.campaign_id
  JOIN locations pk ON pk.location_id=c.park_id LEFT JOIN idm m ON m.tag=lower(btrim(o.scanned_identifier)), w
  WHERE cs.status<>'canceled' AND o.verification_status<>'rejected' AND o.accepted_at>=w.s AND o.accepted_at<w.e),
p AS (SELECT *, lag(wt) OVER x pw, lag(accepted_at) OVER x pa FROM b WINDOW x AS (PARTITION BY k ORDER BY accepted_at, observation_id)),
q AS (SELECT *, (accepted_at AT TIME ZONE 'Asia/Kolkata')::date-(pa AT TIME ZONE 'Asia/Kolkata')::date dd FROM p WHERE pw IS NOT NULL),
lp AS (SELECT DISTINCT ON (k) * FROM q WHERE dd>0 ORDER BY k, accepted_at DESC, observation_id DESC)
SELECT scanned_identifier tag, park||' '||shed pen, round(pw::numeric,1) prev_kg, round(wt::numeric,1) latest_kg, round((wt-pw)::numeric,1) change_kg, dd days_apart,
  (accepted_at AT TIME ZONE 'Asia/Kolkata')::date last_weighed, round(((wt-pw)*1000/dd)::numeric) adg_g
FROM lp WHERE wt<pw ORDER BY (wt-pw)/dd, k LIMIT 200;
```
- Formula: each animal key's LATEST consecutive pair (IST-day gap > 0, both weighs in window, rejected dropped) with weight_kg < previous; order by pair ADG ascending; LIMIT 200. Sex filter = tag list (W0).
- Verified 2026-09-25: row query returns 53 rows (sex=all, 03/08–24/09), pair ADG −3100 to −13 g/day; matches the KPI count.
- CEO: "Which kids lost weight since last weighing?" / "Pichhli tol se kin bacchon ka wajan ghata?"

## W20 Period picker markers + landing end date (GET /weighing/weighing-dates)
- Endpoint: handler.go:216 -> app/service.go:2153 -> weighing_dates.go:20 lumpWeighingDatesQuery (lump_weighing_dates) and :54 latestWeighingDateQuery (latest_weighing_date). admin-web proxy app/api/weighing/lump-markers/route.ts (date-picker dots), landing-window.ts:86 (default "to").
- Formula: markers = distinct IST dates of live (non-withdrawn, non-rejected) whole-pen weighs in non-canceled per_shed_partition buckets in [from, to+1) (UI asks 400 days back). Latest = max IST date over non-rejected scans + live whole-pen weighs under the same park/sex/origin/weighing filters.
```sql
SELECT string_agg(DISTINCT to_char((sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date,'YYYY-MM-DD'), ',') lump_dates
FROM weighing_shed_observations sh JOIN weighing_campaign_sheds cs USING (campaign_shed_id)
WHERE cs.weighing_category='per_shed_partition' AND cs.status<>'canceled' AND sh.withdrawn_at IS NULL AND sh.verification_status<>'rejected'
  AND sh.accepted_at >= now()-interval '400 days';
SELECT max(d) FROM (SELECT (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d FROM weighing_observations o JOIN weighing_campaign_sheds cs USING (campaign_shed_id)
  WHERE cs.status<>'canceled' AND o.verification_status<>'rejected'
  UNION ALL SELECT (sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date FROM weighing_shed_observations sh JOIN weighing_campaign_sheds cs USING (campaign_shed_id)
  WHERE cs.status<>'canceled' AND sh.withdrawn_at IS NULL AND sh.verification_status<>'rejected') z;
```
- Verified 2026-09-25: markers 06/07, 13/07, 20/07, 27/07, 29/07, 03/08, 05/08, 10/08, 11/08, 17/08, 24/08, 31/08, 01/09, 04/09, 07/09, 14/09, 21/09, 22/09; latest (sex=all) 2026-09-23 (male-only 2026-09-22 — 09-23 had female weighs only).
- CEO: "When was the last weighing?" / "Aakhri tol kab hui?"

## W21 Export drawer (GET /weighing/export.csv)
- Endpoint: handler.go:95/:1156 ExportCSV (WeighingMonitor) -> app/service.go:2350 -> export.go:151. UI weights-export.tsx drawer (park, pen multi-select, period, sex) + weights-export-action.ts; the page's origin/weighing filters ride along.
- Columns: date, rfid, old_id (second tag), old_id_suffix (blank), breed, gender, park, shed (display + partition), type (individual|lumpsum), count (whole pen), operator (workforce_members.display_name of recorded_by), approval (verification status label), verified_weight_kg (weight_kg after corrections).
- Formula: every scan (INCLUDING rejected — no verification filter on scans) and every live whole-pen weigh (withdrawn_at NULL) in [from, to+1) IST; canceled buckets are NOT excluded; pen filter = cs.location_id; sex/origin via the same tag/pen scope. Ordered date desc, park, shed, partition, type, rfid. Raw rows, no dedup — counts will not match any tile.
- SQL (row count check; verified 2026-09-25, all parks/sexes 03/08–24/09: 2858 scans + 75 live pen weighs):
```sql
SELECT (SELECT count(*) FROM weighing_observations o WHERE o.accepted_at >= '2026-08-03 00:00+05:30' AND o.accepted_at < '2026-09-25 00:00+05:30') scans,
       (SELECT count(*) FROM weighing_shed_observations so WHERE so.withdrawn_at IS NULL AND so.accepted_at >= '2026-08-03 00:00+05:30' AND so.accepted_at < '2026-09-25 00:00+05:30') pen_weighs;
```
- CEO: "Give me all weighing rows for September" / "September ki saari tol ki list do" -> point to the export, do not dump rows.

## W22 Assumptions drawer (Weighing SOP page + Weights) and sale-price captions
- Endpoints: `GET/PUT /growth-director/assumptions` (growthdirector handler.go:54-55, :81/:89; defaults domain/assumptions.go:111-120), `GET /growth-director/sale-prices` (handler.go:75). UI weights-assumptions.tsx (drawer: sale lines, band edges, targets, sale price default + per stage × sex overrides, "set by"); mounted on /weighing/sops (app/(admin)/weighing/sops/page.tsx, control edit_assumptions) — the SOP page itself is a document (renderSopModulePage), no numbers.
- Values: `SELECT key, value, updated_at FROM growth_assumptions ORDER BY key;` and `SELECT * FROM growth_sale_price_assumptions ORDER BY effective_from DESC;` (stg: sale_ready_lower_kg 30, sale_ready_threshold_kg 35, weight_band_edges_kg {15,20,25,30,35}, bad_scan_loss_g_per_day 300, slow_growth_target_g_per_day 200, default_period_days 15; goat & sheep ₹425/kg from 2026-09-07, no overrides — see growth-director.md / fcr.md §6).
- Effect: sale lines feed Over-30/35 tiles (W4), band edges feed Weight-wise and FCR band charts, sale price feeds FCR gain value and Comparison stock value.
- CEO: "What sale price / sale weight are we assuming?" / "Bechne ka rate aur wajan kya maan ke chal rahe hain?"
