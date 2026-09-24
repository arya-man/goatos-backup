# Logic card: Pen visits and pen routines

Index: V1 Work Board pen visit counts by state (Tasks module) · V2 Pen visit verification queue · V3 Visits stuck in review (off today's board) · R1 Routines table: Open today / Delayed per routine · R2 Routines "Today" KPI tiles (Due / Delayed / In review / Sent back / Done) · R3 Work Board pen routine counts

No ceo_ai view covers these; read `public.pen_visit_tasks` (+ `pen_visit_task_sources`, `pen_visit_park_assignees`) and `public.pen_routine_tasks` JOIN `pen_routine_definitions` USING (routine_id). Verified READ-ONLY on goatos-stg 24/09/2026 ~23:49 IST.

## Pen visits: data model
- Raised automatically the day AFTER PC Care work (deworming / hoof trimming) or a vaccination submission: `reasons` text[] (e.g. {deworming}); source rows in `pen_visit_task_sources` (source_kind pc_care_task | sop_submission). `source_business_date` = day of the work, `planned_business_date` = visit day.
- `status` open -> pending_verification -> completed | rework; `work_state` scheduled | delayed | completed | canceled. Done = `submitted_at` set (clip recorded); accepted = `status='completed'`.
- Roll-forward (backend/internal/penvisits/adapters/postgres/repository.go:945-960) moves only status open/rework. A SUBMITTED visit (pending_verification) freezes on its last `due_business_date` and keeps `work_state='delayed'` -> "delayed + submitted_at set" = done late, awaiting verification, NOT overdue.
- No admin endpoint: `/app/pen-visits` (handler.go:49-51) is the operator's own list. Admin sees visits only on the Work Board.

## V1 Work Board -> Tasks module, "Pen visit · <pen>" rows (admin-web /work-board)
- Endpoint: `GET /work-board/summary?park=<uuid>&business_date=YYYY-MM-DD&module=tasks` (park required; date default IST today).
- Code: backend/internal/penvisits/adapters/boardsource/source.go:63 (workStateSQL), :80 (baseWhere), :135 (countSQL); lanes backend/internal/workboard/domain/types.go:123; attention types.go:483.
- Formula: status completed -> completed; pending_verification -> verification_pending; rework -> rejected; work_state delayed -> overdue; else due. Filter `due_business_date = day AND work_state <> 'canceled'`; "mine" = caller is any configured park visitor. Tasks module also carries pen routines (R3) and leadership tasks: split by source.
```sql
SELECT p.name park, CASE WHEN v.status='completed' THEN 'completed' WHEN v.status='pending_verification' THEN 'verification_pending'
  WHEN v.status='rework' THEN 'rejected' WHEN v.work_state='delayed' THEN 'overdue' ELSE 'due' END board_state, count(*)
FROM pen_visit_tasks v JOIN locations p ON p.location_id=v.park_id
WHERE v.due_business_date=(now() AT TIME ZONE 'Asia/Kolkata')::date AND v.work_state<>'canceled' GROUP BY 1,2;
```
STG 24/09: Coimbatore verification_pending 6; Channapatna none.

## V2 Pen visit verification queue (admin-web /verify, area Preventive care)
`ceo_ai.verification_queue_status` area preventive_care mixes PC Care + vaccination + pen visits: use `verification_items WHERE module='pen_visits'` (category pen_visit, one item per visit).
STG: pending 26, approved 1. Trap: 103 visits are status completed but only 1 approved item (older visits were closed before the verification bridge); count "visits verified" from pen_visit_tasks.status, not verification_items.

## V3 Visits submitted but not reviewed (hidden from today's board)
Because submitted visits stop rolling, the board for today misses older ones.
```sql
SELECT p.name, v.due_business_date, count(*), min((v.submitted_at AT TIME ZONE 'Asia/Kolkata')::date) first_submitted
FROM pen_visit_tasks v JOIN locations p ON p.location_id=v.park_id
WHERE v.status='pending_verification' GROUP BY 1,2 ORDER BY 2;
```
STG: Channapatna 20 visits frozen on 19/09/2026 (planned 14-17/09), Coimbatore 6 on 24/09/2026. Totals by state: completed 103, pending_verification 26, canceled 6 (hoof trimming, 17/09).
"When was pen X visited": `(submitted_at AT TIME ZONE 'Asia/Kolkata')::date`, show `reasons` and park; never use due_business_date.

## Pen routines: data model
- `pen_routine_definitions` (per park: name, scope_kind all_pens|selected_pens|park, cadence daily|weekly|monthly|every_n_days|after_work, review_kind verifier|none, status active|paused|retired). Tasks materialise into `pen_routine_tasks` (one per pen per due day).
- Same two-column model: status open|pending_verification|completed|rework, work_state scheduled|delayed|completed|canceled. Roll-forward (penroutines/adapters/postgres/repository.go:950-968) moves open/rework tasks to today, sets `delayed_since_business_date`, bumps rolled_forward_count.
- STG 24/09/2026: 0 routines, 0 tasks -> every number below is 0.

## R1 Routines table (admin-web /routines, per routine row)
- `GET /admin/pen-routines[?park_id]` (admin_handler.go:51,62). SQL penroutines/adapters/postgres/authoring.go:573-574.
- Open today = tasks with work_state IN (scheduled, delayed) AND due_business_date = IST today. Delayed = work_state 'delayed' (any date). Note: Open today still includes tasks already submitted (pending_verification keeps work_state).
```sql
SELECT l.name park, d.name, d.status,
 (SELECT count(*) FROM pen_routine_tasks t WHERE t.routine_id=d.routine_id AND t.work_state IN ('scheduled','delayed') AND t.due_business_date=(now() AT TIME ZONE 'Asia/Kolkata')::date) open_today,
 (SELECT count(*) FROM pen_routine_tasks t WHERE t.routine_id=d.routine_id AND t.work_state='delayed') delayed
FROM pen_routine_definitions d JOIN locations l ON l.location_id=d.park_id;
```
STG: 0 rows.

## R2 Routines "Today" KPI tiles (Due / Delayed / In review / Sent back / Done)
- `GET /admin/pen-routines/tasks?park_id&business_date&routine_id&cursor&limit` (admin_handler.go:223). Screen filters: park segment -> park_id (default first park, CBE), day arrows -> business_date, routine filter -> routine_id.
- Predicate repository.go:830 (park, due_business_date = day, work_state <> canceled, optional routine); fold repository.go:726-740, precedence: status pending_verification -> In review; rework -> Sent back; work_state completed -> Done; delayed -> Delayed; scheduled -> Due. Whole-filter counts, not page counts.
```sql
SELECT count(*) FILTER (WHERE t.status='pending_verification') in_review, count(*) FILTER (WHERE t.status='rework') sent_back,
 count(*) FILTER (WHERE t.status NOT IN ('pending_verification','rework') AND t.work_state='completed') done,
 count(*) FILTER (WHERE t.status NOT IN ('pending_verification','rework') AND t.work_state='delayed') delayed,
 count(*) FILTER (WHERE t.status NOT IN ('pending_verification','rework') AND t.work_state='scheduled') due
FROM pen_routine_tasks t JOIN locations p ON p.location_id=t.park_id
WHERE p.name='Coimbatore' AND t.due_business_date=(now() AT TIME ZONE 'Asia/Kolkata')::date AND t.work_state<>'canceled';
```
STG: all 0. Trap: past days lose their rolled-forward tasks (due date moved), so "Delayed" on a past day understates.

## R3 Work Board -> Tasks module, pen routine rows
penroutines/adapters/boardsource/source.go:58/71/124: same CASE as V1 (completed / verification_pending / rejected / overdue / due) on pen_routine_tasks, due_business_date = day, not canceled. STG: 0.

## CEO questions
- "Were yesterday's post-deworming pen visits done?" / "Kal deworming ke baad pen visit hua kya?" -> V1/V3 by source_business_date + reasons
- "How many pen visits are waiting for verification?" / "Kitne pen visit verify hone baaki hain?" -> V2/V3 (26 on STG, 20 of them CPT since 19/09)
- "When did someone last visit Mandela 2?" / "Mandela 2 mein last visit kab hua?" -> V3 note (submitted_at)
- "Which routines are running and how many checks are delayed?" / "Kaunse routines chal rahe hain, kitne checks late hain?" -> R1
- "Today's routine checks: done vs pending?" / "Aaj ke routine checks mein kitne hue, kitne baaki?" -> R2

## R4 Routines table columns, today's task table, routine drawer (admin-web /routines)
- Screen: `apps/admin-web/features/pen-routines/routines-page.tsx`.
  - Routine columns `:198-230`: name, park, cadence, evidence, people, status, open today plus delayed (R1).
  - Today's task columns `:239-260`: routine, pen, assignee, state chip, due.
  - Routine filter: `routine-filter.tsx`. Create and edit drawer: `routine-drawer.tsx`, which is fed by `GET /admin/pen-routines/catalog?park_id` (`admin_handler.go:52`) with pens, people and vocabularies.
- Go-only labels:
  - `cadence_line` = `domain.CadenceLine` (`penroutines/domain/routine.go:761`).
  - `evidence_line` = `EvidenceLineForScope` (`adapters/http/payloads.go:289-297`).
  - `state_chip` (`domain/task.go:360-386`):
    - pending_verification -> "In review"; rework -> "Sent back".
    - work_state completed -> "Verified" (review_kind verifier) or "Done".
    - canceled -> "Cancelled".
    - delayed -> "Delayed since <delayed_since_business_date or planned date>".
    - Otherwise "Due today" or "Due <date>".
- SQL (the chip without its labels): R2's CASE applied per row of `pen_routine_tasks` JOIN `pen_routine_definitions`. On stg (25/09/2026) there are 0 definitions and 0 tasks.
```sql
SELECT d.name, d.cadence_kind, d.review_kind, d.status, count(t.*) tasks FROM pen_routine_definitions d
LEFT JOIN pen_routine_tasks t USING (routine_id) GROUP BY 1,2,3,4;
```
- Questions: "Which routines need a verifier?" · "Kaunse routine mein verification lagta hai?"
