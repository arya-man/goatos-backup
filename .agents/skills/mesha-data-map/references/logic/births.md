# Logic card: births (kids born, birth approvals, farm-born)
Index: births per month (Herd Analytics tile + chart) · births recorded in app (goat_births) · birth approvals · farm-born herd (alive/sold/dead) · birth placement (K0 pen rule) · counts_movement_daily.births trap

## How a birth is recorded (write path)
- `POST /app/counts/birth-events` (`backend/internal/counts/adapters/http/app_write_handler.go:1142` RecordBirthEvent) -> `CreateBirthApprovalRequest` (`adapters/postgres/approval_repository.go:203`): one `counts_approval_requests` row (request_type='birth', pending) + one `goats` row per kid (origin_type='birth') + `goat_births` (child, mother, litter_size 1-3, count_status='pending'). A pending birth is removed from the herd projection by trigger until approved.
- Approve -> `goat_births.count_status='approved'`; reject -> 'rejected' (`approval_repository.go:861-866`); the child goat row stays.
- Birth placement: the newborn must be placed in a K0 (kid) pen, resolved through the shifting destination catalog (`GET /app/counts/shifting/destinations`; tests `adapters/http/app_birth_placement_test.go`, decision 2026-08-20). Breed list: `GET /app/counts/breeds`.

## Births per month (Counts > Analytics tile "Births" = "Kids born in the window", + Births line in flow chart)
- Q: kitne bachhe hue / kitne kids paida hue / births this month / farm-born this year.
- Screen: `features/counts/herd-analytics.tsx:284` totals.births, series :212; `GET /counts/herd-analytics`.
- SQL: `adapters/postgres/herd_analytics.go:99-110`: goats not merged, `origin_type='birth'`, park filter, month = `COALESCE(dob, entry_date, created_at IST)`, window inclusive. No lifecycle filter (dead/sold farm-born still count), no `goat_births.count_status` check.
- Filters: window (default 2026-08-01..today), park scope -> goats.park_id. No species/sex split on screen.
- Value 24/09/2026 default window: **1** (Sep 1, Aug 0).
```sql
SELECT to_char(COALESCE(g.dob, g.entry_date, (g.created_at AT TIME ZONE 'Asia/Kolkata')::date),'YYYY-MM') m, count(*)
FROM goats g WHERE g.merged_into_goat_id IS NULL AND g.origin_type='birth'
  AND COALESCE(g.dob, g.entry_date, (g.created_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN '2026-08-01' AND '2026-09-24'
GROUP BY 1;   -- 2026-09 1
```
- Traps: month is by DOB, so a kid recorded 15/08 with dob 04/07 lands in July (outside the default window). DOBs are placeholder-heavy (404 origin_type='birth' kids have dob in May 2026, mostly 09/05; 21/07/2024 x77) -> by-month births before Aug 2026 are import artefacts. Code-read risk: a REJECTED or still-pending birth's child goat keeps origin_type='birth' and would be counted (none on STG today).

## Births recorded in the app (goat_births)
- Q: how many births were entered in GoatOS / litters recorded.
- `goat_births` count_status='approved', by IST created_at (record day) or child dob. Value: **2** kids all-time (15/08/2026 dob 04/07; 16/09/2026 dob 16/09), both approved, both alive. Birth approvals: 1 request (approved 16/09).
```sql
SELECT gb.count_status, (gb.created_at AT TIME ZONE 'Asia/Kolkata')::date recorded, g.dob, g.lifecycle_status
FROM goat_births gb JOIN goats g ON g.goat_id = gb.child_goat_id;   -- 2 rows, approved
SELECT status, count(*) FROM counts_approval_requests WHERE request_type='birth' GROUP BY 1;   -- approved 1
```

## Farm-born herd
- Q: farm-born kitne hain / home-bred animals alive.
- `goats.origin_type='birth'`: 602 total = alive 512, sold 88, dead 2. Mortality screen buckets these as "farm_born" in load/vendor series.
- Trap: 405 goats have NULL origin_type (neither birth nor procured), 734 procured. `created_at` for 512 farm-born is the 05/08/2026 bulk import, never a birth date.

## ceo_ai
- `ceo_ai.counts_movement_daily.births`: origin_type='birth' by `COALESCE(entry_date, created_at IST)` per pen -> Aug 2026 = 466, Sep = 1. DISAGREES with the screen (Aug 0): the Aug figure is the bulk import day, not births. Use the screen formula (dob) or goat_births for "born in the app"; Aug 466 = 458 on the 05/08/2026 IST bulk-import day (UTC 04/08) + 8 others: do not report it as births.
