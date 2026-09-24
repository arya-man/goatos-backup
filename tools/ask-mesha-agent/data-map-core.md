## Mesha data map (EVERY table is readable; ceo_ai.* views are shortcuts, raw public.* tables have full detail — use them when a view lacks it)
Today = (now() AT TIME ZONE 'Asia/Kolkata')::date. Weekday names: take from to_char(d,'Dy') in the query, never work them out yourself. Parks: Coimbatore (CBE), Channapatna (CPT) in park_label.
HARD RULES: (a) Species: never call sheep "goats". If the question says goats/bakre/animals, answer "N animals (X goats, Y sheep)"; species column is 'goat'|'sheep'.
  "bakre"/"goats" with no explicit sheep-vs-goat contrast = ALL animals: "Total bakre, how many male?" -> "1,562 animals (693 goats, 869 sheep);
  578 male (116 goats, 462 sheep)". Only "goats only / not sheep" means species='goat'.
  ALWAYS state the split in the answer, even when one side is 0 (e.g. "80 animals, all sheep, no goats: 49 CBE, 31 CPT").
(b) Arithmetic: every total, difference, %, ratio, per-day or per-animal figure is computed IN SQL (sum/-/ /round) and read back; never add or subtract by hand.
(c) Simple lookups (headcount, one pen, one number) = ONE query, answer immediately; no exploration, no re-check.
Column shed_label = "pen" in answers. Show dates DD/MM/YYYY. Never average *_avg_* columns; weight by scan_count.
Comparisons ("compare CBE and CPT", "how are we doing"): <=8 short lines, one per topic, numbers from SQL; every comparative word
(more/less/bigger) must match the numbers. Vague "how are we doing" = headcount, sales, deaths, weighing, feed, open issues, this month.
No date column = current-state view: answer "as of now".

topic -> view -> key columns -> date column
- weighing dates/progress -> weighing_capture_activity -> park_label, shed_label, work_state ('completed','closed'), animals_weighed, weighing_category -> planned_business_date
- weights -> weighing_capture_activity -> sum(scan_weight_avg_kg*scan_count)/sum(scan_count) -> planned_business_date
- weighing verification -> weighing_verification_status -> pending, rework, verified, oldest_pending_at -> none
- headcount now -> animal_current_scope -> park_label, species, sex, breed, management_stage, lifecycle_status='alive' -> none.
  The view ALSO holds sold/dead/inactive rows: every count, %, ratio or split MUST filter lifecycle_status='alive' (CBE alive=850, not 995).
  ONE-QUERY headcount: SELECT park_label, species, count(*) FROM ceo_ai.animal_current_scope WHERE lifecycle_status='alive'
  [AND shed_label='Castro' AND partition_label='1'] GROUP BY ROLLUP(park_label, species). No park named = both parks, park named per row.
  Pen/part counts: shed_label + partition_label ('1','2' or 'Part 10'); "Castro 1" = shed_label 'Castro' AND partition_label '1'
  (the separate 'Castro 1' location rows hold no animals). Always name the park per row.
- sold / exits / entries -> animals_base -> exit_reason ('sold','died'), park_label -> exit_business_day / entry_date
- deaths / mortality -> mortality_base -> deaths + active population -> event_date
- births / transfers -> counts_movement_daily -> per pen counts -> event_date. "How many births": give BOTH herd-count births
  (sum births; includes a 5 Aug 2026 bulk entry of 458) AND individually registered kids (count public.goat_births), one line why they differ.
- feed directed vs fed -> feed_adherence -> directed_kg, fed_kg, variance_kg, blocked -> feed_day
- feed plan detail -> feed_direction_current; completions -> feed_completions_base (fed_business_day)
- vaccination now -> vaccination_shed_status; over time -> vaccination_obligations_base (due_business_day; status scheduled|completed|canceled|deferred|superseded;
  ~90% of rows are CANCELED re-plans: "due"/"upcoming" = status IN ('scheduled','deferred') only, one row per animal; never count canceled; e.g. next 7 days on 24/09 = 3, all CPT Yashoda); doses -> vaccination_dose_pickup; operators -> vaccination_operator_status; pre-arrival history review -> vaccination_prearrival_history_review (reviewed_date_ist)
- procurement -> procurement_pipeline (now), procurement_loads_base (period, entered_business_day), source_entry_health_status (intake variance)
- workforce -> workforce_tasks_base (due_business_day), workforce_coverage_status (now)
- pen capacity -> shed_capacity_current; inventory -> inventory_stock_position
- queues -> verification_queue_status, verifier_review_integrity; actions -> action_center_current, ops_exception_queue, sop_execution_status
- audit -> audit_activity_summary; notifications -> notification_delivery_health
No sales/revenue view: use public.sales_deals (sales_value, payment_received, buyer_name, status='Deal Closed', sale_date, farm CBE/CPT).
metrics -> how (exact defs + SQL: SKILL.md "Metric definitions"; never invent a proxy)
- ADG/daily gain: compute from per-animal weighs in public.weighing_observations (consecutive weigh-ins); say it may differ slightly from /weighing/analytics. Never tell the CEO only pen averages are readable.
- headcount: animal_current_scope lifecycle_status='alive'. sold: animals_base exit_reason='sold' by exit_business_day.
- cost per kg gain: feed bills (feed_purchases.total_cost by purchase_date, park) / kg gained by the SAME animals in the window, in one SQL.
  Label it an estimate, show both inputs; never extrapolate a subset ADG to the whole herd by hand.
- pen ADG between two weighings: whole-pen arm (SKILL.md) (last avg - first avg)*1000/days computed in SQL -> g/day; don't show the math.
- mortality %: sum(mortality_base.deaths in window)*100 / live 'alive' count now, 1dp (NOT active_population).
- weighing pending: pending+rework. feed fed_kg is always 0: say fed data missing. vaccination: due/done, no %.
Full columns + example per view: .agents/skills/mesha-data-map/references/views.generated.md
Never show ids, table/view names or internal notes in the answer (say "pen routines", not pen_routine_tasks).
Access: read every table (no filter rules); the database login is read-only.
Raw tables (all readable): feed prices -> public.feed_purchases (feed_item_label, farm_label CBE/CPT,
purchase_date, quantity_kg, reached_weight_kg, feed_cost, transport_cost, loading_cost, unloading_cost, total_cost, per_kg_cost;
"assumed price" = latest per_kg_cost for that feed+farm on/before the day). Per-weigh data -> public.weighing_observations /
weighing_shed_observations. Sales money -> public.sales_deals / sales_deal_lines / sales_deal_payments. Prefer these when a view lacks detail.
Module tables (public.*; pen/park names: join public.locations l ON l.location_id = shed_id / park_id, l.name; partition_label = pen part no.):
- preventive care (deworming, hoof trimming, feed & water removal) -> pc_care_tasks: category, work_state completed|canceled|delayed|scheduled,
  planned_business_date=planned, submitted_at=done, verified_at=verified, close_reason (often empty for old cancels). Cancelled != done.
  If planned work was cancelled with no submission, say it plainly, e.g. "Planned for 2 Sep, never submitted in the app,
  cancelled on 5 Sep. If it was done on the farm, it wasn't recorded." Records can be corrected later (canceled -> completed),
  so always re-query; never repeat an earlier answer from this chat.
  completed with submitted_by NULL = office correction, not a field submission: say so in the FIRST answer (who changed it via audit/updated_at,
  when, assigned operator) e.g. Castro CBE deworming: cancelled 05/09, set completed by Manohark 24/09 11:35 IST, no submitter/proof.
- Hinglish: "bike / bika / becha / bechi" = SOLD (sales_deals + animals_base exit_reason='sold'); "aaye / kharide / liye" = bought (procurement).
  "lakh" = /1,00,000, "crore" = /1,00,00,000; tonnes = kg/1000.
- "goats" in a question usually means all animals: count all species and split (e.g. "3 deaths: 1 goat, 2 sheep").
- pen visits -> pen_visit_tasks (reasons, work_state; delayed+submitted_at set = done late, awaiting verification). pen routines -> pen_routine_tasks (routine name: join pen_routine_definitions
  USING routine_id; work_state scheduled|delayed|completed|canceled, status open|pending_verification|completed|rework; planned_business_date,
  submitted_at=done, submitted_by/verified_by users; park_id/shed_id -> locations).
- shifts/pen moves -> shifting_events (event_status applied=done|authorized|pending|canceled; applied_at, source/destination_park_id+shed_id).
  counts_movement_daily has NO shift rows: never say "no shifts" from it.
- leadership / management tasks ("CPT pit work") -> leadership_tasks (task_no, title, status open|in_progress|done|cancelled, deadline_at, done_at). Not workforce tasks.
- animals vaccinated -> vaccination_completions (one row per goat dose; count(distinct goat_id)=animals, count(*)=doses; administered_at, status accepted).
- kid milk feeding -> milk_feeding_tasks (feeding_date, session_no, head_count, status). toxin tests -> toxin_test_tasks (outcome positive=fail, farm_label).
- feed wastage -> feed_wastage_completions (wastage_kg, target_date, status). weighing fasting -> weighing_fasting_tasks / weighing_fasting_shed_proofs.
- attendance -> workforce_clock_entries (clock_in_at). leave -> workforce_leave_requests (who: workforce_member_id ->
  workforce_members.workforce_member_id; status pending|approved|rejected|withdrawn; on leave today = approved AND today BETWEEN starts_on AND ends_on; park_id). market prices -> market_price_entries (city_name, question_label, price, business_date).
- health: health_cases (disease_name, status active|continued|recovered|referred|held_death_review|closed_dead|canceled; sick now = active|continued;
  start_date, diagnosed_by, closed_at; park_id/shed_id). Treatments: health_treatment_sessions (health_case_id -> case for park; business_date,
  session morning|afternoon|evening, status scheduled|due|in_progress|completed|rework|canceled*; completed_by). Medicines given:
  health_medicine_administrations (medicine_name, dosage_text, administered_at, administered_by, goat_id, health_case_id).
- stock on hand -> inventory_stock JOIN inventory_items USING (item_id) (name, category); available = quantity_in_stock - quantity_reserved,
  status='active', expiry_date, location_id -> locations. Item catalogue alone (inventory_items) is not stock.
- more modules: vendors -> procurement_vendors (record_type, status, city); buyer/FPO leads -> sales_buyer_leads / sales_fpo_leads (call_status);
  purchase candidates -> animal_purchase_candidates (decision, decided_by_name); feed transport/packing/distribution -> feed_transport_tasks /
  feed_packing_completions (packed_total_kg) / feed_distribution_completions; goats in wrong pen -> pen_reconciliation_cards; shift/death/birth
  approvals -> counts_approval_requests (decision_reason); config changes -> feed_config_write_log (actor_ref); tag/identity -> identity_decisions;
  RFID sensors -> herd_signal_tag_latest; growth sale price -> growth_sale_price_assumptions; sale allocations -> goat_sale_allocations.
- WHO: every *_by / *_user_id / actor_ref is a user id -> public.workforce_members.user_id -> display_name (one join, no searching).
- Pen names repeat across parks (e.g. Castro is in CBE and CPT): always name the park per row; no park given = answer each park separately.
- Money: feed "paid" = feed_purchase_payments.amount_rupees (paid_on), NOT feed_purchases.total_cost (= bill); owed = bill - payments per
  feed_purchase_id. "Paid this month" headline = sum(feed_purchase_payments.amount_rupees) by paid_on in the month; then a 2nd line "of which
  against this month's bills" (payments joined to purchases dated in the month). Ledger starts 03/09/2026; feed_purchases.payment_released =
  running released total (mirrors ledger), so vendor dues = payment_status='Pending' bills: total_cost - greatest(payment_released, ledger sum).
  Sales dues (status='Deal Closed'): due per deal = greatest(sales_value - payment_received, 0), summed per buyer over deals with
  payment_received NOT NULL. Never net an overpaid deal against another (list received > value separately as a data issue). payment_received NULL on
  older deals = not tracked, not proof of non-payment: list those separately, never headline as owed;
  payment ledger = sales_deal_payments (deal_id -> sales_deals.id, received_on, amount_rupees; ~11 rows, not empty). Say these caveats.
  ALWAYS cross-check (unprompted, same query) for the asked buyer/period: payment_received vs advance_amount + sum(sales_deal_payments.amount_rupees)
  per deal. If advance_amount AND ledger rows together make payment_received > sales_value (ledger alone ~= value), it is a DOUBLE COUNT: add a
  "Worth checking" line naming deal date, short id, value, received, the advance, each ledger row (amount, received_on, IST time) and WHO recorded
  them (recorded_by -> workforce_members.display_name). E.g. Mahendran 02/09 deal d393cdf4: value 1,97,415, received 3,94,830 = advance 1,97,415 +
  ledger 1,77,000+415+20,000 by Hemant 02/09 ~14:19 IST. Anomaly/"data entry mistakes"/"duplicates" questions: run this check for all deals, plus
  same buyer+value+date duplicates, received > value, sales_value 0 with money received, and in other modules same row repeated (same goat/item,
  amount, date, actor within minutes); report each with who entered it.
- RFID "not moving" = public.herd_signal_tag_latest.movement_state='not_moving' (zero motion in the latest 15-min window; states: moving/low/quiet/
  not_moving). It is LIVE and changes minute to minute: one query, give count of all tags (e.g. "12 of 19"), last_seen_at in IST, and list them;
  goat via tag mapping (mapping_state='mapped'). Sustained concern = pattern_state IN ('inactive' (3h+ quiet while packets arrive),'quiet_watch',
  'missing_signal'); say how many (often 0), and note no_movement alone for one window is normal resting. Don't re-query to "confirm" counts.
- Tag device health (= Live Monitor Status column): Missing signal = movement_state='stale'; Weak signal = signal_state='weak';
  Low battery = battery_state IN ('low','critical'); else Good. Never judge battery from battery_mv yourself (3000-3100 mV is
  healthy for these tags): if battery_state is healthy for all, say "no tag has low battery" and stop.
- Tag live data / "watch" / "keep watching" / "tell me when X stop(s)/start(s) moving" -> call the watch_tags tool (NOT repeated run_sql):
  filter = pen/park names, tag ids (A0002A) or animal ids (G-003659), 'all' = every tagged animal; minutes default 5 (max 30); stop_when
  any|all_stops_moving / any|all_starts_moving; compare self (vs own 24h p75 pace, Insights risk rule: <=-70% far below, >=+150% spike) | peers
  (vs pen median right now, <=-70% lower than pen) | both. One-shot "which goats are slower than their pen/own pace now" -> watch_tags minutes=0
  compare=both. Rows: herd_signal_tag_latest (motion_count, 15-min motion_delta, movement_state, last_seen_at, last_rssi_dbm, battery_mv) ->
  goat_identifiers.normalized_value -> goats.shed_id/park_id -> locations.name; own baseline = herd_signal_activity_windows 300s tier, 24h.
  The user sees the live table; answer from the returned summary in 2-4 sentences.
Before saying "not recorded"/"none": search table names + information_schema.columns for the keyword (ILIKE '%deworm%'), then category/status values. Empty table (0 rows) = say plainly "not recorded in the app yet" in one line, no long search. Only say
"not recorded" after that search finds nothing; say which park/pen/status you did find (e.g. "all Castro CBE tasks were cancelled").
- Who changed/corrected a record: public.audit_log WHERE resource_id = <record id> (action e.g. pc_care.task.canceled / sales.deal.payment_record), actor_id -> workforce_members.user_id for the name; always name them in the first answer.
- When advance_amount equals the ledger total and both are counted, state it as a double count (not "possible").
- Pen shorthand: users write pens as initials + numbers: C1 = Castro 1, G2P1 = Godel 2 Part 1, M1P3 = Mandela 1 Part 3, S2 = Sumathi 2, Y1 = Yashoda part 1 (CBE and CPT) and Old Yashoda part 1 (CPT only) - give each with its park, H1 = Ho Chi Minh 1, Q1 = Q1. Resolve any code by matching public.locations (location_type='shed') names; if a letter fits two pens (G = Godel or Gandhi), pick the one that exists in the asked park, say which you assumed. Castro/Godel/Mandela etc. exist in BOTH parks (CBE = Coimbatore, CPT = Channapatna): split by park unless named.
- "Load wise" / load weights (Sales module > loads): public.procurement_loads, one row per purchase load (context->>'load_ref' = load no. e.g. 136, context->>'farm' = CBE|CPT). Avg purchase weight = purchase_weight_kg / expected_count; avg sold weight = sold_weight_kg / sold_weighed_animals; fattening_days. Pen -> load link: public.weighing_shed_load_tags (location_id = pen, load_ref) e.g. CBE Castro 1=126, Castro 2=130, Castro 3=128; CPT Castro 1+2=131, Godel 2 Parts 1+2=129. Fallback: load notes (136 -> CBE Godel 2 Parts 1-6) or procurement_load_goats.goat_id -> animal's current shed. Procurement weight = purchase weight (not sales weight) even though the screen sits in the Sales module.
