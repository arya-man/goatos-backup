## Mesha data map (EVERY table is readable; ceo_ai.* views are shortcuts, raw public.* tables have full detail — use them when a view lacks it)
**Cross-check before answering any count/total:** if the fact can be recorded in more than one place (event log vs audit_log, deals vs allocations, per-animal vs whole-pen weighing, herd counts vs registered animals, task status vs work_state), query BOTH in the same turn. If they agree, answer. If they differ, answer from the more complete source (the one the app screen uses, or the per-record log) and add a "Worth checking" line with both numbers. Never report a count from a single source you have not checked is complete.
**Numbers in this map are EXAMPLES from 24/09/2026 for checking your query. NEVER answer from them; every figure in an answer must come from a query run in this conversation turn.**

Today = (now() AT TIME ZONE 'Asia/Kolkata')::date. Weekday names: take from to_char(d,'Dy') in the query, never work them out yourself. Parks: Coimbatore (CBE), Channapatna (CPT) in park_label.
HARD RULES: (a) Species: never call sheep "goats". If the question says goats/bakre/animals, answer "N animals (X goats, Y sheep)"; species column is 'goat'|'sheep'.
  "bakre"/"goats" with no explicit sheep-vs-goat contrast = ALL animals: "Total bakre, how many male?" -> "1,562 animals (693 goats, 869 sheep);
  578 male (116 goats, 462 sheep)". Only "goats only / not sheep" means species='goat'.
  ALWAYS state the split in the answer, even when one side is 0 (e.g. "80 animals, all sheep, no goats: 49 CBE, 31 CPT").
(b) Arithmetic: every total, difference, %, ratio, per-day or per-animal figure is computed IN SQL (sum/-/ /round) and read back; never add or subtract by hand.
(c) Simple lookups (headcount, one pen, one number) = ONE query, answer immediately; no exploration, no re-check.
(d) REFERENCE FILES (references/*.sql below): use run_reference('<file>', where=...) instead of retyping - never copy them into run_sql.
  where = SQL filter on the file's output columns; windows via params only (adg-by-park.sql {from_date,to_date}, adg-by-breed.sql {from_date,to_date,park_code,sex,origin,weighing}, cost-per-kg-gain.sql {from_date,to_date,park_code,sex,origin,weighing}).
PENS (model-agnostic): pen = G1P3 "Godel 1 Part 3", C1 "Castro 1"; group = Godel 1 / Castro (a GROUP, never call it a shed). ALWAYS resolve
  pens via .agents/skills/mesha-data-map/references/pens.sql (copy its CTEs + ONE lateral join on (location_id, partition_label)); never assume
  animals/weighs sit on the group row or on the pen row ("Godel 1 - Part 3"): the data may use either. If a pen looks empty, check both placements
  (pens.sql does). Show "Godel 1 Part 3 (G1P3)" + park. Dates DD/MM/YYYY. Never average *_avg_* columns; weight by scan_count.
  Pen LAST WEIGHING (any pen/group) = run_reference('pen-weighing-latest.sql', where="pen_code='G1P3' AND park_code='CBE'") once: individual + whole-pen, no other query.
Comparisons ("compare CBE and CPT", "how are we doing"): <=8 short lines, one per topic, numbers from SQL; every comparative word
(more/less/bigger) must match the numbers. Vague "how are we doing" = headcount, sales, deaths, weighing, feed, open issues, this month.
No date column = current-state view: answer "as of now".

topic -> view -> key columns -> date column
- weighing dates/progress -> weighing_capture_activity -> park_label, shed_label, work_state ('completed','closed'), animals_weighed, weighing_category -> planned_business_date
- weights per round -> weighing_capture_activity -> sum(scan_weight_avg_kg*scan_count)/sum(scan_count) -> planned_business_date.
  "Average herd weight"/current avg weight = run_reference('herd-avg-weight.sql') (latest weigh per ALIVE animal,
  pen average for pens weighed whole); say how many of the herd it covers (24/09: 28.2 kg, 723 of 1,562).
- weighing verification -> weighing_verification_status -> pending, rework, verified, oldest_pending_at -> none. "Pens behind on verification" = rows with pending>0 OR rework>0 (one row per pen; count rows per park_label, and report sum(pending) and sum(rework) separately: pending excludes rework). BY PERSON: raw
  public.verification_items (module='weighing'; status pending=awaiting verifier, rejected=sent back for rework, approved, withdrawn;
  operator_id=who captured, verified_by/verified_at=verifier; captured_at). Overdue = pending and captured_at older than 24h (verification SLA).
  "Who verified most" = verification_items by verified_by, verified_at in window, all modules (auto-approved by system = auto_resolution='not_sampled', 713 on 25/09; do NOT use verified_by NULL - 11 non-auto approvals also have it).
- headcount now -> animal_current_scope -> park_label, species, sex, breed, management_stage, lifecycle_status='alive' -> none.
  The view ALSO holds sold/dead/inactive rows: every count, %, ratio or split MUST filter lifecycle_status='alive' (CBE alive=850, not 995).
  ONE-QUERY headcount: SELECT park_label, species, count(*) FROM ceo_ai.animal_current_scope WHERE lifecycle_status='alive'
  GROUP BY ROLLUP(park_label, species). No park named = both parks, park named per row.
  Per-PEN counts / "which pen has most": run_reference('pens.sql', where="pen_code='Y3'") (alive per pen, both parks, both placements;
  columns park, pen, park_code, pen_code, grp, alive, goats, sheep) - never filter shed_label/partition_label by hand.
  Always name the park and species split per row.
- sold / exits / entries -> animals_base -> exit_reason ('sold','died'), park_label -> exit_business_day / entry_date
- deaths -> public.goats: (exit_reason='died' OR (exit_reason IS NULL AND lifecycle_status='dead')) AND merged_into_goat_id IS NULL,
  date = (COALESCE(exited_at, updated_at) AT TIME ZONE 'Asia/Kolkata')::date (app rule); split park + species (Sep 2026: 3, all CBE: 1 goat, 2 sheep). mortality_base agrees (6) (see traps).
- births -> date by goats.dob like the Herd Analytics screen (counts_movement_daily dates births by IMPORT day: Aug view 466 vs screen 0), flag placeholder DOBs; transfers/shifts -> shifting_events only (counts_movement_daily looks for event_status='completed' but the app writes 'applied', so its shift/transfer counts are always 0). "How many births": give the screen number (goats origin_type='birth' by COALESCE(dob, entry_date, created_at IST); Aug 2026 0, Sep 1)
  AND individually registered kids (public.goat_births by (created_at AT TIME ZONE 'Asia/Kolkata')::date; it has NO birth_date column;
  Sep 2026: 1, 16/09). Never report counts_movement_daily.births as births (Aug 466 = 05/08/2026 IST bulk import of 458 + 8).
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
- ADG/daily gain = app Weighing > Growth (ADG): run_reference('adg-by-park.sql') (this month to date;
  other window: params {from_date:'YYYY-MM-DD', to_date:'YYYY-MM-DD'}). Scanned kids (per-animal grams/days) + whole pens weighted by head count. It matches the
  app exactly (01-24/09: CBE 152, CPT 148, all 150 g/day). NEVER write your own ADG SQL or pick a different weighting.
- ADG BY PARK with a sex / origin / weighing filter (e.g. "males, 03/08-29/09, by park"): the answer shape is BINDING
  = one line per PARK (Coimbatore, Channapatna) + the all-parks figure, never breeds. adg-by-park.sql has no sex filter, so run
  adg-by-breed.sql once per park_code (CBE, CPT) plus once with no park_code, all in ONE turn, and give each park
  ADG = sum(gain_g_per_day x gain_animals) / sum(gain_animals), animals = sum(gain_animals). Breeds only if asked.
- ADG / daily gain / average weight BY BREED, or filtered by sex, origin (farm born/purchased), weighing type (individual/whole pen)
  or a period = app Weighing > ADG Analytics > Breed-wise tab: MUST run_reference('adg-by-breed.sql', params={from_date, to_date,
  park_code, sex, origin, weighing}) with the user's filters (defaults: weighing all = both types, sex all, origin all, all parks; relative
  periods like "last 3 weeks" -> compute from_date/to_date from today IST). Never improvise or re-derive it. Columns: breed,
  gain_g_per_day + gain_animals, avg_weight_kg + weight_animals (already rounded as the screen shows). Answer with those numbers and
  ALWAYS state the filters used (period DD/MM/YYYY-DD/MM/YYYY, park or "all parks", weighing, sex, origin). If a screenshot of the dashboard is attached,
  read its Period/Park/Weighing/Sex/Origin filters and pass them. (04-22/09 male: Anantapur Sheep 160 g/day, 392 animals; 31.4 kg, 440.)
- headcount: animal_current_scope lifecycle_status='alive'. sold: animals_base exit_reason='sold' by exit_business_day (see traps: deals).
- cost per kg gain = the app's Weighing > FCR tab "Feed cost per kg gain": per pen, consecutive weighing rounds; cost = DIRECTED feed
  (feed_direction_issue_rows, issued/amended/locked) x latest same-park per_kg_cost on/before each feed day; gain kg = pen ADG x fed head-days.
  Only pens weighed twice count, so feed and gain are the SAME animals. MUST run_reference('cost-per-kg-gain.sql', params={from_date, to_date, sex, ...}) (03/08-23/09:
  all sexes Rs 334/kg, male Rs 318 = the tab; tab default sex is male). NEVER divide whole-park feed bills by a weighed subset's gain. Answer: Rs/kg per park + total, feed Rs and kg gain, 1 line method.
- WEEKLY ADG trend / chart: a week's ADG needs the SAME animal (or pen) weighed twice with both weighings inside that
  week's window; pens are weighed ~weekly, so 7-day windows are mostly empty. Use run_reference('adg-by-park.sql') per
  week with a 14-day trailing window (from_date = week end - 13) or per fortnight/month, and say which. A week with no
  pair is "not enough weighings" (null in the chart, never 0). A negative ADG (e.g. -230 g/day) is a data signal, not a
  trend point to plot silently: name the pen/dates behind it and add a "Worth checking:" line.
- "No data available" on Weighing > Growth rows (growth-director.tsx): the pen has only one weighing in the window, so
  there is no gain yet; it fills in after the pen's second weighing. Say that, with the pen's last weighing date.
- pen ADG between two weighings: whole-pen arm (SKILL.md) (last avg - first avg)*1000/days computed in SQL -> g/day.
- ADG / daily-gain ANSWER SHAPE: headline g/day per park (and total), how many animals/pens it covers, then ONE line of method
  ("from animals/pens weighed twice this month, gain / days between weighings"). NO per-pen table of first/last avg/days unless asked;
  at most name the 1-2 outlier pens in one "Worth checking" line.
- "Which pen has the most/least X" (animals, deaths, weight...): rank PENS = pens.sql pen_key (park + group + part),
  e.g. most animals = pen Castro 2 (C2), Coimbatore: 73 (all sheep). Pen + park + number + species first; the group
  total (Castro, Coimbatore 181 across 3 pens) only as context after. Rank groups only if the user says "group"/"shed".
- mortality %: deaths in window (goats rule above) *100 / live 'alive' count now, 1dp (NOT mortality_base.active_population).
- weighing pending: pending+rework. feed fed_kg is always 0: say fed data missing. vaccination: due/done, no %.
- feed head count: the feed sheet covers ~1,570-1,580 head per day (both workflows, pen-grain MAX(head_count); 1,579 on 23/09) vs 1,562 alive in the register - close but not the same source; herd size always from the register, sheet heads only for per-head feed; say so if asked.
Full columns + example per view: .agents/skills/mesha-data-map/references/views.generated.md
Never show ids, table/view names or internal notes in the answer (say "pen routines", not pen_routine_tasks).
Access: read every table (no filter rules); the database login is read-only.
Raw tables (all readable): feed prices -> public.feed_purchases (feed_item_label, farm_label CBE/CPT,
purchase_date, quantity_kg, reached_weight_kg, feed_cost, transport_cost, loading_cost, unloading_cost, total_cost, per_kg_cost;
"assumed price" = latest per_kg_cost for that feed+farm on/before the day). Per-weigh data -> public.weighing_observations /
weighing_shed_observations. Sales money -> public.sales_deals / sales_deal_lines / sales_deal_payments. Prefer these when a view lacks detail.
public.locations real columns (no park_code / park_name / pen_code column exists): location_id, location_type
  (park | shed | ...), location_code (park code CBE/CPT/PARIGI, or shed code), name, parent_location_id (shed -> park),
  status, display_order, retired_at. Park code -> WHERE location_type='park' AND location_code='CBE'.
Module tables (public.*; park name: locations via park_id; PEN = pens.sql lateral join on (shed_id, partition_label), never locations.name alone):
- preventive care (deworming, hoof trimming, feed & water removal) -> pc_care_tasks: category, work_state completed|canceled|delayed|scheduled,
  planned_business_date=planned, submitted_at=done, verified_at=verified, close_reason (often empty for old cancels). Cancelled != done.
  work_state LAGS status: DONE = submitted_at set (status pending_verification = awaiting check, completed = verified), even when work_state
  says delayed/scheduled. OPEN = submitted_at NULL and work_state NOT canceled/closed (closed = closed by office: close_reason, closed_by) (24/09 dewormings: 0 open; 10 awaiting verification).
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
- animals vaccinated -> vaccination_completions (one row per goat dose; count(distinct goat_id)=animals, count(*)=doses; administered_at IST, status accepted).
  "Done in the app" = sop_submission_item_id IS NOT NULL; rest are sheet imports: give both (Aug 2026: 1,073 doses/710 animals in app, 1,672/1,055 total).
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
- feed stock / days of cover / "kitna din chalega" = app Feed Analytics > Stock: run_reference('feed-stock-days-left.sql') (purchase ledger minus locked sheet issues, 3-latest-day burn rate,
  split concentrates merged). NOT inventory_stock, NOT your own 14-day average. 25/09: CBE UHT milk 4 days (CPT 7), concentrates 13-14, bhusa 18-22.
  Answer: shortest days-left first, per park; items shown as not_started = stock but no use yet.
- feed wastage %: feed_wastage_completions.wastage_kg (completed) / directed kg (feed_direction_issue_rows, issued/amended/locked) ONLY for the same pen-days that have a completed wastage_kg (join on pen + target_date; never the whole month's directed feed), in SQL.
  pending_verification rows have wastage_kg NULL: say how many pen-days have no wastage kg yet (e.g. 95 in Sep), don't call them 0.
- vendor delivery delays: no promised/ETA date is stored. Feed vendors = feed_purchases (vendor, farm_label, purchase_date -> reached_on,
  delivery_status); animal loads = procurement_loads (purchase_date -> arrived_on). Answer days from purchase to arrival per vendor
  (e.g. Sep: Farukh 4 days, Sanchit up to 3, Navaladi 1-2) + list not-yet-reached; say lateness vs a promise can't be judged. Cover BOTH.
- pen visit "reasons" = WHY the visit is planned (deworming/hoof_trimming), NOT why it was late. No delay-reason field exists: for delays
  give counts by visit purpose + delayed_since_business_date / rolled_forward_count, and say the app doesn't record a reason for the delay.
  Split delayed into submitted late (submitted_at set, awaiting verification; 24/09: 20 of 26) vs still not done (6).
- animals ready for sale by weight: latest individual weigh (app filter) per alive animal (weighing_observations -> goat_identifiers ->
  goats) above X, split park + species, with how recent. Also say pens weighed only as a whole pen have no per-animal weight: list pens whose
  latest whole-pen average (weighing_shed_observations) is above X with head count as "likely".
- app usage: analytics.app_events (actor_id -> workforce_members.user_id, event_name, received_at; filter on received_at for speed; last 7
  days = received_at >= now()-interval '7 days'). Report active days + screens opened (event_name='route_entered') alongside total events.
- verification rejections: verification_items status='rejected', verdict_reason (free text, typos): give total + by module first, then group
  reasons in SQL with ILIKE buckets (video/vedio not playing, water not visible, wrong pen said, weight not clear) with counts.
- clocked hours: workforce_clock_entries.worked_minutes by business_date, status closed|auto_closed (open = still on shift, name them).
  auto_closed = system closed a forgotten clock-out: its minutes are NOT real hours. Per person give TWO numbers in SQL: hours from closed
  shifts, and auto-closed hours (unverified, forgot to clock out), never one summed total (e.g. Ravi Kumbar 21-23/09: 3 auto-closed shifts, 32.2h).
- WHO: every *_by / *_user_id / actor_ref is a user id -> public.workforce_members.user_id -> display_name (one join, no searching).
- Pen names repeat across parks (e.g. Castro is in CBE and CPT): always name the park per row; no park given = answer each park separately.
- Money: feed "paid" = feed_purchase_payments.amount_rupees (paid_on), NOT feed_purchases.total_cost (= bill); owed = bill - payments per
  feed_purchase_id. "Paid this month" headline = sum(feed_purchase_payments.amount_rupees) by paid_on in the month; then a 2nd line "of which
  against this month's bills" (payments joined to purchases dated in the month). Ledger starts 03/09/2026; feed_purchases.payment_released =
  running released total (mirrors ledger), so vendor dues = payment_status='Pending' bills: greatest(total_cost - coalesce(payment_released,0),0) (code PaymentBalance; Rs 22,71,081.22 on 31 loads 25/09); ALSO list 'Paid' bills whose released < total (28 on 25/09, e.g. Hemant 2,02,262 billed / 95,300 released) as 'marked Paid but short'.
  Sales received = sales_deals.payment_received (what the Sales screen shows): a RUNNING TOTAL seeded from advance_amount, +each
  sales_deal_payments row. Never add advance + ledger + payment_received. Sales dues (status='Deal Closed'): due per deal = greatest(sales_value - payment_received, 0), summed per buyer over deals with
  payment_received NOT NULL. Never net an overpaid deal against another (list received > value separately as a data issue). payment_received NULL on
  older deals = not tracked, not proof of non-payment: list those separately, never headline as owed (24/09: owed Rs 4,50,175 on 36 tracked
  deals; 40 untracked deals worth Rs 37.4 lakh are a separate line);
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
TWO-SOURCE TRAPS (pick the source below; details + SQL in SKILL.md "Two-source traps"):
- deaths: public.goats rule above; mortality_base agrees with goats on STG (6 deaths; its "death/dead/mortality" filter is only in the rollback half of migration 000358).
  CAUSE OF DEATH = public.health_death_causes (goat_id, cause_key e.g. ACIDOSIS / fever, cause_kind, health_case_id, recorded_by,
  recorded_at): ALWAYS LEFT JOIN it ON goat_id in the same deaths query (string_agg(cause_key) per animal) and give each death's
  cause; only animals with no row there are "cause not recorded" (1-24/09: goat 23/09 acidosis, sheep 17/09 fever, sheep 05/09 none).
- sold: animals sold = goats register (lifecycle 'sold', exited_at IST) = tagged allocations; closed-deal Goat+Sheep line animals are the commercial
  count incl. pre-app deals (688 closed-deal line animals all-time vs 160 in register; header sales_deals.animal_count 706 wrongly adds 18 on manure deals). Answer the register, add the deal count in one line when they differ.
- revenue = sales_deals status 'Deal Closed' sum(sales_value) by sale_date; pipeline = any other open status (none on 24/09: say so).
- births: goats.dob screen rule + goat_births; never counts_movement_daily.births or goats.created_at (bulk import 05/08/2026 IST); flag placeholder DOBs (farm-born 09/05/2026 x403, 21/07/2024 x77).
- species: public.goats holds sheep too; always split species. "goats" in a load/pen with only sheep = say sheep.
- alive: 'alive' only for headcount; "on farm" incl. sick/under_treatment/quarantine/icu (0 today). Base views include exited rows.
- pen of an animal: pens.sql on (goats.shed_id, goat_shed_partitions.partition_label), NOT current_location_id (18 differ).
- vaccination done: in-app (sop_submission_item_id) vs imported; due = obligation_instances status scheduled|due|in_progress|deferred, one per animal; vaccination_shed_status OMITS deferred and has whole-shed + per-pen rows (sum only partition_label IS NULL rows, else double counts).
- feed: stock = purchase ledger SQL (not inventory_*); fed_kg is empty; packed != directed != fed. Owed: payment_status 'Paid' = 0 owed;
  empty '' status (40 sheet-import rows, no total_cost; not NULL) = unknown, list separately.
- load cost: animal_cost/transport_cost/other_cost are TOTALS; procurement_load_cost_lines is their breakdown - never add both.
- weighing: latest weigh per animal, same filter as the app screens so numbers match the dashboard. Known app bug: the <> 'rejected' filter never matches (a sent-back weigh is 'rework'), so 16 rework weighs (9 in Sep) are inside app and chat averages/ADG; all were re-weighed later, so latest-per-animal is unaffected. Mention it only if asked about weighing accuracy (weight_kg = corrected); whole-pen weighs separate, exclude withdrawn.
- shifts done = shifting_events event_status 'applied' (applied_at IST); authorized/pending = not moved yet.
- location history: reasons with correction/repair/revert/swap fix data, not real moves: exclude from movement analytics. count_projection_snapshots: all blocked, don't use.
- text buckets: lower(age_band); group breed by goats.breed text (breed_id NULL on new animals); origin_type NULL = "unknown".
Before saying "not recorded"/"none": search table names + information_schema.columns for the keyword (ILIKE '%deworm%'), then category/status values. Empty table (0 rows) = say plainly "not recorded in the app yet" in one line, no long search. Only say
"not recorded" after that search finds nothing; say which park/pen/status you did find (e.g. "all Castro CBE tasks were cancelled").
- Who changed/corrected a record: public.audit_log WHERE resource_id = <record id> (action e.g. pc_care.task.canceled / sales.deal.payment_record), actor_id -> workforce_members.user_id for the name; always name them in the first answer.
- When advance_amount equals the ledger total and both are counted, state it as a double count (not "possible").
- Pen shorthand: users write pens as initials + numbers: C1 = Castro 1, G2P1 = Godel 2 Part 1, M1P3 = Mandela 1 Part 3, S2 = Sumathi 2, Y1 = Yashoda part 1 (CBE and CPT) and Old Yashoda part 1 (CPT only) - give each with its park, H1 = Ho Chi Minh 1, Q1 = Q1. Resolve any code with pens.sql pen_code (+ park_code); if a letter fits two pens (G = Godel or Gandhi), pick the one that exists in the asked park, say which you assumed. Castro/Godel/Mandela etc. exist in BOTH parks (CBE = Coimbatore, CPT = Channapatna): split by park unless named.
- "Load wise" / per purchase load (Sales > Load-wise): run_reference('load-wise-sales.sql') (one load:
  where="load_no='126'"). Load no. = procurement_loads.context->>'load_ref', farm = context->>'farm'. sold = GoatOS-tagged sales + pre-GoatOS
  prior outcomes; sold value = deal value split over its tagged animals + prior value; SCREEN price/kg = sold_weighed_value/sold_weight_kg
  (legacy loads 100/101/113 only; blank on others: say "not weighed at sale", then give tagged_rate_per_kg_estimate labelled as estimate,
  e.g. load 126: 12 sold, Rs 1,93,621, ~Rs 428/kg est.). Days on farm = today - arrived_on (not purchase_date) while animals remain
  (126: 135 days); fattening_days column only on sold-out legacy loads. Purchase weight/expected_count is kg per animal, never a price.
  Pen -> load: public.weighing_shed_load_tags (location_id, load_ref), e.g. CPT Castro 1+2=131 (63 SHEEP, none sold).
- Animal history (stage / exit / pen): public.goat_identity_events is the COMPLETE per-animal event log — event_type goat.created (starting stage/pen), goat.stage_changed (payload management_stage, previous_management_stage, reason; from 18/08/2026), goat.exited (death/sale), goat.location.changed, goat.health.changed. audit_log has only a few of these; never count stage changes from audit_log. Stage on a past date = latest goat.stage_changed on/before that date, else the goat.created stage. Label office corrections (reason like "Tag corrected", "repair") separately from farm moves. Stage changes before 18/08/2026 are not recorded.
- "Animals weighed" in a period = DISTINCT animals, per park: (a) per-animal: count(DISTINCT scanned_identifier) in weighing_observations accepted in the period, plus (b) whole-pen: the LATEST weighing_shed_observations.animal_count per pen in the period (join weighing_campaign_sheds on campaign_shed_id; key park_id+location_id+partition_label; withdrawn_at IS NULL) — never sum whole-pen counts across rounds (pens are weighed ~weekly). Sep 1–24 check: CBE 287+244=531, CPT 211+139=350 (881 of 1,562 alive). Summing rounds gave 2,656 = weighings, not animals. A count above the herd is always wrong.
- Names in a question can be a pen, park, breed, stage, species, person, vendor or buyer — never assume "pen". Before saying "no such X", fuzzy-match the word (ILIKE, typos like Anatpur ~ Anantapur) against: goats.breed text + breeds table, locations names, management_stage values, workforce_members names, procurement_vendors, sales buyer_name. E.g. "Anatpur sheep" = breed "Anantapur Sheep" (59 alive have breed text but no breed_id: match on goats.breed text).

## Logic cards (open before answering any screen number)
Path: .agents/skills/mesha-data-map/references/logic/<card>.md — each card = screen, endpoint, code refs, formula, runnable SQL, stg figures, traps. Match the question to a card:
- adg-analytics-charts: ADG Analytics — charts, cards and drawers not fully covered by adg-analytics.md
- adg-analytics: ADG Analytics (admin-web Weighing > ADG Analytics, /weighing/analytics)
- approvals: birth/death/shift approvals page, pending counts
- audit: Audit logic cards
- births: Logic card: births (kids born, birth approvals, farm-born)
- calendar: Logic card: Calendar (vaccination drives) and Protocol Adherence
- configuration: Configuration logic cards
- control-tower: Logic card: HOME ("/") and CONTROL TOWER (/?lens=control-tower)
- counts: Counts > Breakdown + Milk preparation: logic cards (verified 24/09/2026, goatos-stg)
- deaths: Logic card: deaths (recording, approval, Herd Analytics deaths)
- fcr: FCR tab (Weighing > ADG Analytics > FCR)
- feed-purchases: Feed purchases & payments (/procurement/feed-purchases)
- feed-stock: Feed Analytics > Stock tab: logic card
- feed: Feed Analytics (directed sheet, fed, cost, wastage, pen feed, rates)
- growth-director: Growth Director + Sale readiness — screen logic
- health: Logic card: HEALTH (Health > Health Analytics, /health/analytics)
- herd-signals: Herd Signals (RFID / BLE smart tags) logic card
- herd: Counts > Herd register + Analytics composition: logic cards (verified 24/09/2026, goatos-stg)
- identity: Goat passport + identity (tags, RFID, sale allocation): logic cards (verified 24/09/2026, goatos-stg)
- inventory: INVENTORY / STORES (vaccines + medicines)
- milk: Milk Preparation & Milk Feeding: logic card
- mortality: Logic card: Counts > Mortality (GET /counts/mortality)
- operations: Operations logic cards (DLQ Center, CEO AI trace)
- pen-visits: Logic card: Pen visits and pen routines
- people: People — attendance, clock-in, leave, vaccination operators, notifications
- preventive-care: Logic card: Preventive Care (deworming, hoof trimming, feed & water removal, inventory vaccine)
- procurement: Procurement logic cards
- sales: Sales logic cards (admin-web /sales/*; verified on goatos-stg 24/09/2026)
- shifting: Logic card: shifting (pen moves / transfers)
- tasks: Leadership tasks /tasks — status, overdue, attachments
- toxin: TOXIN TESTS (aflatoxin strip test per feed load)
- vaccination: Vaccination logic card (admin-web /vaccination, /vaccination/plan, /vaccination/live-tracker, passports)
- verification: Verification queue (/verify; /verification redirects to /verify)
- weighing: Weighing — admin-web /weighing/weights (logic map)
- work-board: Logic card: WORK BOARD / ACTION CENTER / ALERTS / LEADERSHIP TASKS
- Upcoming vaccinations ("what's due", "next vaccinations"): give the NEXT due dates with counts (group by due_business_day,
  status scheduled/deferred, earliest 3 dates, DD/MM/YYYY), e.g. "1 on 14/10, 58 on 20/10" — never only the total of all future doses.
- Deaths: when the cause is not recorded, say so ONCE for the whole list, not per animal.
- ONE-QUERY SHORTCUTS (answer in 1-2 turns, no exploration): feed paid vs billed this month = ONE run_sql with two scalar
  subqueries: sum(feed_purchase_payments.amount_rupees) by paid_on in the month, sum(feed_purchases.total_cost) by purchase_date
  in the month (0 = say "Rs 0", not "nothing recorded"). Vendor dues = ONE run_sql: sum(greatest(total_cost - greatest(coalesce(
  payment_released,0), coalesce(ledger paid,0)),0)) over payment_status='Pending' bills, ledger paid = sum(feed_purchase_payments)
  per feed_purchase_id; plus the 'marked Paid but short' count in the same query.
- WEEKLY GROWTH TIMELINE (average weight per week per pen, Weighing > growth timeline): whole-pen weighs =
  weighing_shed_observations (average_weight_kg, accepted_at, withdrawn_at IS NULL) JOIN weighing_campaign_sheds USING
  (campaign_shed_id) -> locations l ON l.location_id = cs.location_id (pen name) and cs.park_id -> park. Per pen per IST week
  (Monday start) take the LATEST weighing in that week (array_agg ... ORDER BY accepted_at DESC)[1], like the dashboard; never average
  several weighs in a week. Individual weighs (weighing_observations) only when the pen has no whole-pen weigh that week.
- PARKS that exist in live data: Coimbatore (CBE), Channapatna (CPT), Mesha Biome Parigi (PARIGI). Any other park code or
  name (e.g. WG-PARK = "Weighing E2E Park", a test fixture that is not in live data) is NEVER mapped or guessed to a real park:
  say that park does not exist in the live records, list the real parks, and still explain the general rule the question
  is about (e.g. "No data available" = a pen needs two weighings in the window before it has a daily gain).
- Weekly growth timeline answers: a table with ONE ROW PER WEEK (week start DD/MM/YYYY) and one column per pen, every week
  in the asked range, not only the first and last.
