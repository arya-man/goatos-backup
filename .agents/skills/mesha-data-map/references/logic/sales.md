Index: S1 Revenue & deals · S2 Animals sold (deal lines) · S3 Realised Rs/kg · S4 Manure · S5 Monthly series · S6 Price bands (product x breed) · S7 Buyer board (overview) · S8 Buyer analytics (repeat, outstanding) · S9 Deal payments (received / balance / paid-but-short) · S10 Sold weight bands · S11 Load-wise sales · S12 Farm value (live stock valuation) · S13 Deals ledger

# Sales logic cards (admin-web /sales/*; verified on goatos-stg 24/09/2026)

Shared rules
- One read feeds S1-S7 + S10: `GET /sales/overview?farm=CBE|CPT` -> `backend/internal/sales/adapters/postgres/overview_repository.go:33` GetOverview.
  Deal set = `sales_deals WHERE status='Deal Closed'` (+ `farm = 'CBE'|'CPT'`) (overview_repository.go:247). No date filter on the screen: all-time.
- Aggregation is at LINE grain (`sales_deal_lines`, every deal has >=1 line; `domain/overview_build.go:27` BuildDealAggregates).
  Line animals = `coalesce(animal_count, male_count+female_count)` for Goat/Sheep only (`domain/deal_lines.go:51`); Manure never counts as animals.
- Statuses: Deal Closed (revenue) / Advance Paid / In Discussion / Deal Failed (pipeline, never revenue) (`domain/sales.go:34`). stg: 76 closed, 1 failed.
- `farm` on sales_deals is a code (CBE/CPT), not the park name. Dates: `sale_date` (already a business date). Render DD/MM/YYYY.
- "Animals sold" has two sources: deal lines (688 all-time, incl. pre-app sheet deals) vs tagged register `goat_sale_allocations status='tagged'` (160) = `ceo_ai.animals_base exit_reason='sold'`. Name which one; period questions on the herd = register.

Line CTE used below (`L`):
```sql
L AS (SELECT d.id, d.farm, d.sale_date, d.buyer_name, x.product_type pt, x.breed,
  CASE WHEN x.product_type IN ('Goat','Sheep') THEN coalesce(x.animal_count, coalesce(x.male_count,0)+coalesce(x.female_count,0)) ELSE 0 END n,
  coalesce(x.total_weight_kg,0) kg, x.sales_value v
  FROM sales_deals d JOIN sales_deal_lines x ON x.deal_id=d.id WHERE d.status='Deal Closed')
```

## S1 Revenue & deals
Screen: /sales/sold KPI "Revenue" + "N deals" · Endpoint: GET /sales/overview · Code: overview_build.go:44-51 (summary.Deals++, Revenue += d.SalesValue; period_from/to = min/max sale_date)
Formula: sum(sales_deals.sales_value) over Deal Closed. Deal value = sum of its lines (RollupLines), so deal-sum = line-sum.
Filters->SQL: farm toggle -> `d.farm='CBE'`. Period questions: add `sale_date BETWEEN :from AND :to` (screen has none).
Traps: Advance Paid / In Discussion are pipeline, not revenue. Revenue includes manure. Do not sum payment_received as revenue.
SQL: `SELECT farm, count(*), round(sum(sales_value)) FROM sales_deals WHERE status='Deal Closed' GROUP BY ROLLUP(1);`
stg 24/09/2026: 76 deals, Rs 90,97,253 (CBE 46 / Rs 57,45,227; CPT 30 / Rs 33,52,026); period 15/04/2025-23/09/2026. Sep 2026: Rs 14,71,114.
CEO asks: "Total sales revenue so far?" · "How much did CPT sell this month?" · "Kitne deals close hue September mein?" · "Is mahine ki sale kitni hui?"

## S2 Animals sold (deal lines)
Screen: /sales/sold KPI "Animals sold" (sheep · goats) · Code: overview_build.go:68-82 · Formula: sum(line animals) where product_type Sheep/Goat.
Traps: Manure lines give 0 animals. Pre-app sheet deals have no tags, so this is > register (160). For "animals sold in <month> from the herd" use ceo_ai.animals_base (SKILL.md).
SQL: `WITH L AS (...) SELECT sum(n), sum(n) FILTER (WHERE pt='Sheep') sheep, sum(n) FILTER (WHERE pt='Goat') goats FROM L;`
stg 24/09/2026: 688 (400 sheep, 288 goats). Sep 2026: 129.
CEO asks: "How many animals have we sold in total?" · "Sheep vs goats sold?" · "Ab tak kitne janwar bike?" · "Kitni bhed aur kitni bakri bechi?"

## S3 Realised price per kg
Screen: /sales/sold KPI "Realised Rs/kg" · Code: overview_build.go:93-95,136-138
Formula: sum(v)/sum(kg) over live lines (Goat/Sheep) with kg>0 AND v>0 (same set as price bands). 0 shown as "none".
Traps: weight-less lines drop out of both sides; never divide total revenue by total kg (manure kg would swamp it).
SQL: `WITH L AS (...) SELECT round(sum(v)/sum(kg),1) FROM L WHERE pt IN ('Goat','Sheep') AND kg>0 AND v>0;`
stg 24/09/2026: Rs 398.5/kg.
CEO asks: "What price per kg are we getting?" · "Average rate per kilo live weight?" · "Kilo ka rate kya mil raha hai?" · "Per kg kitne mein bech rahe hain?"

## S4 Manure
Screen: /sales/sold KPI "Manure" (kg, Rs) · Code: overview_build.go:83-88 · Formula: sum(kg), sum(v) over lines product_type='Manure'.
Trap: manure is in revenue (S1) but never in animals or Rs/kg.
SQL: `WITH L AS (...) SELECT sum(kg), round(sum(v)) FROM L WHERE pt='Manure';`
stg 24/09/2026: 2,19,305 kg, Rs 6,32,932.
CEO asks: "How much manure did we sell?" · "Khaad se kitna paisa aaya?" · "Manure kitne kg gaya?"

## S5 Monthly series
Screen: /sales/sold "Monthly" charts: revenue (sheep+goat+manure), animals (sheep+goat, sub = live Rs), manure kg · Code: overview_build.go:53-88; web `features/procurement/sales-format.ts:158-174`; leading zero months trimmed (`trimEmptyMonthlyStart`).
Formula: group lines by to_char(sale_date,'YYYY-MM').
SQL: `WITH L AS (...) SELECT to_char(sale_date,'YYYY-MM') m, round(sum(v)) rev, sum(n) animals, round(sum(v) FILTER (WHERE pt<>'Manure')) live_rs, sum(kg) FILTER (WHERE pt='Manure') manure_kg FROM L GROUP BY 1 ORDER BY 1;`
stg 24/09/2026: 2026-07 Rs 69,550 / 1 animal / 14,450 kg manure; 2026-08 Rs 4,95,760 / 31; 2026-09 Rs 14,71,114 / 129.
CEO asks: "Month-wise sales trend?" · "Which month had the best sales?" · "Mahine-wise kitna becha?" · "Pichle mahine se zyada hua kya?"

## S6 Price bands (product x breed)
Screen: /sales/sold "Price bands" · Code: overview_build.go:91-113,146-161
Formula: per (product_type, breed) over live lines kg>0,v>0: avg = sum(v)/sum(kg); min/max = per-line v/kg; sorted by avg desc.
SQL: `WITH L AS (...) SELECT pt, breed, count(*) lines, sum(n), round(sum(v)/sum(kg),1) avg, round(min(v/kg),1), round(max(v/kg),1) FROM L WHERE pt IN ('Goat','Sheep') AND kg>0 AND v>0 GROUP BY 1,2 ORDER BY avg DESC;`
stg 24/09/2026: Goat Beetle 467.2; Sirohi 450; Sojat 440; Osmanabadi 424.8; Sheep Kenguri 423.2; Anantapur 406.0 (389 animals, min 63.3 = data-entry outlier); Goat Malai 357.5.
Trap: min/max are line prices, not animal prices; a low min usually means a mis-keyed weight/value.
CEO asks: "Which breed fetches the best price?" · "Malai ka rate kya chal raha hai?" · "Kaunsi breed sabse mehngi biki?"

## S7 Buyer board (overview)
Screen: /sales/sold "Buyers" table (top 25, paged) · Code: overview_build.go:116-131,163-184
Formula: group by raw `buyer_name` (exact string); deals, animals, revenue, share % = revenue / whole-filter revenue.
Trap: raw names, so spelling variants split; S8 is the de-duplicated view.
SQL: `SELECT buyer_name, count(*), round(sum(sales_value)), round(100*sum(sales_value)/sum(sum(sales_value)) OVER (),1) FROM sales_deals WHERE status='Deal Closed' GROUP BY 1 ORDER BY 3 DESC LIMIT 5;`
stg 24/09/2026: Mahendran 18 deals Rs 30,13,196 (33.1%); Moshin 3 / Rs 11,04,405 (12.1%); Kartik 1 / Rs 5,65,000 (6.2%).
CEO asks: "Who is our biggest buyer?" · "Top 5 buyers by revenue?" · "Sabse zyada kaun khareedta hai?" · "Mahendran ne kitna liya?"

## S8 Buyer analytics
Screen: /sales/buyer-analytics · Endpoint: GET /procurement/buyer-analytics?farm= · Code: `backend/internal/procurement/adapters/postgres/buyer_analytics_repository.go:44` closedBuyerDealsSQL; `domain/buyer_analytics.go:192` BuildBuyerAnalytics
Formula: buyer key = `vendor:<buyer_vendor_id>` else vendor whose normalised business_name (lower, collapsed spaces) is UNIQUE, else `name:<normalised buyer_name>`. Repeat buyer = >=2 closed deals; repeat revenue % = their revenue / all. Outstanding per deal = greatest(sales_value - coalesce(payment_received,0), 0), summed.
SQL:
```sql
WITH v AS (SELECT vendor_id, lower(regexp_replace(btrim(business_name),'\s+',' ','g')) k FROM procurement_vendors),
nm AS (SELECT k, CASE WHEN count(*)=1 THEN min(vendor_id::text) END vid FROM v GROUP BY k),
d AS (SELECT d.*, coalesce(d.buyer_vendor_id::text, nm.vid, 'name:'||lower(regexp_replace(btrim(d.buyer_name),'\s+',' ','g'))) bk
  FROM sales_deals d LEFT JOIN nm ON d.buyer_vendor_id IS NULL AND nm.k=lower(regexp_replace(btrim(d.buyer_name),'\s+',' ','g'))
  WHERE d.status='Deal Closed'),
b AS (SELECT bk, count(*) n, sum(sales_value) rev FROM d GROUP BY 1)
SELECT count(*) buyers, count(*) FILTER (WHERE n>=2) repeat, round(100*sum(rev) FILTER (WHERE n>=2)/sum(rev),1) repeat_pct FROM b;
```
stg 24/09/2026: 31 buyers, 16 repeat, repeat revenue Rs 65,17,279 (71.6%); outstanding Rs 41,89,524.
Trap: outstanding treats NULL payment_received as 0 -> 40 old sheet deals with no payment tracking count as fully owed. Say so (see S9).
CEO asks: "How many repeat buyers?" · "How much is outstanding from buyers?" · "Kitne buyer dobara aaye?" · "Buyers se kitna paisa baaki hai?"

## S9 Deal payments (received / balance / paid-but-short)
Screen: /sales/sold deal drawer "Payments": received so far, balance, receipts table · Endpoints: GET /sales/deals (rows + payments), POST/PUT/DELETE /sales/deals/{id}/payments · Code: `domain/sales.go:147-150` (PaymentReceived = running total), `:184` PaymentBalance; `adapters/postgres/deal_repository.go:329` RecordDealPayment, create seeds payment_received = advance_amount (`:660`).
Formula: received = `sales_deals.payment_received` (running column: advance seed + each receipt delta). Balance = greatest(sales_value - coalesce(payment_received,0), 0). Ledger = `sales_deal_payments` (received_on, amount_rupees), started 30/08/2026.
Traps: (1) NULL payment_received = not tracked (old sheet deals), not "unpaid". (2) received != advance + ledger on some deals (edited advance, float seeds): quote the running column, show ledger separately. (3) Overpaid shows balance 0, never negative. (4) Status is not derived from money: a Deal Closed can be paid short. "Paid-but-short" = closed AND 0 < received < sales_value.
SQL:
```sql
WITH p AS (SELECT deal_id, sum(amount_rupees) led FROM sales_deal_payments GROUP BY 1)
SELECT count(*) FILTER (WHERE payment_received IS NULL) not_tracked, round(sum(payment_received)) received, round(sum(p.led)) ledger,
  round(sum(greatest(sales_value-payment_received,0)) FILTER (WHERE payment_received IS NOT NULL)) balance_tracked,
  count(*) FILTER (WHERE payment_received>0 AND payment_received<sales_value) paid_short,
  count(*) FILTER (WHERE payment_received>sales_value) overpaid
FROM sales_deals d LEFT JOIN p ON p.deal_id=d.id WHERE status='Deal Closed';
```
stg 24/09/2026: 40 not tracked; received Rs 49,08,759; ledger Rs 10,11,269 (11 receipts, 9 deals, 30/08-23/09/2026); balance on tracked deals Rs 4,50,175; paid-short 3 (Mahendran 16/09/2026 Rs 10,000 of 4,39,875; Al Madina 29/08/2026; Kartik 27/12/2025); overpaid 2 (The Meat Chop 18/09/2026 +Rs 30; Mahendran 01/04/2026 value 0).
CEO asks: "Which buyers haven't paid in full?" · "How much did we receive this month?" (ledger by received_on) · "Kaunse deal ka paisa baaki hai?" · "Mahendran ka kitna pending hai?"

## S10 Sold weight bands
Screen: /sales/sold "Sold by weight" (40+, 35-40, 20-35, <20; measured / load average / estimated / unweighed) · Code: `domain/sold_weight_bands.go:32` (edges: <20, <35, <40, else 40+), `:139` BuildSoldWeightBands; weights `overview_repository.go:515` measuredSoldWeightsSQL.
Formula: per closed deal, tagged allocation weights (status 'tagged', weight_kg not null) sorted ascending are handed to live lines in line order (up to line animals) = measured. Rest of a line: if line kg>0, avg = (line kg - claimed kg)/rest (if <=0, line kg/animals) = load_average; else estimated_weight_kg/animals or estimated_weight_band = estimated; else unweighed. Total = S2 animals.
SQL (verified, reproduces exactly):
```sql
WITH d AS (SELECT id FROM sales_deals WHERE status='Deal Closed'),
w AS (SELECT a.sales_deal_id id, a.weight_kg kg, row_number() OVER (PARTITION BY a.sales_deal_id ORDER BY a.weight_kg, a.allocation_id) rk
  FROM goat_sale_allocations a JOIN d ON d.id=a.sales_deal_id WHERE a.status='tagged' AND a.weight_kg IS NOT NULL),
ln AS (SELECT x.deal_id id, x.line_no, round(coalesce(x.animal_count, coalesce(x.male_count,0)+coalesce(x.female_count,0)))::int n,
  x.total_weight_kg kg, x.estimated_weight_kg ekg, x.estimated_weight_band eb FROM sales_deal_lines x JOIN d ON d.id=x.deal_id WHERE x.product_type IN ('Goat','Sheep')),
ln2 AS (SELECT *, sum(n) OVER (PARTITION BY id ORDER BY line_no)-n lo, sum(n) OVER (PARTITION BY id ORDER BY line_no) hi FROM ln WHERE n>0),
meas AS (SELECT w.id, w.kg, ln2.line_no FROM w LEFT JOIN ln2 ON ln2.id=w.id AND w.rk>ln2.lo AND w.rk<=ln2.hi),
cl AS (SELECT ln2.id, ln2.line_no, ln2.n, ln2.kg, ln2.ekg, ln2.eb, count(m.kg) taken, coalesce(sum(m.kg),0) ckg
  FROM ln2 LEFT JOIN meas m ON m.id=ln2.id AND m.line_no=ln2.line_no GROUP BY 1,2,3,4,5,6),
r AS (SELECT 'measured' src, kg, 1 cnt, NULL eb FROM meas
  UNION ALL SELECT 'load_average', CASE WHEN (kg-ckg)/(n-taken)>0 THEN (kg-ckg)/(n-taken) ELSE kg/n END, n-taken, NULL FROM cl WHERE n>taken AND kg>0
  UNION ALL SELECT 'estimated', ekg/n, n-taken, eb FROM cl WHERE n>taken AND coalesce(kg,0)=0 AND (ekg>0 OR eb IS NOT NULL)
  UNION ALL SELECT 'unweighed', NULL, n-taken, NULL FROM cl WHERE n>taken AND coalesce(kg,0)=0 AND ekg IS NULL AND eb IS NULL)
SELECT coalesce(eb, CASE WHEN kg IS NULL THEN 'unweighed' WHEN kg<20 THEN 'under_20' WHEN kg<35 THEN 'from_20_to_35' WHEN kg<40 THEN 'from_35_to_40' ELSE 'at_or_above_40' END) band,
  sum(cnt) total, sum(cnt) FILTER (WHERE src='measured') measured, sum(cnt) FILTER (WHERE src='load_average') load_avg, sum(cnt) FILTER (WHERE src='estimated') est
FROM r GROUP BY ROLLUP(1) ORDER BY 1;
```
stg 24/09/2026: total 688 (160 measured, 476 load average, 52 estimated, 0 unweighed); 40+ 31, 35-40 126, 20-35 486, <20 45.
Trap: only 160 have a real scale weight; say "mostly load averages".
CEO asks: "How many sold animals were above 40 kg?" · "What weight are we selling at?" · "40 kilo se upar kitne bike?" · "Kitne janwar 35 se kam weight pe bech diye?"

## S11 Load-wise sales
Screen: /sales/loads (Load-wise) KPIs Purchased (sold · mortality · remaining), Purchase value (costed/loads), Sold value, Profit/loss incl. stock; per-load table · Endpoint: GET /procurement/loadwise-sales?park_id= (newest 60 loads) · Code: `backend/internal/procurement/adapters/postgres/loadwise_repository.go`, `backend/internal/procurement/adapters/postgres/loadwise_stock_weight.go`, `backend/internal/farmvaluation/sql.go`, `domain/loadwise.go` FinalizeLoadwise/profitLoss.
Formula: per-load rows = `references/load-wise-sales.sql` (run it for sold counts/value and sale/purchase weight facts; do not rewrite). Purchased = declared expected_count if >0 else linked+prior. Purchase value = animal_cost+transport+other (nil if animal_cost nil). Sold value = actual closed-deal value plus prior sold value. Remaining stock value = latest/current weight x Sales Config Farm valuation Rs/kg by stage, species and gender; an unweighed remaining animal carries its species average inside the load, else whole-load average. If no current weight exists but the load is part-sold, remaining animals carry sold animals' average sale weight x Sales Config Rs/kg. If no current weight and no sale weight exists, stock is not valued and position reads Rs 0 with the not-valued basis. Profit = sold value + stock value - purchase value (nil when uncosted). Summary sums served rows.
SQL: `SELECT count(*), sum(purchased), sum(sold), sum(dead), sum(remaining), sum(sold_value_rs) FROM (<load-wise-sales.sql>) q;` For exact stock value/profit, use the app endpoint/read path; do not recreate the live-weight valuation by hand unless also copying `loadwise_stock_weight.go` + `farmvaluation/sql.go`.
stg 24/09/2026 pre-weight-valuation baseline: 9 loads, purchased 659, sold 239, dead 29, remaining 391, sold value Rs 34,33,112, purchase value Rs 60,10,757 (9/9 costed). Old remaining/profit estimates based on average sold price are superseded.
Traps: sold here includes pre-GoatOS prior outcomes; profit includes unsold stock at current weight x configured Rs/kg, say "unrealised"; never value animals still on farm from one sold animal's price.
CEO asks: "Which load made the most profit?" · "How many from load 131 are still left?" · "Load-wise kitna munafa hua?" · "Load 128 ke kitne bache hain?"

## S12 Farm value (live stock valuation)

> **SUPERSEDED 2026-09-25 (migrations 000424/000425):** valuation buckets are now `<stage>_<gender>` (e.g. `fattening_male`, `adult_female`; `adult_male_buck` is gone) and the stage an animal is valued in is authored data (`sales_valuation_assumptions.stages[].matches`, matched on normalized `management_stage`, milk cohort first). The SQL in this section predates that and now returns 0 animals / Rs 0. Until it is rewritten, reproduce farm value from `backend/internal/sales/adapters/postgres/overview_repository.go` `farmValuationSQL` (tenant = the one tenant; farm filter = its `%s` predicate) and check the total against the Farm value page.
Screen: /sales/farm-value "Farm value" total, meat kg, bucket cards, not-valued list · Endpoint: GET /sales/overview (farm_valuation) · Code: `overview_repository.go:161` farmValuation, `:553` farmValuationSQL; rates from `sales_valuation_assumptions.buckets` (edited on /sales/config, PUT /sales/valuation-assumptions).
Formula: live goats (lifecycle not dead/sold/culled/transferred/lost/merged/inactive, not merged) bucketed: F2/F2-Male/F2-Female -> fattening; Mother -> adult female; adult by sex; K0-K3 by milk_cohort/stage; ICU-Kid -> K2; ICU by sex; else unmapped (not valued). Value = count x kg x Rs/kg; fattening kg = avg latest VERIFIED RFID weight of fattening animals; others fixed kg.
SQL: run farmValuationSQL from overview_repository.go:553 with $1 = tenant (`(SELECT tenant_id FROM sales_deals LIMIT 1)`), `%s` removed.
stg 24/09/2026: 1,562 live, 1,504 valued, 58 not valued; fattening 644 x 25.4 kg x Rs 450 = Rs 73,62,657 (317 weighed); adult females 763 x 40 x 600 = Rs 1,83,12,000; adult males 38 x 60 x 500 = Rs 11,40,000; K2 27 = Rs 1,08,000; K3 32 = Rs 2,40,000; total Rs 2,71,62,657.
Traps: an assumption-driven estimate, not a market price; adult females dominate. The page's "over 35 kg sale-ready" card comes from weighing (GET shed weights), not from this SQL.
CEO asks: "What is our herd worth today?" · "Value of fattening stock?" · "Farm ki total value kitni hai?" · "Abhi ke stock ka kitna paisa banta hai?"

## S13 Deals ledger
Screen: /sales/sold ledger (last card): date, farm, buyer, product (Mixed when lines differ), breed, animals, kg, value, status; total = whole-filter count · Endpoint: GET /sales/deals?farm=&limit=&offset= · Code: `deal_repository.go:101` ListDeals (ALL statuses, `ORDER BY sale_date DESC, id`).
Trap: the ledger count includes failed/open deals (stg 77) while KPIs use closed only (76).
SQL: `SELECT sale_date, farm, buyer_name, product_type, breed, animal_count, total_weight_kg, sales_value, status FROM sales_deals ORDER BY sale_date DESC LIMIT 10;`
CEO asks: "Show the last 10 sales." · "Any deals still open?" · "Last hafte ke deals dikhao." · "Koi deal fail hui kya?"

## S7a Buyer board columns (place, products, animals)
Screen: /sales/sold "Buyers" table columns Buyer, Place, Products, Deals, Animals, Revenue, Share % (sales-sold.tsx:262-300) · Endpoint: GET /sales/overview?farm= -> `buyers[]` {buyer_name, buyer_place, product_types, deals, animals, revenue, share_pct} · Code: `backend/internal/sales/domain/overview_build.go:116-131` (grouping), `:163-184` (share, sort, cut to MaxOverviewBuyers=25); deals read `overview_repository.go:247` (`ORDER BY sale_date, id`).
Formula: key = raw buyer_name. Place = FIRST non-empty buyer_place in sale_date,id order. Products = distinct line product_type (incl. Manure). Animals = S2 line animals. Share = revenue / whole-filter revenue x 100 (computed before the 25-row cut). Sort revenue desc, then name. Web pages the 25 rows client-side.
Filters->SQL: farm toggle -> `d.farm='CBE'|'CPT'`.
SQL:
```sql
WITH L AS (SELECT d.id, d.sale_date, d.buyer_name, d.buyer_place, x.product_type pt,
  CASE WHEN x.product_type IN ('Goat','Sheep') THEN coalesce(x.animal_count, coalesce(x.male_count,0)+coalesce(x.female_count,0)) ELSE 0 END n
  FROM sales_deals d JOIN sales_deal_lines x ON x.deal_id=d.id WHERE d.status='Deal Closed')
SELECT buyer_name, count(DISTINCT id) deals, sum(n) animals, string_agg(DISTINCT pt,' · ' ORDER BY pt) products,
  (array_agg(buyer_place ORDER BY sale_date, id) FILTER (WHERE coalesce(buyer_place,'')<>''))[1] place
FROM L GROUP BY 1 ORDER BY animals DESC LIMIT 3;
```
stg 24/09/2026: Mahendran 18 deals, 261 animals, Goat · Sheep, place Coimbatore (later deals say "Pollachi, TN"); Moshin 3 / 80 / Sheep / Bangalore; Kartik 1 / 50 / Goat / Coimbatore.
Traps: place is the OLDEST non-empty value, not the latest; buyers beyond 25 are dropped from the board but still in share denominators.
CEO asks: "Which buyers take goats vs sheep?" · "How many animals has Mahendran bought?" · "Mahendran ne kitne janwar liye?" · "Kaunsa buyer sirf bhed leta hai?"

## S8a Buyer analytics table (per buyer row)
Screen: /sales/buyer-analytics table (buyer-table.tsx:60-150): Buyer (+category · place), Phone, Purchases (+products), Animals, Revenue (+share %), Repeat tag + cadence ("N repeat purchases", "every ~D days"), First sale, Last sale (+ "D days ago"), Outstanding; KPI "Buyers" also counts not-in-register (sales-buyer-analytics.tsx:108-142); paged by `limit/offset` over `total_buyers` · Endpoint: GET /procurement/buyer-analytics?farm=&limit=&offset= -> `buyers[]`, `summary`, `total_buyers`, `phones_visible` · Code: `backend/internal/procurement/adapters/postgres/buyer_analytics_repository.go:44` closedBuyerDealsSQL; `domain/buyer_analytics.go:203-296` BuildBuyerAnalytics.
Formula: buyer key as S8, BUT a buyer_vendor_id counts only if that vendor still exists in the register (`LEFT JOIN vendor direct`); otherwise the deal falls back to the unique-business-name match, then `name:<normalised>`. Register buyers show register name/phone/record_type/city; name-only buyers show the NEWEST deal's spelling and place. Purchases = closed deals; animals = sum line animals; outstanding = sum greatest(value - coalesce(received,0),0). Repeat = purchases>=2; repeat_purchases = purchases-1; avg_days_between = floor((last-first)/repeat_purchases) (nil if span 0); days_since_last = today(UTC date) - last. Sort: last sale desc, revenue desc, name.
Filters->SQL: farm -> `d.farm = 'CBE'|'CPT'`.
SQL (Go-faithful key):
```sql
WITH v AS (SELECT vendor_id, lower(regexp_replace(btrim(business_name),'\s+',' ','g')) k FROM procurement_vendors),
nm AS (SELECT k, CASE WHEN count(*)=1 THEN min(vendor_id::text) END vid FROM v GROUP BY k),
d AS (SELECT d.*, coalesce(dv.vendor_id::text, nm.vid) vid,
  (SELECT sum(CASE WHEN l.product_type IN ('Goat','Sheep') THEN coalesce(l.animal_count, coalesce(l.male_count,0)+coalesce(l.female_count,0)) ELSE 0 END) FROM sales_deal_lines l WHERE l.deal_id=d.id) an
  FROM sales_deals d LEFT JOIN procurement_vendors dv ON dv.vendor_id=d.buyer_vendor_id
  LEFT JOIN nm ON dv.vendor_id IS NULL AND nm.k=lower(regexp_replace(btrim(d.buyer_name),'\s+',' ','g'))
  WHERE d.status='Deal Closed'),
b AS (SELECT coalesce(vid,'name:'||lower(regexp_replace(btrim(buyer_name),'\s+',' ','g'))) bk, max(buyer_name) nm, count(*) purchases, sum(an) animals,
  round(sum(sales_value)) rev, round(sum(greatest(sales_value-coalesce(payment_received,0),0))) outstanding, min(sale_date) first_sale, max(sale_date) last_sale,
  CASE WHEN count(*)>=2 THEN (max(sale_date)-min(sale_date))/(count(*)-1) END avg_gap_days, current_date - max(sale_date) days_since, bool_or(vid IS NOT NULL) in_register
  FROM d GROUP BY 1)
SELECT nm, purchases, animals, rev, round(100*rev/sum(rev) OVER (),1) share, outstanding, first_sale, last_sale, avg_gap_days, days_since, in_register
FROM b ORDER BY last_sale DESC, rev DESC;
```
stg 24/09/2026: 30 buyers (Go-faithful; S8's simpler key gives 31 because one Mahendran deal and the Al Madina deal point at vendor ids no longer in the register), 18 name-only (not in register). Top rows: The Meat Chop 2 purchases, 10 animals, Rs 1,71,828, every ~5 days, last 23/09/2026; Mahendran 18 purchases, 261 animals, Rs 30,13,196 (33.1%), outstanding Rs 19,66,157, every ~16 days, last 16/09/2026.
Traps: dangling buyer_vendor_id silently falls back to name matching; outstanding counts untracked (NULL) receipts as owed (see S9); phone column hidden unless `phones_visible`; name/phone come from the register, not the deal.
CEO asks: "Who bought from us most recently?" · "How often does Mahendran buy?" · "Kaunsa buyer kitne din mein wapas aata hai?" · "Kis buyer ne sabse zyada din se kuch nahi liya?"

## S11a Load-wise per-load columns & charts
Screen: /sales/loads table (loadwise-section.tsx:405-540: Load, Farm, Purchased, Sold (prior tooltip), Mortality (prior tooltip), Remaining, Unaccounted tag, Purchase value, Landed Rs/kg, Sold value (* = some sold unpriced), Profit/loss) and charts (:228-400): counts (purchased/sold/mortality/remaining), value (purchase/sold/profit), weight (avg purchase kg / avg sale kg / current avg kg), per-kg (landed vs sale), fattening (fattening days / days on farm so far); park chip `?park=`; row opens cost drawer (P4) · Endpoint: GET /procurement/loadwise-sales?park_id= -> `loads[]` {purchased, sold, prior_sold, mortality, prior_dead, other_exits, remaining, unaccounted, purchase_value, purchase_weight_kg, landed_price_per_kg, sold_value, sold_priced, profit_loss, avg_purchase_weight_kg, avg_sale_weight_kg, sold_weighed_animals, sale_price_per_kg, fattening_days, days_on_farm_so_far, arrived_on}; current avg kg from GET /weighing/shed-weights by_load (see adg-analytics.md#7) · Code: `backend/internal/procurement/adapters/postgres/loadwise_repository.go:234` (sold_priced), `:278` (arrived_on, fattening_days); `domain/loadwise.go:305-345` FinalizeLoadwise (unaccounted :319, avg weights :329-331, sale Rs/kg :335, days on farm :337 / DaysOnFarmSoFar :278).
Formula: unaccounted = purchased - sold - mortality - other_exits - remaining (non-zero = register vs declared count mismatch). avg purchase kg = purchase_weight_kg / purchased. avg sale kg / sale Rs/kg = from ONE weighed-sale sample (legacy procurement_loads.sold_* if set, else tagged allocations with weight on single-price closed deals). fattening_days = procurement_loads.fattening_days (legacy sold-out loads only). days_on_farm_so_far = IST today - arrived_on, only while remaining>0. Sold value asterisk when sold > sold_priced (animals on zero-value deals).
Filters->SQL: park chip -> load's farm (`context->>'farm'`) = park code.
SQL: `SELECT load_no, farm, purchased, sold, sold_prior, dead, other_exits, remaining, purchased-sold-dead-other_exits-remaining unaccounted, avg_purchase_kg, avg_sale_kg, sale_price_per_kg, sale_price_basis, landed_cost_per_kg, fattening_days_final, days_on_farm_so_far FROM (<references/load-wise-sales.sql>) q;`
stg 24/09/2026: unaccounted 0 on all 9 loads. 126 CBE: 23.5 -> 39.5 kg, sale Rs 428.2/kg (tagged) vs landed Rs 449.7/kg, 135 days on farm, 50 left; 128 CBE: sale Rs 332.2/kg (1 tagged sale) vs landed 463.5; legacy 113/100/101: fattening 183/203/204 days, sale Rs 435.0/431.0/423.8 per kg; 131 CPT: 93 days so far, 63 left.
Traps: sale Rs/kg is a small sample (sold_weighed_animals shown "x / y weighed out"); fattening days only exist for legacy loads; days on farm counts from ARRIVAL not purchase date; current avg kg needs a single-load pen tag.
CEO asks: "Are we selling each load above its landed cost per kg?" · "How long has load 131 been on the farm?" · "Load 126 ka kilo rate khareed se zyada mila kya?" · "Kaunse load mein ginti match nahi ho rahi?"

## S13a Deal detail drawer (lines)
Screen: /sales/sold ledger row -> read-only deal drawer (sales-record-drawer.tsx): header (date, buyer, vendor link, status), "Lines" table (product, breed, animals, total kg, value), then Payments (S9) · Endpoint: GET /sales/deals?farm=&limit=&offset= (lines + payments ride on each deal; drawer = `?deal_id=`) · Code: `backend/internal/sales/adapters/postgres/deal_repository.go:101` ListDeals; line animals `domain/deal_lines.go:51`.
Formula: rows of sales_deal_lines by line_no; animals = coalesce(animal_count, male+female); deal value = sum line sales_value (RollupLines).
SQL: `SELECT d.sale_date, d.buyer_name, x.line_no, x.product_type, x.breed, coalesce(x.animal_count, coalesce(x.male_count,0)+coalesce(x.female_count,0)) animals, x.total_weight_kg, x.sales_value FROM sales_deals d JOIN sales_deal_lines x ON x.deal_id=d.id ORDER BY d.sale_date DESC, d.id, x.line_no LIMIT 10;`
stg 24/09/2026: 77 deals, every one has exactly 1 line (no multi-line deals yet); latest 23/09/2026 The Meat Chop, Sheep Kenguri, 5 animals, 202.7 kg, Rs 87,161.
Traps: "Mixed" in the ledger product column only appears once a deal has lines of different products (none on stg); drawer is read-only (edits live on /sales/config).
CEO asks: "What exactly was in the Meat Chop deal?" · "What weight did Keerthan's goats go at?" · "Meat Chop wali deal mein kya kya tha?" · "Keerthan ko kitne kilo ki bakri di?"


## S14 Farm-born sales (on farm today, sold in period, breakdowns, sold ledger)
Screen: /sales/farm-born: KPIs On farm, Sold (period), Revenue (+ "N unpriced"), Avg price/animal; cards By breed, By sex, By stage, By pen (10/page); Sold animals ledger (tag, breed, sex, stage, pen+park, sale date, buyer, value); filter bar: period (Sold between), park, pen, species, breed, sex, stage · Endpoint: GET /procurement/farm-born-sales?from=&to=&park_id=&pen=<shed|partition>&species=&breed=&sex=&stage=&limit=&offset= · Code: web `features/procurement/sales-farm-born.tsx:194-222` KPIs, `:235-253` breakdowns, `:263` ledger, `:379-459` filters; `farm-born-sold-table.tsx:36-80` columns; backend `procurement/adapters/postgres/farm_born_sales_repository.go:40` farmBornPopulationSQL, `:120` farmBornAnimalsSQL, `:155` farmBornOptionsSQL; `procurement/domain/farm_born_sales.go:288` BuildFarmBornSales, `:372` AvgPrice, `:234` DefaultFarmBornWindow, `:415` bucket sort; `app/farm_born_sales_service.go:63`.
Formula: population = `goats.origin_type='birth'`, not merged. The bucket is sold if lifecycle_status='sold' OR exit_reason='sold'. It is on_farm if lifecycle_status is alive/sick/under_treatment/quarantine/icu. Anything else is 'other' (not shown). Sale date = the closed deal's sale_date through the tagged allocation, else exited_at as an IST date. Animal value = deal sales_value / the deal's tagged-allocation count, and only when sales_value>0 (else "unpriced"). On farm = today, whatever the period. Sold = sale date within [from,to]. Revenue = sum of animal values. Avg price = revenue / priced sold. Breakdown rows show on_farm, sold, share% (row sold / all sold) and revenue. Rows sort by sold desc, then on_farm desc. Pen = allocation snapshot (park/shed/partition when tagged), else the goat's current shed + goat_shed_partitions.
Filters->SQL: period -> `sale_date BETWEEN :from AND :to`. Default is one calendar month back to today (for 24/09 that is 24/08-24/09). park -> `park_id`. pen -> `shed_id` + normalised partition (lower, "part " stripped). species/sex -> lower(). breed -> lower(btrim(breed)). The stage option "F2" (Fattening) expands to F2/F2-Male/F2-Female (`platform/herdstage/fattening_fold.go:31`).
Traps: (1) The population is origin_type only. 333 live + 57 sold animals with NULL origin appear on neither Farm-born nor Load-wise, so do not call farm-born + procured "the herd". (2) The period binds sold only, so On farm never changes with dates. (3) Value is an equal split of the deal over its tagged animals, not a weighed price. (4) Pen for a sold animal is where it was sold from.
SQL:
```sql
WITH ds AS (SELECT a.goat_id, a.park_id, d.sale_date, d.buyer_name,
   CASE WHEN d.sales_value>0 THEN d.sales_value/c.t END share
 FROM goat_sale_allocations a JOIN sales_deals d ON d.id=a.sales_deal_id AND d.status='Deal Closed'
 JOIN (SELECT sales_deal_id, count(*)::numeric t FROM goat_sale_allocations WHERE status='tagged' GROUP BY 1) c USING (sales_deal_id)
 WHERE a.status='tagged'),
p AS (SELECT g.goat_id, lower(g.species) sp, coalesce(g.breed,'') breed, lower(g.sex) sex, coalesce(g.management_stage,'') stage,
   CASE WHEN g.lifecycle_status='sold' OR g.exit_reason='sold' THEN 'sold'
        WHEN g.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu') THEN 'on_farm' ELSE 'other' END b,
   coalesce(ds.park_id,g.park_id) park_id, coalesce(ds.sale_date,(g.exited_at AT TIME ZONE 'Asia/Kolkata')::date) sale_date, ds.share
 FROM goats g LEFT JOIN ds ON ds.goat_id=g.goat_id
 WHERE g.merged_into_goat_id IS NULL AND g.origin_type='birth')
SELECT count(*) FILTER (WHERE b='on_farm') on_farm,
  count(*) FILTER (WHERE b='sold' AND sale_date BETWEEN :from AND :to) sold,
  count(share) FILTER (WHERE b='sold' AND sale_date BETWEEN :from AND :to) priced,
  round(sum(share) FILTER (WHERE b='sold' AND sale_date BETWEEN :from AND :to)) revenue,
  round(avg(share) FILTER (WHERE b='sold' AND sale_date BETWEEN :from AND :to)) avg_price
FROM p;   -- breakdown: add breed,sp | sex | stage | park_id to SELECT and GROUP BY
```
Ledger: the same CTEs, plus a tag = primary `animal_identifier_1`, else `_2`, active, newest (`DISTINCT ON goat_id`); `ORDER BY sale_date DESC, tag`.
stg 24/09/2026 (default window 24/08-24/09): on farm 512; sold 88 (all 88 priced; every farm-born sale falls 29/08-22/09/2026, so all-time = window); revenue Rs 10,02,648; avg Rs 11,394/animal. Park: Coimbatore 187 on farm / 87 sold / Rs 9,86,179; Channapatna 325 / 1 / Rs 16,469. Breed (sold): Malai 38 (Rs 3,98,594), Osmanabadi 22, Anantapur Sheep 17 (274 on farm), Malai x Sojat 5, Beetal 3 (120 on farm). Sex: female 56 sold / 362 on farm; male 32 / 150. Stage: F2-Female 44, F2-Male 32, Non-Pregnant 12. Top pens: Godel 1 Part 7 24 sold; Godel 2 Part 1 14; Yashoda 3 10. Newest sale: 22/09/2026, Anantapur male to Keerthi, Rs 16,469.
CEO asks: "How many farm-born animals did we sell this month?" · "What do our own-bred animals fetch per head?" · "Farm pe paida hue kitne bakre bike aur kitne ka?" · "Ghar ke bachhe kitne abhi farm pe hain?"

## S15 Farm value by farm and sex split (extends S12)

> **SUPERSEDED 2026-09-25 (migrations 000424/000425):** valuation buckets are now `<stage>_<gender>` (e.g. `fattening_male`, `adult_female`; `adult_male_buck` is gone) and the stage an animal is valued in is authored data (`sales_valuation_assumptions.stages[].matches`, matched on normalized `management_stage`, milk cohort first). The SQL in this section predates that and now returns 0 animals / Rs 0. Until it is rewritten, reproduce farm value from `backend/internal/sales/adapters/postgres/overview_repository.go` `farmValuationSQL` (tenant = the one tenant; farm filter = its `%s` predicate) and check the total against the Farm value page.
Screen: /sales/farm-value: farm toggle (All/CBE/CPT) scopes every valuation figure. Bucket cards for fattening/K0-K3 show "N male · N female". The "Total meat" KPI and the "N valued · N not valued · N live" tag · Endpoint: GET /sales/overview?farm= (farm_valuation.buckets[].male_count/female_count/meat_kg, total_meat_kg, valued_animals, excluded_animals, total_animals, not_valued[]) · Code: web `sales-farm-value.tsx:94-99` KPIs, `:140-170` bucket grid, `:158` SEX_SPLIT_BUCKETS, `:226` getSalesOverview({farm}); backend `sales/adapters/postgres/overview_repository.go:161` farmValuation, `:216` farmValuationQuery (farm predicate), `:553` farmValuationSQL (counts CTE with sex FILTERs; rates from sales_valuation_assumptions, else seeded VALUES).
Formula: same as S12. Farm scope = `upper(park.location_code)=:farm`, or farm_id's code when park is NULL. It is applied BEFORE the fattening average weight, so each farm has its own fattening kg. Meat kg = count x kg. Total meat = sum of meat kg. Sex split = count by `lower(btrim(sex))`. Missing sex is counted in the card total but in neither figure.
SQL (run with `-v farm=CBE|CPT|''`):
```sql
WITH lw AS (SELECT DISTINCT ON (lower(btrim(scanned_identifier))) lower(btrim(scanned_identifier)) k, weight_kg::float8 kg, accepted_at
  FROM weighing_observations WHERE verification_status='verified' AND btrim(coalesce(scanned_identifier,''))<>'' ORDER BY 1, accepted_at DESC),
gw AS (SELECT DISTINCT ON (i.goat_id) i.goat_id, lw.kg FROM goat_identifiers i JOIN lw ON lw.k=lower(btrim(i.identifier_value))
  WHERE i.status='active' AND i.identifier_type IN ('animal_identifier_1','animal_identifier_2') ORDER BY i.goat_id, lw.accepted_at DESC),
c AS (SELECT lower(btrim(coalesce(g.sex,''))) sex, gw.kg,
  CASE WHEN g.management_stage IN ('F2','F2-Male','F2-Female') THEN 'fattening'
   WHEN s.n='MOTHER' THEN 'adult_female' WHEN g.age_band='adult' AND g.sex='female' THEN 'adult_female'
   WHEN g.age_band='adult' AND g.sex='male' THEN 'adult_male_buck'
   WHEN g.milk_cohort='K1' OR g.management_stage='K1' THEN 'K1' WHEN g.milk_cohort='K2' OR g.management_stage='K2' THEN 'K2'
   WHEN g.milk_cohort='K3' OR g.management_stage='K3' THEN 'K3' WHEN g.milk_cohort='K0' OR g.management_stage='K0' THEN 'K0'
   WHEN s.n='ICUKID' THEN 'K2' WHEN s.n='ICU' AND g.sex='female' THEN 'adult_female' WHEN s.n='ICU' AND g.sex='male' THEN 'adult_male_buck'
   ELSE 'unmapped' END bucket
  FROM goats g CROSS JOIN LATERAL (SELECT upper(regexp_replace(btrim(coalesce(g.management_stage,'')),'[^A-Za-z0-9]+','','g')) n) s
  LEFT JOIN gw ON gw.goat_id=g.goat_id LEFT JOIN locations pk ON pk.location_id=g.park_id LEFT JOIN locations fm ON fm.location_id=g.farm_id
  WHERE g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive') AND g.merged_into_goat_id IS NULL
    AND (:'farm'='' OR coalesce(upper(pk.location_code), upper(fm.location_code))=upper(:'farm'))),
r AS (SELECT b.* FROM sales_valuation_assumptions a CROSS JOIN LATERAL jsonb_to_recordset(a.buckets) b(bucket text, label text, fixed_weight_kg float8, price_per_kg float8, display_order int)),
fw AS (SELECT avg(kg) a FROM c WHERE bucket='fattening'),
k AS (SELECT bucket, count(*) n, count(*) FILTER (WHERE sex='male') m, count(*) FILTER (WHERE sex='female') f FROM c GROUP BY 1)
SELECT r.bucket, coalesce(k.n,0) n, k.m, k.f, round(coalesce(r.fixed_weight_kg, fw.a)::numeric,1) kg, r.price_per_kg,
  round((coalesce(k.n,0)*coalesce(r.fixed_weight_kg,fw.a,0))::numeric,1) meat_kg,
  round((coalesce(k.n,0)*coalesce(r.fixed_weight_kg,fw.a,0)*r.price_per_kg)::numeric) value
FROM r LEFT JOIN k USING (bucket) CROSS JOIN fw ORDER BY r.display_order;
```
stg 24/09/2026 (all figures reproduce S12). All farms: fattening 644 (452 M / 192 F); K2 27 (13/14); K3 32 (17/15); total meat ~49,858 kg. CBE: 416 adult F, 26 adult M, fattening 328 x 24.6 kg (242 M / 86 F) = Rs 36,28,212; K2 22; total Rs 1,44,80,212; all 58 not-valued animals are here (unmapped). CPT: 347 adult F, 12 adult M, fattening 316 x 26.1 kg (210 M / 106 F) = Rs 37,11,585; K2 5; K3 32; total Rs 1,26,59,585.
Traps: CBE + CPT (Rs 2,71,39,797) != All (Rs 2,71,62,657), because the fattening average weight is taken per scope. Quote the scope asked for. K0/K1 are 0 today.
CEO asks: "What is Coimbatore's herd worth?" · "How many fattening males vs females?" · "CPT farm ki value kitni hai?" · "Fattening mein kitne nar aur kitni mada hain?"

## S16 Market price survey (latest prices, trend, coverage)
Screen: /sales/market-analytics: KPIs Cities, Days recorded, Latest survey date, Today's calls done/total; window chips 30/90 (default)/180/365 days; "Latest prices" table (city x question, price + ▲▼ vs previous, recorded on); Trend chart (one line per city·unit for the chosen question; question chips, city chips) · Endpoints: GET /market/analytics?from=&to= (days, latest[].price/previous_price/business_date, series[].points[]), GET /app/market/survey (done, pending) · Code: web `features/procurement/market-analytics.tsx:65-70` window, `:140-160` KPIs, `:184-242` latest table, `:244-290` trend; backend `market/adapters/http/handler.go:302` GetAnalytics; `market/app/service.go:188` (default 90 days, max 400), `:111-140` GetDay (done/pending); `market/domain/market.go:397` BuildAnalytics (series key = city+question+unit; Days = distinct business_date; latest per city+question, previous = the prior entry in the window), `:315` BuildDayCards (done = every active question answered); `adapters/postgres/repository.go:107` entries between.
Formula: rows are `market_price_entries` (one per city, question and business_date; the city/question/unit labels are snapshotted). Cities KPI = distinct cities in the window's series. Coverage = active cities fully answered today / active cities. Cards stay hidden before call_time (default 08:00 IST) and then show 0/N.
SQL:
```sql
WITH e AS (SELECT e.*, lag(price) OVER (PARTITION BY city_id, question_id ORDER BY business_date) prev,
  row_number() OVER (PARTITION BY city_id, question_id ORDER BY business_date DESC) rn
  FROM market_price_entries e WHERE business_date BETWEEN current_date-89 AND current_date)
SELECT city_name, question_label, unit_label, business_date, price, prev, price-prev delta FROM e WHERE rn=1 ORDER BY 1,2;
-- KPIs: SELECT count(DISTINCT city_id), count(DISTINCT business_date), max(business_date) FROM market_price_entries WHERE business_date BETWEEN current_date-89 AND current_date;
-- trend: SELECT business_date, city_name, price FROM market_price_entries WHERE question_label='Goat live price' ORDER BY 1,2;
```
stg 24/09/2026: 3 cities (Bengaluru, Hyderabad, Chennai), 6 questions (goat/sheep live, carcass, offals; all Rs/kg), 108 entries on 7 days (15/09-23/09/2026), latest 23/09/2026, all recorded by Hemant. Coverage today 0/3. Goat live 23/09: Bengaluru 400 (flat), Chennai 425 (down 5), Hyderabad 420 (flat). Goat carcass: Bengaluru 780 (up 60), Chennai 830, Hyderabad 800. Sheep live: 380 / 400 / 400 (Hyderabad down 20).
Traps: (1) A 0 price is stored and plotted. Hyderabad offals 23/09 = 0 and Hyderabad goat live 16/09 = 0 mean "not quoted", not free; exclude price=0 from averages. (2) "Previous" is the previous recorded day, not yesterday. (3) A unit change starts a new series. The latest table ignores the unit, so the delta could compare two units. (4) Retired questions stay in the analytics.
CEO asks: "What is live goat price in Chennai today?" · "How has carcass price moved this month?" · "Hyderabad mein bakre ka rate kya chal raha hai?" · "Aaj market survey ho gaya kya?"

## S17 Sales config reads (valuation rates, market survey setup, reporters)
Screen: /sales/config: Valuation card (bucket label, fixed kg, Rs/kg, unsold stock price, updated) · Market survey card (call time, cities, questions+unit, active/retired) · Reporters card (who holds the phone Market module) · Endpoints: GET/PUT /sales/valuation-assumptions; GET /market/config, POST/PUT /market/cities|questions, PUT /market/config/call-time; GET/PUT /market/reporters · Code: web `valuation-section.tsx:56-121`, `market-config-section.tsx:85-179`, `market-reporters-section.tsx:34-111`, `sales-config.tsx:99-103,344-360`; backend `sales/adapters/http/sales_handler.go:66-67`, `sales/app/sales_service.go:321`; `market/adapters/http/handler.go:37-44`, `market/adapters/postgres/repository.go:56-66,136`; `workforce/adapters/postgres/market_reporters.go:16` listPeopleWithModuleSQL.
Formula: valuation = the tenant's `sales_valuation_assumptions` row. Buckets are jsonb (fixed_weight_kg NULL = measured fattening average). unsold_stock_price_rupees overrides S11's remaining value when set. A reporter = an active workforce member with a login and `person_module_access` surface='mobile', module_key='market_survey', capabilities non-empty.
SQL: `SELECT jsonb_path_query_array(buckets,'$[*].bucket'), unsold_stock_price_rupees, updated_at FROM sales_valuation_assumptions;` · `SELECT name, status FROM market_cities ORDER BY sort_order; SELECT label, unit_label, status FROM market_questions ORDER BY sort_order; SELECT call_time FROM market_survey_config;` · `SELECT m.display_name FROM workforce_members m WHERE m.status='active' AND m.user_id IS NOT NULL AND EXISTS (SELECT 1 FROM person_module_access p WHERE p.workforce_member_id=m.workforce_member_id AND p.surface='mobile' AND p.module_key='market_survey' AND cardinality(p.capabilities)>0);`
stg 24/09/2026: rates fattening measured x Rs 450; adult F 40 kg x 600; adult M 60 x 500; K0/K1 3 x 500; K2 8 x 500; K3 15 x 500; unsold price NULL; updated 20/09/2026. Market: 3 active cities, 6 active questions, call time 08:00. Reporters: 2 of 40 login users (Hemant, Manju Flokx).
CEO asks: "What rate are we using to value adult females?" · "Who does the morning market calls?" · "Farm value kis rate pe nikal rahe ho?" · "Market ka phone kaun karta hai?"

## S18 Sale tagging (animals allocated to deals)
Screen: /sales/config "Tag animals" drawer (candidates by park/shed/pen → preview → confirm) · Endpoints: GET /admin/goats/sale-locations, GET /admin/goats/sale-candidates, POST /admin/goats/sale-allocations/preview|confirm, GET /admin/goats/sale-allocations/{deal} · Code: `sales-config.tsx:367` SaleAllocationDrawer; `backend/internal/identity/adapters/http/sale_allocation_handler.go:62-65`.
Formula: each confirmed tag = `goat_sale_allocations` status='tagged' (one live row per goat), carrying weight_kg and a park/shed/partition snapshot. It feeds the S2 register count, the S10 measured weights and the S14/S11 per-animal value split.
SQL:
```sql
WITH t AS (SELECT sales_deal_id id, count(*) tagged FROM goat_sale_allocations WHERE status='tagged' GROUP BY 1),
n AS (SELECT deal_id id, sum(CASE WHEN product_type IN ('Goat','Sheep') THEN coalesce(animal_count, coalesce(male_count,0)+coalesce(female_count,0)) ELSE 0 END) n FROM sales_deal_lines GROUP BY 1)
SELECT count(*) FILTER (WHERE t.tagged>0) deals_tagged, sum(t.tagged) tagged, count(*) FILTER (WHERE t.tagged>0 AND t.tagged<n.n) under_tagged,
  count(*) FILTER (WHERE coalesce(t.tagged,0)=0 AND n.n>0 AND d.sale_date>=:since) untagged
FROM sales_deals d JOIN n USING (id) LEFT JOIN t USING (id) WHERE d.status='Deal Closed';
```
stg 24/09/2026: 160 tagged on 14 closed deals, and each of those deals is fully tagged (160 = its line animals). 0 closed deals since 01/08/2026 are untagged. The other 528 deal-line animals are pre-app sheet deals.
CEO asks: "Are all sold animals tagged to their deal?" · "Which deals are missing tags?" · "Bechne wale sab janwar tag hue kya?" · "Kis deal ke tag baaki hain?"
