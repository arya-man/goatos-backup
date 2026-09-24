Metrics: P1 clock-in tiles (working / clocked out / not clocked in / flagged) · P2 hours worked · P3 forgotten clock-outs (auto_closed) · P4 attendance days per person · P5 staff directory headcount · P6 per-person proof stats · P7 leave requests + on leave today · P8 roster coverage / "present" in ceo_ai · P9 operators and capabilities

Paths are under `backend/internal/workforce/` unless shown otherwise. Business day = IST. There is **no "late" and no "absent" rule** in the code: no shift start time exists, so "late" cannot be computed. "Absent" only means "not clocked in" (P1) or "approved leave" (P7).

## P1 Clock-in tiles (People > Attendance tab)
- Screen: `/people` Attendance tab, `apps/admin-web/features/people/clock-screen.tsx:80-90` (4 tiles; tapping a tile sets `bucket`).
- Endpoint: `GET /admin/workforce/clock-entries?date&park_id&designation&bucket&q` (`adapters/http/clock_handler.go:37`).
- Code: `adapters/postgres/clock_repository.go:279-311` (shared WHERE plus bucket/flag expressions), `:313-342` (summary).
- Formula: population = `workforce_members` with `status='active'`, LEFT JOIN `workforce_clock_entries` on the selected `business_date` (default today IST).
  - not_clocked_in: no entry for that date.
  - working: `status='open'` AND the date is today.
  - clocked_out: every other entry (closed, auto_closed, or open on a past date).
  - flagged: the entry has `offline_punch`, `location_missing`, `status='auto_closed'`, or is open on a past date. Flagged overlaps the other three tiles.
- Filters -> SQL: park `wm.primary_location_id`; designation `wm.primary_role_hint`; q `lower(display_name) LIKE`. The tiles ignore `bucket` and paging, so they always count the whole filter.
- Traps:
  - Park comes from the person's home park (`primary_location_id`), not from where they punched. 9 active people have no park.
  - Inactive staff are never counted.
  - The tiles count one entry per person per day. The unique key is (member, business_date).
- SQL (run 2026-09-24):
```sql
SELECT coalesce(l.name,'(no park)') park,
  count(*) FILTER (WHERE e.status='open') working,
  count(*) FILTER (WHERE e.status IN ('closed','auto_closed')) clocked_out,
  count(*) FILTER (WHERE e.clock_entry_id IS NULL) not_clocked_in
FROM workforce_members wm LEFT JOIN locations l ON l.location_id=wm.primary_location_id
LEFT JOIN workforce_clock_entries e ON e.workforce_member_id=wm.workforce_member_id
  AND e.business_date=(now() AT TIME ZONE 'Asia/Kolkata')::date
WHERE wm.status='active' GROUP BY ROLLUP(1);
```
  - Result for 24/09 (re-run 25/09, after the midnight sweep): working 0, clocked_out 10, not_clocked_in 30, total 40. Flagged = 5 (4 auto_closed + 1 offline punch). By park (working/out/not in): CPT 0/1/16; CBE 0/8/6; no park 0/1/8. (Before the sweep on 24/09 the same day read working 4, clocked_out 6.)
  - 25/09 00:15 IST: nobody clocked in yet (not_clocked_in 40).
- Questions: "How many people clocked in today?" · "Aaj kitne log kaam pe aaye?" · "Channapatna mein kisne clock-in nahi kiya?"

## P2 Hours worked
- Screen: Hours column of the Attendance table (`clock-screen.tsx:222`, `hours_label`), and the phone presence board.
- Code:
  - Punch out: `clock_repository.go:172` sets `worked_minutes = floor((out - in)/1 min)`, with a floor of 0 if the device clock was skewed.
  - Label: `app/clock_service.go:498-507`. It shows `worked_minutes` as "Xh MMm" (`:576`). For an open entry today it shows live elapsed time as "Xh MMm so far". A past-day open entry shows no hours.
- Formula: hours = `sum(worked_minutes)/60`. Only closed and auto_closed rows have minutes.
- Traps:
  - auto_closed rows count hours up to 23:59:59 IST (see P3), so they overstate. Report own-punch hours (`status='closed'`) separately.
  - Open rows have NULL minutes.
- SQL (run 25/09/2026): month to date, all rows = 2694.9 h (280 entries, 22 people; own 1775.0 h + auto_closed 919.9 h). Own punches = 1775.0 h, which is also the all-dates own total.
```sql
SELECT round(sum(worked_minutes) FILTER (WHERE status='closed')/60.0,1) own_hrs,
       round(sum(worked_minutes) FILTER (WHERE status='auto_closed')/60.0,1) auto_hrs
FROM workforce_clock_entries
WHERE business_date >= date_trunc('month', now() AT TIME ZONE 'Asia/Kolkata')::date;
```
- Questions: "Total man-hours this month?" · "Naveen ne is mahine kitne ghante kaam kiya?" · "Average hours per day per operator?"

## P3 Forgotten clock-outs (auto_closed)
- Code: `clock_repository.go:533-581`.
  - A midnight sweeper runs `AutoCloseStaleClockEntries`.
  - The person's next punch also closes any stale open days first (`:73-80`).
- Formula: an open entry whose `business_date` is before today gets `status='auto_closed'`, `clock_out_at` = 23:59:59 IST of that day, and `worked_minutes` counted up to that instant. On the tiles it shows as clocked_out plus flagged.
- SQL (run 25/09/2026): 93 auto_closed rows, 01/09 to 24/09.
```sql
SELECT wm.display_name, count(*) FROM workforce_clock_entries e JOIN workforce_members wm USING (workforce_member_id)
WHERE e.status='auto_closed' AND e.business_date >= date_trunc('month', now() AT TIME ZONE 'Asia/Kolkata')::date GROUP BY 1 ORDER BY 2 DESC;
```
  - Most this month: Sagar Mahoor 17, Ravi Kumbar 15, Sahid Gazi 12, Kumar Sharath 11, Dinakar 9.
- Questions: "Who forgets to clock out?" · "Clock-out bhoolne wale kaun hain?"

## P4 Attendance days per person
- There is no screen total. It is derived from the same table: one row per person per business day = days attended.
- Trap: a day with only an auto_closed entry still counts as attended, because the person did clock in.
- SQL (run 2026-09-24): Munna Kumar 24 days (204.3 own hours), Naveen 22, Dinakar 21.
```sql
SELECT wm.display_name, count(*) days FROM workforce_clock_entries e JOIN workforce_members wm USING (workforce_member_id)
WHERE e.business_date BETWEEN :from AND :to GROUP BY 1 ORDER BY 2 DESC;
```
- Questions: "Attendance of operators this month?" · "Kaun sabse zyada din aaya?"

## P5 Staff directory headcount
- Screen: `/people` directory (`features/people/people-board.tsx:175`, "N shown" = rows on the current page only).
- Endpoint: `GET /admin/workforce/people?park_id&department_id&status&q` (`adapters/http/people_handler.go:30`).
- Code: `adapters/postgres/people_repository.go:176-226`.
- Filters -> SQL: `primary_location_id`, `department_id`, `status`, and `display_name/email LIKE`. Default page size is 25.
- Trap: the screen count is a page count, not a total. Role = `primary_role_hint`, or the HR grade for leadership.
- SQL (run 25/09/2026): active 40 (CPT 17, CBE 14, no park 9); inactive 2.
  - By role: operator 30, cxo 5, verifier / health_director / feed_director / pc_director / other 1 each.
```sql
SELECT coalesce(l.name,'(no park)'), wm.primary_role_hint, count(*) FROM workforce_members wm
LEFT JOIN locations l ON l.location_id=wm.primary_location_id WHERE wm.status='active' GROUP BY ROLLUP(1,2);
```
- Questions: "How many staff do we have per park?" · "Kitne operators hain Coimbatore mein?"

## P6 Per-person proof stats (directory columns)
- Code: `people_repository.go:108-121` (LATERAL over `verification_items` where `operator_id = wm.user_id`).
- Formula:
  - uploads: status <> 'withdrawn'.
  - approved: status 'approved' AND `auto_resolution IS NULL` (approved by a person).
  - rejected: status 'rejected'.
  - pending: status 'pending'.
  - not_reviewed: `auto_resolution IS NOT NULL` (settled by sampling).
- Trap: approvals made by the sampling policy are excluded from "approved". Rejection rate = rejected / (approved + rejected).
- SQL: group the same FILTERs by `operator_id`, then join `workforce_members.user_id`. Tenant totals (run 25/09/2026): approved by a person 18,565 (plus 713 auto-approved, not_reviewed), pending 3,142, rejected 138, withdrawn 188.
- Questions: "Which operator has most rejected proofs?" · "Kiske proof sabse zyada reject hue?"

## P7 Leave requests and on leave today
- Screens: `/leave` (list plus the approver queue, `features/leave/leave-page.tsx`).
- Endpoints:
  - `GET /admin/leave/requests?status&park_id` (`adapters/http/leave_handler.go:45`).
  - `GET /admin-web/leave/approvals` (`:42`).
- Code: `adapters/postgres/leave_repository.go`.
  - Queue: `:180-189`. Pending, and the caller's slot is open. A park head sees `park_head_required AND park_head_decision IS NULL` in their parks. HR sees `hr_required AND hr_decision IS NULL`.
  - Outcome: `:445-457`. Any rejection makes it rejected. The last required approval makes it approved and creates a `workforce_absences` row.
  - On leave today: `:199` = approved AND `starts_on <= day <= ends_on`.
  - Days: `:885`, inclusive calendar days (`ends_on - starts_on + 1`, weekends included).
- Statuses: pending / approved / rejected / withdrawn. `workforce_leave_requests.workforce_member_id` -> `workforce_members`.
- SQL (run 2026-09-24): 0 rows in `workforce_leave_requests`, and 0 in `workforce_absences`.
```sql
SELECT wm.display_name, r.starts_on, r.ends_on FROM workforce_leave_requests r JOIN workforce_members wm USING (workforce_member_id)
WHERE r.status='approved' AND (now() AT TIME ZONE 'Asia/Kolkata')::date BETWEEN r.starts_on AND r.ends_on;
```
- Questions: "Who is on leave today?" · "Aaj kaun chutti pe hai?" · "Kitni leave requests pending hain?"

## P8 Roster coverage and "present" in ceo_ai
- Endpoint: `GET /admin/roster/coverage?active=true` (`adapters/http/roster_handler.go:48`). This is the vaccination operator seat covered by a backup.
- Code: `roster_repository.go:1752-1790`. Active = absence status approved/escalation_required AND `starts_at <= now() < ends_at`.
- `ceo_ai.workforce_coverage_status`:
  - `coverage_status='present'` means **not on approved leave**. It does NOT mean clocked in.
  - `active_work_count` / `overdue_work_count` come from `sop_tasks` only (see tasks.md T5).
- Trap: never answer "who is present today" from this view. Use P1.
- SQL (run 2026-09-24): 40 rows, all 'present', 0 active absences. 23 active `workforce_positions`.
- Questions: "Is any operator seat uncovered?" · "Backup kaun cover kar raha hai?"

## P9 Operators and capabilities
- Endpoint: `GET /admin/operators?status&role_hint&location_id&q` (`adapters/http/handler.go:36`; `adapters/postgres/repository.go:50`).
- Capabilities: `workforce_member_capabilities` JOIN `workforce_capabilities` (status active).
- Module access for a person: `GET /admin/workforce/people/{id}/access`.
- SQL (run 2026-09-24): 0 active member capabilities. Access is granted through role grants and module access, not this table.
- Questions: "Which operators can vaccinate?" · "Kaun kaun operator hai Channapatna mein?"

## P10 Attendance table columns, flag chips and the entry drawer (People > Attendance tab)
- Screen: `/people?tab=clock`, table in `apps/admin-web/features/people/clock-screen.tsx` (columns: person, designation, park, clock in, clock out, hours (P2), location, device, flags). Row click opens `features/people/clock-entry-drawer.tsx` (same fields for one entry).
- Endpoint: `GET /admin/workforce/clock-entries` (P1). Fields: `clock_in_label`, `clock_out_label`, `hours_label`, `location_label`, `device_label`, `flags[]`, `park_label`, `designation`.
- Code: `backend/internal/workforce/app/clock_service.go:508-531`.
  - location_label = the punch address. device_label = "device model · app version".
  - Flags: `offline` (offline_punch), `no_location` (location_missing), `auto_clocked_out` (status auto_closed), `not_clocked_out` (status open on a past business_date; the sweeper has not run yet).
- Filters: date, park, designation, q, bucket (see P1). Keyset paged.
- Traps: `not_clocked_out` rows exist only in the minutes before the midnight sweep. After the sweep they become `auto_clocked_out`.
- SQL (run 25/09/2026 00:15 IST, month to date): offline 4, no_location 0, auto_closed 93, open on a past day 0 (the sweep had run).
```sql
SELECT count(*) FILTER (WHERE offline_punch) offline, count(*) FILTER (WHERE location_missing) no_location,
  count(*) FILTER (WHERE status='auto_closed') auto_closed,
  count(*) FILTER (WHERE status='open' AND business_date < (now() AT TIME ZONE 'Asia/Kolkata')::date) not_clocked_out
FROM workforce_clock_entries WHERE business_date >= date_trunc('month', now() AT TIME ZONE 'Asia/Kolkata')::date;
```
- Questions: "Who punched offline this month?" · "Kisne bina location ke clock-in kiya?"

## P11 Directory row: clock-in chip and filters (People > All tab)
- Screen: `features/people/people-board.tsx:112-164` (filters: search, park, department, status) and `:247-252` (chip "Clocked in HH:MM" or "Not clocked in").
- Endpoint: `GET /admin/workforce/people` (P5). Field `clock_in_today_label`, set at `adapters/postgres/people_repository.go:161-163` = today's IST `clock_in_at` formatted "15:04".
- Formula: chip = the person has a `workforce_clock_entries` row for today's IST business_date. Same population as P1 not_clocked_in, per person.
- SQL: P1's query without GROUP BY, selecting `wm.display_name, to_char(e.clock_in_at AT TIME ZONE 'Asia/Kolkata','HH24:MI')`.
- Questions: "What time did Naveen clock in today?" · "Naveen aaj kitne baje aaya?"

## P12 Leave screen rows, status line and approval config (/leave)
- Screen: `features/leave/leave-page.tsx`.
  - Approver queue table `:117-140`: person, park, dates, "my slot" chip, status line, approve / reject.
  - All-requests table `:216-239` with status chips `:194` (all / pending / approved / rejected / withdrawn).
  - "Who approves" flags: `features/leave/leave-config-panel.tsx` (CEO only).
- Endpoints: `GET /admin-web/leave/approvals` (`leave_handler.go:42`), `GET /admin/leave/requests?status` (`:45`), `GET /admin/leave/approval-config` (`:46`).
- Code: `backend/internal/workforce/app/leave_service.go`.
  - `dates_label` `:424` = "DD/MM/YYYY – DD/MM/YYYY · N days" (inclusive, P7).
  - `my_slot_label` `:454-458`: the slot (park head or HR) the viewer can still sign.
  - `status_line` `:464-500`: withdrawn / approved lines; rejected = "Rejected by <slot> · <note>"; pending = "Park head approved" or "Waiting for park head", joined with the HR equivalent, only for required slots.
- Config: `workforce_leave_approval_config` (park_head_required, hr_required). No row = both required (`domain/leave_types.go:122`).
- SQL (run 25/09/2026): 0 config rows (both required), 0 requests.
```sql
SELECT r.status, count(*), count(*) FILTER (WHERE r.park_head_required AND r.park_head_decision IS NULL) waiting_park_head,
  count(*) FILTER (WHERE r.hr_required AND r.hr_decision IS NULL) waiting_hr
FROM workforce_leave_requests r GROUP BY 1;
```
- Questions: "Whose approval is leave waiting for?" · "Leave kiske approval pe atki hai?" · "Does HR need to approve leave?"

## P13 Vaccination operators tab (People > Vaccination)
- Screen: `/people?tab=vaccination`, `features/people/vaccination-operators-screen.tsx`. Loader `vaccination-operators-scope.ts:35-43,114`.
- Endpoints:
  - `GET /admin/roster/positions?status=active&scope_type=center&scope_id=<park>` (`workforce/adapters/http/roster_handler.go:35`).
  - `GET /admin/roster/leave` (`:45`), the planned-leave column.
  - `GET /vaccination/capacity-config` and `GET /vaccination/operator-assignment/config?park_id` (`vaccinationexecution/adapters/http/handler.go:195,197`; SQL `adapters/postgres/repository.go:3249,4071`).
- KPI tiles (`:764-790`, computed in the browser at `:608-609`):
  - Operators = active **non-backup** positions at the park (`:423`). All center positions count, including weighing operators and directors.
  - Cap / operator = `vaccination_capacity_config.max_per_day`.
  - Operators / day = `vaccination_operator_assignment_config.active_operators_per_day` (1 = single + fallback, 2 = pair, 3 = all parallel).
  - Daily capacity = operators/day × max_per_day.
- Roster table (`:849-985`): Person, Park, Shift, Cap (`vaccination_daily_animal_cap`, else max_per_day, `:526`), Week off (`week_off_weekday`), Planned leave, Weekly schedule, Status.
  - Status: "On leave today" if a roster leave covers today; else "Week-off today" if the weekday matches; else "Available" (`:872-985`).
- Weekly assignment preview (`:1103-1140`): computed in the browser from the config's default, selected operators and week-offs. It is not stored. Actual drive assignments are in `vaccination_drive_assignments` (vaccination.md §5).
- Status: tiles and roster COVERED by the SQL below. Preview is client-only logic.
- Traps:
  - The Operators tile at Coimbatore is 6, but only 3 are vaccination seats (the other 3 are weighing and director positions).
  - The browser uses the viewer's clock for "today" on the Status column.
- SQL (run 25/09/2026, a Friday):
  - Coimbatore: operators 6 (vaccination seats 3), 3 per day, cap 200, capacity 600.
  - Channapatna: 6 (6), 1 per day, cap 200, capacity 200. 4 of them have Friday week-off.
```sql
SELECT l.name park, count(*) FILTER (WHERE NOT p.is_backup_slot) operators_kpi,
  count(*) FILTER (WHERE NOT p.is_backup_slot AND p.position_code LIKE 'vaccination_operator%') vacc_seats,
  c.active_operators_per_day, cc.max_per_day, c.active_operators_per_day*cc.max_per_day daily_capacity,
  count(*) FILTER (WHERE NOT p.is_backup_slot AND p.week_off_weekday = trim(lower(to_char((now() AT TIME ZONE 'Asia/Kolkata')::date,'day')))) week_off_today
FROM workforce_positions p JOIN locations l ON l.location_id=p.scope_id
LEFT JOIN vaccination_operator_assignment_config c ON c.park_id=l.location_id CROSS JOIN vaccination_capacity_config cc
WHERE p.status='active' AND p.scope_type='center' GROUP BY l.name, c.active_operators_per_day, cc.max_per_day;
```
- Default operator: Channapatna = Sagar Mahoor. Coimbatore = Arun Kumar (3 selected).
- Questions: "How many vaccination operators per park, and daily capacity?" · "Channapatna mein roz kitne animals vaccinate ho sakte hain?" · "Aaj kaun operator week-off pe hai?"

## P14 Notifications tab (who hears which alert)
- Screen: `/people?tab=notifications`, `features/people/notification-matrix.tsx:106-256`. Rows = alerts grouped by module. Columns = designations. A tick means that designation receives the alert.
- Endpoint: `GET /admin/notifications/designations` (`backend/internal/notificationaudience/adapters/http/handler.go`). App: `app/config_service.go:72-106`.
- Formula: designations = the tenant override row in `notification_alert_audiences.designation_codes` if one exists, otherwise the Go catalog default (`domain/catalog.go`, `DefaultDesignations`).
- Status: GO-ONLY for alerts without an override (the defaults live only in Go).
- SQL (run 25/09/2026): 11 alerts have overrides (feed.*, weighing.*, pen_visits.proof_pending).
```sql
SELECT alert_key, designation_codes FROM notification_alert_audiences ORDER BY 1;
```
- Questions: "Who gets the pen visit proof alert?" · "Feed proof ka alert kis kis ko jaata hai?"
