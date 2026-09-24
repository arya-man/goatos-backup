Sections: Model & states | Verify > Toxin tab: review list | Tester list counts (by status) | Outcome (positive/negative) | Rounds & retests | Per vendor/feed batch | Pending vs done | Traps

# TOXIN TESTS (aflatoxin strip test per feed load)

## Model & states
- Table `public.toxin_test_tasks` (one row per feed_purchase_id x round_no); steps in `public.toxin_test_step_completions` (<=6 working steps + step 7 reading). No `ceo_ai.*` view; query public tables, filter `tenant_id`.
- Created automatically when a feed purchase is marked Reached: `backend/internal/toxin/app/feed_purchase_reached_handler.go`, insert `backend/internal/toxin/adapters/postgres/repository.go:114` (round_no=1, origin='purchase'; snapshots farm_label, feed_item_label, vendor, batch_no, purchase_date, quantity_kg).
- Status (`backend/internal/toxin/domain/task.go:32`): in_progress -> pending_review -> accepted | cancelled.
- Outcome (task.go:40): negative | positive | invalid. Qualitative kit strip. NO ppb value, NO numeric pass/fail threshold is stored.
- Submit (task.go:289): invalid -> cancelled + auto retest (origin invalid_retest); negative/positive -> pending_review.
- Verdict (task.go:313, CEO/CXO only): accept -> accepted; reject (reason required) -> cancelled + retest (origin rejected_retest, round_no+1). Retest mint: repository.go:460.
- Labels (task.go:337/350/363): Negative / Positive / Invalid strip; chip "Test due", "Step N of M", "Waiting — next step in N min", "Waiting for review", "Reviewed", "Cancelled — retest created".

## Verify > Toxin tab > Review list (rows, outcome, round chip)
Screen: admin-web `/verify?toxin=1` (features/verification-review/toxin-review-section.tsx, toxin-review-list.tsx; tab gated by control `toxin_tab`, verification-review-page.tsx:90)
Endpoint: GET /toxin/review (handler.go:62,78) -> default statuses [pending_review]; `cursor`, `limit` (default 20); `status=a,b` overrides.
Code: repository.go:182 ListTasks (keyset created_at DESC, task_id DESC); row mapping toxin-rows.ts:31-36 (round chip only when round_no>1).
Formula: rows = tasks WHERE tenant AND status='pending_review', newest first.
Filters->SQL: tab only; no date/farm/vendor filter on screen. Pagination via tx_cursor.
Traps: "Reviewed"=accepted says nothing about outcome — an accepted round can be Positive. List is not date-bounded.
```sql
SELECT created_at AT TIME ZONE 'Asia/Kolkata' AS created_ist, round_no, farm_label, feed_item_label, vendor, batch_no, outcome
FROM public.toxin_test_tasks
WHERE tenant_id='00000000-0000-4000-8000-000000000001' AND status='pending_review'
ORDER BY created_at DESC, task_id DESC;
```
stg value (24/09/2026): 6 rows awaiting review (4 negative, 2 positive).
Q: "How many toxin tests are waiting for my review?" / "kitne toxin test review ke liye pending hain?"

## Status counts (tester list GET /app/toxin/tasks)
Endpoint: GET /app/toxin/tasks?filter=all|pending|completed (handler.go:58,71). Mobile tester list; same data backs any count question.
Code: counts repository.go:250 (`GROUP BY status`, whole tenant, not page-local); filters task.go:414-470: pending = in_progress+pending_review; completed = accepted+cancelled.
```sql
SELECT status, count(*) FROM public.toxin_test_tasks
WHERE tenant_id='00000000-0000-4000-8000-000000000001' GROUP BY status;
```
stg value (24/09/2026): in_progress 9, pending_review 6, accepted 0, cancelled 0 (pending=15, done=0).
Q: "How many toxin tests are pending vs done?" / "toxin test kitne pending aur kitne complete?"

## Outcome: positive (fail) / negative (pass)
Formula: fail = outcome='positive'; pass = outcome='negative'; invalid = strip failed (auto retest, not a feed result). outcome is NULL until step 7.
Traps: positive is only a strip reading until CEO verdict; count positives regardless of status for "kitne fail", but say how many are still pending_review. Week = IST Monday start on created_at (test date) — or purchase_date if asked per load.
```sql
SELECT count(*) FILTER (WHERE outcome='positive') fail, count(*) FILTER (WHERE outcome='negative') pass,
       count(*) FILTER (WHERE outcome='invalid') invalid, count(*) FILTER (WHERE outcome IS NULL) not_read
FROM public.toxin_test_tasks
WHERE tenant_id='00000000-0000-4000-8000-000000000001'
  AND (created_at AT TIME ZONE 'Asia/Kolkata')::date >= date_trunc('week', now() AT TIME ZONE 'Asia/Kolkata')::date;
```
stg value (24/09/2026): all-time fail 2, pass 4, invalid 0, not read 9; this week (from 21/09) 4 tests created.
Q: "How many toxin tests failed this week?" / "is hafte toxin test kitne fail hue?"

## Rounds & retests
Formula: round_no>1 exist only via invalid strip or reject; origin tells why.
```sql
SELECT round_no, origin, count(*) FROM public.toxin_test_tasks
WHERE tenant_id='00000000-0000-4000-8000-000000000001' GROUP BY 1,2;
```
stg value (24/09/2026): only round 1 / origin purchase (15). No retests yet.
Q: "Any feed load retested?" / "kisi load ka toxin dobara test hua?"

## Per vendor / feed item / batch
Formula: group by snapshot columns vendor, feed_item_label, batch_no (join feed_purchase_id to the feed purchase for live data).
```sql
SELECT vendor, feed_item_label, count(*) tests, count(*) FILTER (WHERE outcome='positive') positive
FROM public.toxin_test_tasks WHERE tenant_id='00000000-0000-4000-8000-000000000001'
GROUP BY 1,2 ORDER BY positive DESC, tests DESC;
```
stg value (24/09/2026): 3 vendors; both positives = Navaladi poultry farm and cattle feeds / Mesha Adult Concentrate (pending_review).
Q: "Which supplier's feed failed toxin?" / "kis supplier ka feed toxin me fail hua?"

## Traps
- No aflatoxin ppb/threshold in DB; answer "Positive/Negative strip", never invent a number.
- Vendor/feed labels are snapshots at creation; edits to the purchase later do not flow in.
- cancelled = invalid strip or rejected review, never "feed failed".
- Admin-web has no toxin screen other than the Verify toxin tab; feed purchases page does not show toxin status.
