Sections: Shared rules | M1 Sheds | M2 Kids | M3 Milk litres required | M4 Citric acid | M5 Per-row sessions & daily litres | M6 Verification states | M7 UHT litres submitted (stock) | M8 Milk feeding (kids fed) | Traps

# Milk Preparation & Milk Feeding: logic card

Screen: admin-web `/counts/milk-preparation` (`apps/admin-web/app/(admin)/counts/milk-preparation/page.tsx` -> `apps/admin-web/features/counts/milk-preparation.tsx`).
Endpoint: `GET /counts/milk-preparation?park_id=&limit=&offset=`. The same handler also serves the app at `GET /app/counts/milk-preparation` (`backend/internal/counts/adapters/http/handler.go:53-54`, handler :280) -> `Repository.GetMilkPreparation` (`counts/adapters/postgres/milk_preparation.go:277`).
Filters -> SQL: the top-bar park wins over the local `mp_park` filter, and either becomes `$2 park_id` (text, '' = all). `mp_limit`/`mp_offset` page ONLY the table, never the KPIs. `$3` = today's IST business date (`biztime.BusinessDayStart(now)`). There is no date picker.

## Shared rules
- **Who counts** (`milk_preparation.go:51-72, 109-113`): `goats` with `lifecycle_status='alive'` and `merged_into_goat_id IS NULL`. The band is `management_stage` if it is K1/K2/K3, else `milk_cohort` (a recovered band for kids moved to ICU/quarantine).
- **K3 window**: a K3 kid counts only while `$3 BETWEEN k3_milk_started_on AND k3_milk_started_on+6` (7 days). A NULL `k3_milk_started_on` means no milk.
- **Volume matrix** (`counts/domain/milk_preparation.go:155-165`):

  | band | ml per session | sessions | ml per day |
  |---|---|---|---|
  | K1 | 200 | S1-S4 | 800 |
  | K2 | 300 | S1-S4 | 1200 |
  | K3 | 200 | S1 & S4 only | 400 |

  K0 colostrum and ICU feeding are excluded.
- **Dates**: preparation_date = today (IST). feeding_date = tomorrow (`milk_preparation.go:368`).

## M1 Milk Preparation > KPI "Sheds"
Code: `summary.shed_count`, computed in the summary CTE (`milk_preparation.go:163-176`). Formula: `COUNT(DISTINCT shed)` over the qualifying kids. A shed shows as blocked when shed_id is empty.
## M2 Milk Preparation > KPI "Kids" (head count)
Code: the summary `head_count` = `k1_heads + k2_heads + k3_heads` (:167-169). Formula: the count of qualifying kids after the K3 window is applied.
## M3 Milk Preparation > KPI "Milk required (L)"
Code: `milk_preparation.go:356-359` (`MilkPreparationDailyMLPerHead`). Formula: `(K1*800 + K2*1200 + K3*400)/1000` L. The display is `litres()` (ml/1000).
## M4 Milk Preparation > KPI "Citric acid (g)"
Code: `domain/milk_preparation.go:242` `MilkPreparationCitricAcidGrams`. Formula: `round(L * 5.5, 1)` g.

```sql
-- M1-M4, live plan for today (IST); add AND g.park_id='<uuid>' for a park
WITH g AS (
 SELECT l.name park, g.shed_id,
        CASE WHEN g.management_stage IN ('K1','K2','K3') THEN g.management_stage ELSE g.milk_cohort END st
 FROM goats g LEFT JOIN locations l ON l.tenant_id=g.tenant_id AND l.location_id=g.park_id
 WHERE g.tenant_id='00000000-0000-4000-8000-000000000001' AND g.merged_into_goat_id IS NULL
   AND g.lifecycle_status='alive' AND (g.management_stage IN ('K1','K2','K3') OR g.milk_cohort IS NOT NULL)
   AND ((CASE WHEN g.management_stage IN ('K1','K2','K3') THEN g.management_stage ELSE g.milk_cohort END)<>'K3'
        OR (g.k3_milk_started_on IS NOT NULL
            AND (now() AT TIME ZONE 'Asia/Kolkata')::date BETWEEN g.k3_milk_started_on AND g.k3_milk_started_on+6)))
SELECT COALESCE(park,'ALL') park, count(DISTINCT shed_id) sheds, count(*) kids,
       count(*) FILTER (WHERE st='K1') k1, count(*) FILTER (WHERE st='K2') k2, count(*) FILTER (WHERE st='K3') k3,
       sum(CASE st WHEN 'K1' THEN 800 WHEN 'K2' THEN 1200 WHEN 'K3' THEN 400 END)/1000.0 litres,
       round(sum(CASE st WHEN 'K1' THEN 800 WHEN 'K2' THEN 1200 WHEN 'K3' THEN 400 END)/1000.0*5.5,1) citric_g
FROM g GROUP BY ROLLUP(park);
```
stg value (24/09/2026), prep for feeding on 25/09:

| scope | sheds | kids | K1 | K2 | K3 | litres | citric acid |
|---|---|---|---|---|---|---|---|
| ALL | 2 | 15 | 0 | 10 | 5 | 14.0 L | 77.0 g |
| Channapatna | 1 | 10 | 0 | 5 | 5 | 8.0 L | 44 g |
| Coimbatore | 1 | 5 | 0 | 5 | 0 | 6.0 L | 33 g |

Q: "How much milk to prepare today?" / "Aaj kitna doodh banana hai?" · "How many kids are on milk?" / "Kitne bachche doodh pe hain?" · "Citric acid kitna daalna hai?" · "Kitne K3 weaning mein hain?"

## M5 Milk Preparation > table rows: Park, Shed/partition, Stage, Heads, S1-S4 L, Daily L, Status
Code: `grouped` CTE (`milk_preparation.go:74-120`), grain (park, shed, band, partition). Per-row maths: `BuildMilkPreparationRow` (`domain/milk_preparation.go:169`). A session cell is `heads*ml_per_session`, or "inactive" (K3 S2/S3). Daily = the sum of the active sessions.
Traps:
- The table is paged (default 10, `has_more`), while the KPIs cover the whole filter.
- A partition splits one shed into several rows.
- Status "Blocked" means the shed is missing (`shed_id=''`).

stg value (24/09/2026):
- Channapatna K2: 5 kids, 1.5 L x4 sessions = 6 L.
- Channapatna K3: 5 kids, 1 L in S1 and 1 L in S4 = 2 L.
- Coimbatore K2: 5 kids = 6 L.

Q: "Shed-wise milk plan?" / "Shed ke hisaab se doodh kitna?"

## M6 Milk Preparation > Verification strip (not submitted / pending / verified / rework farm counts) + row tag
Code: `farm_verification` CTE (`milk_preparation.go:141-151`) and the counts at :172-175. Domain: `counts/domain/milk_preparation_verification.go`. Approve/rework handler: `counts/app/milk_preparation_verification_handler.go`.
Formula:
- There is one `milk_preparation_completions` row per farm (`shed_id IS NULL`) with `preparation_date=$3` and `status<>'retired'`.
- The status is `not_submitted` (no row), `pending_verification`, `completed` (shown as "Verified") or `rework` (the tooltip shows `rework_reason`).
- The strip counts FARMS, not sheds. Every row of a farm shows that farm's status.

```sql
SELECT l.name, c.preparation_date, c.feeding_date, c.status, c.rework_reason, a.answers
FROM milk_preparation_completions c JOIN locations l ON l.location_id=c.park_id
JOIN milk_preparation_proof_attempts a ON a.tenant_id=c.tenant_id AND a.completion_id=c.completion_id AND a.attempt_no=c.current_attempt_no
WHERE c.tenant_id='00000000-0000-4000-8000-000000000001' AND c.shed_id IS NULL AND c.status<>'retired'
  AND c.preparation_date=(now() AT TIME ZONE 'Asia/Kolkata')::date;
```
stg value (24/09/2026): both farms are `pending_verification` (not submitted 0, pending 2, verified 0, rework 0). On 23/09 and 22/09 both farms were `completed`.
Q: "Was milk prep verified today?" / "Aaj milk preparation verify hua?" · "Which farm hasn't submitted milk prep?" / "Kis farm ne milk prep submit nahi kiya?"

## M7 UHT litres submitted (reaches Feed Stock > UHT Milk)
Source: the operator's answers on the current attempt: `uht_milk_quantity_litres`, `citric_acid_grams`, `morning_/evening_milk_collected_litres`. Validation is in `domain/milk_preparation_verification.go:51`.
The view `feed_effective_external_consumption` (migration `000216_uht_stock_from_milk_preparation.sql`) turns these litres into UHT kg (1:1) on **feeding_date**. The view counts pending rows too, i.e. any status except retired. The `feed_external_consumption` ledger (`000185_feed_external_consumption.sql`) is only used for days that have no prep.

```sql
SELECT l.name, x.feed_day, x.quantity_kg AS uht_litres
FROM feed_effective_external_consumption x JOIN locations l ON l.location_id=x.park_id
WHERE x.tenant_id='00000000-0000-4000-8000-000000000001' AND x.feed_day >= date '2026-09-21' ORDER BY 2 DESC, 1;
```
stg value (24/09/2026): Channapatna 6 L/day and Coimbatore 7 L/day on every day from 21/09 to 25/09. The 25/09 row already exists, from today's submission.
Traps:
- Submitted litres are not the same as required litres. At Channapatna the operator enters 6 L of UHT against a requirement of 8 L. Goat milk collected (morning/evening) may cover the rest; on stg it is 0.
- For "UHT used", read `uht_milk_quantity_litres`. For "milk needed", use the formula in M3.
- UHT packets are not stored anywhere; only litres are.

Q: "How much UHT milk was used yesterday?" / "Kal kitna UHT doodh use hua?" · "How many days of UHT milk are left?" / "UHT kitne din chalega?" (see feed-stock.md S1: CBE 4 days (LOW), CPT 7 days)

## M8 Milk Feeding (per session, app + verification queue; NO admin-web screen)
Endpoints (app only):
- `GET /app/counts/milk-feeding/tasks`
- `POST /app/counts/milk-feeding/tasks/{task_id}/submit` (`handler.go:56-57`)

Code: `counts/domain/milk_feeding.go` and `adapters/postgres/milk_feeding.go`. Verification goes through the review queue (module `milk_feeding`).

Formula:
- There is one task per (park, feeding_date, session 1-4). Sessions are due at 08:00, 12:00, 16:00 and 21:00 IST.
- `head_count` is snapshotted on the task.
- The answers are `total_kids_fed`, `attempt_1_not_drinking` >= `attempt_2_not_drinking` >= `udder_milk_not_drinking` >= `ors_not_drinking`, plus watchlist answers.
- Status is `not_submitted`, `pending_verification`, `completed` or `rework`.
- Per-kid ml is not stored. It is derived from the band: K1 200, K2 300, K3 200 ml per session.

```sql
SELECT l.name, t.feeding_date, t.session_no, t.status, t.head_count,
       a.answers->>'total_kids_fed' kids_fed, a.answers->>'attempt_1_not_drinking' refused_a1
FROM milk_feeding_tasks t JOIN locations l ON l.location_id=t.park_id
LEFT JOIN milk_feeding_attempts a ON a.tenant_id=t.tenant_id AND a.task_id=t.task_id AND a.attempt_no=t.current_attempt_no
WHERE t.tenant_id='00000000-0000-4000-8000-000000000001' AND t.feeding_date=(now() AT TIME ZONE 'Asia/Kolkata')::date
ORDER BY 1,3;
```
stg value (24/09/2026):
- Coimbatore: S1-S4 all pending_verification, 5 kids fed each session, 0 refused.
- Channapatna: S1-S3 not_submitted; S4 pending, 5 fed.
- 23/09 Channapatna: all 4 not_submitted.

Q: "Did all kids drink milk today?" / "Aaj sab bachchon ne doodh piya?" · "Which milk feeding sessions are missed?" / "Kaunse milk feeding session miss hue?" · "How many kids refused milk?" / "Kitne bachchon ne doodh nahi piya?"

## Traps (summary)
1. The prep page is always "today, for tomorrow" (IST). It cannot answer questions about past days. For a past day, read `milk_preparation_completions`/answers (M6/M7) instead.
2. K3 counts only within its 7-day window. K3 kids without a `k3_milk_started_on` are excluded, even if they are alive in K3.
3. Paging does not change the KPIs. The verification strip counts farms.
4. Required litres (formula) are not the same as UHT litres used (operator entry). Stock depletion uses the operator's entry.
5. The UHT stock deduction is keyed on feeding_date (tomorrow), and pending submissions count too.
6. Milk feeding has no admin screen. Its per-session statuses live only in `milk_feeding_tasks` and the review queue.
