Metrics: T1 leadership task status chips and scope tabs · T2 leadership tasks overdue · T3 deadline countdown / finished late · T4 unseen badge · T5 SOP tasks (sop_tasks, ceo_ai.workforce_tasks_base) · T6 SOP workflow cards (overdue / due / completed / awaiting video) · T7 Work Board lanes and needs attention

Leadership tasks (`leadership_tasks`) and SOP tasks (`sop_tasks` / `workflow_instances`) are different systems. Never add them together.

## T1 Leadership task status chips and scope tabs
- Screen: `/tasks` (`apps/admin-web/app/(admin)/tasks/page.tsx`; `features/leadership-tasks/*`), shown as a board (lanes) or a table.
- Default scope on the web is `team_progress` (`features/leadership-tasks/task-url.ts:189`).
- Endpoint: `GET /app/leadership-tasks?scope&status&q&assignee&raiser&deadline_from/to&raised_from/to&sort` (`backend/internal/leadershiptasks/adapters/http/handler.go:63`).
- Code: `backend/internal/leadershiptasks/adapters/postgres/repository.go`. Rows at `:121-160`. Status chips `:384-403`. Scope tabs `:414-420`. Tab sizes `:446-451`. One UNION statement at `:477-513`.
- Formula by scope:
  - assigned_to_me: `assignee_user_id = me`.
  - assigned_by_me: `raised_by = me`.
  - team_progress: the whole tenant with `status <> 'cancelled'`.
- Tab counts exclude cancelled rows. Statuses: open ("To do"), in_progress ("Doing"), done, cancelled.
- Filters -> SQL:
  - q: ILIKE over title, body and task_no.
  - Assignee / raiser: `assignee_user_id` / `raised_by`.
  - Deadline range: `deadline_at BETWEEN`. This drops tasks with no deadline.
  - Raised range: `raised_at`.
- Traps:
  - The status chips on team_progress still show a `cancelled` bucket that the list hides (`:385-390`, a known issue).
  - Names come from `workforce_members.user_id` -> `display_name` (active row).
- SQL (run 2026-09-24): 16 tasks = open 1, in_progress 3, done 9, cancelled 3. team_progress = 13.
```sql
SELECT status, count(*) FROM leadership_tasks GROUP BY ROLLUP(1);
```
- Questions: "How many leadership tasks are open?" · "Chandrakant ke paas kitne kaam pending hain?" · "Team ka progress kya hai?"

## T2 Leadership tasks overdue
- Screen: the Overdue chip / lens on `/tasks`.
- Code: `repository.go:151-155` (lens), `:519-520` (chip count), `domain/task.go:392` (`OverdueStatuses = open, in_progress`).
- Formula: `status IN ('open','in_progress') AND deadline_at < now()`. The comparison is strict, and the time used is the request's clock instant (not end of day).
- Trap: tasks with no deadline are never overdue (5 of the 16 have none). Done or cancelled tasks are never overdue.
- SQL (run 2026-09-24): overdue = 2, both assigned to Chandrakant (4 working in total).
```sql
SELECT coalesce(a.display_name,'(none)') assignee,
  count(*) FILTER (WHERE t.status IN ('open','in_progress')) working,
  count(*) FILTER (WHERE t.status IN ('open','in_progress') AND t.deadline_at < now()) overdue
FROM leadership_tasks t LEFT JOIN workforce_members a ON a.user_id=t.assignee_user_id AND a.status='active'
WHERE t.status <> 'cancelled' GROUP BY 1 ORDER BY 3 DESC;
```
- Questions: "Which leadership tasks are overdue?" · "Kaunse task deadline cross kar gaye?" · "Kiske tasks late chal rahe hain?"

## T3 Deadline countdown / finished late
- Code: `domain/deadline.go`, composed once on the server.
  - The count is IST calendar days from today to `deadline_at`: "5 days left", "due today", or "3 days over".
  - Red when 2 or fewer days are left (`NearDays`, `:45`), and red when overdue.
  - A done or cancelled task freezes the count at `done_at`: "finished 2 days early" or "finished 1 day late".
- Formula (SQL): days left = `deadline_at_IST::date - today_IST`. Finished late = `status='done' AND done_at > deadline_at`.
- Trap: no deadline means no counter and no colour. Do not call those tasks late.
- SQL (run 2026-09-24): 1 task done this IST week (`done_at >= date_trunc('week', now() IST)`).
```sql
SELECT task_no, title, ((deadline_at AT TIME ZONE 'Asia/Kolkata')::date - (now() AT TIME ZONE 'Asia/Kolkata')::date) days_left
FROM leadership_tasks WHERE status IN ('open','in_progress') AND deadline_at IS NOT NULL ORDER BY 3;
```
- Questions: "What's due this week?" · "Is hafte kya deadline pe hai?" · "Kitne tasks time pe khatam hue?"

## T4 Unseen badge
- Code: `repository.go:526-527`. `assignee_user_id = me AND seen_at IS NULL AND status <> 'cancelled'`. It is per viewer.
- SQL: `SELECT count(*) FROM leadership_tasks WHERE seen_at IS NULL AND status<>'cancelled';` gives the total across all assignees.
- Questions: "Has Dinakar seen the task I gave?" · "Task dekha ki nahi?"

## T5 SOP tasks (sop_tasks)
- Screen: no admin-web list. `GET /admin/tasks` (`backend/internal/sop/adapters/http/handler.go:41`; `sop/adapters/postgres/repository.go:247`) is read by the verification drawer and the phone.
- Filters: state, assigned_to, scope_type, scope_id, ordered by `due_at NULLS LAST`.
- `ceo_ai.workforce_tasks_base`: sop_tasks plus `due_business_day` (IST).
  - Trap: its `park_label` is the parent of `scope_id`. That is only correct for scope_type 'shed'. For 'park' or 'tenant' scope it is NULL or wrong, so join `locations` on `scope_id` yourself.
- `ceo_ai.workforce_coverage_status`:
  - active = state NOT IN (completed, verified, canceled).
  - overdue = that AND `due_at < now()`, per `assigned_to`.
- Traps:
  - 548 of 573 rows are `queued` vaccination tasks, almost all with `due_at` NULL. They are not real backlog.
  - State values on stg: queued, needs_review, accepted, submitted, assigned.
- SQL (run 2026-09-24): queued 548, needs_review 11, accepted 10, submitted 3, assigned 1. Past due = 4.
```sql
SELECT state, task_type, scope_type, count(*), count(due_at) with_due FROM sop_tasks GROUP BY 1,2,3 ORDER BY 4 DESC;
```
- Questions: "How many SOP tasks are pending review?" · "SOP ke kitne kaam baaki hain?"

## T6 SOP workflow cards (phone Tasks / workflows)
- Endpoint: `GET /app/workflows?module&event_date&filter` (`backend/internal/tasks/adapters/http/handler.go:72`).
- Code: `backend/internal/tasks/adapters/postgres/repository.go:444-453` (chips) and `:460-475` (overdue-dates bell, last 5 past dates).
- Formula, per (module, event_date), over `workflow_instances`:
  - all: state <> 'canceled'.
  - overdue: state 'open' AND `next_due_at < now()`.
  - due: open AND (next_due_at NULL OR >= now).
  - completed: state 'completed' AND NOT awaiting_verification.
  - awaiting_video: `awaiting_verification`.
- Modules: birth, death, general, procurement, reconcile, sales.
- SQL (run 2026-09-24): open overdue = reconcile 39, procurement 4, sales 3, general 1. Awaiting verification = death 3, birth 1.
```sql
SELECT module, count(*) FILTER (WHERE state='open' AND next_due_at < now()) overdue,
  count(*) FILTER (WHERE awaiting_verification) awaiting FROM workflow_instances GROUP BY 1;
```
- Questions: "Which SOP workflows are overdue?" · "Reconcile ke kitne kaam atke hain?"

## T7 Work Board lanes and needs attention
- Screen: `/work-board` (`features/work-board/*`).
- Endpoints: `GET /work-board/summary` and `GET /work-board/rows` (`backend/internal/workboard/adapters/http/handler.go:60-62`), for one park and one business date.
- Code: `backend/internal/workboard/app/service.go:212` (Summary, one aggregate per module source); `domain/types.go:123` (LaneFor) and `:468` (Add).
- Formula:
  - Lanes: todo = scheduled/due/overdue/deferred/missed; in_progress = in_progress/proof_pending/rejected/blocked; in_review = verification_pending; done = completed.
  - Needs attention = overdue + missed + rejected + blocked.
- Can't reproduce in one SQL. The board has 10 wired sources over 9 modules (feed, health, vaccination, weighing, counts approvals, milk, pc_care, verification, tasks = pen visits + pen routines; `bootstrap/api.go:918-937`); each derives work_state in its own Go code. `toxin` and `procurement` are module constants with NO source, so they never appear (work-board.md §1). Give the dashboard path, or answer per module from that module's table.
- Questions: "What's pending on the work board today?" · "Aaj ka kaam kitna baaki hai?"

## T8 Leadership task table and card columns, sort, evidence, activity drawer
- Screen: `/tasks`.
  - Table `features/leadership-tasks/leadership-tasks-table.tsx`, and board card `task-board-card.tsx:95-109`.
  - Columns: number, title, assignee, raised by, raised date ("age"), deadline plus countdown (T3), status, evidence.
  - Row mapper: `task-row.ts:54-100`.
- Endpoints:
  - List: `GET /app/leadership-tasks` (T1).
  - Detail drawer: `GET /app/leadership-tasks/{id}` and `/{id}/activity` (`leadershiptasks/adapters/http/handler.go:65-66`), in `task-detail-panel.tsx` and `task-activity-feed.tsx`.
  - Assignee and raiser filter options: `GET /app/leadership-tasks/assignees` (`:64`).
- Fields:
  - `raised_on_label` = IST farm date of `raised_at` (`domain/task.go:348`).
  - `attachments[]` and `attachment_count` come from `leadership_task_attachments` (`adapters/postgres/repository.go:~1632`). Evidence = the attachment kinds joined with commas.
  - Activity = `leadership_task_events` (kinds created / status_changed / commented) plus `leadership_task_notes`.
- Sort (`repository.go:332-343`): the default is raised_at DESC. Options are raised_at ASC, deadline_at ASC NULLS LAST, and deadline_at DESC NULLS LAST.
- Filters: see T1. The view switch (board or table) is URL state only.
- SQL (run 25/09/2026): 16 tasks, 1 attachment in total (task 16). Events: created 16, commented 11, status_changed 3.
```sql
SELECT t.task_no, t.status, (SELECT count(*) FROM leadership_task_attachments a WHERE a.task_id=t.task_id) attachments,
  (SELECT count(*) FROM leadership_task_notes n WHERE n.task_id=t.task_id) notes,
  (SELECT count(*) FROM leadership_task_events e WHERE e.task_id=t.task_id) events
FROM leadership_tasks t ORDER BY task_no;
```
- Questions: "Which tasks have proof attached?" · "Kis task mein photo/video laga hai?" · "Who commented on task 16?"
