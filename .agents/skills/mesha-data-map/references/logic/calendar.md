# Logic card: Calendar (vaccination drives) and Protocol Adherence

Index: C1 Calendar week / month / history lists · C2 Drive card and full drive detail · C3 Event drawer and drive targets · C4 Protocol Adherence KPIs and ledger

Scope: both screens cover **vaccination only** (owner tab "PC"). Business day = IST. Both backends build their numbers in a single large Go-owned SQL (2,000+ lines). Answer an agent question from the vaccination tables using the approximations below, and point to vaccination.md §3-§5 for animal-level numbers. Verified read-only on goatos-stg 25/09/2026 00:00 IST.

## C1 Calendar lists (admin-web /calendar)
- Screen: `apps/admin-web/features/calendar/calendar.tsx`.
  - View tabs `:414-430`: Week, Month picker, History.
  - Owner tabs `:434` and workstream tabs `:451`, both from `presentation`.
  - Day chip band `:638` and event rows (`EventRow`).
  - Windows (`calendar-window.ts`): Week = Monday to Sunday of the anchor date (`:84`). History = anchor − 44 days to anchor, with `status=completed` (`:112`, `calendar.tsx:174`). Month picker = a markers-only call (`include_date_markers`, `markers_only`).
- Endpoint: `GET /calendar/vaccination/events?park_id&owner_key&status&date_from&date_to&cursor` (`backend/internal/calendar/adapters/http/handler.go:51`). SQL `calendarCanonicalListSQL` (`adapters/postgres/canonical_read.go:2140-2222`).
- Formula (Go-only):
  - Rows are **drive events** (`event_type='vaccination_drive'`). Single-dose rows are excluded from the list.
  - A drive = batch_events plus catchup_drive_events, grouped per (drive key, park, IST due day) in `park_drive_groups` (`:758`).
  - Status precedence: missed > review > in_progress > overdue > due / scheduled. `obligation_drive_effective_state` (`:1462`) is submission-aware: a drive whose work is all submitted is not "overdue".
  - Default list hides completed, canceled and deferred. Catch-up drives look back 45 days for missed, in-progress or review work.
  - Event row fields: title, subtitle, status, `due_at`, `park_code`, `shed_name`, `summary_tertiary`, `drive_summary`.
- Approximate SQL (drive days per park = assignment days):
```sql
SELECT p.name park, a.planned_date, count(DISTINCT a.shed_id) sheds, count(m.obligation_id) animal_doses
FROM vaccination_drive_assignments a JOIN locations p ON p.location_id=a.park_id
LEFT JOIN vaccination_drive_assignment_members m ON m.assignment_id=a.assignment_id AND m.canceled_at IS NULL
WHERE a.planned_date BETWEEN (now() AT TIME ZONE 'Asia/Kolkata')::date - 3 AND (now() AT TIME ZONE 'Asia/Kolkata')::date + 30
GROUP BY 1,2 ORDER BY 2;
```
  - STG (25/09 window): CBE 22/09 (116), CPT 24/09 (3). CPT 21/09 (1) drops out of the −3-day window.
  - Next drives: CBE 13/10 (58); CPT 15-19/10 (177/120/148/121/149); CBE 20-24/10 (200/123/199/136/154).
- Traps:
  - The calendar also shows unassigned catch-up drives, which are not in `vaccination_drive_assignments`.
  - Month markers use their own whole-month aggregate.
  - Drive-date overrides (`vaccination_drive_date_overrides`) move a drive's day.
- Questions: "When is the next vaccination drive?" · "Agla drive kab hai, kaunse park mein?" · "Is hafte kitne drives hain?"

## C2 Drive card and full drive detail (/calendar, /calendar/drive/[eventId])
- Screens: `features/calendar/calendar-drive-card.tsx` and `calendar-drive-detail.tsx`. Route `app/(admin)/calendar/drive/[eventId]/page.tsx`.
- Endpoint: `GET /calendar/vaccination/events/{event_id}` (`handler.go:52`; `calendarCanonicalDetailSQL`, `canonical_read.go:2224`).
- Fields (`drive_summary`, built at `canonical_read.go:1323` and `:1702-1746`): drive_name, park_name, due_date, owner_label, drive_total, submitted_animals, shed_count, sheds_completed, vaccine_labels.
- Card counts (`obligation_drive_effective_state`, `:1462-1517`), counted per obligation, with disjoint buckets:
  - submitted = not completed AND `submitted_for_verification`.
  - due = not completed, not submitted, not deferred, status open or review.
  - deferred = deferred AND not submitted.
  - total = completed + submitted + due + overdue + deferred.
- Status: GO-ONLY. For an animal-level answer on the drive day, use vaccination.md §4 (live tracker SQL).
- Questions: "How much of today's drive is submitted?" · "Aaj ke drive mein kitne animals ho gaye?"

## C3 Event drawer and drive targets
- Screen: `features/calendar/calendar-event-drawer.tsx`. Sections: summary, source and rule, execution, stock, proof, verification, notification channels, recent actions. Target list with defer / exit reason.
- Endpoints: `GET /calendar/vaccination/events/{id}` and `/{id}/targets?cursor&limit=10` (`handler.go:52-53`; SQL `adapters/postgres/targets.go:60-227`, over obligation_batches, obligation_instances, drive assignments and `obligation_status_events`). `/{id}/history` (`:54`).
- Writes (nudge, snooze, escalation acknowledge / resolve, `:55-58`) are not metrics.
- Traps: severity is critical when missed or `due_at < now()`, and warning when due within 24 h (`canonical_read.go:105-110`). This is a time-of-day comparison, not an IST day comparison.
- Status: GO-ONLY (per-target rows are the obligations in vaccination.md §4).
- Questions: "Which animals were deferred in this drive and why?" · "Is drive mein kaunse animal chhoote, kyun?"

## C4 Protocol Adherence (admin-web /protocol-adherence)
- Screen: `apps/admin-web/features/process-integrity/protocol-adherence.tsx`.
  - 4 KPI tiles `:292-302`: Overall adherence %, Open process gaps, Deferred (explained), On track with "done/expected".
  - Severity chips and work-state chips `:311-330`.
  - Ledger table `:357-440`: expected, actual, gap, severity, owner chain, next action, evidence, work state.
- Endpoint: `GET /vaccination/adherence?park_id&as_of&work_state&severity&limit&cursor` (`backend/internal/processintegrity/adapters/http/handler.go:62`). Service `app/service.go:80-126`. Summary SQL `processIntegrityCanonicalAdherenceSummarySQL` (`adapters/postgres/repository.go:2113-2123`). Percent `repository.go:295`.
- Formula (Go-only):
  - Scope: category vaccination, due in the last 30 days before `as_of` (`service.go:82-85`), and the **latest drive only**. `selected_adherence_scope` (`repository.go:1950-1990`) picks ONE (batch, park, shed, partition, rule, IST day): the most recent `due_at <= as_of`, else the next one.
  - Grain: one row per park / shed / batch / rule / protocol / business day.
  - work_state (`repository.go:1425-1455`): completed (all done, nothing rejected or recorded) > rejected > blocked (not usable) > deferred (deferred, health deferred, quarantine, ICU) > missed > verification_pending > in_progress > overdue (IST due day < as_of day) > due > scheduled.
  - severity: completed = ok; scheduled / due / in_progress / deferred / verification_pending = watch; proof_pending / overdue = at_risk; else broken.
  - process_intact = work_state IN (completed, scheduled, due, in_progress, deferred).
  - Adherence % = Σcompleted_count ÷ Σexpected_count × 100. Tile colour: ≥ 90 green, ≥ 70 amber, else red.
  - Open gaps = rows where NOT process_intact. On track = process_intact rows. Deferred = Σ GREATEST(deferred_count, 1) on deferred rows.
- Traps:
  - The screen shows ONE drive, not a 30-day average. "Adherence this month" cannot be read off this tile.
  - An unassigned operator is not a gap (owner shows "missing" instead).
- Status: GO-ONLY. A per-drive approximation comes from vaccination.md §4 (done ÷ assigned for that drive day).
- Questions: "Was the last vaccination drive done on time?" · "Pichhle drive ka adherence kitna tha?" · "Kitne process gaps khule hain?"
