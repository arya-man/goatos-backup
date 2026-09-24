Metrics: A1 birth / death / shift approvals (counts) · A2 approval turnaround and who decided · A3 shifts authorized but not yet applied · A4 leave approvals · A5 proofs pending verification · A6 toxin verdicts pending

"Approvals" means different queues. Name the queue in the answer. There is no single approvals total in the app.

## A1 Birth / death / shift approvals (Approvals page)
- Screen: `/approvals` (`apps/admin-web/features/approvals/approvals-page.tsx`). Status tabs (default pending), type tabs (birth / death / shifting), farm tabs.
- Endpoint: `GET /admin-web/counts/approvals?status&cursor&page_size` (`backend/internal/permissions/routes.go:1236`; handler `backend/internal/counts/adapters/http/approval_handler.go:146`).
- Code: `backend/internal/counts/adapters/postgres/approval_repository.go:660-700` over `counts_approval_requests`.
- Formula: `status = :tab` AND `request_type = ANY(the types the caller may decide)` (`permissions/permissions.go:1540`).
  - `counts.approve_lifecycle` covers birth and death.
  - `counts.approve_shifting` covers shifting.
  - A park-scoped caller only sees their parks. The park comes from `payload->>'destination_park_id'` (shift), `payload->>'park_id'` (birth), or `goats.park_id` of `subject_goat_id` (death).
- Traps:
  - The 4 KPI tiles ("pending in view", "birth/death in view", "shifting in view", "rows in view") count **only the fetched page of at most 20 rows**. Type and farm are filtered client-side. They are not totals, so use SQL for totals.
  - Statuses: pending / approved / rejected / cancelled.
- SQL (run 2026-09-24): 77 requests.
  - shifting: approved 53, rejected 19.
  - death: approved 4.
  - birth: approved 1.
  - **pending 0**.
```sql
SELECT r.request_type, r.status, coalesce(p.name,'?') park, count(*)
FROM counts_approval_requests r LEFT JOIN goats g ON g.goat_id=r.subject_goat_id
LEFT JOIN locations p ON p.location_id = coalesce(CASE r.request_type
  WHEN 'shifting' THEN (r.payload->>'destination_park_id')::uuid WHEN 'birth' THEN (r.payload->>'park_id')::uuid END, g.park_id)
GROUP BY 1,2,3 ORDER BY 1,2;
```
- Questions: "Any approvals pending for me?" · "Kitne shift requests approve karne baaki hain?" · "Death approvals pending hain kya?"

## A2 Approval turnaround and who decided
- Columns: `raised_at`, `decided_at`, `decided_by_user_id` -> `workforce_members.user_id`, and `decision_reason` (the rejection reason).
- Formula: turnaround = `decided_at - raised_at`, for decided rows.
- SQL (run 2026-09-24):
  - Average turnaround 9.4 h over 77 decided rows.
  - Deciders: Dinakar 31, Manohark 28, Chandrakant 18.
```sql
SELECT m.display_name, count(*), round(avg(extract(epoch FROM decided_at-raised_at)/3600)::numeric,1) avg_hrs
FROM counts_approval_requests r LEFT JOIN workforce_members m ON m.user_id=r.decided_by_user_id
WHERE r.decided_at IS NOT NULL GROUP BY 1 ORDER BY 2 DESC;
```
- Questions: "How fast are approvals decided?" · "Approve karne mein kitna time lagta hai?" · "Sabse zyada reject kisne kiye?"

## A3 Shifts authorized but not yet applied
- Code: `approval_repository.go:1052-1063`. Approving a shift request moves the `shifting_events` row from `pending` to `authorized`. The move becomes `applied` (with `applied_at`) when it is carried out.
- Formula: approved but not moved = `event_status='authorized'`. Waiting for approval = `pending`.
- SQL (run 2026-09-24): authorized 13, pending 2, applied 25, canceled 32.
```sql
SELECT event_status, count(*) FROM shifting_events GROUP BY 1;
```
- Questions: "Which approved shifts haven't happened yet?" · "Approve hue shift abhi tak kyun nahi hue?"

## A4 Leave approvals
- Screen: `/leave`, the approver queue.
- Endpoint: `GET /admin-web/leave/approvals` (`backend/internal/workforce/adapters/http/leave_handler.go:42`).
- Code: `backend/internal/workforce/adapters/postgres/leave_repository.go:180-189`.
  - Pending for me = `status='pending'` AND my slot is still open. That is (park head: `park_head_required AND park_head_decision IS NULL` in my parks) OR (HR: `hr_required AND hr_decision IS NULL`). Leadership with the `any` flag sees all pending.
  - Outcome at `:445-457`. Any reject means rejected. The last required approval means approved.
- Who must approve: `GET /admin/leave/approval-config` (CEO only).
- SQL (run 2026-09-24): `workforce_leave_requests` has 0 rows, so pending = 0.
```sql
SELECT count(*) FILTER (WHERE park_head_required AND park_head_decision IS NULL) park_head_open,
       count(*) FILTER (WHERE hr_required AND hr_decision IS NULL) hr_open
FROM workforce_leave_requests WHERE status='pending';
```
- Questions: "Any leave waiting for my approval?" · "Chutti ki request pending hai kya?"

## A5 Proofs pending verification (verifier queue)
- Screen: `/verify` (`/verification` redirects there; `features/verification-review/*`).
- Endpoint: `GET /verification/queue` (`backend/internal/verification/adapters/http/handler.go:63`). The action queue forces `status=pending` (`:347`).
- Code: `backend/internal/verification/adapters/postgres/repository.go:376-440`.
- Filters -> SQL: `vi.status`, `category`, `vertical`, `module`, `park_id`, `shed_id`/partition, captured_at date. Sorted by `captured_at`.
- Traps:
  - An approval by sampling has `auto_resolution` NOT NULL. Nobody watched it, so it is not a human approval.
  - `ceo_ai.verification_queue_status` gives the same pending counts per area.
- SQL (re-run 2026-09-25, after the nightly not_sampled sweep): pending 3142, oldest 05/08/2026 (CBE weighing_proof; ~1,214 h).
  - Largest queues: CBE feed_packing 902, CPT feed_packing 718, CBE feed_distribution 540.
```sql
SELECT coalesce(p.name,'(none)') park, vi.category, count(*), min(vi.captured_at AT TIME ZONE 'Asia/Kolkata')::date oldest
FROM verification_items vi LEFT JOIN locations p ON p.location_id=vi.park_id
WHERE vi.status='pending' GROUP BY 1,2 ORDER BY 3 DESC;
```
- Questions: "How many proofs are waiting for verification?" · "Verify karne ke liye kitne video pending hain?" · "Sabse purana pending proof kab ka hai?"

## A6 Toxin verdicts pending (CEO/CXO)
- Endpoints: `GET /toxin/review` and `POST /toxin/tasks/{id}/verdict` (`backend/internal/toxin/adapters/http/handler.go:62-63`).
- Formula: `toxin_test_tasks.status='pending_review'` (`toxin/domain/task.go:34`) means the strip was read and is waiting for the leadership verdict.
- SQL (run 2026-09-24): pending_review 6, in_progress 9.
```sql
SELECT status, count(*) FROM toxin_test_tasks GROUP BY 1;
```
- Questions: "Any toxin results waiting for my decision?" · "Toxin test ka verdict dena baaki hai?"

## A7 Approvals table columns + review drawer (`/approvals`)
- Screen: table `features/approvals/approvals-page.tsx:150-212` (columns Type, Subject, Raised, Status, Review); drawer `features/approvals/approvals-drawer.tsx:200-300` (meta Type/Status/Raised/Decided/Decision reason, Approve/Reject forms) and capture panel `:373-455`.
- Endpoint: same `GET /admin-web/counts/approvals` rows; fields `request_type`, `raised_at`, `status`, `decided_at`, `decision_reason`, `summary`, `subject_animal_location`, `capture`, `capture_review_status/reason` (`backend/internal/counts/adapters/http/approval_handler.go:120-230`).
- Formula: Subject = death -> backend `subject_animal_location`; shifting -> `park: source shed -> destination shed` from summary `source_shed_id`/`destination_shed_id` (`approvals-page.tsx:248-265`); birth -> park name. Capture panel (Go-only): birth/death capture from the report's SOP form (`approvalCaptureDTO`, `approval_handler.go:213`); shifting falls back to `summary.capture` (`:234-250`).
- Traps: a shift of "Yashoda -> Yashoda" is a partition move inside one pen, not a data error. `decision_reason` is filled mainly on rejects (all 19 shift rejects have one; 1 death approval has one).
- SQL (run 2026-09-24): last raised birth 16/09, death 23/09, shifting 24/09. Top moves: Yashoda -> Yashoda 23, Ho Chi Minh -> Godel 1 16, Godel 2 -> Yashoda 12.
```sql
SELECT s.name||' -> '||d.name mv, count(*) FROM counts_approval_requests r
LEFT JOIN locations s ON s.location_id=(r.payload->>'source_shed_id')::uuid
LEFT JOIN locations d ON d.location_id=(r.payload->>'destination_shed_id')::uuid
WHERE r.request_type='shifting' GROUP BY 1 ORDER BY 2 DESC;
```
- Questions: "Which pen moves were requested most?" · "Sabse zyada shift kis pen se kis pen mein hue?" · "Why was this shift rejected?" · "Yeh shift reject kyun hua?"
