# FCR tab (Weighing > ADG Analytics > FCR)

Index: page scope · base SQL · 1 Farm FCR · 2 Gain value · 3 Feed spent + cost/kg gain · 4 Margin · 5 Break-even FCR · 6 Sale-price caption · 7 FCR by pen · 8 Money by pen · 9 Breed (estimated) · 10 Sex · 11 Weekly · 12 Band · 13 Park · 14 Origin · 15 Pens table · 16 Pen statuses / unmatched · cost-per-kg-gain.sql gap

## Page scope (applies to every metric)
- Endpoint: `GET /growth-director/fcr?park_id&from&to&sex&origin&weighing_category` (one read carries every figure; UI divides nothing).
  Route handler.go:50, `GetFCR` handler.go:60 -> app/service.go:138 (validates sex male|female, origin farm_born|procured_no_load|procured_load with retired purchased mapped to procured_load, category individual_animal|per_shed_partition, `all`->'') -> postgres/fcr.go:390 `GetFCR` -> domain/fcr.go:336 `BuildFCRReport`.
  UI: features/weighing/weights-analytics.tsx:358 `getWeighingFCR({...scope, ...readWindow})`; fcr-tab.tsx, fcr-pens-table.tsx.
- Window: landing-window.ts:51 — explicit `wt_from`/`wt_to` (clamped to today) else default_from (landing-window-constants.ts; DB `weighing_calendar_config`: stg = fixed_date 2026-08-03) through `latest_weighing_date` (stg 2026-09-23). Backend window = [from 00:00 IST, to+1 00:00 IST) on `accepted_at`; feed_day in [from, to+1) (service.go:231 resolveWindow).
- Sex: default **male** (weights-analytics.tsx:195); `sex=all` -> '' (both). Origin: farm_born|procured_no_load|procured_load|'' . Both are PEN-grain agree-or-neither filters (domain/fcr.go:562): a pen passes only if its cohort sex/origin equals the filter exactly — mixed pens drop out of any sex/origin filter.
- Cohort = live residents in the pen (goats alive, shed_id + scrubbed goat_shed_partitions label) — fallback to animals scanned in the pen in-window when the pen has no live residents (fcr.go:207-240, domain :517).
- Feed = **eaten sheet kg, as-fed**: directed sheet kg (feed_direction_issue_rows.quantity_kg, issues state issued|amended|locked) minus verifier-approved wastage for those fed days, floored at 0 per segment. NOT dry matter, NOT measured intake. NULL quantity = blocked cell, never 0.
- Gain = segment ADG x fed head-days/1000. Segment = two consecutive rounds of one pen (ordered period_start_date, d). Whole-pen arm: Δ pen average_weight_kg / days (latest non-withdrawn, non-rejected shed observation per pen per campaign). Scanned arm: mean per-animal daily gain over animals weighed in BOTH rounds (identity merge through ResolveAnimalIdentityMap: double-tagged animals keyed by canonical tag, identity_scope.go:146). Rejected scans dropped. Head-days = Σ over feed days in [d_prev, d) of max(head_count) per (pen, day, shed_tag_key, breed_key).
- Pen join weighing<->feed sheet: bucket -> (physical shed id, scrubbed partition) via pen_map (fcr.go:79); feed side uses generated partition_key ('whole'->'', 'part 3'->'3'). Not by name.
- A segment counts only when feed_kg not null AND head_days>0 (domain :632). Losing segments (ADG<0) ARE netted in; pen needs Σgain>0 for a ratio.
- FEED WASTAGE (2026-09-26): segment feed_kg = directed kg minus the verifier-APPROVED leftover (feed_wastage_completions
  status 'completed', ANY workflow) on the segment's days the sheet fed with a known quantity. Cost is NOT reduced,
  so feed_cost_per_kg_inr (and break-even) are per kg EATEN. The pen and summary carry wastage_kg; unpriced kg is prorated
  onto the eaten basis the same way priced feed is.
- Feed price: latest same-park feed_purchases row on/before feed_day (per_kg_cost else total_cost/quantity_kg); unpriced kg counted separately, not free.
- Sale price: growth_sale_price_assumptions effective TODAY (fcr.go:416); per (species, stage, sex) override else species default; pen price = head-weighted over cohort mix. stg: goat 425, sheep 425 ₹/kg, no overrides.

## Base SQL (fcr.go fcrScopeCTEs + fcrPensSQL + fcrSegmentsSQL + domain roll-up, read-only)
psql vars: `-v from=2026-08-03 -v to=2026-09-23 -v cat= -v sex=male -v origin=` (to inclusive; sex ''=all). Sale price hard-coded 425 (stg defaults; re-check growth_sale_price_assumptions). `fpen` = one row per pen after filters, exactly the tab's pen list.
```sql
WITH prm AS (SELECT '00000000-0000-4000-8000-000000000001'::uuid t,
  ARRAY['00000000-0000-4000-8000-000000003001','00000000-0000-4000-8000-000000003002']::uuid[] parks,
  :'from'::date fd, (:'to'::date + 1) td, :'cat'::text cat, :'sex'::text sx,
  CASE WHEN :'origin'::text='purchased' THEN 'procured_load' ELSE :'origin'::text END org),
idm AS (
  SELECT tag, canonical_tag FROM (
    SELECT i.tag, first_value(i.tag) OVER (PARTITION BY i.goat_id ORDER BY (i.identifier_type='animal_identifier_1') DESC, i.tag) canonical_tag,
           count(*) OVER (PARTITION BY i.goat_id) n
    FROM (SELECT DISTINCT ON (lower(btrim(gi.identifier_value))) lower(btrim(gi.identifier_value)) tag, gi.goat_id, gi.identifier_type
          FROM goat_identifiers gi, prm WHERE gi.tenant_id=prm.t AND gi.status='active' AND gi.identifier_type IN ('animal_identifier_1','animal_identifier_2') AND btrim(gi.identifier_value)<>''
          ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC) i
    WHERE i.goat_id IN (SELECT i2.goat_id FROM goat_identifiers i2 WHERE i2.status='active' AND lower(btrim(i2.identifier_value)) IN (
       SELECT lower(btrim(o.scanned_identifier)) FROM weighing_observations o JOIN weighing_campaign_sheds cs USING (campaign_shed_id) JOIN weighing_campaigns c ON c.campaign_id=cs.campaign_id, prm
       WHERE c.park_id=ANY(prm.parks) AND cs.status<>'canceled' AND o.verification_status<>'rejected' AND btrim(o.scanned_identifier)<>''
         AND o.accepted_at >= (prm.fd::timestamp AT TIME ZONE 'Asia/Kolkata') AND o.accepted_at < (prm.td::timestamp AT TIME ZONE 'Asia/Kolkata')))
  ) x WHERE n>1),
scoped AS (SELECT cs.campaign_shed_id, cs.location_id, COALESCE(cs.partition_label,'') bucket_partition, cs.weighing_category, c.campaign_id, c.period_start_date, c.park_id, l.name loc_name, l.parent_location_id
  FROM weighing_campaign_sheds cs JOIN weighing_campaigns c ON c.campaign_id=cs.campaign_id JOIN locations l ON l.location_id=cs.location_id, prm
  WHERE cs.tenant_id=prm.t AND c.park_id=ANY(prm.parks) AND cs.status<>'canceled' AND c.status<>'canceled' AND (prm.cat='' OR cs.weighing_category=prm.cat)),
pen_map AS (SELECT DISTINCT ON (s.location_id, s.bucket_partition) s.location_id, s.bucket_partition,
  CASE WHEN s.bucket_partition<>'' THEN s.location_id ELSE COALESCE(par.id, s.location_id) END pen_shed_id,
  CASE WHEN s.bucket_partition<>'' THEN s.bucket_partition ELSE COALESCE(par.lbl,'') END pen_partition_label
  FROM scoped s LEFT JOIN LATERAL (SELECT sh.location_id id, regexp_replace(btrim(substr(s.loc_name,length(sh.name)+1)),'^[-\s]+','') lbl
    FROM locations sh WHERE sh.parent_location_id=s.parent_location_id AND sh.location_type='shed' AND sh.status='active' AND sh.retired_at IS NULL AND sh.name<>s.loc_name
      AND (s.loc_name LIKE sh.name||' %' OR s.loc_name LIKE sh.name||' - %') ORDER BY length(sh.name) DESC LIMIT 1) par ON s.bucket_partition=''
  ORDER BY s.location_id, s.bucket_partition),
bucket_pen AS (SELECT s.*, pm.pen_shed_id, pm.pen_partition_label, regexp_replace(lower(btrim(pm.pen_partition_label)),'^[-\s]*(part|pt)?[\s.-]*','') pen_key
  FROM scoped s JOIN pen_map pm ON pm.location_id=s.location_id AND pm.bucket_partition=s.bucket_partition),
lump_rounds AS (SELECT DISTINCT ON (bp.pen_shed_id,bp.pen_key,bp.campaign_id) bp.pen_shed_id,bp.pen_key,bp.campaign_id,bp.period_start_date,
  (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, so.average_weight_kg::float8 avg_kg, so.animal_count animals
  FROM bucket_pen bp JOIN weighing_shed_observations so ON so.campaign_shed_id=bp.campaign_shed_id, prm
  WHERE bp.weighing_category='per_shed_partition' AND so.withdrawn_at IS NULL AND so.verification_status<>'rejected'
    AND so.accepted_at >= (prm.fd::timestamp AT TIME ZONE 'Asia/Kolkata') AND so.accepted_at < (prm.td::timestamp AT TIME ZONE 'Asia/Kolkata')
    AND so.animal_count>0 AND so.average_weight_kg IS NOT NULL
  ORDER BY bp.pen_shed_id,bp.pen_key,bp.campaign_id,so.accepted_at DESC,so.shed_observation_id DESC),
scan_rounds AS (SELECT DISTINCT ON (bp.pen_shed_id,bp.pen_key,bp.campaign_id,COALESCE(m.canonical_tag,lower(btrim(o.scanned_identifier))))
  bp.pen_shed_id,bp.pen_key,bp.campaign_id,bp.period_start_date, COALESCE(m.canonical_tag,lower(btrim(o.scanned_identifier))) animal_key,
  (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, o.weight_kg::float8 w
  FROM bucket_pen bp JOIN weighing_observations o ON o.campaign_shed_id=bp.campaign_shed_id LEFT JOIN idm m ON m.tag=lower(btrim(o.scanned_identifier)), prm
  WHERE bp.weighing_category='individual_animal' AND o.verification_status<>'rejected' AND btrim(o.scanned_identifier)<>''
    AND o.accepted_at >= (prm.fd::timestamp AT TIME ZONE 'Asia/Kolkata') AND o.accepted_at < (prm.td::timestamp AT TIME ZONE 'Asia/Kolkata')
  ORDER BY bp.pen_shed_id,bp.pen_key,bp.campaign_id,COALESCE(m.canonical_tag,lower(btrim(o.scanned_identifier))),o.accepted_at DESC,o.observation_id DESC),
scan_pen_rounds AS (SELECT pen_shed_id,pen_key,campaign_id,period_start_date,max(d) d,avg(w) avg_kg,count(*)::int animals FROM scan_rounds GROUP BY 1,2,3,4),
pen_rounds AS (SELECT pen_shed_id,pen_key,campaign_id,period_start_date,d,avg_kg,animals FROM lump_rounds UNION ALL SELECT * FROM scan_pen_rounds),
feed_rows AS (SELECT r.shed_id pen_shed_id,
  CASE WHEN r.partition_key='whole' THEN '' WHEN r.partition_key LIKE 'part %' THEN btrim(substr(r.partition_key,6)) WHEN r.partition_key LIKE 'pt %' THEN btrim(substr(r.partition_key,4)) ELSE r.partition_key END pen_key,
  i.feed_day,i.park_id,r.feed_item_key,r.quantity_kg,r.head_count,r.shed_tag_key,r.breed_key
  FROM feed_direction_issue_rows r JOIN feed_direction_issues i ON i.feed_direction_issue_id=r.feed_direction_issue_id, prm
  WHERE i.park_id=ANY(prm.parks) AND i.feed_day>=prm.fd AND i.feed_day<prm.td AND i.state IN ('issued','amended','locked') AND r.shed_id IN (SELECT pen_shed_id FROM pen_map)),
wd AS (SELECT w.shed_id pen_shed_id,
  CASE WHEN w.partition_key='whole' THEN '' WHEN w.partition_key LIKE 'part %' THEN btrim(substr(w.partition_key,6)) WHEN w.partition_key LIKE 'pt %' THEN btrim(substr(w.partition_key,4)) ELSE w.partition_key END pen_key,
  w.target_date feed_day, sum(w.wastage_kg)::float8 kg
  FROM feed_wastage_completions w, prm
  WHERE w.tenant_id=prm.t AND w.park_id=ANY(prm.parks) AND w.target_date>=prm.fd AND w.target_date<prm.td AND w.status='completed' AND w.wastage_kg IS NOT NULL
    AND w.shed_id IN (SELECT pen_shed_id FROM pen_map) GROUP BY 1,2,3),
wfed AS (SELECT wd.* FROM wd WHERE EXISTS (SELECT 1 FROM feed_rows fr WHERE fr.pen_shed_id=wd.pen_shed_id AND fr.pen_key=wd.pen_key AND fr.feed_day=wd.feed_day AND fr.quantity_kg IS NOT NULL)),
pens AS (SELECT pen_shed_id,pen_key,count(*) rounds,(array_agg(avg_kg ORDER BY period_start_date,d))[1] first_avg,
  (array_agg(animals ORDER BY period_start_date DESC,d DESC))[1] last_animals FROM pen_rounds GROUP BY 1,2),
res AS (SELECT p.pen_shed_id,p.pen_key,g.goat_id,g.breed,lower(g.sex) sex,lower(g.species) species,
  CASE WHEN EXISTS (SELECT 1 FROM procurement_load_goats plg WHERE plg.goat_id=g.goat_id) THEN 'procured_load'
       WHEN g.origin_type='birth' THEN 'farm_born'
       WHEN g.origin_type='procured' THEN 'procured_no_load'
       ELSE '' END origin
  FROM pens p JOIN goats g ON g.lifecycle_status='alive' AND g.shed_id=p.pen_shed_id LEFT JOIN goat_shed_partitions gsp ON gsp.goat_id=g.goat_id
  WHERE regexp_replace(lower(btrim(COALESCE(NULLIF(gsp.partition_label,'whole'),''))),'^[-\s]*(part|pt)?[\s.-]*','')=p.pen_key),
wtd AS (SELECT sr.pen_shed_id,sr.pen_key,g.goat_id,g.breed,lower(g.sex) sex,lower(g.species) species,
  CASE WHEN EXISTS (SELECT 1 FROM procurement_load_goats plg WHERE plg.goat_id=g.goat_id) THEN 'procured_load'
       WHEN g.origin_type='birth' THEN 'farm_born'
       WHEN g.origin_type='procured' THEN 'procured_no_load'
       ELSE '' END origin
  FROM (SELECT DISTINCT pen_shed_id,pen_key,animal_key FROM scan_rounds) sr JOIN goat_identifiers gi ON gi.normalized_value=upper(sr.animal_key) JOIN goats g ON g.goat_id=gi.goat_id),
coh_src AS (SELECT * FROM res UNION ALL SELECT w.* FROM wtd w WHERE NOT EXISTS (SELECT 1 FROM res r WHERE r.pen_shed_id=w.pen_shed_id AND r.pen_key=w.pen_key)),
coh AS (SELECT pen_shed_id,pen_key,count(*) n,
  CASE WHEN count(DISTINCT sex)=1 AND min(sex)<>'' THEN min(sex) ELSE 'mixed' END sex,
  CASE WHEN count(DISTINCT breed)=1 AND btrim(min(breed))<>'' THEN lower(btrim(min(breed))) ELSE 'mixed' END breed,
  CASE WHEN count(DISTINCT origin)=1 AND min(origin)<>'' THEN min(origin) ELSE 'mixed' END origin,
  bool_and(species IN ('goat','sheep')) priced FROM coh_src GROUP BY 1,2),
lump_seg AS (SELECT pen_shed_id,pen_key,d_prev,d,(avg_kg-avg_prev)*1000.0/(d-d_prev) adg_g FROM (
  SELECT lr.*, lag(d) OVER w d_prev, lag(avg_kg) OVER w avg_prev FROM lump_rounds lr WINDOW w AS (PARTITION BY pen_shed_id,pen_key ORDER BY period_start_date,d)) x WHERE d_prev IS NOT NULL AND d>d_prev),
sps AS (SELECT * FROM (SELECT sr.*, lag(campaign_id) OVER w campaign_prev, lag(d) OVER w d_prev FROM scan_pen_rounds sr WINDOW w AS (PARTITION BY pen_shed_id,pen_key ORDER BY period_start_date,d)) x WHERE campaign_prev IS NOT NULL AND d>d_prev),
scan_seg AS (SELECT s.pen_shed_id,s.pen_key,s.d_prev,s.d,avg((c.w-p.w)*1000.0/(c.d-p.d)) adg_g FROM sps s
  JOIN scan_rounds c ON c.pen_shed_id=s.pen_shed_id AND c.pen_key=s.pen_key AND c.campaign_id=s.campaign_id
  JOIN scan_rounds p ON p.pen_shed_id=s.pen_shed_id AND p.pen_key=s.pen_key AND p.campaign_id=s.campaign_prev AND p.animal_key=c.animal_key
  WHERE c.d>p.d GROUP BY 1,2,3,4),
segments AS (SELECT * FROM lump_seg UNION ALL SELECT * FROM scan_seg),
fp AS (SELECT fr.park_id,fr.feed_item_key,fr.feed_day,(SELECT COALESCE(p.per_kg_cost,p.total_cost/NULLIF(p.quantity_kg,0))::float8 FROM feed_purchases p
  WHERE p.park_id=fr.park_id AND p.feed_item_key=fr.feed_item_key AND p.purchase_date<=fr.feed_day ORDER BY p.purchase_date DESC,p.batch_no DESC LIMIT 1) per_kg
  FROM (SELECT DISTINCT park_id,feed_item_key,feed_day FROM feed_rows) fr),
sf AS (SELECT sg.pen_shed_id,sg.pen_key,sg.d_prev,sg.d, sum(fr.quantity_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL)::float8 feed_kg,
  sum(fr.quantity_kg*fp.per_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL AND fp.per_kg IS NOT NULL)::float8 cost,
  COALESCE(sum(fr.quantity_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL AND fp.per_kg IS NULL),0)::float8 unpriced
  FROM segments sg LEFT JOIN feed_rows fr ON fr.pen_shed_id=sg.pen_shed_id AND fr.pen_key=sg.pen_key AND fr.feed_day>=sg.d_prev AND fr.feed_day<sg.d
  LEFT JOIN fp ON fp.park_id=fr.park_id AND fp.feed_item_key=fr.feed_item_key AND fp.feed_day=fr.feed_day GROUP BY 1,2,3,4),
sh AS (SELECT sg.pen_shed_id,sg.pen_key,sg.d_prev,sg.d,sum(g.h)::float8 head_days FROM segments sg JOIN (SELECT pen_shed_id,pen_key,feed_day,shed_tag_key,breed_key,max(head_count) h FROM feed_rows WHERE head_count IS NOT NULL GROUP BY 1,2,3,4,5) g
  ON g.pen_shed_id=sg.pen_shed_id AND g.pen_key=sg.pen_key AND g.feed_day>=sg.d_prev AND g.feed_day<sg.d GROUP BY 1,2,3,4),
sw AS (SELECT sg.pen_shed_id,sg.pen_key,sg.d_prev,sg.d,sum(wf.kg)::float8 wastage FROM segments sg JOIN wfed wf
  ON wf.pen_shed_id=sg.pen_shed_id AND wf.pen_key=sg.pen_key AND wf.feed_day>=sg.d_prev AND wf.feed_day<sg.d GROUP BY 1,2,3,4),
seg AS (SELECT sg.*, GREATEST(f.feed_kg-COALESCE(w.wastage,0),0) feed_kg, COALESCE(w.wastage,0) wastage_kg, f.cost,
  CASE WHEN f.feed_kg > 0 THEN f.unpriced * GREATEST(f.feed_kg-COALESCE(w.wastage,0),0) / f.feed_kg ELSE 0 END unpriced,
  h.head_days, (f.feed_kg IS NOT NULL AND h.head_days>0) ok, sg.adg_g*h.head_days/1000 gain
  FROM segments sg LEFT JOIN sf f USING (pen_shed_id,pen_key,d_prev,d) LEFT JOIN sh h USING (pen_shed_id,pen_key,d_prev,d)
  LEFT JOIN sw w USING (pen_shed_id,pen_key,d_prev,d)),
pen AS (SELECT p.pen_shed_id,p.pen_key,shed.name shed,pk.name park,pk.location_code pcode,p.rounds,p.last_animals animals,p.first_avg,
  COALESCE(c.sex,'unknown') sex, COALESCE(c.breed,'unknown') breed, COALESCE(c.origin,'unknown') origin, COALESCE(c.priced,false) priced,
  bool_or(s.ok) anyok, sum(s.feed_kg) FILTER (WHERE s.ok) feed_kg, sum(s.wastage_kg) FILTER (WHERE s.ok) wastage_kg, sum(s.gain) FILTER (WHERE s.ok) gain_kg, sum(s.head_days) FILTER (WHERE s.ok) head_days,
  sum(s.cost) FILTER (WHERE s.ok) cost, COALESCE(sum(s.unpriced) FILTER (WHERE s.ok),0) unpriced
  FROM pens p JOIN locations shed ON shed.location_id=p.pen_shed_id LEFT JOIN locations pk ON pk.location_id=shed.parent_location_id
  LEFT JOIN coh c USING (pen_shed_id,pen_key) LEFT JOIN seg s USING (pen_shed_id,pen_key)
  GROUP BY 1,2,3,4,5,6,7,8,9,10,11,12),
fpen AS (SELECT pen.*, CASE WHEN rounds<2 OR anyok IS NULL THEN 'weighed_once' WHEN NOT anyok THEN 'no_feed' WHEN gain_kg<=0 THEN 'no_gain' ELSE 'ok' END status,
  CASE WHEN rounds>=2 AND anyok AND gain_kg>0 THEN feed_kg/gain_kg END fcr,
  CASE WHEN rounds>=2 AND anyok AND gain_kg>0 AND priced THEN gain_kg*425 END gain_value
  FROM pen, prm WHERE (prm.sx='' OR pen.sex=prm.sx) AND (prm.org='' OR pen.origin=prm.org))
```

## 1 Farm FCR (KPI "Farm FCR", kg feed per kg gain)
- summary.fcr = Σfeed_kg / Σgain_kg over pens with status ok (domain :458); sub-line pens_with_fcr · animals (Σ latest-round head count over ALL pens in scope). UI 2 dp.
- `SELECT round((sum(feed_kg)/sum(gain_kg))::numeric,2), count(fcr), sum(animals) FROM fpen WHERE fcr IS NOT NULL` (animals: drop the WHERE).
- verified 2026-09-24 all parks 2026-08-03..09-23 male -> 9.22 (21 pens with FCR of 24; 501 animals; 30,933 kg feed; 3,355.9 kg gain; ADG 160 g). sex=all -> 9.84 (39/45 pens, 766 animals).
- CEO: "What's our FCR?" / "Kitna feed khila ke ek kilo wajan badha?"

## 2 Weight gained, at sale price (KPI)
- Σ pen gain_kg x pen head-weighted sale price, only pens with FCR and fully priced mix (domain :622). Sub: Σgain_kg (0 dp). UI ₹ 0 dp.
- verified 2026-09-24 same params male -> ₹14,26,238 on 3,356 kg.
- CEO: "What is the weight we put on worth?" / "Badha hua wajan kitne ka hai?"

## 3 Feed spent + Feed cost per kg gain (KPI "Feed spent", sub "₹/kg gain")
- feed_cost_inr = Σ priced segment cost (qty x purchase price) over FCR pens; cost_per_kg_gain = Σcost / Σgain_kg (gain includes pens' unpriced kg — cost understated if unpriced_kg>0). UI ₹ 0 dp.
- verified 2026-09-24 male -> ₹10,68,120 on 30,933 kg; ₹318 per kg gain; unpriced 1,129 kg. sex=all -> ₹14,09,021; ₹334/kg.
- CEO: "Feed pe kitna kharcha hua, ek kilo badhane me kitna laga?"

## 4 Money made over feed (KPI margin)
- Σgain_value − Σfeed_cost (only when both exist; negative shows "ate more value than gained"). verified 2026-09-24 male -> ₹3,58,117.
- CEO: "Are we making money over feed?" / "Feed ke upar kitna kamaya?"

## 5 Break-even FCR (KPI, also dashed line on FCR-by-pen)
- (Σgain_value/valued_gain_kg) ÷ (Σcost / priced_feed_kg), priced_feed_kg = Σ(feed_kg−unpriced_kg) on pens with cost (domain :467-478). UI 1 dp.
- verified 2026-09-24 male -> 11.9 (sex=all 12.1). Pen FCR above this = losing money.
- CEO: "Kis FCR ke upar ghata hai?"

## 6 Sale-price caption
- From response sale_prices (fcr.go:606 salePricesSQL, as-of today). verified 2026-09-24 -> ₹425/kg goat, ₹425/kg sheep, effective 2026-09-07, 0 overrides.

## 7 FCR by pen (horizontal bars, 2 dp; tag breed · sex)
- Per pen feed_kg/gain_kg, status ok only; order park CODE (CBE, CPT) then natural pen name. Rows = §15 query.
- verified 2026-09-24 male: best Channapatna Mandela 1 part 1 5.45, worst Coimbatore Godel 1 part 8 26.51.
- CEO: "Which pen converts feed worst?" / "Kaunsa pen sabse zyada khaata hai aur kam badhta hai?"

## 8 Money by pen (gain value / feed cost / margin bars, ₹ rounded)
- Same per-pen fields as §15 (gv, cost, margin). verified 2026-09-24 male: loss-making pens Mandela 1 part 4 −₹5,033, Godel 2 part 3 −₹3,577, Godel 1 part 8 −₹3,440, Yashoda 3 −₹915.
- CEO: "Kis pen me ghata ho raha hai?"

## 9 FCR by breed (card shows estimated_by_breed; fallback by_breed)
- Each FCR pen's feed/gain/head-days/cost/value split by cohort breed headcount share; pens counted once per breed present (domain :662). Official by_breed (agree-or-mixed) is NOT what renders. Sorted FCR desc; bars 2 dp + margin ₹.
```sql
, ok AS (SELECT * FROM fpen WHERE fcr IS NOT NULL),
mem AS (SELECT pen_shed_id,pen_key,COALESCE(NULLIF(lower(btrim(breed)),''),'unknown') b,count(*) n,sum(count(*)) OVER (PARTITION BY pen_shed_id,pen_key) tot FROM coh_src GROUP BY 1,2,3)
SELECT m.b,count(*) pens,round((sum(ok.feed_kg*m.n/m.tot)/sum(ok.gain_kg*m.n/m.tot))::numeric,2) fcr FROM ok JOIN mem m USING (pen_shed_id,pen_key) GROUP BY 1 ORDER BY 3 DESC;
```
- verified 2026-09-24 male -> anantapur sheep 8.89 (17 pens, ₹311/kg), beetal 11.50, sojat 12.30, malai 26.51 (1 animal). Trap: breeds with 1 animal carry a whole mixed pen's share — noisy.
- CEO: "Kaunsi nasl feed ko sabse achha wajan me badalti hai?"

## 10 FCR by sex
- Group by pen cohort sex (male/female/mixed/unknown). Under the default male filter it is a single bar = Farm FCR. verified 2026-09-24 male -> male 9.22 (21 pens).
- CEO: "Nar vs mada FCR?" (use sex=all).

## 11 FCR week by week
- Segments grouped by Monday of the LATER round (domain :442); FCR = Σfeed/Σgain; pens = distinct pens. UI 2 dp, weeks with gain≤0 hidden.
- `SELECT date_trunc('week',s.d)::date, count(DISTINCT (s.pen_shed_id,s.pen_key)), round((sum(s.feed_kg)/sum(s.gain))::numeric,2) FROM seg s WHERE s.ok AND (s.pen_shed_id,s.pen_key) IN (SELECT pen_shed_id,pen_key FROM fpen WHERE fcr IS NOT NULL) GROUP BY 1 ORDER BY 1`
- verified 2026-09-24 male -> 08-10 13.31, 08-17 7.30, 08-24 6.88, 08-31 18.83, 09-07 6.96, 09-14 9.18, 09-21 11.01. Trap: week FCR can swing wildly (one round's weighing noise); early weeks' ₹/kg low because Aug feed often unpriced.
- CEO: "Is FCR getting better week on week?" / "Har hafte FCR sudhar raha hai?"

## 12 FCR by weight band
- Band from pen average at FIRST round in window (first_avg), edges growth_assumptions weight_band_edges_kg (stg 15,20,25,30,35 -> <15,15-20,…,35+); `width_bucket(first_avg, ARRAY[15,20,25,30,35])`.
- verified 2026-09-24 male -> <15 11.99 (1), 15-20 9.46 (2), 20-25 8.83 (11), 25-30 9.87 (4), 30-35 9.84 (2), 35+ 5.45 (1).
- CEO: "Kis wajan pe FCR sabse achha hai?"

## 13 FCR by park (also the park cut the old reference covered)
- `SELECT park, round((sum(feed_kg)/sum(gain_kg))::numeric,2), round((sum(cost)/sum(gain_kg))::numeric) FROM fpen WHERE fcr IS NOT NULL GROUP BY park`
- verified 2026-09-24 male -> Coimbatore 9.73 (₹335/kg, 12 pens, margin ₹1,70,571); Channapatna 8.55 (₹296/kg, 9 pens, margin ₹1,87,546).
- CEO: "Coimbatore vs Channapatna FCR?" / "Kaunsa farm feed me behtar hai?"

## 14 Farm born, procured (no load), procured (load)
- Pen origin (three cohorts since 26/09/2026, platform/animalorigin; FCR domain originFor): per animal, on a load -> procured_load, else origin_type 'birth' -> farm_born, else 'procured' -> procured_no_load, else none. A pen takes a cohort only when EVERY cohort animal has it; otherwise mixed (an animal with no recorded origin makes its pen mixed). Before 26/09/2026 farm_born meant "on no load", so it also held animals bought without a load.
- verified 2026-09-26 (STG, cost-per-kg-gain.sql, 27/08-26/09): farm_born FCR 11.67 (26 pens), procured_load 8.91 (11), procured_no_load 1 pen with no FCR. Older figure, two-way rule: 2026-09-24 male -> purchased 8.91 (11 pens), farm_born 10.85 (8), mixed 6.98 (2).
- CEO: "Khareede hue jaanwar better convert karte hain ya ghar ke?"

## 15 Pens table (columns: pen, cohort, animals, weighed, daily gain, head-days, gain kg, feed kg, FCR, feed cost, gain value, margin, ₹/kg gain, feed sheet)
- pen = park-prefixed shed + partition (feed-sheet label wins), sub = whole pen / scanned; cohort = breed + sex tags; animals = head count at latest round; weighed = first → last round date (single date if weighed once); daily gain = Σgain·1000/Σhead-days (g, 0 dp); head-days 0 dp; gain/feed 1 dp; FCR 2 dp; money ₹ 0 dp (+ "N kg unpriced"); feed sheet = status or "N blocked". Paged by offset/limit (10/25/50).
```sql
SELECT park, shed, pen_key, breed, sex, animals, rounds, round((gain_kg*1000/head_days)::numeric) adg_g, round(head_days::numeric) hd, round(gain_kg::numeric,1) gain,
 round(feed_kg::numeric,1) feed, round(fcr::numeric,2) fcr, round(cost::numeric) cost, round(gain_value::numeric) gv, round((gain_value-cost)::numeric) margin, round((cost/gain_kg)::numeric) rs_kg, status
FROM fpen ORDER BY pcode, shed, pen_key;
```
- verified 2026-09-24 male -> 24 rows; e.g. Coimbatore Castro 1: 54 animals, 173 g, 2,727 head-days, 471.7 kg gain, 4,417.6 kg feed, FCR 9.37, ₹1,57,090 cost, ₹2,00,466 value, ₹43,376 margin, ₹333/kg. Trap: `feed` for a weighed-once pen is whole-window directed kg (domain :556) but no FCR.
- CEO: "Pen-wise FCR aur munafa dikhao."

## 16 Pen statuses / unmatched pens / blocked cells
- weighed_once (<2 rounds or no segment), no_feed (segments but no feed rows+head count between rounds — the "unmatched pen" case), no_gain (Σgain≤0), ok. Blocked cells = NULL quantity_kg rows in segment days.
- verified 2026-09-24 male -> 3 weighed_once (CBE Godel 1 part 6, Godel 2 part 2, Godel 2 part 6), 0 no_feed, 0 no_gain, 0 blocked cells. sex=all -> 6 weighed_once, 0 no_feed.
- The tab has no "unmatched pens" list; cost-per-kg-gain.sql's unmatched_pens lists pens with ANY segment lacking head-days (stg: CPT Castro part 1/2, Godel 2 part 1/2) — those pens still get an FCR in the app from their other segments.

## Gap (HISTORICAL — the pre-2026-09-24 cost-per-kg-gain.sql; superseded, see next section)
The old trailing-days file did not reproduce the tab:
- Drops losing segments (`adg_g>0` filter); the app nets every segment with feed+head-days and gates at pen Σgain>0. Main cause of the gap.
- Window = last N days to today+1 (days param) — page uses from/to (default 2026-08-03..latest weighing).
- No sex/origin/category filter (page default is male-only pens), no tenant/park filter, no identity merge for double-tagged scans, rounds ordered by date only (app: period_start_date, d).
- Feed cost/gain summed per segment, not per pen status.
- verified 2026-09-24 days=52 (from 2026-08-03) -> ALL ₹312/kg (CPT 303, CBE 320); same window through fcr.go logic sex=all -> ₹334/kg (adding its adg>0 filter gives ₹311, confirming the cause); page default (male) -> ₹318/kg.

## Gap closed 2026-09-24: cost-per-kg-gain.sql re-derived
- references/cost-per-kg-gain.sql is now the Base SQL above (params from_date, to_date, park_code, sex, origin, weighing; default = landing window, all sexes) with a park ROLLUP. The "Gap" list above no longer applies.
- verified 2026-09-24 2026-08-03..09-23: all sexes ALL ₹334/kg (CBE 347, CPT 319, 39 pens, FCR 9.84); sex=male ALL ₹318/kg (CBE 335, CPT 296, FCR 9.22) — both equal the tab.
