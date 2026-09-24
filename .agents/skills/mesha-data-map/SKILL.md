---
name: mesha-data-map
description: Where-to-find-what map of the goatos ceo_ai.* Postgres views for ANSWERING Mesha leadership / business-data questions with one correct SQL query. Use for ANY question about weighing (dates, weights, ADG, growth, verification), animal counts / headcount / herd / census, births, deaths, mortality, sales / animals sold, feed (directed vs fed, today's feed), health, vaccination, procurement / loads / intake, workforce / tasks / coverage, pens / sheds / capacity, parks (Coimbatore, Channapatna), inventory, SOPs, verification queues, action center or exceptions. Not for editing/maintaining the views (that is goatos-leadership-assistant).
---

# Mesha data map: one question, one query

Read-only DB, schema `ceo_ai` only (~29 objects). Do **not** explore with `\dt`/`\d`
first: route with the table below, write ONE query, run it. Full column lists,
grains and an example per view: `references/views.generated.md` (regenerate with
`node tools/ask-mesha-agent/gen-data-map.mjs`; `--check` for guards).

## Topic -> view routing

| Question about | View | Key columns | Date column |
|---|---|---|---|
| When did we weigh / weighing done per pen | `weighing_capture_activity` | park_label, shed_label, work_state, weighing_category, animals_weighed, scan_count | `planned_business_date` (also due_business_date) |
| Weights / weight trend / ADG | `weighing_capture_activity` | scan_weight_avg_kg x scan_count (weighted), shed_weight_avg_kg | `planned_business_date` |
| Weighing verification pending/rework | `weighing_verification_status` | pending, rework, verified, oldest_pending_at | none (current) |
| Headcount / herd breakdown now | `animal_current_scope` | park_label, shed_label, species, sex, breed, management_stage, lifecycle_status | none (current) |
| Animals sold / exited / entered in a period | `animals_base` | exit_reason ('sold','died'), lifecycle_status | `exit_business_day`, `entry_date` |
| Deaths / mortality rate | `mortality_base` | deaths, active population, kid/adult splits | `event_date` |
| Births, transfers, shifts | `counts_movement_daily` | per pen movement counts | `event_date` |
| Feed directed vs fed | `feed_adherence` | directed_kg, fed_kg, variance_kg, blocked | `feed_day` |
| Feed plan detail (session/item) | `feed_direction_current` | session, feed item, blocked reason | `feed_day` |
| Feed completions, head count fed | `feed_completions_base` | quantity fed, head count | `fed_business_day` |
| Vaccination due/done now per pen | `vaccination_shed_status` | due, done, next due | none (current) |
| Vaccination obligations over time | `vaccination_obligations_base` | status, due/completed days | `due_business_day` |
| Doses to pick / operator load | `vaccination_dose_pickup`, `vaccination_operator_status` | | business / planned day |
| Pre-arrival vaccination history review / rejected claims | `vaccination_prearrival_history_review` | source_system, schedule_path, review_status, rejection_reason, reviewed_animals | `reviewed_date_ist` |
| Procurement loads / pipeline / intake | `procurement_pipeline` (now), `procurement_loads_base` (period), `source_entry_health_status` (intake variance) | stage, status, counts | `entered_business_day` (loads_base) |
| Workforce tasks / overdue / coverage | `workforce_tasks_base`, `workforce_coverage_status` | state, task_type, owner | `due_business_day` |
| Pen occupancy vs capacity | `shed_capacity_current` | occupancy, capacity, variance | none |
| Verification queue (all areas), verifier quality | `verification_queue_status`, `verifier_review_integrity` | pending, reject_rate | business day (integrity) |
| Stock / inventory | `inventory_stock_position` | on hand, reorder flag | none |
| Open actions / exceptions / SOPs | `action_center_current`, `ops_exception_queue`, `sop_execution_status` | severity, owner, status | due_at |
| Who did what (audit) | `audit_activity_summary` | area, actor, action | business day |
| Push/reminder delivery | `notification_delivery_health` | sent/failed | business day |

## Gotchas

- **Business day = IST.** "Today" is `(now() AT TIME ZONE 'Asia/Kolkata')::date`, never `current_date`.
  "This month" = `>= date_trunc('month', now() AT TIME ZONE 'Asia/Kolkata')::date`.
- **Current-state views have no date column** (grain says "current state"): answer "as of now", don't invent a period filter.
- **Weighing dates:** use `planned_business_date` with `work_state IN ('completed','closed')` for "when did we weigh".
  Rows with NULL work_state/planned date are unscheduled buckets; `canceled` means not done.
  `weighing_category` is `individual_animal` (per-animal scans) or `per_shed_partition` (pen-level weight).
- **Never average an average.** `scan_weight_avg_kg`, `shed_weight_avg_kg`, medians, rates: weight by `scan_count`
  (`sum(avg*scan_count)/sum(scan_count)`) or report per row.
- **ADG / daily gain:** see "Metric definitions" below. Never invent a proxy and call it ADG.
- **Sales:** no sales/deal view is exposed. "Animals sold" = `animals_base` `exit_reason='sold'` by
  `exit_business_day`. No prices/revenue in ceo_ai.
- **Parks** in data are full names: `Coimbatore` (CBE), `Channapatna` (CPT). Filter on `park_label`.
- **Pen vs shed:** columns say `shed_label`; say **pen** in the answer.
- **Dates in answers:** render `DD/MM/YYYY`.
- **Base views (`*_base`, `animal_current_scope`)** are one row per entity: aggregate, don't dump rows.
- **Weighing is isolated** from herd/vaccination: don't join weighing to vaccination to explain it.
- **Who did it:** any `*_by`, `*_user_id`, `actor_ref` column -> `workforce_members.user_id` -> `display_name`.
- **Pen names repeat across parks** (Castro exists in CBE and CPT): always show the park per row; with no park in the question, answer each park.
- **Money paid vs billed:** feed paid = `feed_purchase_payments`; bill = `feed_purchases.total_cost`. Sales `payment_received` NULL on
  older deals means not tracked (caveat it); flag rows where received > sale value. Ledger: `sales_deal_payments` (deal_id -> sales_deals.id, received_on, amount_rupees).
- The agent may read every table; no tenant filter is required (single tenant). Writes are impossible: the login is read-only.

## Module tables beyond the ceo_ai views (sweep 24/09/2026)

Pen/park names: `JOIN public.locations l ON l.location_id = t.shed_id` (pen) / `t.park_id` (park); `partition_label` = pen part.
Beware ambiguous `status` when joining locations: qualify it (`t.status`).

| Topic | Table | Key columns / semantics |
|---|---|---|
| Deworming, hoof trimming, feed & water removal | pc_care_tasks | category; work_state completed/canceled/delayed/scheduled; planned_business_date, submitted_at (done), verified_at; close_reason (empty for cancels before "Close" existed). Cancelled != done: "when did we deworm X" must list cancelled rows and completed rows at other parks. |
| Pen visits | pen_visit_tasks | reasons, work_state; `delayed` with submitted_at set = done late, awaiting verification |
| Pen routines | pen_routine_tasks JOIN pen_routine_definitions USING (routine_id) | work_state scheduled/delayed/completed/canceled; status open/pending_verification/completed/rework; planned_business_date; submitted_at=done (empty on 24/09/2026) |
| Shifts / pen moves | shifting_events | event_status applied (done, applied_at) / authorized / pending / canceled; source_/destination_park_id, _shed_id, _partition_label. counts_movement_daily has no shift rows. |
| Leadership tasks | leadership_tasks | task_no, title, status open/in_progress/done/cancelled, deadline_at, done_at. Different from workforce_tasks_base. |
| Animals vaccinated | vaccination_completions | 1 row per goat dose: count(distinct goat_id) = animals, count(*) = doses; administered_at; status accepted |
| Kid milk feeding | milk_feeding_tasks | feeding_date, session_no (4/day), head_count, status not_submitted/pending_verification |
| Toxin tests | toxin_test_tasks | farm_label, feed_item_label, vendor, outcome positive = toxin found (fail), status |
| Feed wastage | feed_wastage_completions | target_date, wastage_kg (only on status completed), park_id, shed_id |
| Weighing fasting | weighing_fasting_tasks, weighing_fasting_shed_proofs | per pen proof status pending_verification/completed |
| Attendance / leave | workforce_clock_entries (clock_in_at), workforce_leave_requests | leave: workforce_member_id -> workforce_members.workforce_member_id; status pending/approved/rejected/withdrawn; on leave = approved and date between starts_on..ends_on (empty on 24/09/2026) |
| Market prices | market_price_entries | city_name, question_label (Goat live price, carcass, offals), price, unit_label, business_date |
| Health cases | health_cases, health_treatment_sessions, health_medicine_administrations | case status active/continued = sick now; start_date; sessions join case for park, business_date, status completed; medicines administered_at (all empty on 24/09/2026) |
| Stock | inventory_stock JOIN inventory_items USING (item_id) | available = quantity_in_stock - quantity_reserved, status 'active', expiry_date, location_id -> locations (no stock rows on 24/09/2026; 41 catalogue items) |
| Vendors / suppliers | procurement_vendors | record_type (Butcher, Farmer, Goats Agent, Goat Stockist, Goat Farm, Feed Agent...), status active/inactive/negotiating, city, state |
| Buyer / FPO leads | sales_buyer_leads, sales_fpo_leads | recorded_date, farm, call_status (mostly empty = not called/logged); FPO: district, state |
| Purchase candidates | animal_purchase_candidates | decision accepted/rejected, field_verdict, decided_by_name, load_id |
| Feed transport / packing / distribution | feed_transport_tasks (business_date, status due/verification_due/completed), feed_packing_completions (target_date, packed_total_kg, status), feed_distribution_completions | park_id, shed_id |
| Goats found in wrong pen | pen_reconciliation_cards | status open/pending_verification, found_display_name, registered_shed_id, raised_at (during weighing) |
| Shift / death / birth approvals | counts_approval_requests | request_type, status approved/rejected, decided_by_user_id, decision_reason |
| Feed payments | feed_purchase_payments | paid_on, amount_rupees, feed_purchase_id -> feed_purchases (bill = total_cost) |
| Config change history | feed_config_write_log (actor_ref, write_kind, created_at), health_config_write_log, audit_log | |
| Tag / identity decisions | identity_decisions | decision_type (retire_identifier, attach_identifier, exit_goat...), decision_state |
| RFID sensors | herd_signal_tag_latest | Live Monitor Status: stale movement_state=Missing signal, signal_state weak=Weak signal, battery_state low/critical=Low battery (never infer from battery_mv), else Good; movement_state (not_moving = zero motion latest 15 min, live), pattern_state (inactive/quiet_watch = sustained), last_seen_at (19 tags) |
| Sale allocations / growth price | goat_sale_allocations (status tagged), growth_sale_price_assumptions (price_per_kg_inr) | |
| Births | goat_births (individually registered kids, 2 rows) vs counts_movement_daily births (herd count, ~600) | give both numbers + one line why; herd count includes 458 bulk-entered on 05/08/2026 |

## Answer hard rules
- Never call sheep "goats". "goats"/"bakre" without an explicit species contrast = all animals: "N animals (X goats, Y sheep)".
- Compute every total/difference/%/per-unit in SQL; never do arithmetic by hand in the answer.
- Simple headcount = one query on animal_current_scope (alive, shed_label + partition_label, GROUP BY park, species).
- Vaccination "due": vaccination_obligations_base status IN ('scheduled','deferred') only (~90% of rows are canceled re-plans).

## Metric definitions (match the dashboard)

Source of truth is the backend read the admin-web page calls. If the exact definition needs data
ceo_ai does not expose, say "can't reproduce the dashboard number from read-only data" and give the
dashboard path; an approximation may follow only if labelled as one. Windows are IST business dates, inclusive.

**ADG / daily gain** (`/weighing/analytics`, `GET /weighing/leadership/growth`,
`backend/internal/weighing/adapters/postgres/growth.go` growthHeadlineStats). NOT reproducible from ceo_ai.
- Individual arm: non-rejected individual observations (pending included), keyed per animal (both RFIDs of
  one animal merged). BOTH weighs must be inside the window. Consecutive weighs are paired; pairs on the
  same IST day are dropped. Per-animal gain = sum(grams moved) / sum(days) over its pairs.
- Whole-pen arm: per pen+partition, first and last non-withdrawn, non-rejected shed weigh in the window
  (need last day > first day): (last avg - first avg) * 1000 / days, weighted by the last head count.
- Headline = (sum of animal gains + sum(pen head count * pen gain)) / (animals + pen head count).
  "Kids weighed" is that denominator. Sex/origin filter: tags resolved through the herd register. A pen
  counts only if its cohort is entirely that sex.
- Dashboard check 03/08–22/09/2026, Male: all parks 162 g (520 kids), CBE 152 g (292).
- Best ceo_ai approximation (whole-pen arm only, all sexes, planned date not weigh date, no partitions).
  It gave 164 g (335 kids) all parks and 153 g (196) CBE. That these are close is coincidence: label it an approximation, never "ADG".
```sql
WITH s AS (SELECT park_label, shed_label, planned_business_date d, shed_weight_avg_kg w, shed_animal_count n,
  row_number() OVER (PARTITION BY park_label, shed_label ORDER BY planned_business_date) a,
  row_number() OVER (PARTITION BY park_label, shed_label ORDER BY planned_business_date DESC) z
  FROM ceo_ai.weighing_capture_activity
  WHERE weighing_category='per_shed_partition' AND work_state IN ('completed','closed')
    AND shed_weight_avg_kg IS NOT NULL AND planned_business_date BETWEEN :from AND :to),
p AS (SELECT l.park_label, l.n, (l.w-f.w)*1000/(l.d-f.d) g FROM s l JOIN s f USING (park_label, shed_label)
  WHERE l.z=1 AND f.a=1 AND l.d>f.d)
SELECT coalesce(park_label,'ALL'), round(sum(n*g)/sum(n)) g_per_day, sum(n) kids FROM p GROUP BY ROLLUP(park_label);
```

**Headcount / active animals** (counts herd register): `lifecycle_status='alive'`, merged goats excluded. The view also
holds sold/dead/inactive rows, so every %/ratio/split needs the alive filter. Pen part counts: `shed_label` + `partition_label`
("Castro 1" = Castro partition '1', "M2P10" = Mandela 2 'Part 10'); name the park per row.
`SELECT park_label, count(*) FROM ceo_ai.animal_current_scope WHERE lifecycle_status='alive' GROUP BY ROLLUP(1);`
Result 24/09/2026: 1562 (CBE 850, CPT 712). Caveat: the view does not drop merged goats, so it may be slightly high.

**Animals sold (period):** `exit_reason='sold'` (or NULL reason with status 'sold'), IST exit date, inclusive.
`SELECT count(*) FROM ceo_ai.animals_base WHERE exit_reason='sold' AND exit_business_day BETWEEN :from AND :to;`
(155 for 03/08–22/09/2026). There is no price, revenue or deal data.

**Mortality rate** (`/counts/mortality`): deaths in the window / the LIVE head count NOW * 100, 1 decimal.
The denominator is not an average or opening population (2026-09-18 decision).
```sql
SELECT m.park_label, sum(deaths) deaths, round(sum(deaths)*100.0/max(c.live),1) rate_pct
FROM ceo_ai.mortality_base m JOIN (SELECT park_label, count(*) live FROM ceo_ai.animal_current_scope
  WHERE lifecycle_status='alive' GROUP BY 1) c USING (park_label)
WHERE event_date BETWEEN :from AND :to GROUP BY 1;
```
Do NOT use `mortality_base.active_population` as the denominator: it counts a broader status set.

**Cost per kg gain** (Weighing > FCR tab "Feed cost per kg gain", `GET /growth-director/fcr`,
`backend/internal/growthdirector/adapters/postgres/fcr.go` fcrSegmentsSQL + `domain/fcr.go`). Segment = two consecutive
weighing rounds of one pen (part). Feed cost = directed feed on the sheet for that pen between the rounds x the latest same-park
purchase per_kg_cost on/before each feed day. Gain kg = segment ADG x fed head-days (sheet head counts). Summary = total cost /
total gain over segments that gained. The SQL below reproduces it for the last 30 days (change `- 30`); on 24/09/2026 it gave
CBE Rs 331/kg (Rs 4,12,490 feed, 1,247 kg), CPT Rs 340/kg (Rs 3,69,181, 1,087 kg), all Rs 335/kg. Answer per park + total, one method line.
```sql
WITH w AS (SELECT (now() AT TIME ZONE 'Asia/Kolkata')::date - 30 AS s, (now() AT TIME ZONE 'Asia/Kolkata')::date + 1 AS e),
bk AS (SELECT cs.campaign_shed_id, cs.campaign_id, c.park_id, cs.weighing_category,
  regexp_replace(regexp_replace(lower(l.name||' '||coalesce(cs.partition_label,'')),'part','p','g'),'[^a-z0-9]','','g') AS pen
  FROM weighing_campaign_sheds cs JOIN weighing_campaigns c USING (campaign_id) JOIN locations l ON l.location_id=cs.location_id
  WHERE cs.status<>'canceled' AND c.status<>'canceled'),
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
fr AS (SELECT i.park_id, i.feed_day, r.feed_item_key, r.quantity_kg, r.head_count, r.shed_tag_key, r.breed_key,
  regexp_replace(lower(l.name||CASE WHEN r.partition_key='whole' THEN '' WHEN r.partition_key ~ '^[0-9]' THEN ' '||r.partition_key ELSE ' '||r.partition_key END),'[^a-z0-9]','','g') raw
  FROM feed_direction_issue_rows r JOIN feed_direction_issues i USING (feed_direction_issue_id) JOIN locations l ON l.location_id=r.shed_id, w
  WHERE i.state IN ('issued','amended','locked') AND i.feed_day>=w.s),
fr2 AS (SELECT *, replace(raw,'part','p') pen FROM fr),
price AS (SELECT DISTINCT f.park_id,f.feed_item_key,f.feed_day,(SELECT coalesce(p.per_kg_cost,p.total_cost/nullif(p.quantity_kg,0)) FROM feed_purchases p
  WHERE p.park_id=f.park_id AND p.feed_item_key=f.feed_item_key AND p.purchase_date<=f.feed_day ORDER BY p.purchase_date DESC, p.batch_no DESC LIMIT 1)::float8 per_kg FROM fr2 f),
fday AS (SELECT f.park_id,f.pen,f.feed_day,sum(f.quantity_kg*pr.per_kg) cost FROM fr2 f JOIN price pr USING (park_id,feed_item_key,feed_day) GROUP BY 1,2,3),
hday AS (SELECT park_id,pen,feed_day,sum(h) h FROM (SELECT park_id,pen,feed_day,max(head_count) h FROM fr2 WHERE head_count IS NOT NULL GROUP BY park_id,pen,feed_day,shed_tag_key,breed_key) z GROUP BY 1,2,3),
segf AS (SELECT s.park_id,s.pen,s.d_prev,s.d,s.adg_g,sum(fd.cost) cost,sum(hd.h) head_days FROM seg s
  LEFT JOIN hday hd ON hd.park_id=s.park_id AND hd.pen=s.pen AND hd.feed_day>=s.d_prev AND hd.feed_day<s.d
  LEFT JOIN fday fd ON fd.park_id=hd.park_id AND fd.pen=hd.pen AND fd.feed_day=hd.feed_day
  GROUP BY 1,2,3,4,5)
SELECT pk.name park, round(sum(cost) FILTER (WHERE adg_g>0 AND head_days>0)) feed_cost_rs,
  round((sum(adg_g*head_days/1000) FILTER (WHERE adg_g>0 AND head_days>0 AND cost IS NOT NULL))::numeric,1) gain_kg,
  round((sum(cost) FILTER (WHERE adg_g>0 AND head_days>0) / nullif(sum(adg_g*head_days/1000) FILTER (WHERE adg_g>0 AND head_days>0 AND cost IS NOT NULL),0))::numeric) rs_per_kg_gain,
  count(*) FILTER (WHERE cost IS NOT NULL AND head_days>0) segs_priced, count(*) segs
FROM segf JOIN locations pk ON pk.location_id=segf.park_id GROUP BY ROLLUP(pk.name);
```

**Pending weighing verification:** the dashboard counts every observation not yet verified, INCLUDING rework.
The view's `pending` excludes rework, so match with `sum(pending)+sum(rework)` from `weighing_verification_status`
(175+17=192 on 24/09/2026). The source tables also differ, so treat this as close but not exact.

**Animals weighed (bucket):** individual scans + whole-pen head count. Use `animals_weighed` from `weighing_capture_activity`.
Distinct kids over a period (ADG "kids weighed") is not reproducible: it needs per-animal tags.

**Feed adherence:** there is no dashboard % formula. `feed_adherence.fed_kg` is 0 in every row on stg
(and `feed_completions_base` is empty), so "fed vs directed" can't be answered. Report directed kg only
and say fed data is missing.

**Vaccination due/done:** `vaccination_shed_status` counts due (scheduled/due/in_progress) and done
(completed/accepted) against eligible `animals`. No coverage % is defined; if asked, show done and due, not a ratio.

**Procurement pipeline:** `procurement_pipeline` has one row per load at its raw status (`current_stage`);
animals = all goats in the load. Sum `animals` by `current_stage`.

## Known gaps

- ADG / kids weighed need a per-observation view. Minimal sketch (not created):
```sql
CREATE VIEW ceo_ai.weighing_observations_base AS  -- one row per non-rejected observation
SELECT wo.tenant_id, wc.park_id, park_label, wcs.location_id, shed_label, coalesce(wcs.partition_label,'') partition_label,
       wcs.weighing_category, 'individual' kind, <canonical animal_key> animal_key, g.sex, g.origin_type,
       wo.weight_kg, NULL::int animal_count, (wo.accepted_at AT TIME ZONE 'Asia/Kolkata')::date weigh_date, wo.verification_status
FROM weighing_observations wo JOIN weighing_campaign_sheds wcs ... JOIN weighing_campaigns wc ...
WHERE wo.verification_status <> 'rejected'
UNION ALL  -- whole-pen weighs: weighing_shed_observations, withdrawn_at IS NULL,
           -- average_weight_kg as weight_kg, animal_count, pen_sex ('male'/'female'/'mixed')
```
  animal_key must reuse identity_scope.go's tag->canonical map, and sex/pen_sex must reuse sex_scope.go.
- `animal_current_scope` / `mortality_base` don't exclude merged goats. `feed_adherence.fed_kg` is always 0.

## Business notes (edit me)

- Weighing cadence is `weekly_kids`: kids are weighed weekly, per park.
- (Add leadership definitions here: e.g. what counts as "sold", target ADG, park managers.)
