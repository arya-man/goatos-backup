Sections: 0 Shared rules · 1 Direction sheet · 2 Packing · 3 Analytics Overview (KPIs, daily/item/mix/per-head, spend, status-wise, feed-by-pen) · 4 Execution tab (status, packed-vs-given, mismatch, completion table) · 5 Experiment tab + wastage · 6 Follow-up tab · 7 Feed config · 8 ceo_ai feed views (traps) · Not covered: Stock/days-left (see stock card)

Paths: `A` = backend/internal/feeddirection/adapters/postgres/analytics.go · `FU` = .../postgres/feed_follow_up.go · `UI` = apps/admin-web/features/feed/feed-analytics.tsx · copy strings = backend/internal/adminui/app/service.go:6815-6990. Tenant `'00000000-0000-4000-8000-000000000001'` (= `$T` below). Park ids: CBE Coimbatore `…-000000003001`, CPT Channapatna `…-000000003002`. All stg values run 24/09/2026 (IST "today" = 2026-09-24, "yesterday" = 2026-09-23).

## 0 Shared rules (apply to every block)
- Tables are `public.*` (readable by `mesha_ceo_readonly`). Sheet = `feed_direction_issues` (header: park_id, feed_day, workflow `normal|experiment`, state `issued|amended|locked`, amendment_count, issued_at/locked_at) 1:N `feed_direction_issue_rows` (cells at shed_id, partition_key, session_no, shed_tag_key, breed_key, feed_item_key; carries park_label `CBE`/`CPT`, shed_label, partition_label, head_count, quantity_kg).
- Live-sheet filter everywhere: `state IN ('issued','amended','locked')` AND `workflow IN ('normal','experiment')` (both workflows, maintainer 2026-08-19). At most one live issue per (park, feed_day, workflow) (unique index), so no dedupe needed.
- `quantity_kg IS NULL` ⇔ blocked cell (`blocked_reason_code` e.g. `unknown_shed_tag`, `no_ration_rate`). Blocked contributes nothing; 0 means authored zero.
- **Head counts come from the sheet, not the register**: `head_count` repeats on every session × item cell → collapse to pen-grain `(feed_day, shed_id, partition_key, shed_tag_key, breed_key)` with `MAX(head_count)`, then SUM. Summing cells inflates ~6×.
- Day clock: sheet for feed day D is issued on D-1 (normal 07:00, experiment 14:00), corrected 14:00, locked 15:30 IST (`feed_schedule_config`). Packing day P packs the sheet for feed day P+1 (`target_date` on completions = feed day).
- Analytics window: range chips 30/61/92 days ending **yesterday**; the two daily charts end **today**; max 92 days (`domain/analytics.go:290`). Park chip → `park_id = ANY($2)`; none = all parks.
- UHT Milk is NOT on the sheet: it comes from `feed_effective_external_consumption` (kg, zero heads). Included in item charts/mix/spend, excluded from day totals/per-head.

## 1 Direction sheet (/feed/direction)
**Sheet rows + summary tiles** | `GET /feed-direction/preview?park_id&target_date&shed_id&session&limit&offset` | app/service.go:534 Preview → app/lifecycle.go:613 loadServedSheet; totals domain/generate.go:329 SummarizeScope; state banner app/lifecycle.go:882 aggregateLifecycle
- Formula: stored frozen rows for (park, feed_day); if none and date ∈ [today, tomorrow] → live-generated `preview` (not in DB); beyond → empty `beyond_horizon`. Summary over whole filtered scope (not page): shed_count = distinct sheds; blocked_count = blocked cells; blocked_shed_count; total_kg_by_feed_item = Σ quantity_kg per item. Lifecycle = lowest state across workflows (issued<amended<locked), total amendments, earliest issued_at, latest locked_at.
- Filters: park (required), date (today..tomorrow), shed, session_no. Traps: rows collapsed per operational location (pen); a date with no stored sheet shows a *generated* preview you cannot reproduce in SQL; row park_label is a code (`CBE`), not `Coimbatore`.
```sql
SELECT r.park_label, i.workflow, i.state, i.amendment_count, i.locked_at AT TIME ZONE 'Asia/Kolkata' locked,
       count(DISTINCT r.shed_id) sheds, count(*) FILTER (WHERE r.quantity_kg IS NULL) blocked_cells, round(sum(r.quantity_kg),1) kg
FROM feed_direction_issues i JOIN feed_direction_issue_rows r ON r.tenant_id=i.tenant_id AND r.feed_direction_issue_id=i.feed_direction_issue_id
WHERE i.tenant_id=$T AND i.feed_day='2026-09-24' GROUP BY 1,2,3,4,5 ORDER BY 1,2;
```
stg value (24/09/2026): feed day 24/09 all 4 sheets `locked` (normal amended once); CBE normal 741.2 kg/8 sheds + experiment 548.8 kg/4 sheds = **1,290.0 kg**; CPT 493.0 + 434.2 = **927.2 kg**; 0 blocked cells. CBE by item: Bhusa 632.0, Kids Conc 456.8, Adult Conc 201.2.
Q: "What's on tomorrow's feed sheet for CBE?" / "kal CBE ki feed sheet mein kitna hai?" · "Is the sheet locked?" / "sheet lock hui kya?" · "Any blocked pens?" / "koi pen blocked hai?"

## 2 Packing (/feed/packing)
**Packing worklist + tiles (pens, blocked pens, kg per item)** | `GET /feed-packing/worklist?park_id&target_date&limit&offset` | app/service.go:613 PackingWorklist → app/lifecycle.go:516 servePacking
- Formula: same frozen sheet as §1, but the page date is the PACKING day P and the backend reads feed day P+1; both workflows unless filtered; tile totals whole-scope. Per-line status from `feed_packing_completions` (completed / pending_verification / rework→"pending").
- Traps: "today's packing" = sheet for tomorrow. If no sheet is stored, serving may freeze it (gateOrFreeze) — it's a write path, SQL just sees stored rows.
```sql
-- packing day 23/09 = feed day 24/09
SELECT c.status, count(*) FROM feed_packing_completions c WHERE c.tenant_id=$T AND c.target_date='2026-09-24' GROUP BY 1;
-- kg to pack: reuse §1 query with feed_day = packing_day + 1
```
stg value (24/09/2026): packing day 23/09 → kg to pack = §1 totals (CBE 1,290.0, CPT 927.2).
Q: "How much feed was packed today?" / "aaj kitna feed pack hua?" · "Packing done for all pens?" / "sab pen ka packing ho gaya?"

## 3 Feed Analytics > Overview (/feed/analytics)
### 3a KPI "Directed yesterday" (kg) | "Animals fed yesterday" | "Avg ration per animal" (g/head/day)
| `GET /feed-analytics/directed?park_id&date_from&date_to` (window through today; tile picks feed_day = yesterday) | A:219 directedAnalyticsDaysSQL / A:258 combined (day rows); tile UI:903-921
- Formula: live sheets both workflows → pen-grain SUM(kg), MAX(heads) → day: directed_kg = Σkg; head_days = Σ pen-grain heads; per_head_grams = Σkg×1000/Σheads. Sheet only (no milk).
- Traps: "animals fed" is the **sheet's** head count, not the register herd (`animal_current_scope`). Blocked pens drop their heads too.
```sql
WITH iss AS (SELECT feed_direction_issue_id, feed_day FROM feed_direction_issues WHERE tenant_id=$T AND feed_day='2026-09-23'
             AND state IN ('issued','amended','locked') AND workflow IN ('normal','experiment')),
pen AS (SELECT i.feed_day, r.park_label, r.shed_id, r.partition_key, r.shed_tag_key, r.breed_key, SUM(r.quantity_kg) kg, MAX(r.head_count) heads
        FROM iss i JOIN feed_direction_issue_rows r ON r.tenant_id=$T AND r.feed_direction_issue_id=i.feed_direction_issue_id
        WHERE r.quantity_kg IS NOT NULL GROUP BY 1,2,3,4,5,6)
SELECT coalesce(park_label,'ALL'), round(SUM(kg),1) kg, SUM(heads) animals, round(SUM(kg)*1000/NULLIF(SUM(heads),0),1) g_per_head
FROM pen GROUP BY ROLLUP(park_label);
```
stg value (24/09/2026): 23/09 ALL **2,227.2 kg · 1,579 animals · 1,410.5 g/head**; CBE 1,298.4 kg · 866 · 1,499.3 g; CPT 928.8 kg · 713 · 1,302.7 g.
Q: "How much feed went yesterday in CBE?" / "kal CBE mein kitna feed gaya?" · "How many animals were fed?" / "kitne janwar ko feed mila?" · "Average ration per goat?" / "ek janwar ko kitne gram feed?"

### 3b KPI "Execution verified" (%)
| `GET /feed-analytics/execution?sections=days` (window ending yesterday) | A:619 executionStatusSQL; % in UI:873-884
- Formula: over window, (packing completed + distribution completed) / (all packing + distribution rows in completed|pending_verification|rework), `target_date` in window, from `feed_packing_completions` + `feed_distribution_completions`. Rounded %.
- Traps: pens nobody touched (no completion row) are NOT in the denominator — 100% ≠ every pen fed; use 4d for not-started.
```sql
SELECT round(100.0*sum(n) FILTER (WHERE status='completed')/sum(n)) pct FROM (
  SELECT status, count(*) n FROM feed_packing_completions WHERE tenant_id=$T AND target_date BETWEEN '2026-08-25' AND '2026-09-23' GROUP BY 1
  UNION ALL SELECT status, count(*) FROM feed_distribution_completions WHERE tenant_id=$T AND target_date BETWEEN '2026-08-25' AND '2026-09-23' GROUP BY 1) s
WHERE status IN ('completed','pending_verification','rework');
```
stg value (24/09/2026): 30 days → **83%** (packing 4,599/5,897; distribution 5,169/5,915); 23/09 alone 100%.
Q: "How much feeding got verified?" / "feed ka kitna verify hua?"

### 3c KPI "Feed cost per animal yesterday" (₹) + spend tiles (7d / month / 3 mo / year) + "Daily feed expenditure" chart
| `GET /feed-analytics/stock?sections=expenditure,spend,item_expenditure` | A:2135 stockExpenditureSQL, A:2194 stockItemExpenditureSQL, A:2247 stockSpendSQL; cost/animal UI:890-899
- Formula: kg per (feed_day, park, item) = live sheet cells (states filter; no workflow filter) + external milk; ₹ = kg × per-kg of the **latest reached load** for that park+item with `depletes_from <= feed_day` (`COALESCE(per_kg_cost, total_cost/stock_kg)`, order depletes_from, purchase_date, batch_no DESC). Unpriced items drop out (not ₹0). Spend tiles: days < today; 7d = feed_day ≥ today-7; month = from 1st; quarter = ≥ today-91; year = from Jan 1. Cost/animal = yesterday ₹ ÷ 3a animals.
- Traps: it is directed kg priced, not invoices paid (`feed_purchase_payments`). Chart floored at 11/08/2026.
```sql
WITH day_item AS (SELECT feed_day, park_id, feed_item_key, SUM(kg) kg FROM (
   SELECT i.feed_day, i.park_id, r.feed_item_key, SUM(r.quantity_kg) kg FROM feed_direction_issues i
   JOIN feed_direction_issue_rows r ON r.tenant_id=$T AND r.feed_direction_issue_id=i.feed_direction_issue_id
   WHERE i.tenant_id=$T AND i.state IN ('issued','amended','locked') AND i.feed_day>='2026-01-01' AND i.feed_day<'2026-09-24' AND r.quantity_kg IS NOT NULL GROUP BY 1,2,3
   UNION ALL SELECT feed_day, park_id, feed_item_key, SUM(quantity_kg) FROM feed_effective_external_consumption
   WHERE tenant_id=$T AND park_id IS NOT NULL AND feed_day>='2026-01-01' AND feed_day<'2026-09-24' GROUP BY 1,2,3) s GROUP BY 1,2,3),
priced AS (SELECT di.feed_day, di.kg*p.per_kg spend FROM day_item di JOIN LATERAL (
   SELECT COALESCE(per_kg_cost, total_cost/NULLIF(stock_kg,0)) per_kg FROM feed_purchases
   WHERE tenant_id=$T AND delivery_status='reached' AND park_id=di.park_id AND feed_item_key=di.feed_item_key AND depletes_from<=di.feed_day
   ORDER BY depletes_from DESC, purchase_date DESC, batch_no DESC LIMIT 1) p ON p.per_kg IS NOT NULL)
SELECT round(SUM(spend) FILTER (WHERE feed_day='2026-09-23')) yday, round(SUM(spend) FILTER (WHERE feed_day>=date '2026-09-24'-7)) wk,
       round(SUM(spend) FILTER (WHERE feed_day>='2026-09-01')) mtd, round(SUM(spend) FILTER (WHERE feed_day>=date '2026-09-24'-91)) qtr, round(SUM(spend)) ytd FROM priced;
```
stg value (24/09/2026): yesterday **₹65,107**; 7d ₹4,59,810; month ₹15,23,417; 3 mo = year ₹23,37,928. Cost/animal yesterday = 65,107/1,579 = **₹41.2**.
Q: "How much did we spend on feed this month?" / "is mahine feed pe kitna kharcha hua?" · "Feed cost per goat per day?" / "ek bakri ka roz ka feed kharcha?"

### 3d Charts "Daily directed feed" (stacked by item), "Feed mix", "Ration per animal" (g/head by item), per-item cards
| `/feed-analytics/directed` item rows | A:142 directedAnalyticsSQL / A:258 item_rows
- Formula: per (feed_day, item): kg = Σ pen-grain kg + milk kg; head_days = Σ pen-grain heads (per item!); g/head = kg×1000/heads (milk → blank). Mix = share of kg over window. Item cards hide retired concentrates/Baking Soda (copy key `chart.item.hidden_feeds`).
- Trap: per-item head_days differ by item (only pens that got that item); never add item head_days into a day total.
```sql
-- 3a CTEs grouped by feed_item_key instead of park, UNION ALL milk from feed_effective_external_consumption with 0 heads
```
stg value (24/09/2026): 23/09 Bhusa 1,083.8 kg (686.4 g/head), Kids Conc 815.8 (516.7 g), Adult Conc 327.6 (365.2 g over 897), UHT Milk 13.0 kg (no per-head).
Q: "Which feed are we using most?" / "sabse zyada kaunsa feed ja raha hai?" · "How much bhusa per animal?" / "ek janwar ko kitna bhusa?"

### 3e Status-wise view (fc_view=status): per pen tag avg animals, g/head/day, ₹/day
| `/feed-analytics/directed` pen-tag arm | A:365 directedPenTagSQL
- Formula: pen_day grain (items collapsed) → per shed_tag_key: avg_animals = Σheads/distinct days; g/head = Σkg×1000/Σheads; ₹/day = priced (3c rule) ÷ distinct days. Tag with `+` = mixed pen.
```sql
WITH pen_day AS (SELECT i.feed_day, r.shed_id, r.partition_key, r.shed_tag_key, r.breed_key, MIN(r.shed_tag) tag, SUM(r.quantity_kg) kg, MAX(r.head_count) heads
  FROM feed_direction_issues i JOIN feed_direction_issue_rows r ON r.tenant_id=$T AND r.feed_direction_issue_id=i.feed_direction_issue_id
  WHERE i.tenant_id=$T AND i.feed_day BETWEEN '2026-08-26' AND '2026-09-24' AND i.state IN ('issued','amended','locked') AND r.quantity_kg IS NOT NULL GROUP BY 1,2,3,4,5)
SELECT MIN(tag), round(SUM(heads)::numeric/COUNT(DISTINCT feed_day)) avg_animals, round(SUM(kg)*1000/NULLIF(SUM(heads),0),1) g_per_head
FROM pen_day GROUP BY shed_tag_key ORDER BY 3 DESC NULLS LAST;
```
stg value (24/09/2026): 30d to today: Buck 39 animals 2,344.2 g; F2-Male 456 animals 1,560.1 g; Mother 5 · 1,559.7 g.
Q: "How much do bucks eat vs F2 males?" / "buck aur F2 male ka feed kitna?"

### 3f "Feed by pen — last 7 days" (directed vs verified g/head per pen-day)
| `GET /feed-analytics/shed-feed?park_id&date_from=today-7&date_to=yesterday` | A:2818 shedFeedAnalyticsSQL
- Formula: pen-day directed kg & heads (pen-grain MAX, heads only from non-blocked cells); verified kg = Σ `feed_packing_verified_quantities.entered_kg` of `completed` packing completions for that pen-day; verified g/head uses the SHEET heads; planned_bags = distinct (session, workflow).
```sql
-- pen-day: 3a pen CTE grouped by shed_id, partition_key, feed_day (+ HAVING SUM(kg) IS NOT NULL)
-- LEFT JOIN (SELECT c.shed_id,c.partition_key,c.target_date feed_day,SUM(q.entered_kg) vkg FROM feed_packing_verified_quantities q
--   JOIN feed_packing_completions c ON c.tenant_id=q.tenant_id AND c.completion_id=q.completion_id WHERE c.status='completed' GROUP BY 1,2,3)
```
stg value (24/09/2026): CBE Godel 1 Part 1, 17–23/09: 13–14 animals, 1,400 g directed daily (1,507.7 on 18/09); verified = directed every day (incl. 1,507.7 on 18/09) except 20/09 1,450 g and 21/09 not verified.
Q: "Is G1P1 getting its ration?" / "G1P1 ko pura feed mil raha hai?"

## 4 Feed Analytics > Execution tab
### 4a "Daily execution status" chart (+ transport, median verify minutes)
| `GET /feed-analytics/execution?sections=days,...` | A:619 status, A:627 transport, A:637 latency; merge A:886
- Formula: per target_date count by status in packing and distribution (completed / pending_verification / rework); transport by business_date (completed / due / verification_due / rework); median minutes created_at→verified_at by IST verdict day.
```sql
SELECT status, count(*) FROM feed_distribution_completions WHERE tenant_id=$T AND target_date='2026-09-23' GROUP BY 1;
SELECT status, count(*) FROM feed_transport_tasks WHERE tenant_id=$T AND business_date='2026-09-23' GROUP BY 1;
```
stg value (24/09/2026): 23/09 transport 15 completed, 1 due, 2 verification_due; median verify latency 56 min.
Q: "How fast are verifiers approving feed?" / "verify hone mein kitna time lag raha?"

### 4b "Packed vs given over time" (directed vs measured per day)
| same endpoint, sections=consumption | A:798 executionConsumptionSQL (window end +1 day)
- Formula: planned (session×item cells) LEFT JOIN measured bags on full key (feed_day, park, shed, partition, session, workflow, item) → per pen-day; per day: directed = Σtarget, measured = Σactual (NULL if no reading), pens off = |actual-target| > 0.2 kg, pens compared.
- Trap: measured is only for verified bags; directed covers every pen, so directed > measured does not mean underfeeding.
stg value (24/09/2026): feed day 23/09 directed 2,227.2 kg, measured 2,169.2 kg, 95 pens compared, 0 off by >200 g.
Q: "Did pens get what the sheet said?" / "sheet ke hisaab se feed diya kya?"

### 4c "Packed vs directed" mismatch table (every measured bag)
| sections=packing_variance, `fav_day` = PACKING day (backend feed day = +1), filters variance_park_label (full name), variance_feed_item_key, limit/offset | A:689 executionPackingVarianceSQL
- Formula: bag = verified_quantities row of `completed` packing completion; planned = Σ sheet kg at same key; diff = entered − planned (planned missing → "Not on sheet"); sorted |diff| desc. Tolerance 0.2 kg (`domain/analytics.go:449`).
stg value (24/09/2026): packing day 22/09 → 386 measured bags, largest |diff| = 0.000 (all matched).
Q: "Which bags were packed wrong?" / "kaunse bag galat pack hue?"

### 4d "Feed direction completion" table + tiles (Not fed / Waiting / Sent back / Fed and approved)
| sections=distribution_completions, `completion_day` (default = window end = yesterday), completion_park_id, completion_shed_id, completion_status | A:1348 distributionCompletionScopeSQL (+1440 rows, 1452 totals)
- Formula: expected pen-sessions from live sheet (feed_day) UNION completion rows; bucket = completion status else `not_started`. Pen-session grain (shed, partition, session, workflow).
```sql
WITH e AS (SELECT DISTINCT i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow FROM feed_direction_issues i
   JOIN feed_direction_issue_rows r ON r.tenant_id=$T AND r.feed_direction_issue_id=i.feed_direction_issue_id
   WHERE i.tenant_id=$T AND i.feed_day='2026-09-23' AND i.state IN ('issued','amended','locked')),
d AS (SELECT park_id, shed_id, partition_key, session_no, workflow, status FROM feed_distribution_completions WHERE tenant_id=$T AND target_date='2026-09-23'),
k AS (SELECT * FROM e UNION SELECT park_id, shed_id, partition_key, session_no, workflow FROM d)
SELECT COALESCE(NULLIF(d.status,''),'not_started') b, count(*) FROM k LEFT JOIN d USING (park_id, shed_id, partition_key, session_no, workflow) GROUP BY 1;
```
stg value (24/09/2026): 23/09 **188 fed and approved, 16 not fed**, 0 waiting, 0 sent back.
Q: "Which pens were not fed yesterday?" / "kal kaunse pen ko feed nahi mila?"

## 5 Experiment tab + wastage
### 5a "Experiment feed — by item" | `GET /feed-analytics/experiment?park_id&date_from&date_to&wastage_day` | A:1473 experimentAnalyticsSQL
- Formula: live sheets `workflow='experiment'` only, Σ quantity_kg per (feed_day, item).
```sql
SELECT i.feed_day, round(SUM(r.quantity_kg),1) FROM feed_direction_issues i JOIN feed_direction_issue_rows r ON r.tenant_id=$T AND r.feed_direction_issue_id=i.feed_direction_issue_id
WHERE i.tenant_id=$T AND i.feed_day='2026-09-23' AND i.state IN ('issued','amended','locked') AND i.workflow='experiment' AND r.quantity_kg IS NOT NULL GROUP BY 1;
```
stg value (24/09/2026): 23/09 experiment **993.2 kg** (both parks).
### 5b "Leftover feed (wastage)" table | same endpoint | A:1499 experimentWastagePensSQL (admin mobile worklist: `GET /feed-wastage/worklist`, app/wastage_service.go:259)
- Formula: experiment pens on that day's sheet LEFT JOIN `feed_wastage_completions` (target_date, workflow experiment) → status + verifier-measured `wastage_kg`.
```sql
WITH pen AS (SELECT DISTINCT r.shed_id, r.partition_key FROM feed_direction_issues i JOIN feed_direction_issue_rows r ON r.tenant_id=$T AND r.feed_direction_issue_id=i.feed_direction_issue_id
  WHERE i.tenant_id=$T AND i.feed_day='2026-09-23' AND i.state IN ('issued','amended','locked') AND i.workflow='experiment')
SELECT count(*) pens, count(c.wastage_kg) measured, round(sum(c.wastage_kg),2) kg FROM pen p LEFT JOIN feed_wastage_completions c
  ON c.tenant_id=$T AND c.shed_id=p.shed_id AND c.partition_key=p.partition_key AND c.target_date='2026-09-23' AND c.workflow='experiment';
```
stg value (24/09/2026): 23/09 36 experiment pens, 30 measured, **193.5 kg leftover**.
Q: "How much feed was wasted in experiment pens?" / "experiment pen mein kitna feed bacha/waste hua?"

## 6 Follow-up tab (did the sheet react to purchases/sales/deaths)
| `GET /feed-analytics/follow-up?park_id&date_from&date_to` | FU:190 causes SQL, FU:91 sheet SQL, rule domain/feed_follow_up.go:265 ResolveFeedFollowUpDay (reaction window 2 days, :101)
- Formula: causes = animals purchased (procurement_load_goats accepted intake), sold (goat_sale_allocations tagged), died (goats exit) per pen-day; sheet = **locked-only** sheets, pen key from partition LABEL, heads per pen-day. Verdict: compare last sheet on/before event day vs first sheet 2 days later — heads moved = `followed`, same = `not_followed`, sheet not issued yet = `pending`. Shifts are not causes.
- Not reproducible in one SQL (Go composition); sheet side = 3a pen CTE with `state='locked'`, grouped by `(shed_id, lower(btrim(partition_label)), feed_day)`.
Q: "After selling goats, did we cut their feed?" / "bakri bechne ke baad feed kam kiya kya?"

## 7 Feed config (/feed/config)
| GET `/feed-config/ration-rates` (feedconfig/adapters/postgres/repository.go:153), `/feed-items` (:381), `/session-templates` (:421), `/experiment` (:636), `/schedule`, `/shed-factors`, `/ration-groups`, `/shed-tags`, `/pens` | handler feedconfig/adapters/http/handler.go:134-152
- Rations grid: `feed_ration_rates` `valid_to IS NULL` AND item active in `feed_item_catalog` (grams_per_head per park × ration_group × shed_tag × item). Breed → ration group via `feed_ration_groups`. Items: catalog `status`. Sessions: `feed_session_templates` (split_fraction). Experiment: `feed_experiment_config` per pen×item (absolute_kg or grams_per_head, status active/retired). Schedule: `feed_schedule_config` valid_to IS NULL.
```sql
SELECT l.name, count(*) rates, count(DISTINCT ration_group_key) groups, count(DISTINCT shed_tag_key) tags, count(DISTINCT r.feed_item_key) items
FROM feed_ration_rates r JOIN locations l ON l.location_id=r.park_id
WHERE r.tenant_id=$T AND r.valid_to IS NULL AND EXISTS (SELECT 1 FROM feed_item_catalog c WHERE c.tenant_id=r.tenant_id AND c.feed_item_key=r.feed_item_key AND c.status='active') GROUP BY 1;
```
stg value (24/09/2026): each park 231 rates in force (sparse grid over 7 groups, 31 tags, 3 items; not every combination has a rate); catalog 4 active / 13 retired items; sessions Morning 0.5 + Evening 0.5 both parks; experiment active pens CBE 19 (58 cells), CPT 17 (66); schedule normal 07:00 issue / 14:00 correct / 15:30 lock, experiment 14:00/14:00/15:30.
Q: "What's the ration for F2 males?" / "F2 male ka ration kitna gram hai?" · "Which pens are in the experiment?" / "experiment mein kaunse pen hain?"

## 8 ceo_ai feed views — traps
- `ceo_ai.feed_adherence`: directed = Σ all rows with **no state/workflow filter** (OK on stg today: one issue per park/day/workflow); `fed_kg` reads legacy `feed_direction_completions`, which is **empty (0 rows)** → fed_kg = 0 and variance = −directed everywhere. stg 23/09: directed 2,227.2 (matches 3a), fed 0.0. Never answer "fed" from it; use §4b measured kg or §4d approved pens.
- `ceo_ai.feed_completions_base`: same empty legacy table → 0 rows. Use `feed_distribution_completions` / `feed_packing_completions`.
- `ceo_ai.feed_direction_current`: raw cells, no state filter, park_label = `CBE`/`CPT`, head counts absent — fine for kg by item/session; don't derive animals from it.

## 9 Coverage additions (appended 24/09/2026, feed screen sweep)
Same `$T`, paths and live-sheet rules as §0. All figures run read-only on goatos-stg 24/09/2026.

### 9a Overview "Daily feed expenditure" chart, **Per animal** mode (`?spend=per_animal`)
| `/feed-analytics/stock` `expenditure[]` (A:2135) ÷ `/feed-analytics/directed` `days[].head_days` (A:219) | UI feed-analytics.tsx:979-1028
- Formula: for each expenditure feed_day, ₹(day) ÷ sheet animals of the SAME feed_day (3a head grain). Matched on feed_day, not position; a day with 0 animals is a gap, never ₹0. "Overall" mode = ₹(day) as in 3c.
- SQL: 3c `priced` grouped by feed_day, joined to 3a `pen` summed by feed_day.
- Trap: expenditure has no workflow filter and includes UHT milk ₹; the denominator is sheet heads (no milk) – so ₹/animal carries the milk cost over sheet animals.
stg value: 23/09 = 65,107 / 1,579 = **₹41.2/animal** (same as the 3c tile).
Q: "Feed cost per animal trend?" / "per janwar feed kharcha roz ka kaisa chal raha?"

### 9b Overview "Where the money goes" pie + per-item card strip (₹/day · kg/day · ₹/kg)
| `/feed-analytics/stock?sections=item_expenditure` (A:2194 stockItemExpenditureSQL, rows feed_day × item: rupees, directed_kg) + `/directed` item series | UI buildItemMoney :744, strip :1080-1105, pie :839-845/:1031
- Formula per item over the window's priced days (< today): **₹/day** = Σ rupees ÷ count of priced (day,item) rows; **₹/kg** = Σ rupees ÷ Σ priced kg; **kg/day** = Σ directed item kg ÷ days the item appears in the directed series (window through today, incl. milk). Pie slice = the item's ₹/day; hidden feeds (copy `chart.item.hidden_feeds`) and unpriced items get no slice. Cards ordered by window ₹ desc, unpriced last ("Not priced").
- Filters→SQL: park chip → `park_id = ANY`; window = range chip.
```sql
-- 3c day_item + priced CTEs for feed_day BETWEEN '2026-08-26' AND '2026-09-23', keep feed_item_key and kg in priced, then:
SELECT feed_item_key, count(DISTINCT feed_day) days, round(sum(spend)/count(DISTINCT feed_day)) rs_per_day,
       round(sum(spend)/NULLIF(sum(kg),0),2) rs_per_kg FROM priced GROUP BY 1 ORDER BY 3 DESC;
```
stg value (30-day range, priced days 26/08–23/09): Mesha Kids Concentrate ₹32,123/day @ ₹40.77/kg (15 priced days); Dry Masoor Bhusa ₹17,985/day @ ₹16.36/kg (29 days); Mesha Adult Concentrate ₹10,139/day @ ₹40.76/kg; UHT Milk ₹1,525/day @ ₹63.64/kg. Retired split concentrates also priced (e.g. kids_sheep ₹18,548/day) but hidden from cards/pie.
- Trap: ₹/day divides by PRICED days of that item, not calendar days – an item priced only 15 of 29 days shows a higher ₹/day than its window total ÷ 30.
Q: "Which feed costs us most per day?" / "roz sabse mehenga feed kaunsa?" · "What's bhusa costing per kg?" / "bhusa kitne rupaye kilo pad raha?"

### 9c Status-wise cards: ₹/day and kg per animal per day (extends 3e)
| A:365 directedPenTagSQL (priced_day A:~417, output :~447) | UI FeedStatusWise :638-725
- Formula: per shed_tag_key: avg_animals = Σ pen-day heads ÷ distinct feed days; per_head_kg = Σkg ÷ Σheads (2 dp, kg not g); rupees_per_day = Σ(tag×item×park×day kg × 3c load rate) ÷ distinct feed days. UI drops `mixed` tags (key contains `+`). Day line = same per feed_day.
```sql
-- see 3e pen_day; ₹ side: cells grouped (feed_day, park_id, shed_tag_key, feed_item_key) JOIN LATERAL 3c load price; divide by COUNT(DISTINCT feed_day) of the tag
```
stg value (30d to 24/09): F2-Male 456 animals · 1.56 kg · **₹25,964/day**; Non-Pregnant 792 · 1.41 kg · ₹25,789/day; F2-Female 191 · 1.27 · ₹8,133; Buck 39 · 2.34 · ₹2,128; Warmup 58 · 1.00 · ₹2,016; K3 39 · 0.54 · ₹673.
Q: "How much do we spend per day on F2 males?" / "F2 male pe roz kitna feed kharcha?"

### 9d Execution tab KPI tiles: Packing verified % · Distribution verified % · Transport done % · Median verify minutes
| `/feed-analytics/execution?sections=days,...` A:619/627/637, merged A:886-975 | UI ExecutionTab :1253-1320
- Formula over the range window (ends yesterday): packing % = completed ÷ (completed+pending_verification+rework) in `feed_packing_completions` by target_date; distribution % same on `feed_distribution_completions`; transport % = completed ÷ (completed+due+verification_due+rework) on `feed_transport_tasks` by business_date (**`retired` tasks excluded**); latency tile = median minutes created_at→verified_at of the LATEST IST verdict day in window (packing+distribution pooled).
```sql
SELECT status, count(*) FROM feed_transport_tasks WHERE tenant_id=$T AND business_date BETWEEN '2026-08-25' AND '2026-09-23' GROUP BY 1;
-- packing/distribution: 3b query split by table
```
stg value (30d 25/08–23/09): packing **78%** (4,599/5,897); distribution **87%** (5,169/5,915); transport **73%** (394/540; 57 retired ignored); median verify **56 min** (23/09; 22/09 was 642, 21/09 288).
Q: "Is transport getting done?" / "feed transport time pe ho raha hai?" · "How long do verifiers take?" / "verify mein kitna time?"

### 9e Execution tab completion table rows + drawer (extends 4d)
| sections=distribution_completions A:1440 rows / 1452 totals / 1459 options | UI feed-completion-table.tsx (cols park, pen, session, status, who, when, videos; drawer: submitted/verified by+at, rework_reason, proofs)
- Row = expected pen-session UNION completion row; who/when = submitted_by/at then verified_by/at of the `feed_distribution_completions` row; videos = its proof media. Filters: completion_park_id, completion_shed_id, completion_status (`not_started` = no row).
```sql
-- 4d CTE e, then rows with no completion:
SELECT e.shed_label, e.partition_label, e.session_no, e.workflow FROM e LEFT JOIN feed_distribution_completions d
  ON d.tenant_id=$T AND d.target_date='2026-09-23' AND (d.park_id,d.shed_id,d.partition_key,d.session_no,d.workflow)=(e.park_id,e.shed_id,e.partition_key,e.session_no,e.workflow)
WHERE d.status IS NULL;   -- add r.shed_label, r.partition_label to e's DISTINCT
```
stg value 23/09 not fed (16 pen-sessions): CPT Yashoda/1 both sessions (normal); CBE Godel 1 Part 8 both sessions (normal); CBE Godel 2 Parts 1–6 both sessions (experiment).
Q: "Which pens were not fed yesterday?" / "kal kaunse pen ko feed nahi mila?"

### 9f Follow-up tab columns (extends §6, **Go-only**)
| FU:91/190, domain/feed_follow_up.go:265, :341 | UI feed-follow-up.tsx:71-180
- Columns: pen · changes (cause chips purchased/sold/died with counts) · when (event day; "pending"/sheet dates) · animals (head count before→after on locked sheets = HeadDelta) · feed (kg before→after) · status (`followed`="Feed changed", `not_followed`="Feed unchanged", `pending`="Direction not locked yet") · chip "{n} unexplained" = HeadDelta − net animals from causes (non-zero only on followed rows; usually shifting in/out).
- Go-only: the verdict pairs sheets across a 2-day reaction window in Go; SQL can reproduce only the two sides (causes; locked-sheet heads per pen-day).

### 9g Feed config extras (extends §7)
| `/feed-config/feed-items`, `/session-templates`, `/shed-factors`, `/schedule` | UI feed-config.tsx:1126-1265, feed-config-editor.tsx:919-1157
- Feed items: `feed_item_catalog` label, status, energy_kcal_per_kg, dry_matter_factor, wastage_factor, display_order. Session feeds: `feed_session_template_items` (park × session × slot, status active, valid_to NULL). Shed factors: `feed_shed_factors` multiplier per shed × item (valid_to NULL). Schedule columns: direction_time, correction_time, **transport_time** (the 15:30 column is `transport_time`; UI note `label.transport_time_note`).
```sql
SELECT l.name, i.session_no, string_agg(i.feed_item_label, ', ' ORDER BY slot_no) FROM feed_session_template_items i JOIN locations l ON l.location_id=i.park_id
WHERE i.tenant_id=$T AND i.valid_to IS NULL AND i.status='active' GROUP BY 1,2;
SELECT count(*) FROM feed_shed_factors WHERE tenant_id=$T AND valid_to IS NULL;
```
stg value: 4 active items (Bhusa, UHT Milk, Kids Conc, Adult Conc), energy/DM/wastage all blank; both parks both sessions list Concentrate, Dry Masoor Bhusa, Mesha Adult Concentrate, Baking Soda, Mesha Kids Concentrate (retired items still on templates); **0 shed factors** (every multiplier = default 1.0); schedule both parks normal 07:00/14:00/15:30, experiment 14:00/14:00/15:30.
- Trap: session templates still list retired items (Concentrate, Baking Soda) – the grid omits them because rates join to active catalog only.
Q: "Is any shed getting extra feed factor?" / "kisi shed ka feed multiplier laga hai?" · "What time is the feed sheet locked?" / "feed sheet kitne baje lock hoti hai?"
