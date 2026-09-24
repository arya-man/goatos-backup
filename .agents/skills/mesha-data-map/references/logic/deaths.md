# Logic card: deaths (recording, approval, Herd Analytics deaths)
Index: deaths in a period (Herd Analytics tile + monthly series) · net herd change (deaths part) · death approvals pending/approved/rejected · death date semantics · death cause recorded · -> rates/causes/breakdowns live in mortality.md

## How a death gets into the data (write path)
- Operator: `POST /app/counts/death-events` (`backend/internal/counts/adapters/http/app_write_handler.go:1364` RecordDeathEvent) -> one `counts_approval_requests` row (request_type='death', status='pending', subject_goat_id) + `death.reported` outbox (`adapters/postgres/approval_repository.go:126`). The cause picked on the death form is written to `health_death_causes` (health module, `internal/health/adapters/postgres/repository.go`).
- Approver: admin-web Approvals (`features/approvals`, `POST /admin-web/counts/approvals/{id}/approve|reject`) or mobile `/app/counts/approvals/...`. Approve (`approval_repository.go:986-1006`) re-checks the animal is not already dead/sold/..., requires death evidence, then ExitGoatInTx sets `goats.lifecycle_status='dead', exit_reason='died', exited_at=<approval time>`. Reject changes nothing on goats.
- So: a PENDING or REJECTED death report is NOT a death anywhere (Herd Analytics, Mortality, ceo_ai). "Reported but not approved" = `counts_approval_requests` request_type='death' AND status='pending'.

## Deaths in a period (Counts > Analytics tile "Deaths" + "Deaths" line in the monthly flow chart)
- Q: kitne mare / maut / deaths this month / deaths Aug vs Sep / how many died in Coimbatore.
- Screen: `app/(admin)/counts/analytics` -> `features/counts/herd-analytics.tsx:292` (totals.deaths), chart series :213; API `GET /counts/herd-analytics` (`lib/api/server.ts:1050`, handler.go:51).
- SQL: `adapters/postgres/herd_analytics.go:111-127` exits CTE: goats not merged, lifecycle_status IN ('dead','sold','culled','transferred','lost'), deaths = `exit_reason='died' OR (exit_reason IS NULL AND lifecycle_status='dead')`, month = IST `COALESCE(exited_at, updated_at)`; tile = Go sum of months (:265). Same predicate as Mortality (pinned by TestMortalityDeathsMatchHerdAnalytics).
- Filters: window `from`/`to` (default 2026-08-01..today, see mortality.md); park via top-bar scope -> `g.park_id`. No species filter (split with `species` yourself).
- Value 24/09/2026 (default window): **6** (Aug 3, Sep 3); CBE 5, CPT 1; goat 3, sheep 3.
```sql
SELECT to_char(COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,(g.updated_at AT TIME ZONE 'Asia/Kolkata')::date),'YYYY-MM') m,
       count(*) deaths, count(*) FILTER (WHERE g.species='goat') goats, count(*) FILTER (WHERE g.species='sheep') sheep
FROM goats g WHERE g.merged_into_goat_id IS NULL
  AND (g.exit_reason='died' OR (g.exit_reason IS NULL AND g.lifecycle_status='dead'))
  AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,(g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN '2026-08-01' AND '2026-09-24'
GROUP BY ROLLUP(1);   -- 2026-08 3, 2026-09 3, total 6
```
- ceo_ai: `ceo_ai.animals_base` exit_reason='died' by `exit_business_day` = 6 (agrees); `ceo_ai.mortality_base` sum(deaths) = 6 (agrees). `ceo_ai.counts_movement_daily.deaths` uses `exit_reason='died'` by IST exited_at only = Aug 3, Sep 3 (agrees today; would miss NULL-reason 'dead' rows and NULL exited_at).
- Traps: exited_at is the APPROVAL time, not the day the animal died/was reported (G-003210 reported 17/08, dead from 19/08). All 6 dead rows have exit_reason='died' and exited_at set today; only 4 have a death approval (G-003420 16/08 and G-003284 17/09 came in without one). Never use `exit_reason IN ('death','dead','mortality')` (goats CHECK only allows 'died').

## Net herd change (tile, deaths component)
- `net_change = births - (deaths + sold + other_exits)` (herd_analytics.go:262/276). other_exits = exit_reason IN ('culled','transferred','lost') or NULL reason with that lifecycle. Movements are NOT in net change.
- Value default window: 1 - (6 + 160 + 0) = **-165** (sold Aug 31, Sep 129). 13 `inactive` goats are in no bucket.

## Death approvals (Approvals screen, type "Death")
- Q: death reports pending / kitne death approval baaki hain / who approved.
- Screen: `features/approvals/approvals-page.tsx`; `GET /admin-web/counts/approvals?status=pending|approved|rejected` (`approval_repository.go:660`). KPIs ("Pending in view", "Birth / death in view") count only the fetched 20-row page, filtered client-side by type/farm: NOT a total.
- Value: death requests all-time: approved 4, pending 0, rejected 0.
```sql
SELECT status, count(*) FROM counts_approval_requests WHERE request_type='death' GROUP BY 1;  -- approved 4
```

## Death cause recorded
- `health_death_causes` (PK goat): 2 rows (fever, ACIDOSIS). Cause coverage and cause breakdown: see mortality.md "Cause established".
