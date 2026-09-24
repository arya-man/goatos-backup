## Mesha data map (EVERY table is readable; ceo_ai.* views are shortcuts, raw public.* tables have full detail — use them when a view lacks it)
Today = (now() AT TIME ZONE 'Asia/Kolkata')::date. Parks: Coimbatore (CBE), Channapatna (CPT) in park_label.
Column shed_label = "pen" in answers. Show dates DD/MM/YYYY. Never average *_avg_* columns; weight by scan_count.
No date column = current-state view: answer "as of now".

topic -> view -> key columns -> date column
- weighing dates/progress -> weighing_capture_activity -> park_label, shed_label, work_state ('completed','closed'), animals_weighed, weighing_category -> planned_business_date
- weights -> weighing_capture_activity -> sum(scan_weight_avg_kg*scan_count)/sum(scan_count) -> planned_business_date
- weighing verification -> weighing_verification_status -> pending, rework, verified, oldest_pending_at -> none
- headcount now -> animal_current_scope -> park_label, species, sex, breed, management_stage, lifecycle_status='alive' -> none
- sold / exits / entries -> animals_base -> exit_reason ('sold','died'), park_label -> exit_business_day / entry_date
- deaths / mortality -> mortality_base -> deaths + active population -> event_date
- births / transfers / shifts -> counts_movement_daily -> per pen counts -> event_date
- feed directed vs fed -> feed_adherence -> directed_kg, fed_kg, variance_kg, blocked -> feed_day
- feed plan detail -> feed_direction_current; completions -> feed_completions_base (fed_business_day)
- vaccination now -> vaccination_shed_status; over time -> vaccination_obligations_base (due_business_day); doses -> vaccination_dose_pickup; operators -> vaccination_operator_status; pre-arrival history review -> vaccination_prearrival_history_review (reviewed_date_ist)
- procurement -> procurement_pipeline (now), procurement_loads_base (period, entered_business_day), source_entry_health_status (intake variance)
- workforce -> workforce_tasks_base (due_business_day), workforce_coverage_status (now)
- pen capacity -> shed_capacity_current; inventory -> inventory_stock_position
- queues -> verification_queue_status, verifier_review_integrity; actions -> action_center_current, ops_exception_queue, sop_execution_status
- audit -> audit_activity_summary; notifications -> notification_delivery_health
No sales/revenue view: use public.sales_deals (sales_value, payment_received, buyer_name, status='Deal Closed', sale_date, farm CBE/CPT).
metrics -> how (exact defs + SQL: SKILL.md "Metric definitions"; never invent a proxy)
- ADG/daily gain: compute from per-animal weighs in public.weighing_observations (consecutive weigh-ins); say it may differ slightly from /weighing/analytics. Never tell the CEO only pen averages are readable.
- headcount: animal_current_scope lifecycle_status='alive'. sold: animals_base exit_reason='sold' by exit_business_day.
- mortality %: sum(mortality_base.deaths in window)*100 / live 'alive' count now, 1dp (NOT active_population).
- weighing pending: pending+rework. feed fed_kg is always 0: say fed data missing. vaccination: due/done, no %.
Full columns + example per view: .agents/skills/mesha-data-map/references/views.generated.md
Access: read every table (no filter rules); the database login is read-only.
Raw tables (all readable): feed prices -> public.feed_purchases (feed_item_label, farm_label CBE/CPT,
purchase_date, quantity_kg, reached_weight_kg, feed_cost, transport_cost, loading_cost, unloading_cost, total_cost, per_kg_cost;
"assumed price" = latest per_kg_cost for that feed+farm on/before the day). Per-weigh data -> public.weighing_observations /
weighing_shed_observations. Sales money -> public.sales_deals / sales_deal_lines / sales_deal_payments. Prefer these when a view lacks detail.
Preventive care (deworming, ticks removal, hoof trimming, feed & water removal) -> public.pc_care_tasks
(category 'deworming'|'hoof_trimming'|'feed_water_removal'|...; work_state completed/canceled/delayed/scheduled;
planned_business_date = planned, submitted_at = done, verified_at = verified). Pen/park names: join public.locations
on location_id = shed_id / park_id (locations.name); partition_label = pen number (Castro 1/2/3). Cancelled != done.
Before answering "not recorded": search information_schema.columns / table names and category/status values for the
keyword (e.g. ILIKE '%deworm%'); many activities live in module tables (pc_care_*, penroutines, penvisits, obligations).
