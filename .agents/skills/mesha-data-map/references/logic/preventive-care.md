# Logic card: Preventive Care (deworming, hoof trimming, feed & water removal, inventory vaccine)

Index: M1 Work Board PC Care counts by state/lane · M2 Needs attention · M3 Vaccination page "Inventory vaccine progress" (current / overdue / with verifier / done today) · M4 PC Care verification queue (pending/accepted) · M5 Last done per pen (+ cancelled) · M6 Animals treated · (PC Care SOP page = config only, no numbers)

No ceo_ai view covers PC Care; everything below reads `public.pc_care_tasks` (+ `pc_care_task_animals`, `pc_care_task_assignees`, `pc_care_removal_pen_proofs`, `verification_items`). Verified READ-ONLY on goatos-stg 24/09/2026 ~23:49 IST.

## Data model (read first)
- One row = one category on one pen part on one day. `category` in deworming | hoof_trimming | feed_water_removal | inventory_vaccine (ticks removal is SOP-only, no rows yet).
- TWO state columns: `status` = evidence (open -> pending_verification -> completed | rework); `work_state` = kernel clock (scheduled | delayed | completed | closed | canceled). Status wins for "done": `status='completed'` = verifier accepted.
- `work_state='delayed' AND status='completed'` = work accepted, clock still waiting on the next-day pen visit (see pen-visits.md). Counts as DONE.
- `submitted_at` = when the operator finished; `verified_at` = accepted. `planned_business_date` = original plan day. `due_business_date` MOVES: the midnight sweep (backend/internal/pccare/adapters/postgres/kernel.go:46) rolls every scheduled/delayed task with status <> 'completed' (INCLUDING pending_verification) to today and bumps `rolled_forward_count` (STG: hoof trimming planned 07/09 rolled 17 times). Deworming whose feed & water removal was not submitted is pushed to today+1 (kernel.go:122, :181).
- `canceled`: history only (no writer since migration 000257); never "done", never "work". `closed` (+ `closed_by`, `close_reason`) = pen-day closed by a person (0 rows on STG).
- Pen rows: `shed_id` + `partition_label` (partition_key 'whole' = no part). Feed & water removal of a ROUND has `shed_id NULL` (gates_round_id set); per-pen evidence is in `pc_care_removal_pen_proofs`. inventory_vaccine rows are mostly park-level (shed NULL).

## M1 Work Board -> PC Care module (admin-web /work-board, module chip "Preventive care")
- Endpoint: `GET /work-board/summary?park=<uuid>&business_date=YYYY-MM-DD&module=pc_care[&state=][&lane=]` (also /work-board/page, /rows). Park is REQUIRED (one park per request, handler.go:643); date defaults to IST today.
- Code: backend/internal/pccare/adapters/boardsource/source.go:79 (countWorkStateSQL), :118 (baseWhere), :178 (countSQL); lane map backend/internal/workboard/domain/types.go:123.
- Formula (first match wins): status completed -> completed; pending_verification -> verification_pending; rework -> rejected; work_state closed -> completed; delayed -> overdue; any animal scanned -> in_progress; else due. Filter: `due_business_date = day AND work_state <> 'canceled'`. Lanes: due/overdue -> To do; in_progress/rejected -> In progress; verification_pending -> In review; completed -> Done. "Mine" = assignee OR task with no assignee (park pool).
- Trap: the board is keyed on CURRENT `due_business_date`, so a past day's board no longer shows tasks that rolled forward; old pending tasks pile onto today.
```sql
SELECT p.name park, CASE WHEN t.status='completed' THEN 'completed' WHEN t.status='pending_verification' THEN 'verification_pending'
  WHEN t.status='rework' THEN 'rejected' WHEN t.work_state='closed' THEN 'completed' WHEN t.work_state='delayed' THEN 'overdue'
  WHEN EXISTS (SELECT 1 FROM pc_care_task_animals an WHERE an.task_id=t.task_id) THEN 'in_progress' ELSE 'due' END board_state, count(*)
FROM pc_care_tasks t JOIN locations p ON p.location_id=t.park_id
WHERE t.due_business_date=(now() AT TIME ZONE 'Asia/Kolkata')::date AND t.work_state<>'canceled' GROUP BY 1,2;
```
STG 24/09: Coimbatore verification_pending 13, Channapatna verification_pending 4 (all in In review; To do/Done 0). 25/09: the same 17 rows, rolled to due 25/09 by the midnight sweep (24/09 now reads 0).

## M2 Needs attention (Work Board tile)
types.go:468-483: sum of overdue + missed + rejected + blocked. PC Care today = 0 on both parks (pending_verification is not attention).

## M3 Inventory vaccine progress card (admin-web /vaccination, section "pc-care-inventory-progress")
- Endpoint: `GET /app/pc-care/tasks?date=<IST today>&category=inventory_vaccine&current_or_carry=true[&park_id]` paged 100 x 20 (apps/admin-web/features/preventive-care-vaccination/inventory-vaccine-progress.tsx:66-93).
- Backend filter backend/internal/pccare/adapters/postgres/tasks.go:824-860: due = date OR (due < date AND work_state IN (scheduled,delayed) AND status <> completed); work_state <> canceled. Feed & water removal rows hidden until IST cutoff (`feed_water_removal_config.cutoff_time`, STG 21:30) on their due day; category=deworming also returns feed_water_removal rows.
- Client (tsx:54-61, 115-117): rows = category inventory_vaccine AND (work_state IN scheduled,delayed OR (work_state completed AND due = today)). Current = rows; Overdue = work_state delayed; With verifier = status pending_verification; Done today = status completed OR work_state completed.
```sql
SELECT p.name, count(*) cur, count(*) FILTER (WHERE t.work_state='delayed') overdue,
  count(*) FILTER (WHERE t.status='pending_verification') verifier,
  count(*) FILTER (WHERE t.status='completed' OR t.work_state='completed') done_today
FROM pc_care_tasks t JOIN locations p ON p.location_id=t.park_id
WHERE t.category='inventory_vaccine' AND t.work_state<>'canceled'
  AND (t.work_state IN ('scheduled','delayed') OR (t.work_state='completed' AND t.due_business_date=(now() AT TIME ZONE 'Asia/Kolkata')::date))
GROUP BY 1;
```
STG: 0 rows (all 61 inventory_vaccine tasks are canceled) -> card shows 0/0/0/0.

## M4 PC Care verification queue (admin-web /verify, area Preventive care)
- `ceo_ai.verification_queue_status` area `preventive_care` = verification_items.vertical. TRAP: that vertical ALSO holds vaccination proofs and pen visit clips. For PC Care only use `public.verification_items` with `module='pc_care'`.
- Unit = one item per task (deworming/hoof) or per removal PEN (`source_ref_type='pc_care_removal_pen'`), not per animal.
```sql
SELECT category, status, count(*) FROM verification_items WHERE module='pc_care' GROUP BY 1,2;
```
STG: pc_deworming pending 10 / approved 87; pc_hoof_trimming pending 6 / approved 57; pc_feed_water_removal pending 6 / approved 48. (ceo_ai area total pending = 48 = 22 pc_care + 26 pen_visits + 0 vaccination.)

## M5 When was pen X last dewormed / hoof-trimmed (no screen; agent answer)
- Done = `status='completed'` (or submitted_at set for "done, awaiting check"); date = `submitted_at` in IST (NOT due_business_date). Always list cancelled rows separately and check both parks (pen names repeat). Resolve pen via references/pens.sql.
```sql
SELECT p.name park, s.name pen, coalesce(t.partition_label,'') part,
  max((t.submitted_at AT TIME ZONE 'Asia/Kolkata')::date) FILTER (WHERE t.status='completed') last_done,
  count(*) FILTER (WHERE t.status='pending_verification') awaiting_check, count(*) FILTER (WHERE t.work_state='canceled') cancelled
FROM pc_care_tasks t JOIN locations p ON p.location_id=t.park_id LEFT JOIN locations s ON s.location_id=t.shed_id
WHERE t.category='deworming' GROUP BY 1,2,3 ORDER BY 1,2,3;
```
STG sample: Channapatna Godel 1 Part 1-4 last dewormed 13/09/2026 (1 cancelled each); Coimbatore Castro 1 02/09/2026.

## M6 Animals treated
`count(pc_care_task_animals)` on completed tasks (feed_water_removal has no animal rows; it is pen-level).
STG completed: CPT deworming 33 tasks / 641 animals, hoof 28 / 468; CBE deworming 60 / 610, hoof 29 / 310; removal CPT 10, CBE 9 tasks.

## Other surfaces
- PC Care SOP page (/pc-care/sops): `/admin/sops` config (what to capture, whether tablet-in-feed deworming removes feed & water). No counts.
- Rounds (`GET /app/pc-care/rounds`, `pc_care_rounds`, 28 on STG) are mobile only.

## CEO questions
- "Which pens are due for deworming today / what's pending with the verifier?" / "Aaj deworming kitne pen mein baaki hai? Verifier ke paas kitna pending hai?" -> M1, M4
- "When did we last deworm Godel 1 Part 3?" / "Godel 1 Part 3 mein last deworming kab hua?" -> M5
- "How many goats were dewormed / hoof trimmed this month?" / "Is mahine kitne bakron ka deworming hua?" -> M6 (+ submitted_at window)
- "Was feed & water removed last night before deworming?" / "Kal raat feed-paani hataya tha kya?" -> feed_water_removal rows + pc_care_removal_pen_proofs
- "Any overdue preventive care?" / "Koi PC care kaam late chal raha hai?" -> M1/M2 (overdue) + rolled_forward_count
