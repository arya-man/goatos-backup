-- App: Sales > Load-wise (GET /procurement/loadwise; procurement/adapters/postgres/loadwise_repository.go
-- + domain/loadwise.go). One row per purchase load. One load: run_reference('load-wise-sales.sql', where="load_no='131'").
-- sold = tagged GoatOS sales on 'Deal Closed' deals only (an animal exited against a deal still open is
--   'unaccounted' until it closes) + pre-GoatOS prior outcomes (counts only);
-- sold_value = each animal priced from ITS OWN sale line (2026-09-25 option B): the deal's animal line of its species
--   (+ breed when the deal has several lines of that species; same species+breed lines pooled) / animals tagged to it;
--   an unmatched animal takes the deal's unclaimed animal-line value / unmatched animals; a deal with no lines at all
--   splits its value; feed/manure/other lines are never animal value. + prior sold value;
-- sale_price_per_kg = sold_weighed_value / sold_weight_kg from ONE sample (sale_price_basis says which):
--   'legacy'  = procurement_loads.sold_* columns, whenever pl.sold_weight_kg IS NOT NULL (always wins);
--   'tagged'  = else tagged allocations with weight_kg>0 on 'Deal Closed' deals (total_weight_kg>0, sales_value>0),
--               kg x the deal's live-animal line price (sales_deal_lines stamped animal sales_value/total_weight_kg);
--               only when all animal lines share ONE price (no blended mixed prices; manure/non-live lines
--               ignored) and deal product_type is 'Mixed' or matches its single live line;
--   NULL      = neither. sold_weighed_animals / sold_weight_kg follow the same sample (avg_sale_kg).
-- Stock valuation (not in this query): remaining x load avg sold price (sold_value/priced sold), else overall
--   avg over all tagged sales, else none; sales_valuation_assumptions.unsold_stock_price_rupees overrides all.
-- fattening/days on farm: fattening_days_final (legacy sold-out loads) else days_on_farm_so_far = today - arrived_on (NOT purchase_date).
-- tagged_rate_per_kg = extra estimate (deal Rs/kg weighted by allocation weight) - NOT on the screen; label it.
WITH member AS (
  SELECT DISTINCT ON (plg.goat_id) plg.goat_id, plg.load_id
  FROM procurement_load_goats plg WHERE plg.current_state = 'accepted_herd_intake'
  ORDER BY plg.goat_id, plg.intake_accepted_at DESC NULLS LAST, plg.created_at DESC, plg.load_goat_id DESC),
sa AS (
  SELECT a.goat_id, a.sales_deal_id, a.weight_kg, (d.status='Deal Closed') closed, d.sales_value dv,
         d.sales_value / NULLIF(d.total_weight_kg,0) deal_rate,
         NOT EXISTS (SELECT 1 FROM sales_deal_lines l WHERE l.deal_id=d.id) lineless,
         lower(btrim(coalesce(g.species,''))) sp, lower(btrim(coalesce(g.breed,''))) br
  FROM goat_sale_allocations a JOIN sales_deals d ON d.id=a.sales_deal_id JOIN goats g ON g.goat_id=a.goat_id
  WHERE a.status='tagged'),
sl AS (
  SELECT l.deal_id, coalesce(l.sales_value,0) v, lower(btrim(coalesce(c.species_code,l.product_code))) sp,
         lower(btrim(coalesce(l.breed,''))) br,
         count(*) OVER (PARTITION BY l.deal_id, lower(btrim(coalesce(c.species_code,l.product_code)))) sp_lines
  FROM sales_deal_lines l LEFT JOIN sellable_product_catalog c ON c.tenant_id=l.tenant_id AND c.product_code=l.product_code
  WHERE l.product_kind='animal'),
lb AS (SELECT deal_id, sp, CASE WHEN sp_lines=1 THEN '*' ELSE br END bkt, sum(v) v FROM sl GROUP BY 1,2,3),
ab AS (SELECT sa.*, lb.sp b_sp, lb.bkt b_bkt, lb.v b_v FROM sa LEFT JOIN lb ON lb.deal_id=sa.sales_deal_id AND lb.sp=sa.sp AND (lb.bkt='*' OR lb.bkt=sa.br)),
bc AS (SELECT sales_deal_id, b_sp, b_bkt, count(*)::numeric n FROM ab WHERE b_sp IS NOT NULL GROUP BY 1,2,3),
un AS (SELECT lb.deal_id, sum(lb.v) v FROM lb WHERE NOT EXISTS (SELECT 1 FROM bc WHERE bc.sales_deal_id=lb.deal_id AND bc.b_sp=lb.sp AND bc.b_bkt=lb.bkt) GROUP BY 1),
uc AS (SELECT sales_deal_id, count(*)::numeric n FROM ab WHERE b_sp IS NULL GROUP BY 1),
ds AS (
  SELECT ab.goat_id, ab.weight_kg, ab.closed, CASE WHEN ab.closed THEN ab.deal_rate END deal_rate,
    CASE WHEN NOT ab.closed THEN NULL
         WHEN ab.b_sp IS NOT NULL THEN CASE WHEN ab.b_v>0 THEN ab.b_v/bc.n END
         WHEN ab.lineless THEN CASE WHEN ab.dv>0 THEN ab.dv/uc.n END
         ELSE CASE WHEN un.v>0 THEN un.v/uc.n END END share
  FROM ab LEFT JOIN bc ON bc.sales_deal_id=ab.sales_deal_id AND bc.b_sp=ab.b_sp AND bc.b_bkt=ab.b_bkt
  LEFT JOIN un ON un.deal_id=ab.sales_deal_id LEFT JOIN uc ON uc.sales_deal_id=ab.sales_deal_id),
o AS (
  SELECT m.load_id, ds.share, ds.weight_kg, ds.deal_rate, lower(g.species) species,
    CASE WHEN (g.lifecycle_status='sold' OR g.exit_reason='sold') AND (ds.goat_id IS NULL OR ds.closed) THEN 'sold'
         WHEN g.lifecycle_status='dead' OR g.exit_reason='died' THEN 'dead'
         WHEN g.lifecycle_status IN ('culled','transferred','lost') OR g.exit_reason IN ('culled','transferred','lost') THEN 'other'
         WHEN g.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu') THEN 'remaining'
         ELSE 'unaccounted' END outcome
  FROM member m JOIN goats g ON g.goat_id = m.goat_id LEFT JOIN ds ON ds.goat_id = m.goat_id),
s AS (
  SELECT load_id, count(*) linked,
    count(*) FILTER (WHERE outcome='sold') sold, count(*) FILTER (WHERE outcome='dead') dead,
    count(*) FILTER (WHERE outcome='other') other, count(*) FILTER (WHERE outcome='remaining') remaining,
    count(*) FILTER (WHERE outcome='remaining' AND species='goat') remaining_goats,
    count(*) FILTER (WHERE outcome='remaining' AND species='sheep') remaining_sheep,
    coalesce(sum(share) FILTER (WHERE outcome='sold'),0) sold_value,
    sum(weight_kg*deal_rate) FILTER (WHERE outcome='sold') / NULLIF(sum(weight_kg) FILTER (WHERE outcome='sold' AND deal_rate IS NOT NULL),0) tagged_rate
  FROM o GROUP BY 1),
p AS (
  SELECT load_id, coalesce(sum(animal_count) FILTER (WHERE outcome='sold'),0) prior_sold,
    coalesce(sum(sales_value) FILTER (WHERE outcome='sold'),0) prior_value,
    coalesce(sum(animal_count) FILTER (WHERE outcome='died'),0) prior_dead
  FROM procurement_load_prior_outcomes GROUP BY 1),
sw AS (
  SELECT m.load_id, sum(a.weight_kg) kg, count(*) animals, sum(a.weight_kg*lp.price_per_kg) val
  FROM member m
  JOIN goat_sale_allocations a ON a.goat_id=m.goat_id AND a.status='tagged' AND a.weight_kg>0
  JOIN sales_deals d ON d.id=a.sales_deal_id AND d.status='Deal Closed' AND d.total_weight_kg>0 AND d.sales_value>0
  JOIN LATERAL (
    SELECT min(l.sales_value/l.total_weight_kg) FILTER (WHERE l.product_kind='animal' AND l.total_weight_kg>0 AND l.sales_value>0) price_per_kg
    FROM sales_deal_lines l WHERE l.deal_id=d.id GROUP BY l.deal_id
    HAVING count(*) FILTER (WHERE l.product_kind='animal' AND l.total_weight_kg>0 AND l.sales_value>0) > 0
      AND min(l.sales_value/l.total_weight_kg) FILTER (WHERE l.product_kind='animal' AND l.total_weight_kg>0 AND l.sales_value>0)
        = max(l.sales_value/l.total_weight_kg) FILTER (WHERE l.product_kind='animal' AND l.total_weight_kg>0 AND l.sales_value>0)
      AND (d.product_type='Mixed' OR (count(*) FILTER (WHERE l.product_kind='animal')=1
        AND min(l.product_type) FILTER (WHERE l.product_kind='animal')=d.product_type))) lp ON true
  GROUP BY m.load_id)
SELECT pl.context->>'load_ref' load_no, pl.context->>'farm' farm, pl.purchase_date, pl.arrived_on,
  coalesce(nullif(pl.expected_count,0), coalesce(s.linked,0)+coalesce(p.prior_sold,0)+coalesce(p.prior_dead,0)) purchased,
  coalesce(s.linked,0) linked_in_app,
  coalesce(s.sold,0)+coalesce(p.prior_sold,0) sold, coalesce(s.sold,0) sold_tagged, coalesce(p.prior_sold,0) sold_prior,
  coalesce(s.dead,0)+coalesce(p.prior_dead,0) dead, coalesce(s.other,0) other_exits,
  coalesce(s.remaining,0) remaining, s.remaining_goats, s.remaining_sheep,
  round(coalesce(s.sold_value,0)+coalesce(p.prior_value,0)) sold_value_rs,
  round(CASE WHEN pl.sold_weight_kg IS NOT NULL THEN pl.sold_weighed_value/NULLIF(pl.sold_weight_kg,0) ELSE sw.val/NULLIF(sw.kg,0) END,1) sale_price_per_kg,
  CASE WHEN pl.sold_weight_kg IS NOT NULL THEN 'legacy' WHEN sw.kg > 0 THEN 'tagged' END sale_price_basis,
  round(CASE WHEN pl.sold_weight_kg IS NOT NULL THEN pl.sold_weight_kg/NULLIF(pl.sold_weighed_animals,0) ELSE sw.kg/NULLIF(sw.animals,0) END,1) avg_sale_kg,
  round(s.tagged_rate,1) tagged_rate_per_kg_estimate,
  round((pl.animal_cost+coalesce(pl.transport_cost,0)+coalesce(pl.other_cost,0))/NULLIF(pl.purchase_weight_kg,0),1) landed_cost_per_kg,
  round(pl.purchase_weight_kg/NULLIF(nullif(pl.expected_count,0),0),1) avg_purchase_kg,
  pl.fattening_days fattening_days_final,
  CASE WHEN coalesce(s.remaining,0) > 0 THEN (now() AT TIME ZONE 'Asia/Kolkata')::date - pl.arrived_on END days_on_farm_so_far,
  (now() AT TIME ZONE 'Asia/Kolkata')::date - pl.purchase_date days_since_purchase
FROM procurement_loads pl LEFT JOIN s ON s.load_id=pl.load_id LEFT JOIN p ON p.load_id=pl.load_id LEFT JOIN sw ON sw.load_id=pl.load_id
ORDER BY pl.purchase_date DESC;
