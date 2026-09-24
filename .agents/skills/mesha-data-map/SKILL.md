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
| Weights / weight trend (ADG: references/adg-by-park.sql) | `weighing_capture_activity` | scan_weight_avg_kg x scan_count (weighted), shed_weight_avg_kg | `planned_business_date` |
| Weighing verification pending/rework | `weighing_verification_status` | pending, rework, verified, oldest_pending_at | none (current) |
| Headcount / herd breakdown now | `animal_current_scope` | park_label, shed_label, species, sex, breed, management_stage, lifecycle_status | none (current) |
| Animals sold / exited / entered in a period | `animals_base` | exit_reason ('sold','died'), lifecycle_status | `exit_business_day`, `entry_date` |
| Deaths / mortality rate | `public.goats` (see Mortality rate; not `mortality_base`) | exit_reason 'died', lifecycle_status 'dead', species | IST `exited_at` |
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
- **ADG / daily gain:** run references/adg-by-park.sql as-is (see "Metric definitions"). Never invent a proxy and call it ADG.
- **Sales:** no sales/deal view in ceo_ai; money is in `public.sales_deals` (see Two-source traps). "Animals sold" = `animals_base`
  `exit_reason='sold'` by `exit_business_day` (= goats register = tagged allocations).
- **Parks** in data are full names: `Coimbatore` (CBE), `Channapatna` (CPT). Filter on `park_label`.
- **Pen vs shed:** the PEN is the part (`shed_label` + `partition_label`, e.g. Godel 1 Part 3 = G1P3, Castro 1 = Castro part '1');
  `shed_label` alone is the shed/group. "Which pen has most" ranks parts (with park); shed totals are context only.
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

**ADG / daily gain** (`/weighing/analytics` Growth tab, `GET /weighing/leadership/growth`,
`backend/internal/weighing/adapters/postgres/growth.go` growthHeadlineStats / growthParkGainsQuery). Reproducible:
run `references/adg-by-park.sql` AS-IS (default window = this calendar month to date, IST; for another window edit
only the two dates in its `w` CTE). Never write your own ADG SQL, never use ceo_ai views for it, never re-weight.
- Individual arm: non-rejected scans (pending included), one key per animal (both RFIDs merged via goat_identifiers).
  BOTH weighs inside the window; same-IST-day pairs dropped; per-animal gain = sum(grams)/sum(days) over its pairs.
- Whole-pen arm: per pen+partition, first and last non-withdrawn, non-rejected shed weigh in the window (last day > first):
  (last avg - first avg)*1000/days, counted once per animal of the last head count.
- ADG = (sum animal gains + sum(pen head x pen gain)) / (animals + pen heads); "kids" = that denominator.
  Answer per park + ALL row with kids, rounded g/day, and the window. Checked 01/09-24/09/2026 against the API:
  CBE 152 g (382 kids), CPT 148 g (315), all 150 g (697).
- Sex/origin filters are not in the SQL (needs sex_scope.go); say so if asked for a sex cut.

**Headcount / active animals** (counts herd register): `lifecycle_status='alive'`, merged goats excluded. The view also
holds sold/dead/inactive rows, so every %/ratio/split needs the alive filter. Pen part counts: `shed_label` + `partition_label`
("Castro 1" = Castro partition '1', "M2P10" = Mandela 2 'Part 10'); name the park per row.
`SELECT park_label, count(*) FROM ceo_ai.animal_current_scope WHERE lifecycle_status='alive' GROUP BY ROLLUP(1);`
Result 24/09/2026: 1562 (CBE 850, CPT 712). Caveat: the view does not drop merged goats, so it may be slightly high.

**Animals sold (period):** `exit_reason='sold'` (or NULL reason with status 'sold'), IST exit date, inclusive.
`SELECT count(*) FROM ceo_ai.animals_base WHERE exit_reason='sold' AND exit_business_day BETWEEN :from AND :to;`
(155 for 03/08–22/09/2026; Sep 2026 = 129). Deal money/counts: see Two-source traps.

**Mortality rate** (`/counts/mortality`, `internal/counts/adapters/postgres/mortality.go`): deaths in the window / the LIVE head
count NOW * 100, 1 decimal (2026-09-18 decision). Deaths come from `public.goats` with the app's predicate, not `mortality_base`
(migration 000358 redefines that view with `exit_reason IN ('death','dead','mortality')`, which the goats CHECK never allows -> 0).
```sql
WITH d AS (SELECT p.name park_label, g.species FROM goats g JOIN locations p ON p.location_id = g.park_id
  WHERE (g.exit_reason='died' OR (g.exit_reason IS NULL AND g.lifecycle_status='dead')) AND g.merged_into_goat_id IS NULL
    AND (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN :from AND :to),
live AS (SELECT park_label, count(*) live FROM ceo_ai.animal_current_scope WHERE lifecycle_status='alive' GROUP BY 1)
SELECT park_label, count(d.*) deaths, count(*) FILTER (WHERE species='goat') goats, count(*) FILTER (WHERE species='sheep') sheep,
  round(count(d.*)*100.0/max(live.live),1) rate_pct FROM live LEFT JOIN d USING (park_label) GROUP BY 1;
```
Sep 2026: CBE 3 (1 goat, 2 sheep), CPT 0. Cause: `health_death_causes` / death `health_cases` are mostly empty: say cause not recorded.

**Cost per kg gain** (Weighing > FCR tab "Feed cost per kg gain", `GET /growth-director/fcr`,
`backend/internal/growthdirector/adapters/postgres/fcr.go` fcrSegmentsSQL + `domain/fcr.go`). Segment = two consecutive
weighing rounds of one pen (part). Feed cost = directed feed on the sheet for that pen between the rounds x the latest same-park
purchase per_kg_cost on/before each feed day. Gain kg = segment ADG x fed head-days (sheet head counts). Summary = total cost /
total gain over segments that gained. references/cost-per-kg-gain.sql reproduces it for the last 30 days (change `- 30`); on 24/09/2026 it gave
CBE Rs 331/kg (Rs 4,12,490 feed, 1,247 kg), CPT Rs 340/kg (Rs 3,69,181, 1,087 kg), all Rs 335/kg. Answer per park + total, one method line.
Run `references/cost-per-kg-gain.sql` as-is. If `unmatched_pens` is non-empty, name those pens in one line (their gain has no feed cost).

**Pending weighing verification:** the dashboard counts every observation not yet verified, INCLUDING rework.
The view's `pending` excludes rework, so match with `sum(pending)+sum(rework)` from `weighing_verification_status`
(175+17=192 on 24/09/2026). The source tables also differ, so treat this as close but not exact.

**Animals weighed (bucket):** individual scans + whole-pen head count. Use `animals_weighed` from `weighing_capture_activity`.
Distinct kids behind ADG: the `kids` column of references/adg-by-park.sql.

**Feed adherence:** there is no dashboard % formula. `feed_adherence.fed_kg` is 0 in every row on stg
(and `feed_completions_base` is empty), so "fed vs directed" can't be answered. Report directed kg only
and say fed data is missing.

**Vaccination due/done:** `vaccination_shed_status` counts due (scheduled/due/in_progress) and done
(completed/accepted) against eligible `animals`. No coverage % is defined; if asked, show done and due, not a ratio.

**Procurement pipeline:** `procurement_pipeline` has one row per load at its raw status (`current_stage`);
animals = all goats in the load. Sum `animals` by `current_stage`.

## Known gaps

- A ceo_ai per-observation view would let ADG run without raw tables. Minimal sketch (not created):
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
- `animal_current_scope` doesn't exclude merged goats (0 merged on 24/09/2026). `feed_adherence.fed_kg` is always 0.

## Two-source traps (verified 24/09/2026 against code + stg DB)

| Question | Use | Not | Why / check |
|---|---|---|---|
| Deaths | goats predicate (Mortality rate SQL) | `mortality_base` | 000358 view filter -> 0; 6 dead all-time, 3 in Sep |
| Animals sold | goats register / `animals_base` sold (= `goat_sale_allocations` tagged, 160) | `sales_deals.animal_count` (706, incl. pre-app deals) | give register, mention deal count when asked "in total" |
| Revenue | `sales_deals` status 'Deal Closed', `sum(sales_value)` by `sale_date`; product split via `sales_deal_lines` (Manure is a product_type) | any "Advance Paid"/open status = pipeline, not revenue | Sep 2026: Rs 14,71,114 on 10 deals; no open deals on 24/09 |
| Money received | `payment_received` (running total: advance seeded by 000227 + each `sales_deal_payments` row) | advance + ledger + payment_received added | balance = greatest(value - received, 0) (`sales/domain/sales.go` PaymentBalance). NULL = not tracked. If ledger re-enters the advance, received doubles: flag as double count |
| Load-wise sold / price | `references/load-wise-sales.sql` | only allocations, or only legacy columns | screen price/kg = legacy weighed columns only; tagged rate = labelled estimate |
| Load purchased / linked | `expected_count` else attributed; `linked_in_app` separately | linked count as purchased | loads 136/131 fully linked; 113/100/101 are pre-GoatOS (prior outcomes) |
| Load cost | `procurement_loads.animal_cost/transport_cost/other_cost` (totals) | + `procurement_load_cost_lines` | lines are the breakdown (31 rows) |
| Days on farm | today - `arrived_on` while animals remain | `purchase_date`, NULL `fattening_days` | fattening_days only on sold-out legacy loads |
| Births | `counts_movement_daily.births` + `goat_births` (created_at IST) | `goats.dob`, `origin_type='birth'` counts, goats.created_at | placeholder DOBs (09/05/2026 x403, 21/07/2024 x77), bulk import 04/08 |
| Vaccinations done | `vaccination_completions`, split `sop_submission_item_id` NOT NULL (in app) vs imported | total only | Aug 2026: 1,073 doses / 710 animals in app; 1,672 / 1,055 incl. imports |
| Vaccinations due | `vaccination_obligations_base` status scheduled/deferred | canceled rows (76k), `vaccination_shed_status.due` sums | this week (21-27/09): 3, CPT Yashoda; overdue 0 |
| Headcount | `lifecycle_status='alive'` | base views unfiltered | 1,562 (693 goats, 869 sheep) |
| Animal's pen | `goats.shed_id` + `goat_shed_partitions.partition_label` | `current_location_id` (18 differ) | legacy 'Godel 1 - Part N' rows hold 0 animals |
| Avg herd weight | `references/herd-avg-weight.sql` | averaging `weighing_capture_activity` rows | 28.2 kg over 723 of 1,562 alive |
| Feed stock / days | `references/feed-stock-days-left.sql` | `inventory_stock_position` | CBE concentrates 15 days (adult 201, kids 454 kg/day) |
| Feed owed | `payment_status='Pending'` bills: total_cost - greatest(payment_released, ledger) | 'Paid' rows (owed = 0, `procurement/domain/feed_purchase.go`) | NULL status = 40 sheet imports with no bill: unknown |
| Preventive care done/open | `submitted_at` / status | `work_state` alone | work_state stays delayed/scheduled after verification; canceled rows keep status open |
| Shifts done | `shifting_events` event_status 'applied' by applied_at IST | authorized/pending, counts_movement_daily | this week: 2 applied (CPT) |
| Staff hours | `workforce_clock_entries` status closed + auto_closed flagged | summing auto_closed as real | auto_closed = forgotten clock-out closed by system |
| Movement analytics | `goat_location_history` minus correction/repair/revert/swap reasons | all rows | |
| Count projections | none | `count_projection_snapshots` | all 144 blocked |

## Business notes (edit me)

- Weighing cadence is `weekly_kids`: kids are weighed weekly, per park.
- (Add leadership definitions here: e.g. what counts as "sold", target ADG, park managers.)
