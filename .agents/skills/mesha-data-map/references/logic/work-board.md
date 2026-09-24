# Logic card: WORK BOARD / ACTION CENTER / ALERTS / LEADERSHIP TASKS

Index: Work Board lane counts (To do / In progress / In review / Done) · needs-attention · per-module counts · Action Center vaccination work states (due / overdue / deferred / missed / blocked / verification pending) + total · Alerts (pen feed did not follow head count, feed stock running out, optional event alerts) · Leadership Tasks (open / in progress / done / overdue)

## 1. Work Board (/work-board)

- Page: `apps/admin-web/features/work-board/work-board-page.tsx`; filters in URL: `park` (top-bar scope), `date` (business day, default today IST), `module`, `state`, `owner`.
- Endpoints: `GET /work-board/summary` (lane headers + KPIs), `GET /work-board/page`, `GET /work-board/rows`, `GET /work-board/rows/{row_key}/subtasks` (`backend/internal/workboard/adapters/http/handler.go:60-64`).
- The board owns NO table. `workboard/app/service.go:212` Summary calls each module's `CountByState` for ONE park + ONE business date and folds them with `domain.Summary.Add` (`workboard/domain/types.go:468`). "All parks" = the page calls once per park and SUMS (page L64-91).
- Lane = `LaneFor(work_state)` (`types.go:123`): in_progress/proof_pending/rejected/blocked -> **In progress**; verification_pending -> **In review**; completed -> **Done**; everything else (scheduled, due, overdue, deferred, missed) -> **To do**.
- Needs attention = states overdue + missed + rejected + blocked (`types.go:482`). Total = sum of all states. A module whose count read fails is listed in `degraded` and is MISSING from totals.
- Filters -> SQL: `date` -> each source's date column = $date; `state` -> `board_state = ANY(...)`; `owner` -> owner column = user OR unassigned (pool rows always included); `module` -> which sources run.

| Module | Source (file:line) | Rows | Date column | State mapping (first match wins) | Excluded |
|---|---|---|---|---|---|
| feed | feeddirection/adapters/boardsource/source.go:105,127,159 | ONE card per pen per day rolled up from transport, sheet sessions (feeding, packing), wastage | `feed_transport_tasks.business_date`, issued sheet `feed_day`, completions `target_date` | completed / verification_due->verification_pending / rework->rejected / else due; pen card takes rolled lane rank | `status='retired'`; sheet only `state IN (issued,amended,locked)` |
| health | health/.../boardsource/source.go:71,92 | `health_treatment_sessions` (park from case) | `business_date` | scheduled & due_at<=now -> due; scheduled; due; in_progress; completed; rework->rejected; held_death_review->blocked | canceled, canceled_death |
| vaccination | processintegrity/.../boardsource/source.go | Action Center grains (see 2) for the day | planned/due day | Action Center work_state | canceled |
| weighing | weighing/.../boardsource/source.go:77-127 | `weighing_work_items` JOIN `weighing_campaign_sheds` (bucket) | `due_business_date` | item or bucket closed -> completed; item or bucket completed -> verification_pending; bucket in_progress + any rework scan -> rejected; delayed & due-planned > 2 days -> overdue; bucket in_progress; else due | item OR bucket canceled |
| counts | counts/.../boardsource/approvals.go:69,127 | `counts_approval_requests` birth/shifting/death | IST day of `raised_at`; park from payload (death: goat's park) | pending->due; rejected; non-shifting approved->completed; shifting by `shifting_events.event_status` applied/pending_verification/rejected/authorized->in_progress | approved shifting whose event is canceled |
| milk | counts/.../boardsource/milk_feeding.go:64,80 | `milk_feeding_tasks` | `feeding_date` | not_submitted -> scheduled (due_at future) / due; pending_verification; completed; rework->rejected | retired |
| pc_care | pccare/.../boardsource/source.go:69-127 | `pc_care_tasks` | `due_business_date` | **status first**: completed; pending_verification; rework->rejected; then work_state closed->completed; delayed->overdue; any animal scanned->in_progress; else due | work_state canceled |
| tasks | penvisits/.../source.go:63; penroutines/.../source.go:58 | `pen_visit_tasks`, `pen_routine_tasks` | `due_business_date` | status completed / pending_verification / rework->rejected; work_state delayed->overdue; else due | work_state canceled |
| toxin | module constant only (`types.go:27`, visibility `workboard/app/visibility.go:20`); **no board source is wired** (`bootstrap/api.go:918-937`), so it is not in `RegisteredModules()` (`service.go:39`), never counted and never shown as degraded | none | none | none | all: the 15 open `toxin_test_tasks` on STG (in_progress 9, pending_review 6) are NOT on the board |
| procurement | module constant only (`types.go:28`, visibility `visibility.go:24`); **no board source is wired** (same bootstrap list) | none | none | none | all: `procurement_loads` (9 accepted_intake) and procurement SOP workflows never row here |
| verification | verification/.../boardsource/source.go:68,90 | `verification_items` | IST day of `captured_at` | approved->completed; rejected; else verification_pending | withdrawn; a rejected item superseded by a newer item for the same source ref |

Traps
- **status vs work_state**: status (evidence gate) wins over work_state (kernel clock) for pc_care/pen visits/routines. A `delayed` task with `submitted_at` set reads In review, not overdue.
- Weighing counts at **bucket (pen/partition) grain**, not animals; a closed bucket with item still `completed` is Done. Canceled buckets vanish.
- Verification rows are an extra module: the same field work can show once under its module and again under verification.
- The board is **one day**; "overdue" work only appears on the day it is now due (rolled-forward `due_business_date`), not on its original day.
- `ceo_ai.workforce_tasks_base` is `sop_tasks` (states queued/assigned/submitted/needs_review/accepted), NOT the Work Board. Do not answer board questions from it.

Verified (STG, 25/09/2026 00:15 IST, today, sources reproducible in SQL: health, weighing, pc_care, tasks, milk, verification, counts):
```
park module          todo in_prog in_review done attention total
CBE  milk               4     0       0      0     0        4
CBE  pc_care            0     0      13      0     0       13
CPT  milk               4     0       0      0     0        4
CPT  pc_care            0     0       4      0     0        4
(health/weighing/pen_visit/pen_routine/verification/counts: 0 rows yet today; feed + vaccination not included -> Go-composed)
```
The 17 pc_care tasks are `pending_verification` + `delayed` and were rolled to 25/09 at midnight, so they left 24/09. Re-running for date 24/09 now gives CBE milk 4 in review, pen_visit 6 in review, verification 31 in review / 236 done (267); CPT milk 3 todo + 1 in review, verification 4 / 189 (193), counts 1 done.
SQL sketch (one module shown; the rest follow the table, UNION ALL, then lane-bucket):
```sql
WITH d AS (SELECT (now() AT TIME ZONE 'Asia/Kolkata')::date AS day),
rows AS (
  SELECT 'pc_care' m, t.park_id,
    CASE WHEN t.status='completed' THEN 'completed' WHEN t.status='pending_verification' THEN 'verification_pending'
         WHEN t.status='rework' THEN 'rejected' WHEN t.work_state='closed' THEN 'completed' WHEN t.work_state='delayed' THEN 'overdue'
         WHEN EXISTS (SELECT 1 FROM pc_care_task_animals an WHERE an.task_id=t.task_id) THEN 'in_progress' ELSE 'due' END st
  FROM pc_care_tasks t, d WHERE t.due_business_date=d.day AND t.work_state<>'canceled'
  -- UNION ALL pen_visit_tasks / pen_routine_tasks / milk_feeding_tasks / verification_items / weighing / health ...
)
SELECT l.location_code park, m,
  count(*) FILTER (WHERE st IN ('scheduled','due','overdue','deferred','missed')) todo,
  count(*) FILTER (WHERE st IN ('in_progress','proof_pending','rejected','blocked')) in_progress,
  count(*) FILTER (WHERE st='verification_pending') in_review,
  count(*) FILTER (WHERE st='completed') done,
  count(*) FILTER (WHERE st IN ('overdue','missed','rejected','blocked')) needs_attention
FROM rows JOIN locations l ON l.location_id=rows.park_id GROUP BY ROLLUP(1,2);
```

## 2. Action Center (/action-center) = vaccination workflows

- Page: `apps/admin-web/features/process-integrity/action-center.tsx` (tiles: total, overdue, due from `counts_by_work_state`). Filters: `park`, `state` -> `work_state`, `severity`, `as_of`, "my tasks".
- Endpoints: `GET /vaccination/action-center`, `GET /vaccination/action-center/counts`, `GET /action-center/obligations` (`processintegrity/adapters/http/handler.go:59-61`). Horizon: due before `as_of + 30 days` (handler.go:70, :181, :194).
- SQL: `processintegrity/adapters/postgres/repository.go`. Grain = one row per **park, pen(shed+partition), batch, rule, protocol, dose, business date** (GROUP BY L1318) over goat `obligation_instances` with status IN (scheduled, due, in_progress, deferred, completed, missed, waived) — **canceled and superseded never count**. Counts: `processIntegrityCanonicalCountsSQL` L2101 (completed older than cutoff hidden unless include_completed).
- work_state (L1420-1455): all expected completed & nothing rejected/unrecorded -> completed; completion rejected -> rejected; pen not usable for vaccination -> blocked; any deferred / health-deferred / quarantine pen / **ICU pen** -> deferred; missed -> missed; task rework -> rejected; submitted -> verification_pending; in progress -> in_progress; IST due date < today -> overdue; any obligation with status `due` -> due; else scheduled.
- Not reproducible as one sketch (~1,300 lines, drive assignments, operator capacity). Approximation that matches obligation status on STG:
```sql
SELECT l.location_code, oi.status,
  count(*) FILTER (WHERE (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date <  (now() AT TIME ZONE 'Asia/Kolkata')::date) past_due,
  count(*) FILTER (WHERE (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date =  (now() AT TIME ZONE 'Asia/Kolkata')::date) today,
  count(*) FILTER (WHERE (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date >  (now() AT TIME ZONE 'Asia/Kolkata')::date) future
FROM obligation_instances oi JOIN protocol_versions pv USING (tenant_id, protocol_version_id)
JOIN protocol_definitions pd ON pd.protocol_id=pv.protocol_id AND pd.category='vaccination'
JOIN goats g ON g.goat_id=oi.target_id JOIN locations l ON l.location_id=g.park_id
WHERE oi.target_type='goat' AND oi.status NOT IN ('canceled','completed','superseded') GROUP BY 1,2;
-- STG 25/09: CBE scheduled 2453 future, deferred 29 · CPT scheduled 3 past_due (24/09 drive) + 2000 future, deferred 3
-- (11 CBE past-due rows are 'superseded' -> excluded by the screen)
```
Traps: these are ANIMAL obligations; the screen shows pen/batch grains, so its row count is far smaller than obligation counts. `ceo_ai.action_center_current` = every shed-scoped obligation with status scheduled/due/in_progress/missed (4,456 on STG, incl. future months) plus pending count approvals; its "severity" is `window_end < today`. Use it for "what is open", never to reproduce Action Center tile numbers. Vaccination "due" = status scheduled/deferred only; ~90% of obligation rows are canceled re-plans.

## 3. Alerts (/alerts)

- Page: `apps/admin-web/features/alerts/alerts-page.tsx`; filters `park`, `business_date` (default today IST).
- Endpoint: `GET /alerts/rows?park=&business_date=` (`alerts/adapters/http/handler.go:52,82`); service `alerts/app/service.go:142` runs every enabled rule for one park-day. Config `alert_rule_config` (empty on STG -> defaults) and `alert_event_rules` (0 on STG -> no event alerts).
- **Pen feed did not follow head count** (`pen_feed_quantity_change`, default threshold 1 animal): `alerts/adapters/postgres/repository.go:64 penFeedDaySQL` per pen (shed + normalized partition) from issued sheets (`state IN issued/amended/locked`) for day D and D-1; detector `alerts/domain/pen_feed.go`: |Δhead| >= threshold AND |Δkg| < 0.05 -> CRITICAL; same head and |Δkg| >= 0.05 -> WARNING; blocked cells today (NULL quantity) but none yesterday -> CRITICAL. Pens missing either day are skipped. Shifting register (penMovementsSQL L96) is only a note.
- **Feed stock running out** (`feed_low_stock`, default < 5 days, critical < 2): `feeddirection/adapters/postgres/analytics.go:1988 feedLowStockSQL` = Feed Analytics Stock tab; days_left = floor(balance / (override or recent avg daily kg)); feeds with no recent burn never alert. Farm grain, shown under that farm's park. Reproduce: `run_reference('feed-stock-days-left.sql', where="days_left < 5")`.
- Event rules (births, deaths, sold, added, shifting raised/approved, feed purchase): `alerts/adapters/postgres/events.go:178-266`, IST business day, per park; only fire if configured.
- Verified STG 25/09/2026: low stock -> CBE UHT Milk 32.0 kg / 7.0 kg/day = 4 days (warning). Pen feed -> CPT Yashoda 1: 1 -> 1 animal, feed 0.0 -> 0.8 kg (warning). Total 2 alerts (CBE 1, CPT 1), critical 0. (24/09: the same pen was critical, 2 -> 1 animals with feed 0.0 kg unchanged.)

## 4. Leadership Tasks (/tasks)

- `GET /app/leadership-tasks` (`leadershiptasks/adapters/http/handler.go:63`), counts `adapters/postgres/repository.go:~464` (sqlListAggregatesTemplate). Status open / in_progress / done / cancelled; overdue = status open or in_progress AND `deadline_at < now()` (a lens, not a status); team totals exclude cancelled.
- STG 24/09: open 1, in_progress 3, done 9, cancelled 3; overdue 2 (`SELECT status, count(*), count(*) FILTER (WHERE status IN ('open','in_progress') AND deadline_at < now()) FROM leadership_tasks GROUP BY ROLLUP(1)`).

## CEO questions this card answers
- What work is pending today at each park? — "Aaj har park mein kitna kaam baaki hai?"
- How much is waiting for verification? — "Kitna kaam verification ke liye pending hai?"
- What is overdue / needs attention? — "Kya overdue hai, kis cheez pe dhyan dena hai?"
- Which module is behind today (feed, weighing, PC care, milk)? — "Aaj kaunsa module peeche chal raha hai?"
- How many vaccinations are due or overdue? — "Kitne vaccination due ya overdue hain?"
- Why is a pen's vaccination deferred? — "Is pen ka vaccination defer kyun hua?"
- What alerts fired today? — "Aaj kaunse alerts aaye?"
- Which feed is about to run out? — "Kaunsa feed khatam hone wala hai?"
- Did feed follow the head count change? — "Janwar kam/zyada hue par feed badla ya nahi?"
- How many leadership tasks are open / overdue? — "Leadership ke kitne task khule hain, kitne overdue?"

## 5. Work Board cards, lane headers, card drawer (/work-board)

- Lane header number: `features/work-board/work-board-board.tsx:303` = `summary.by_lane[lane]` from `GET /work-board/summary` (section 1 formula; while the search box has text it falls back to the visible-card count). Lane pager `first–last of count` (:330) uses the same number.
- Card tiles `done/total done`, `N in review`, `N started`, `N attention` (`work-board-board.tsx:151-176`) and drawer tiles Done / Pending / Needs attention (`work-board-modal.tsx:126-145`) = `row.counts.{done,pending,needs_attention}` from `GET /work-board/page` (`workboard/adapters/http/handler.go:62`). **Go-only**: each module's board source computes its own units (animals, bags, steps) and the board never recomputes them (`workboard/domain/types.go:196-201`). `Row.Finalize` (`types.go:239-268`) forces a `rejected` row to severity >= watch, needs_attention >= 1 and pending >= 1.
- Owner avatar / "Pool" / "!" (missing): `owner_state` (`types.go:162-174`, Finalize :261): assigned when the source names a user or workforce member, pool when the module is a claim pool by design (health sessions, toxin steps), else missing.
- Clock label ("staged by 15:00", "13:00 session"): `row.clock_label`, source-owned copy (`types.go:220`). Go-only.
- Drawer Subtasks list: `GET /work-board/rows/{row_key}/subtasks` (`handler.go:63`), step states todo / in_progress / in_review / done / rework / needs_attention / locked (`types.go:498-508`), one adapter per module. Go-only.
- Flag form in the drawer is a write (`POST /work-board/flags`, `handler.go:64`), not a number.
- Filters -> SQL: assignee dropdown -> `owner` (owner column = user OR pool rows); park picker -> one call per park, summed; ‹ date › -> `date`. Module chips -> `module`. All as in section 1.
- Traps: lane headers are server totals but the cards shown are one page per lane (25/50/100), so the column can say 267 and show 25 cards. "Needs attention" on a card is the module's unit count, not the lane-level attention (overdue+missed+rejected+blocked row count in section 1).
- CEO questions: "How many animals are still pending on this pen's card?" — "Is pen ke card pe kitne janwar baaki hain?" · "Whose card has nobody assigned?" — "Kis card ka koi owner nahi hai?"

## 6. Alerts page KPIs, severity filter, skipped rules (/alerts)

- Tiles (`features/alerts/alerts-page.tsx:118-135`): **Total alerts** = Σ `page.total` over parks (:64), **Critical** = Σ `page.critical` (:65), **Rules checked** = distinct `rules_run` across parks with their labels (:66). Source `GET /alerts/rows` (`alerts/adapters/http/handler.go:52`), service `alerts/app/service.go:142`.
- Severity chips (All / Critical / Warning) filter rows client-side only (:62); tiles stay whole-scope. Table columns severity / alert / park / pen / detail / rule (`alerts-page.tsx:213-233`; contract `adminui/app/service.go:517`).
- Skipped note (`alerts-page.tsx:186`): `feed_low_stock` is a live figure and is skipped for any past `business_date` ("Checked for today only", `service.go:190-193`); the pen-feed rule is skipped when either day's issued sheet is missing (`service.go:257`). A skipped or degraded rule is NOT in "Rules checked".
- Configure drawer (`alerts-configure.tsx`) reads/writes `alert_rule_config` and `alert_event_rules` (`GET/PUT /alerts/config`, handler.go:53-57); STG has 0 rows in both -> built-in defaults (feed low stock < 5 days, pen feed threshold 1 animal).
- Verified STG 25/09/2026 (from section 3 rows): Total 2, Critical 0, Warning 2 (CPT pen feed, CBE UHT Milk low stock); rules checked 2 (pen feed, feed stock). `SELECT (SELECT count(*) FROM alert_rule_config) cfg, (SELECT count(*) FROM alert_event_rules) ev;` -> 0 | 0.
- Traps: looking at yesterday shows fewer rules (low-stock skipped), so the Total can drop for a past date without anything being fixed. `notification_requests` rows of type `feed_low_stock` (9 on 24/09) are push deliveries, not the Alerts page count.
- CEO questions: "How many critical alerts today?" — "Aaj kitne critical alert hain?" · "Why is yesterday's alert list shorter?" — "Kal ki list chhoti kyun hai?"

## 7. Action Center tiles, board columns, verification queue (/action-center)

- Route `app/(admin)/action-center/page.tsx` -> `features/process-integrity/action-center.tsx`. `/actions` is only a redirect to `/verify` (`app/(admin)/actions/page.tsx`) -> see `verification.md`.
- Quick tabs All / Overdue / Due (`action-center.tsx:388-397`) and the Vaccination pill (:382-385): `total_count` and `counts_by_work_state[overdue|due]` from `GET /vaccination/action-center` (`processintegrity/adapters/http/handler.go:59`, 30-day horizon :70). State chips in Filters drawer use the same server counts (:230-238); "My tasks" owner filter runs after the page loads (page-local). **Go-only** (section 2 grain; obligation-level approximation SQL in section 2).
- Board columns work_state / owner / due / task / next_action (contract `adminui/app/service.go:482`; UI `process-integrity/work-board.tsx`): owner = operator (drive assignment) else park head else verifier (`workforce_members` `primary_role_hint='verifier'`, nearest location, `processintegrity/adapters/postgres/repository.go:~1590-1612`); next_action = backend CASE on work_state (repository.go:~1472). Board lane headers in this embedded board are counted from **visible rows only** (comment :182), not server totals.
- Severity (chips and tags): `repository.go:1458-1471`: completed -> ok; scheduled/due/in_progress/deferred/verification_pending -> watch; proof_pending/overdue -> at_risk; everything else (rejected, blocked, missed) -> broken.
- Drawer (`action-center-local-drawer.tsx:241-308`): work_state, severity, due_at, next_action, protocol + dose, pen, stage, completed/expected, SOP / proof / verification state, blocker_reason, owner. All row fields of the same endpoint. Accept/reject buttons are writes.
- **"SOP queues" / "Awaiting verification" count** (`action-center.tsx:271`, :397) = `total_count` of `GET /vaccination/verification-queue` (`vaccination/adapters/http/handler.go:82,526` -> `app/service.go:287` -> sqlc `CountRecordedCompletions`, `vaccination/adapters/postgres/sqlc/query.sql:48`). Columns goat / administered / doses / verify. Formula: `vaccination_completions` with `status='recorded'`, park = the goat's park, earliest `administered_at` first.
```sql
SELECT coalesce(l.location_code,'(no park)') park, count(*)
FROM vaccination_completions vc
LEFT JOIN goats g ON g.tenant_id=vc.tenant_id AND g.goat_id=vc.goat_id
LEFT JOIN locations l ON l.location_id=g.park_id
WHERE vc.status='recorded' GROUP BY ROLLUP(1);
-- STG 24/09/2026: 0 (all 6,214 completions are 'accepted')
```
- Traps: the queue is per GOAT dose; the board is per pen/batch grain, so "Awaiting verification" (queue) and the board's verification_pending count can differ. The queue has no date filter: it is everything recorded and not yet reviewed.
- CEO questions: "How many vaccine doses are waiting to be verified?" — "Kitne vaccine dose verify hone baaki hain?" · "Who owns this overdue vaccination?" — "Is overdue vaccination ka zimmedar kaun hai?"

## 8. Workflows (/workflows) and workflow record (/workflows/[row_id])

- Route `app/(admin)/workflows/page.tsx` -> `process-integrity/workflows-landing.tsx`. Endpoints: `GET /vaccination/action-center/counts` (tiles) and `GET /vaccination/action-center` (catalog page) (`processintegrity/adapters/http/handler.go:59-60`).
- Tiles (`workflows-landing.tsx:145-154, 193-196`): **Workflows** = counts `total_count`; **Active runs** = counts[in_progress] + counts[scheduled]; **Blocked / gated** = counts[blocked] + counts[proof_pending] + counts[rejected]; **Avg progress %** = mean over the CURRENT PAGE rows with expected_count > 0 of completed_count/expected_count×100 (page-local, changes with paging). Go-only (same grain as section 2).
- Catalog columns workflow / stage / owner / next_action / status (contract `adminui/app/service.go:494`), runs = expected_count, dot colour = severity.
- Chain stepper (`workflows-landing.tsx:35-58`, client-derived): steps config -> obligation -> drive opened (SOP started or proof or any completion) -> SOP submitted/accepted -> proof uploaded/accepted -> verification accepted -> completion completed; the first not-done step is "blocked" if blocker_reason, owner missing, or work_state blocked/rejected.
- Record page (`workflow-drilldown.tsx:60-90`): `GET /workflows/{row_id}` (`handler.go:64`, service `processintegrity/app/service.go:215`): tags work_state / severity / SOP / proof / verification, `completed/expected done`, blocker_reason, next_action, backend-built nodes. Go-only.
- Traps: "Active runs" includes scheduled (future) work; "Avg progress" is not a farm-wide figure.
- CEO questions: "How many vaccination workflows are stuck?" — "Kitne vaccination workflow atke hue hain?" · "At which step is this pen's vaccination?" — "Is pen ka vaccination kis step pe hai?"

## 9. Notification bell (top bar, every screen; features/notifications)

- `notification-bell.tsx:288-332` badge = `unread_count` from `GET /app/notifications` (`notificationcentre/adapters/http/handler.go:45`); popover lists the caller's latest notifications; mark read = `POST /app/notifications/read` (:46, write).
- Formula (`notificationcentre/adapters/postgres/repository.go:96-136, 280-300`): caller's ACTIVE workforce member; rows `notification_requests` with `context->>'member_id'` = that member; unread = `read_at IS NULL AND status <> 'read'`; count DISTINCT `coalesce(context->>'event_key', notification_request_id)` (one per transition, not per phone). List hides older duplicates of the same key.
```sql
SELECT wm.display_name,
  count(DISTINCT COALESCE(NULLIF(nr.context->>'event_key',''), nr.notification_request_id::text)) unread_badge,
  count(*) delivery_rows
FROM notification_requests nr
JOIN workforce_members wm ON wm.tenant_id=nr.tenant_id AND wm.workforce_member_id::text=nr.context->>'member_id' AND wm.status='active'
WHERE nr.read_at IS NULL AND nr.status<>'read' GROUP BY 1 ORDER BY 2 DESC;
-- STG 25/09/2026: Aryaman 26,737 (41,616 rows) · Manohark 26,540 · Jyothi 20,194 (103,737 rows) · Hemant 13,602 · Dinakar 8,351 · Ravi 4,379
-- Types sent 24/09 (IST): verification_pending 200, reminder 168, due_today 107, escalation 50, procurement_load_overdue 42, feed_low_stock 9
```
- Traps: statuses `sent`, `exhausted` (push failed) and `suppressed` all count as unread; only 60 rows are ever `read`. Badge caps at a max label ("99+"-style). Former staff see nothing (fails closed).
- CEO questions: "How many unread notifications does Jyothi have?" — "Jyothi ke kitne notification unread hain?" · "What notifications went out today?" — "Aaj kaunse notification gaye?"
