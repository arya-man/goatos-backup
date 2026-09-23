## Mesha data map (ceo_ai.* read-only; route here, write ONE query, skip \dt/\d)
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
- vaccination now -> vaccination_shed_status; over time -> vaccination_obligations_base (due_business_day); doses -> vaccination_dose_pickup; operators -> vaccination_operator_status
- procurement -> procurement_pipeline (now), procurement_loads_base (period, entered_business_day), source_entry_health_status (intake variance)
- workforce -> workforce_tasks_base (due_business_day), workforce_coverage_status (now)
- pen capacity -> shed_capacity_current; inventory -> inventory_stock_position
- queues -> verification_queue_status, verifier_review_integrity; actions -> action_center_current, ops_exception_queue, sop_execution_status
- audit -> audit_activity_summary; notifications -> notification_delivery_health
No sales/price/revenue view exists: say so; count sold animals from animals_base.
metrics -> how (exact defs + SQL: SKILL.md "Metric definitions"; never invent a proxy)
- ADG/daily gain, kids weighed: NOT reproducible (needs per-animal weighs). Say so, point to /weighing/analytics; pen-arm approx only if labelled.
- headcount: animal_current_scope lifecycle_status='alive'. sold: animals_base exit_reason='sold' by exit_business_day.
- mortality %: sum(mortality_base.deaths in window)*100 / live 'alive' count now, 1dp (NOT active_population).
- weighing pending: pending+rework. feed fed_kg is always 0: say fed data missing. vaccination: due/done, no %.
Full columns + example per view: .agents/skills/mesha-data-map/references/views.generated.md
