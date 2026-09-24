# Logic card: shifting (pen moves / transfers)
Index: shifts completed + animals moved (Herd Analytics movements, API-only) · shift pipeline tabs (pending approval / to execute / rework / completed) · shift approvals approved/rejected · shifts by type (category) · verification (verified/unverified) · counts_movement_daily shifts trap

## Lifecycle (one `shifting_events` row per move; animals in `shifting_event_impacts.head_count`)
1. Raise: `POST /app/counts/shifting-events` (`backend/internal/counts/adapters/http/app_write_handler.go:442` RecordShiftingEvent). Type = `category` IN growth, health, breeding, delivery, spacing, flushing, normal (`app_write_handler.go:416`; tag rules `internal/counts/domain/shifting_type.go:176` ResolveShiftTypeDecision decide the destination stage tag). Writes shifting_events (event_status='pending', authorization_state='pending') + a `counts_approval_requests` row (request_type='shifting', payload.goat_ids).
2. Approve (admin-web Approvals / mobile): only AUTHORIZES, moves nothing (`adapters/postgres/approval_repository.go:1008-1035`): authorization_state='authorized', event_status='authorized'. Reject -> request 'rejected' (event normally canceled).
3. Execute: operator completes on phone `POST /app/counts/shifting-events/{id}/complete` (`adapters/http/shifting_execution_handler.go:71`, `adapters/postgres/shifting_execution.go:76`, flip at :898): relocates goats, sets event_status='applied', applied_at=completed_at. Cancel: `/cancel` -> 'canceled'.
4. Verify evidence (video): verification_state unverified -> verified / rejected (= rework) (`shifting_execution.go:365/407`); does not undo the move.
- SOP content (the three cards, captures, questions) is config only: admin-web Counts > SOPs `features/sops/shifting-editor.tsx` / `shifting-summary.tsx`, backend `internal/shiftingsop`. No numbers.

## Shifts done + animals moved (Herd Analytics `months[].movements`, `animals_moved`, totals)
- Q: kitne shift kiye / kitne janwar shift hue / pen transfers this month / movements.
- API `GET /counts/herd-analytics` returns them, but the Counts > Analytics screen does NOT draw them (only used for the empty state, `herd-analytics.tsx:230`).
- SQL `adapters/postgres/herd_analytics.go:128-148`: event_status='applied' AND completed_at NOT NULL, month = IST completed_at; movements = events, animals = sum(impacts.head_count) (pre-aggregated per event). Park filter matches source OR destination park. Verification state ignored (unverified and verified both count).
- Value 24/09/2026 default window (2026-08-01..24/09): **25 moves / 25 animals** (Aug 7/7, Sep 18/18). By type (all-time applied): health 15, growth 4, breeding 4, delivery 2.
```sql
SELECT to_char((se.completed_at AT TIME ZONE 'Asia/Kolkata')::date,'YYYY-MM') m, se.category, count(*) moves, sum(i.hc) animals
FROM shifting_events se LEFT JOIN (SELECT shifting_event_id, sum(head_count) hc FROM shifting_event_impacts GROUP BY 1) i USING (shifting_event_id)
WHERE se.event_status='applied' AND se.completed_at IS NOT NULL
  AND (se.completed_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN '2026-08-01' AND '2026-09-24'
GROUP BY ROLLUP(1,2);   -- total 25/25
```
- Pen names: source/destination = `source_shed_id`+`source_partition_label` / `destination_shed_id`+`destination_partition_label` -> label via references/pens.sql rules (shed name + partition, e.g. "Godel 1 Part 3"), never shed alone.

## Shift pipeline (mobile "pending execution" tabs, `GET /app/counts/shifting-events/pending-execution`)
- Q: kitne shift baaki hain / approved but not moved / shifts pending approval / rework.
- SQL `shifting_execution.go:1215-1245` (list) and :1388 (tab counts, only when a raised_at window is passed), canceled always excluded, plus role/park visibility filters:
  pending = event_status='pending'; to execute ("authorized") = 'authorized' AND verification_state<>'rejected'; rework = verification_state='rejected' (not pending/canceled); completed = 'applied' AND verification_state<>'rejected'; all = not canceled/pending.
- Values all-time (no visibility filter): pending **2**, to execute **13**, rework **0**, completed **25**, all 38; canceled 32. Verified 16 of 25 applied (9 unverified).
```sql
SELECT count(*) FILTER (WHERE event_status='pending') pending,
       count(*) FILTER (WHERE event_status='authorized' AND verification_state<>'rejected') to_execute,
       count(*) FILTER (WHERE verification_state='rejected' AND event_status NOT IN ('canceled','pending')) rework,
       count(*) FILTER (WHERE event_status='applied' AND verification_state<>'rejected') completed
FROM shifting_events;   -- 2 / 13 / 0 / 25
```
- Trap: the 2 'pending' events (raised 21/08 and 29/08, health) have REJECTED approval requests: stale rows, not real open requests. 15 events were approved then canceled.

## Shift approvals (admin-web Approvals, type "Shifting")
- `GET /admin-web/counts/approvals?status=` (`approval_repository.go:660`); KPI "Shifting in view" counts only the 20-row page after client-side type/farm filter (`features/approvals/approvals-page.tsx:46-90`): not a total.
- Values all-time: approved **53**, rejected **19**, pending 0.
```sql
SELECT status, count(*) FROM counts_approval_requests WHERE request_type='shifting' GROUP BY 1;
```

## ceo_ai
- `ceo_ai.counts_movement_daily` shifts_in / shifts_out / transfers_out filter `event_status='completed'` (and `applied_at`), a status shifting_events never has -> always **0** (Aug/Sep 2026 all 0). DISAGREES with the screen/API (25). Do not use it for shifts; use shifting_events 'applied' as above. Its approvals_pending counts `counts_approval_requests` status='pending' by raised day (0 today).
