-- Cost per kg gain = Weighing > FCR tab "Feed cost per kg gain" (GET /growth-director/fcr,
-- backend/internal/growthdirector/adapters/postgres/fcr.go fcrScopeCTEs/fcrSegmentsSQL + domain/fcr.go).
-- Last 30 days (change `- 30`). Pens are joined weighing<->feed sheet by SHED ID + scrubbed partition (fcr.go pen_map),
-- so a renamed pen still matches; weighed pens with no sheet rows are listed in unmatched_pens. Read-only.
WITH w AS (SELECT (now() AT TIME ZONE 'Asia/Kolkata')::date - 30 AS s, (now() AT TIME ZONE 'Asia/Kolkata')::date + 1 AS e),
sc AS (SELECT cs.campaign_shed_id, cs.campaign_id, c.park_id, cs.weighing_category, cs.location_id,
  coalesce(cs.partition_label,'') bpart, l.name loc_name, l.parent_location_id
  FROM weighing_campaign_sheds cs JOIN weighing_campaigns c USING (campaign_id) JOIN locations l ON l.location_id=cs.location_id
  WHERE cs.status<>'canceled' AND c.status<>'canceled'),
-- pen bridge = fcr.go pen_map: bucket -> (physical shed id, scrubbed partition). Joins feed by shed ID, not by name.
pm AS (SELECT DISTINCT ON (s.location_id, s.bpart) s.location_id, s.bpart,
  CASE WHEN s.bpart<>'' THEN s.location_id ELSE coalesce(par.id, s.location_id) END pen_shed_id,
  CASE WHEN s.bpart<>'' THEN s.bpart ELSE coalesce(par.lbl,'') END plabel
  FROM sc s LEFT JOIN LATERAL (SELECT sh.location_id id, regexp_replace(btrim(substr(s.loc_name,length(sh.name)+1)),'^[-\s]+','') lbl
    FROM locations sh WHERE sh.parent_location_id=s.parent_location_id AND sh.location_type='shed' AND sh.status='active'
      AND sh.retired_at IS NULL AND sh.name<>s.loc_name AND (s.loc_name LIKE sh.name||' %' OR s.loc_name LIKE sh.name||' - %')
    ORDER BY length(sh.name) DESC LIMIT 1) par ON true
  ORDER BY s.location_id, s.bpart),
bk AS (SELECT s.campaign_shed_id, s.campaign_id, s.park_id, s.weighing_category,
  pm.pen_shed_id::text||'|'||regexp_replace(lower(btrim(pm.plabel)),'^[-\s]*(part|pt)?[\s.-]*','') pen
  FROM sc s JOIN pm ON pm.location_id=s.location_id AND pm.bpart=s.bpart),
lump AS (SELECT DISTINCT ON (bk.park_id,bk.pen,bk.campaign_id) bk.park_id,bk.pen,bk.campaign_id,
  (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, so.average_weight_kg::float8 avg_kg
  FROM bk JOIN weighing_shed_observations so USING (campaign_shed_id), w
  WHERE bk.weighing_category='per_shed_partition' AND so.withdrawn_at IS NULL AND so.verification_status<>'rejected'
    AND so.animal_count>0 AND so.average_weight_kg IS NOT NULL AND (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date >= w.s
  ORDER BY bk.park_id,bk.pen,bk.campaign_id,so.accepted_at DESC),
lseg AS (SELECT park_id,pen,d_prev,d,(avg_kg-avg_prev)*1000/(d-d_prev) adg_g FROM (
  SELECT *, lag(d) OVER p d_prev, lag(avg_kg) OVER p avg_prev FROM lump WINDOW p AS (PARTITION BY park_id,pen ORDER BY d)) x WHERE d>d_prev),
scan AS (SELECT DISTINCT ON (bk.park_id,bk.pen,bk.campaign_id,lower(btrim(o.scanned_identifier))) bk.park_id,bk.pen,bk.campaign_id,
  lower(btrim(o.scanned_identifier)) tag,(o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d,o.weight_kg::float8 wt
  FROM bk JOIN weighing_observations o USING (campaign_shed_id), w
  WHERE bk.weighing_category='individual_animal' AND o.verification_status<>'rejected' AND btrim(o.scanned_identifier)<>''
    AND (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date >= w.s
  ORDER BY bk.park_id,bk.pen,bk.campaign_id,lower(btrim(o.scanned_identifier)),o.accepted_at DESC),
sr AS (SELECT park_id,pen,campaign_id,max(d) d FROM scan GROUP BY 1,2,3),
srl AS (SELECT *, lag(campaign_id) OVER p cprev, lag(d) OVER p d_prev FROM sr WINDOW p AS (PARTITION BY park_id,pen ORDER BY d)),
sseg AS (SELECT s.park_id,s.pen,s.d_prev,s.d,avg((c.wt-p.wt)*1000/(c.d-p.d)) adg_g FROM srl s
  JOIN scan c ON c.park_id=s.park_id AND c.pen=s.pen AND c.campaign_id=s.campaign_id
  JOIN scan p ON p.park_id=s.park_id AND p.pen=s.pen AND p.campaign_id=s.cprev AND p.tag=c.tag
  WHERE s.cprev IS NOT NULL AND s.d>s.d_prev AND c.d>p.d GROUP BY 1,2,3,4),
seg AS (SELECT * FROM lseg UNION ALL SELECT * FROM sseg),
fr2 AS (SELECT i.park_id, i.feed_day, r.feed_item_key, r.quantity_kg, r.head_count, r.shed_tag_key, r.breed_key,
  r.shed_id::text||'|'||CASE WHEN r.partition_key='whole' THEN '' WHEN r.partition_key LIKE 'part %' THEN btrim(substr(r.partition_key,6))
    WHEN r.partition_key LIKE 'pt %' THEN btrim(substr(r.partition_key,4)) ELSE r.partition_key END pen
  FROM feed_direction_issue_rows r JOIN feed_direction_issues i USING (feed_direction_issue_id), w
  WHERE i.state IN ('issued','amended','locked') AND i.feed_day>=w.s),
price AS (SELECT DISTINCT f.park_id,f.feed_item_key,f.feed_day,(SELECT coalesce(p.per_kg_cost,p.total_cost/nullif(p.quantity_kg,0)) FROM feed_purchases p
  WHERE p.park_id=f.park_id AND p.feed_item_key=f.feed_item_key AND p.purchase_date<=f.feed_day ORDER BY p.purchase_date DESC, p.batch_no DESC LIMIT 1)::float8 per_kg FROM fr2 f),
fday AS (SELECT f.park_id,f.pen,f.feed_day,sum(f.quantity_kg*pr.per_kg) cost FROM fr2 f JOIN price pr USING (park_id,feed_item_key,feed_day) GROUP BY 1,2,3),
hday AS (SELECT park_id,pen,feed_day,sum(h) h FROM (SELECT park_id,pen,feed_day,max(head_count) h FROM fr2 WHERE head_count IS NOT NULL GROUP BY park_id,pen,feed_day,shed_tag_key,breed_key) z GROUP BY 1,2,3),
segf AS (SELECT s.park_id,s.pen,s.d_prev,s.d,s.adg_g,sum(fd.cost) cost,sum(hd.h) head_days FROM seg s
  LEFT JOIN hday hd ON hd.park_id=s.park_id AND hd.pen=s.pen AND hd.feed_day>=s.d_prev AND hd.feed_day<s.d
  LEFT JOIN fday fd ON fd.park_id=hd.park_id AND fd.pen=hd.pen AND fd.feed_day=hd.feed_day
  GROUP BY 1,2,3,4,5),
pens AS (SELECT DISTINCT split_part(pen,'|',1)::uuid shed_id, split_part(pen,'|',2) part FROM segf WHERE head_days IS NULL)
SELECT coalesce(pk.name,'ALL') park, round(sum(cost) FILTER (WHERE adg_g>0 AND head_days>0)) feed_cost_rs,
  round((sum(adg_g*head_days/1000) FILTER (WHERE adg_g>0 AND head_days>0 AND cost IS NOT NULL))::numeric,1) gain_kg,
  round((sum(cost) FILTER (WHERE adg_g>0 AND head_days>0) / nullif(sum(adg_g*head_days/1000) FILTER (WHERE adg_g>0 AND head_days>0 AND cost IS NOT NULL),0))::numeric) rs_per_kg_gain,
  count(*) FILTER (WHERE cost IS NOT NULL AND head_days>0) segs_priced, count(*) segs,
  -- weighed pens with NO feed-sheet rows on their shed id + partition (renamed/moved pen, or sheet gap): never silently dropped
  (SELECT string_agg(DISTINCT l.name||CASE WHEN p.part<>'' THEN ' part '||p.part ELSE '' END, ', ') FROM pens p JOIN locations l ON l.location_id=p.shed_id
    WHERE l.parent_location_id IS NOT NULL AND (pk.location_id IS NULL OR EXISTS (SELECT 1 FROM segf x WHERE x.park_id=pk.location_id AND x.pen=p.shed_id::text||'|'||p.part))) unmatched_pens
FROM segf JOIN locations pk ON pk.location_id=segf.park_id GROUP BY ROLLUP((pk.name, pk.location_id));
