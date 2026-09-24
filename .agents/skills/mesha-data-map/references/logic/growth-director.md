# Growth Director + Sale readiness — screen logic

Index: [A] Road to sale (band board + 6 KPI tiles) · [B] Fair fight (median ADG by shed within breed×sex) · [C] Sale readiness "Over 30 / Over 35 kg" KPIs (Weights + ADG Analytics) · [D] Sales > Farm value "Over 35 kg" card · [E] legacy `/weighing/growth` sale_readiness (not rendered) · Assumptions · Traps

Screen: admin-web Weighing > Weights (`apps/admin-web/features/weighing/weights.tsx`) — KPI strip on top, Growth Director block below (`growth-director.tsx`). ADG Analytics (`weights-analytics.tsx`) repeats the same Over 30/35 KPIs.

## Shared inputs

- Filters (URL -> API query): `park_id`, `from`, `to` (IST business dates, inclusive; API turns them into `[from 00:00 IST, to+1 00:00 IST)`), `sex`, `origin`, `weighing_category` (`individual_animal` | `per_shed_partition` | empty = all).
- Default window on the Weights pages is `DEFAULT_WINDOW_FROM = 2026-08-03` (`apps/admin-web/features/weighing/landing-window-constants.ts:11`) -> today. If the API is called with no dates: Growth Director = last `default_period_days` (15) days (`backend/internal/growthdirector/app/service.go:231`); shed-weights = 15 days (`weighing/domain/shed_weights.go:328`).
- Assumptions (table `growth_assumptions`, one row per key, editable in the Assumptions drawer via `PUT /growth-director/assumptions`; defaults in `backend/internal/growthdirector/domain/assumptions.go:111-120`). Live stg values: `sale_ready_lower_kg`=30, `sale_ready_threshold_kg`=35, `weight_band_edges_kg`={15,20,25,30,35}, `bad_scan_loss_g_per_day`=300, `slow_growth_target_g_per_day`=200, `default_period_days`=15. Rule: lower < threshold (`service.go:212`).
- Same-animal key: two RFIDs of one goat collapse to one key = its `animal_identifier_1` tag (active identifiers only) — `backend/internal/weighing/adapters/postgres/identity_scope.go:180-222`. Unmapped tags key on `lower(btrim(scanned_identifier))`.
- Sex/Origin filter: tags via `sex_scope.go` / `origin_scope.go`; whole-shed pens are included only when their bucket is entirely that sex/origin (mixed pens drop out of both sides).

Reusable akmap CTE (used in every sketch below):
```sql
ident AS (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id, identifier_type
  FROM goat_identifiers WHERE status='active' AND identifier_type IN ('animal_identifier_1','animal_identifier_2') AND btrim(identifier_value)<>''
  ORDER BY lower(btrim(identifier_value)), created_at DESC),
akmap AS (SELECT tag, first_value(tag) OVER (PARTITION BY goat_id ORDER BY (identifier_type='animal_identifier_1') DESC, tag) canonical_tag FROM ident)
```

---

## [A] Road to sale weight

Endpoint: `GET /growth-director/weights?sections=road_to_sale,fair_fight&park_id&from&to&sex&origin&weighing_category` (handler `backend/internal/growthdirector/adapters/http/handler.go:44`). SQL: `roadToSale` in `backend/internal/growthdirector/adapters/postgres/growth_director.go:348-485` (CTEs `weighingObsCTE` :30, `roundLatestCTE` :78).

| Tile / list | Formula |
|---|---|
| Total animals (`total_animals`) | scanned kids (1 each) + whole-shed pens (their `animal_count`) at their latest round |
| Weighed as whole shed (`lump_sum_animals`) | sum of `animal_count` of each pen's latest whole-shed weigh |
| Had a previous weigh (`movement.pair_animals`) | animals whose key/pen has a 2nd-latest round |
| Moved up / Held / Slipped back | band(latest) >, =, < band(previous round); pens move all their head count together |
| Band bars `<15,15-20,20-25,25-30,30-35,35+` | `width_bucket(latest_weight, band_edges)`; a pen puts its whole head count in the band of its average |
| Note "unmatched" | `total_identities - matched_identities` (scanned tags with no `goat_identifiers` row) |

Rules:
- Window selects CAMPAIGNS, not weighs: `c.period_end_date >= from AND c.period_start_date < to+1`, `c.status<>'canceled'` (so weighs slightly outside the dates can count when their campaign week overlaps).
- One weigh per animal per campaign week (the newest); "latest" = latest campaign week, "previous" = the one before. Per-animal (scanned) or per-pen (location + partition).
- Scanned arm: blank tags dropped, `verification_status <> 'rework'` (pending AND rejected are still counted — only rework is dropped). Whole-shed arm: `withdrawn_at IS NULL`, not rework, `animal_count>0`, avg not null, bucket and campaign not canceled.
- Band edges come from the assumption `weight_band_edges_kg`; 35+ is the sale band.
- Integers; rendered with `en-IN` 1 dp max.

Verified 2026-09-24, all parks, 2026-08-03..2026-09-24, no filters: total 833 (450 scanned + 383 whole-shed); bands `<15` 35, `15-20` 141, `20-25` 102, `25-30` 97, `30-35` 350, `35+` 108; pairs 754 = up 67 + held 673 + down 14.

```sql
WITH <akmap>,
obs AS (SELECT o.observation_id, COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))) tag_key, o.weight_kg, o.accepted_at, o.campaign_id, c.period_start_date
  FROM weighing_observations o JOIN weighing_campaigns c ON c.campaign_id=o.campaign_id
  LEFT JOIN akmap ak ON ak.tag=lower(btrim(o.scanned_identifier))
  WHERE c.status<>'canceled' AND c.period_end_date>=:from AND c.period_start_date<:to_excl
    AND btrim(o.scanned_identifier)<>'' AND o.verification_status<>'rework'),
rl AS (SELECT DISTINCT ON (tag_key,campaign_id) * FROM obs ORDER BY tag_key,campaign_id,accepted_at DESC,observation_id DESC),
ranked AS (SELECT *, row_number() OVER (PARTITION BY tag_key ORDER BY period_start_date DESC,accepted_at DESC,observation_id DESC) rn FROM rl),
lump_round AS (SELECT DISTINCT ON (cs.location_id, COALESCE(cs.partition_label,''), c.campaign_id)
    cs.location_id, COALESCE(cs.partition_label,'') part, c.period_start_date, so.average_weight_kg, so.animal_count, so.accepted_at, so.shed_observation_id
  FROM weighing_shed_observations so JOIN weighing_campaign_sheds cs ON cs.campaign_shed_id=so.campaign_shed_id JOIN weighing_campaigns c ON c.campaign_id=cs.campaign_id
  WHERE c.status<>'canceled' AND cs.status<>'canceled' AND c.period_end_date>=:from AND c.period_start_date<:to_excl
    AND so.withdrawn_at IS NULL AND so.verification_status<>'rework' AND so.animal_count>0 AND so.average_weight_kg IS NOT NULL
  ORDER BY 1,2,c.campaign_id, so.accepted_at DESC, so.shed_observation_id DESC),
lr AS (SELECT *, row_number() OVER (PARTITION BY location_id,part ORDER BY period_start_date DESC,accepted_at DESC,shed_observation_id DESC) rn FROM lump_round),
e AS (SELECT ARRAY[15,20,25,30,35]::numeric[] a),   -- from growth_assumptions.weight_band_edges_kg
scored AS (
  SELECT width_bucket(l.weight_kg,e.a) b, width_bucket(p.weight_kg,e.a) pb, 1 animals, true scanned
    FROM ranked l CROSS JOIN e LEFT JOIN ranked p ON p.tag_key=l.tag_key AND p.rn=2 WHERE l.rn=1
  UNION ALL
  SELECT width_bucket(l.average_weight_kg,e.a), width_bucket(p.average_weight_kg,e.a), l.animal_count, false
    FROM lr l CROSS JOIN e LEFT JOIN lr p ON p.location_id=l.location_id AND p.part=l.part AND p.rn=2 WHERE l.rn=1)
SELECT b, sum(animals) animals, sum(animals) FILTER (WHERE NOT scanned) lump,
  sum(animals) FILTER (WHERE pb IS NOT NULL) pairs, sum(animals) FILTER (WHERE b>pb) up,
  sum(animals) FILTER (WHERE b=pb) held, sum(animals) FILTER (WHERE b<pb) down
FROM scored GROUP BY ROLLUP(b) ORDER BY b;   -- b: 0='<15' .. 5='35+'
```
Park filter: add `AND c.park_id = :park` in both arms.

CEO asks: "How many kids are in the 35+ band?" / "35 kilo se upar kitne bacche hain band board pe?" · "How many moved up a band since last weigh?" / "Pichle weigh se kitne upar band mein gaye, kitne neeche gire?" · "Weight distribution of the herd?" / "Herd ka weight band-wise kaisa hai?"

---

## [B] Fair fight (median daily gain by shed, same breed + sex)

Endpoint: same call, `fair_fight` section. SQL: `fairFight` in `backend/internal/growthdirector/adapters/postgres/growth_cohorts.go:21-94` (+ `firstLastPairCTE` / `breedSexJoin` in `growth_director.go:90,115`).

- Per animal (scanned tags only; whole-shed pens never appear): FIRST vs LAST weekly round inside the window (same campaign window + rework rule as [A]), needs >=2 rounds and last date > first date.
- ADG g/day = `(w_last - w_first) * 1000 / (t_last::date - t_first::date)` (dates in UTC `::date` of accepted_at — not IST).
- Drops implausible loss: ADG <= -`bad_scan_loss_g_per_day` (300).
- Breed/sex from `goats` via `goat_identifiers.normalized_value = upper(tag_key)`, merged goat's canonical row wins; blank breed -> `(unknown)`. Untagged/unmatched kids drop out.
- Shed = the animal's LATEST shed (location + partition). Row = median (`percentile_cont(0.5)`) ADG per breed×sex×shed, needs >=3 kids; a cohort shows only if >=2 sheds qualify.
- Screen: rows listed alphabetically, but Leader/Behind chip and "spread" (best median - worst median) use the backend's median ranking. Bar = share of the cohort leader. `nf` rounds to 1 dp.

Verified (same window, no filters): 45 shed rows across 9 breed×sex cohorts. E.g. Anantapur Sheep·male leader Godel 2 - Part 5 / Coimbatore 260.9 g (9 kids), last Mandela 2 - Part 5 / Channapatna 127.6 g (14) -> spread 133.3 g; Beetal·male leader Yashoda 4 144.9 g (5).

```sql
WITH <akmap>, obs AS (... as [A] plus cs.location_id shed_id, cs.display_name shed, COALESCE(cs.partition_label,'') part ...), rl AS (...),
pairs AS (SELECT tag_key,
   (array_agg(weight_kg   ORDER BY period_start_date,accepted_at,observation_id))[1] w_first,
   (array_agg(accepted_at ORDER BY period_start_date,accepted_at,observation_id))[1] t_first,
   (array_agg(weight_kg   ORDER BY period_start_date DESC,accepted_at DESC,observation_id DESC))[1] w_last,
   (array_agg(accepted_at ORDER BY period_start_date DESC,accepted_at DESC,observation_id DESC))[1] t_last,
   (array_agg(shed_id     ORDER BY period_start_date DESC,accepted_at DESC,observation_id DESC))[1] shed_id,
   (array_agg(part        ORDER BY period_start_date DESC,accepted_at DESC,observation_id DESC))[1] part
  FROM rl GROUP BY tag_key HAVING count(*)>=2),
adg AS (SELECT *, (w_last-w_first)*1000.0/(t_last::date-t_first::date) g FROM pairs WHERE t_last::date>t_first::date),
co AS (SELECT a.*, COALESCE(NULLIF(btrim(COALESCE(canon.breed,g.breed)),''),'(unknown)') breed, COALESCE(canon.sex,g.sex) sex
  FROM adg a JOIN goat_identifiers gi ON gi.normalized_value=upper(a.tag_key) JOIN goats g ON g.goat_id=gi.goat_id
  LEFT JOIN goats canon ON canon.goat_id=g.merged_into_goat_id WHERE a.g > -300),
sc AS (SELECT breed,sex,shed_id,part,count(*) n, percentile_cont(0.5) WITHIN GROUP (ORDER BY g) med
  FROM co WHERE shed_id IS NOT NULL GROUP BY 1,2,3,4 HAVING count(*)>=3)
SELECT * FROM (SELECT *, count(*) OVER (PARTITION BY breed,sex) k FROM sc) x WHERE k>=2 ORDER BY breed,sex,med DESC;
```

CEO asks: "Which shed grows Beetal males fastest?" / "Beetal male sabse tez kis shed mein badh rahe hain?" · "Which pen is lagging for its breed?" / "Apni breed ke hisaab se kaunsa pen peeche hai?" · "Gap between best and worst shed?" / "Best aur worst shed mein kitna farak hai?"
For breed-level ADG (not per shed) use `references/adg-by-breed.sql` (different screen, different formula).

---

## [C] Sale readiness — "Over 30 kg" / "Over 35 kg" KPI tiles

Where shown: Weights KPI strip (`weights.tsx:911-927`) and ADG Analytics KPI strip (`weights-analytics.tsx:782-790`). Labels filled from `sale_ready_lower_kg` / `sale_ready_threshold_kg`; the page passes them as `sale_lower_kg` / `sale_threshold_kg` (page errors out if the assumptions read fails).
Endpoint: `GET /weighing/shed-weights?park_id&from&to&sex&origin&weighing_category&sale_lower_kg&sale_threshold_kg[&sale_threshold_tolerance_g]`. SQL: `backend/internal/weighing/adapters/postgres/shed_weights.go:180-480` (`summary_individual`, `summary_lump`, `summary_rollup`); fields `summary.at_or_above_30kg`, `at_or_above_35kg`, `threshold_basis_animals` (sub-line "of N kids").

- Denominator (`threshold_basis_animals` = `animals_weighed`): scanned kids with a weigh in the window that has an EARLIER weigh on a strictly earlier IST date also in the window (lookback = 0 days), + whole-shed pens weighed on >=2 IST dates in the window (latest head count).
- Individual: each animal's latest qualifying weigh (same-animal key), `>= lower` / `>= threshold`. Only `individual_animal` buckets; `verification_status <> 'rejected'` (pending counted; rework counted — differs from Growth Director).
- Whole-shed: pen's latest live (`withdrawn_at IS NULL`, not rejected) average; the WHOLE head count counts if the average clears the line, none if not.
- Window by `accepted_at` in IST (not campaign week). Buckets `status<>'canceled'`.
- Thresholds inclusive (`>=`). `sale_threshold_tolerance_g` (Farm value only) lowers the upper line by g/1000 and clamps `from` to >= 2026-08-03 (`weighing/app/service.go:2088,2138`).

Verified (all parks, 2026-08-03..2026-09-24, no filters): basis 754 (419 individual + 335 whole-shed), Over 30 = 458, Over 35 = 108.

```sql
WITH w AS (SELECT :from::timestamp AT TIME ZONE 'Asia/Kolkata' s, (:to + 1)::timestamp AT TIME ZONE 'Asia/Kolkata' e), <akmap>,
scoped AS (SELECT * FROM weighing_campaign_sheds WHERE status<>'canceled'),
iobs AS (SELECT o.observation_id, o.weight_kg::float8 kg, o.accepted_at, COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))) k,
   LAG(o.accepted_at) OVER (PARTITION BY COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))) ORDER BY o.accepted_at,o.observation_id) prev_at
  FROM scoped s JOIN weighing_observations o ON o.campaign_shed_id=s.campaign_shed_id CROSS JOIN w
  LEFT JOIN akmap ak ON ak.tag=lower(btrim(o.scanned_identifier))
  WHERE s.weighing_category='individual_animal' AND o.accepted_at>=w.s AND o.accepted_at<w.e AND o.verification_status<>'rejected'),
ind AS (SELECT DISTINCT ON (k) k, kg FROM iobs
  WHERE prev_at IS NOT NULL AND (accepted_at AT TIME ZONE 'Asia/Kolkata')::date > (prev_at AT TIME ZONE 'Asia/Kolkata')::date
  ORDER BY k, accepted_at DESC, observation_id DESC),
lp AS (SELECT s.location_id, COALESCE(s.partition_label,'') p, sh.shed_observation_id id, sh.animal_count n, sh.average_weight_kg avg, sh.accepted_at at
  FROM scoped s JOIN weighing_shed_observations sh ON sh.campaign_shed_id=s.campaign_shed_id CROSS JOIN w
  WHERE s.weighing_category='per_shed_partition' AND sh.withdrawn_at IS NULL AND sh.verification_status<>'rejected' AND sh.accepted_at>=w.s AND sh.accepted_at<w.e),
ll AS (SELECT DISTINCT ON (location_id,p) * FROM lp ORDER BY location_id,p,at DESC,id DESC),
lf AS (SELECT DISTINCT ON (location_id,p) * FROM lp ORDER BY location_id,p,at,id),
lump AS (SELECT ll.* FROM ll JOIN lf USING (location_id,p) WHERE (ll.at AT TIME ZONE 'Asia/Kolkata')::date > (lf.at AT TIME ZONE 'Asia/Kolkata')::date)
SELECT (SELECT count(*) FROM ind)+(SELECT coalesce(sum(n),0) FROM lump) basis,
  (SELECT count(*) FILTER (WHERE kg>=30) FROM ind)+(SELECT coalesce(sum(n) FILTER (WHERE avg>=30),0) FROM lump) over30,
  (SELECT count(*) FILTER (WHERE kg>=35) FROM ind)+(SELECT coalesce(sum(n) FILTER (WHERE avg>=35),0) FROM lump) over35;
```
Park filter: join `weighing_campaigns c ON c.campaign_id=scoped.campaign_id AND c.park_id=:park`.

CEO asks: "How many goats are ready for sale?" / "Kitne bakre bechne layak (35 kilo+) hain?" · "How many are over 30 kg?" / "30 kilo se upar kitne hain?" · "Sale-ready males in Channapatna?" / "Channapatna mein kitne male sale ke liye ready hain?"

---

## [D] Sales > Farm value "Over 35 kg" card

`apps/admin-web/features/procurement/sales-farm-value.tsx:200-245`: same `GET /weighing/shed-weights` as [C] but window = last 42 IST days (`OVER35_WINDOW_DAYS`, from = today-42 .. today), optional `sale_ready_tolerance_g` (0-1000 g) lowers the 35 line, farm tab -> `park_id`. Shows `summary.at_or_above_35kg`. Only when the card control is enabled.
Verified (today 2026-09-24, from 2026-08-13, tolerance 0, all farms): Over 35 = 98 of basis 735. Sketch = [C] with `:from = today-42`.

CEO asks: "Farm pe kitne 35+ kilo ke bakre hain jo abhi bik sakte hain?" / "How many sellable 35 kg+ animals on the farm now?"

---

## [E] Legacy `GET /weighing/growth` section `sale_readiness` (not rendered)

`backend/internal/weighing/adapters/postgres/growth.go:1202-1266`: latest-EVER individual weigh per same-animal key (ignores dates; not rejected; individual only; whole-shed excluded), hard-coded `>=30` / `>=35` (not the assumptions). No admin screen requests this section today (Weights asks `headline,shed_leaderboard,losing_animals`). Stg value: 450 animals, 133 >=30, 54 >=35. Do not quote it as the dashboard number.

---

## Traps

- Three different "ready for sale" numbers: band board 35+ (108, campaign-week window, all latest animals, rework dropped), KPI Over 35 (108 here but a different population: only animals/pens with >=2 weigh dates in window, rejected dropped), legacy latest-ever (54, individual only). Quote [C] for "sale ready" unless the user points at the band board.
- Whole-shed pens count all-or-nothing at the pen average in both [A] and [C]; a 34.9 kg-average pen of 50 adds 0 to Over 35.
- Growth Director window selects campaigns by period overlap, not `accepted_at`; shed-weights uses IST `accepted_at`. Edges can differ by a few weighs.
- Fair fight ADG uses UTC `::date` day counts; breed/sex join on `goat_identifiers.normalized_value` has no `status='active'` filter (a retired duplicate identifier can fan a kid into two rows).
- Fair fight excludes whole-shed pens and cohorts with <2 qualifying sheds; missing breeds are not "zero growth".
- Thresholds/bands are tenant settings; read `growth_assumptions` (`value`, or `value_list` for band edges) before hard-coding 30/35.
- Sex/Origin filtered whole-shed pens appear only when the pen is 100% that sex/origin.
