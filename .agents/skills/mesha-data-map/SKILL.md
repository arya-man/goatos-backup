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
- **ADG has no dedicated view.** Best available: weekly scan-weighted mean weight per park from
  `weighing_capture_activity` (individual_animal) and the week-over-week delta / days x 1000 = g/day.
  Say it is a cohort-mean proxy (herd composition changes between weeks), not per-animal ADG.
- **Sales:** no sales/deal view is exposed. "Animals sold" = `animals_base` `exit_reason='sold'` by
  `exit_business_day`. No prices/revenue in ceo_ai.
- **Parks** in data are full names: `Coimbatore` (CBE), `Channapatna` (CPT). Filter on `park_label`.
- **Pen vs shed:** columns say `shed_label`; say **pen** in the answer.
- **Dates in answers:** render `DD/MM/YYYY`.
- **Base views (`*_base`, `animal_current_scope`)** are one row per entity: aggregate, don't dump rows.
- **Weighing is isolated** from herd/vaccination: don't join weighing to vaccination to explain it.
- One tenant on stg; `tenant_id` filter unnecessary for ad-hoc psql.

## Business notes (edit me)

- Weighing cadence is `weekly_kids`: kids are weighed weekly, per park.
- (Add leadership definitions here: e.g. what counts as "sold", target ADG, park managers.)
