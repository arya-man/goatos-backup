-- Cost per kg gain = Weighing > ADG Analytics > FCR tab KPI "Feed spent" sub-line "₹/kg gain" (+ FCR by park card),
-- GET /growth-director/fcr -> backend/internal/growthdirector/adapters/postgres/fcr.go (fcrScopeCTEs/fcrPensSQL/fcrSegmentsSQL)
-- + domain/fcr.go BuildFCRReport. Same SQL as logic/fcr.md "Base SQL", re-derived 2026-09-24. Read-only.
-- Rule (app): segment = two consecutive rounds of one pen; counts when directed feed kg present AND fed head-days>0;
-- LOSING segments (ADG<0) are netted in; a pen gets FCR/cost only if its net gain>0 and it has >=2 rounds.
-- rs_per_kg_gain = sum(pen feed cost) / sum(pen gain kg) over those pens (unpriced kg shown separately, cost understated).
-- FEED WASTAGE (2026-09-26): feed_kg and fcr are feed EATEN = directed kg minus the verifier-APPROVED leftover
-- (feed_wastage_completions status 'completed', any workflow) on the segment's days that the sheet fed with a known
-- quantity, floored at 0 per segment. Cost is NOT reduced (wasted feed was still bought), so rs_per_kg_gain is unchanged.
-- Window default = the tab's landing window: weighing_calendar_config default_from_date (stg 2026-08-03) through latest
-- accepted weighing date. Tab default sex is MALE; this file defaults to all sexes -> pass sex:'male' to match an untouched tab.
-- Run: run_reference('cost-per-kg-gain.sql', params={from_date:'2026-08-03', to_date:'2026-09-23', sex:'male'}).
-- param: from_date date  first IST date (default: SOP default_from_date, else today-30)
-- param: to_date date    last IST date inclusive (default: latest weighing date, capped at today)
-- param: park_code text  CBE | CPT (code or name); empty/all = every active park
-- param: sex text        male | female; empty/all = both (tab default male)
-- param: origin text     farm_born | purchased; empty/all = both
-- param: weighing text   individual | whole_pen; empty/all = both
-- Verified 2026-09-24 (2026-08-03..2026-09-23): all sexes ALL ₹334/kg (tab ₹334); male ALL ₹318/kg (tab ₹318).
-- Verified 2026-09-26 with wastage (2026-08-20..latest weighing, all sexes): ALL 39 pens, feed eaten 23240.8 kg, wastage
-- 4739.1 kg, gain 2616.4 kg, ₹382/kg gain, FCR 8.88 -- identical to GET /growth-director/fcr on the same data.
-- Not here: gain value / margin / break-even (sale price per species x stage x sex) and the estimated-by-breed split: see logic/fcr.md.
WITH prm AS (SELECT '00000000-0000-4000-8000-000000000001'::uuid t,
  ARRAY(SELECT l.location_id FROM locations l WHERE l.location_type='park' AND l.status='active'
    AND (x.pc='' OR x.pc='all' OR lower(l.location_code)=x.pc OR lower(l.name)=x.pc)) parks,
  x.fd, x.td, x.cat, x.sx, x.org
  FROM (SELECT /*param:from_date*/COALESCE((SELECT default_from_date FROM weighing_calendar_config WHERE default_from_mode='fixed_date' LIMIT 1), (now() AT TIME ZONE 'Asia/Kolkata')::date - 30)/*end*/::date fd,
    (LEAST(/*param:to_date*/(SELECT max((a AT TIME ZONE 'Asia/Kolkata')::date) FROM (SELECT accepted_at a FROM weighing_observations WHERE verification_status<>'rejected'
       UNION ALL SELECT accepted_at FROM weighing_shed_observations WHERE withdrawn_at IS NULL AND verification_status<>'rejected') z)/*end*/::date,
       (now() AT TIME ZONE 'Asia/Kolkata')::date) + 1) td,
    lower(btrim(/*param:park_code*/''/*end*/::text)) pc,
    CASE lower(btrim(/*param:sex*/''/*end*/::text)) WHEN 'male' THEN 'male' WHEN 'female' THEN 'female' ELSE '' END sx,
    CASE replace(lower(btrim(/*param:origin*/''/*end*/::text)),' ','_') WHEN 'farm_born' THEN 'farm_born' WHEN 'purchased' THEN 'purchased' ELSE '' END org,
    CASE replace(lower(btrim(/*param:weighing*/''/*end*/::text)),'-','_') WHEN 'individual' THEN 'individual_animal' WHEN 'individual_animal' THEN 'individual_animal'
      WHEN 'whole_pen' THEN 'per_shed_partition' WHEN 'per_shed_partition' THEN 'per_shed_partition' ELSE '' END cat) x),
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
  EXISTS (SELECT 1 FROM procurement_load_goats plg WHERE plg.goat_id=g.goat_id) bought
  FROM pens p JOIN goats g ON g.lifecycle_status='alive' AND g.shed_id=p.pen_shed_id LEFT JOIN goat_shed_partitions gsp ON gsp.goat_id=g.goat_id
  WHERE regexp_replace(lower(btrim(COALESCE(NULLIF(gsp.partition_label,'whole'),''))),'^[-\s]*(part|pt)?[\s.-]*','')=p.pen_key),
wtd AS (SELECT sr.pen_shed_id,sr.pen_key,g.goat_id,g.breed,lower(g.sex) sex,lower(g.species) species,
  EXISTS (SELECT 1 FROM procurement_load_goats plg WHERE plg.goat_id=g.goat_id) bought
  FROM (SELECT DISTINCT pen_shed_id,pen_key,animal_key FROM scan_rounds) sr JOIN goat_identifiers gi ON gi.normalized_value=upper(sr.animal_key) JOIN goats g ON g.goat_id=gi.goat_id),
coh_src AS (SELECT * FROM res UNION ALL SELECT w.* FROM wtd w WHERE NOT EXISTS (SELECT 1 FROM res r WHERE r.pen_shed_id=w.pen_shed_id AND r.pen_key=w.pen_key)),
coh AS (SELECT pen_shed_id,pen_key,count(*) n,
  CASE WHEN count(DISTINCT sex)=1 AND min(sex)<>'' THEN min(sex) ELSE 'mixed' END sex,
  CASE WHEN count(DISTINCT breed)=1 AND btrim(min(breed))<>'' THEN lower(btrim(min(breed))) ELSE 'mixed' END breed,
  CASE WHEN count(*) FILTER (WHERE bought)=count(*) THEN 'purchased' WHEN count(*) FILTER (WHERE bought)=0 THEN 'farm_born' ELSE 'mixed' END origin,
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
seg AS (SELECT sg.*, GREATEST(f.feed_kg-COALESCE(w.wastage,0),0) feed_kg, COALESCE(w.wastage,0) wastage_kg, f.cost, f.unpriced, h.head_days,
  (f.feed_kg IS NOT NULL AND h.head_days>0) ok, sg.adg_g*h.head_days/1000 gain
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
  CASE WHEN rounds>=2 AND anyok AND gain_kg>0 THEN feed_kg/gain_kg END fcr
    FROM pen, prm WHERE (prm.sx='' OR pen.sex=prm.sx) AND (prm.org='' OR pen.origin=prm.org))
SELECT COALESCE(park,'ALL') park, count(*) FILTER (WHERE fcr IS NOT NULL) pens_with_fcr, count(*) pens_in_scope,
  round(sum(feed_kg) FILTER (WHERE fcr IS NOT NULL)::numeric,1) feed_kg,
  round(sum(wastage_kg) FILTER (WHERE fcr IS NOT NULL)::numeric,1) wastage_kg,
  round(sum(cost) FILTER (WHERE fcr IS NOT NULL)::numeric) feed_cost_rs,
  round(sum(gain_kg) FILTER (WHERE fcr IS NOT NULL)::numeric,1) gain_kg,
  round((sum(cost) FILTER (WHERE fcr IS NOT NULL)/NULLIF(sum(gain_kg) FILTER (WHERE fcr IS NOT NULL),0))::numeric) rs_per_kg_gain,
  round((sum(feed_kg) FILTER (WHERE fcr IS NOT NULL)/NULLIF(sum(gain_kg) FILTER (WHERE fcr IS NOT NULL),0))::numeric,2) fcr,
  round(sum(unpriced) FILTER (WHERE fcr IS NOT NULL)::numeric) unpriced_feed_kg,
  -- pens with >=2 rounds but NO feed-sheet rows + head count between them on their shed id + partition (renamed/moved pen or sheet gap)
  string_agg(shed||CASE WHEN pen_key<>'' THEN ' part '||pen_key ELSE '' END, ', ') FILTER (WHERE status='no_feed') unmatched_pens
FROM fpen GROUP BY ROLLUP (park) ORDER BY park NULLS LAST
