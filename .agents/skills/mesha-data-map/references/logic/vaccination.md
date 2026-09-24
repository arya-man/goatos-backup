Metrics: command-board tiles (Animals/targets, Missed, Verified, Awaiting verification, Rework, Overdue, Scheduled ahead, Closed-no-dose) · cohort matrix pending/submitted/verified · pen×vaccine behind · full schedule month (overdue/due/scheduled/deferred/accepted/proof-pending/rejected/animals) · live tracker (Scheduled, Proofs, Scans, Remaining, Awaiting close, Combo, Attention) · operator load/utilization · doses to pick · capacity + operator config · passport (next due, last accepted, history) · pre-arrival history review · procurement vaccination_pending

# Vaccination logic card (admin-web /vaccination, /vaccination/plan, /vaccination/live-tracker, passports)

Values below are goatos-stg, as of 24/09/2026 IST. Tenant `00000000-0000-4000-8000-000000000001`.

## Shared facts (read first)
- **Obligation = one dose owed by one animal** (`public.obligation_instances`, target_type 'goat'). Stored `status` on stg: canceled 76 051, completed 6 240, scheduled 4 456, deferred 32, superseded 11 (no due/in_progress/missed rows right now).
  "Open" everywhere in the backend = `status IN ('scheduled','due','in_progress','deferred','missed')`. `canceled`/`superseded`/`waived` are dead.
- **Doses given = `public.vaccination_completions`** (status recorded / accepted / rejected; stg: all 6 214 are `accepted`). A dose is:
  verified = `status='accepted'`; awaiting verification = `status='recorded' AND verified_at IS NULL`;
  rework = rejected proof (`verification_items.status='rejected'` for category `vaccination_proof`, or a row in `vaccination_completion_rejections` with no live recorded/accepted completion) — latest verdict wins.
- **In-app vs imported doses:** in-app completions carry `sop_submission_item_id` (1 302 on stg); imported/backfilled history has it NULL (4 912). Every screen counts both as "verified" if accepted. Sept 1–24: 345 accepted = 229 in-app + 116 imported.
- **Live herd filter** (command board only): `goats.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu') AND merged_into_goat_id IS NULL`. ceo_ai vaccination views do NOT apply it.
- **Protocol category:** obligation engine is shared; live tracker + schedule join `protocol_definitions.category='vaccination'`, the command board does not. On stg 100% of obligations are vaccination, so no gap today.
- **Business day = IST.** Drive day = assignment `planned_date` (member) → batch `planned_date` → `due_at` IST date. Drive-date overrides (`vaccination_drive_date_overrides`, 0 active on stg) move it later.
- **Partitions:** `goat_shed_partitions.partition_label` ("1".."4" at Yashoda vs "Part N" in assignments — normalise with `regexp_replace(lower(btrim(x)),'^part[[:space:]]*','')`). ceo_ai shed/pickup views emit a whole-shed row (partition NULL) AND per-partition rows — never sum both.
- Vaccine name: `ceo_ai.vaccine_label_for(dose_code)` / `ceo_ai.vaccine_label_map` (et_tt→ET+TT, ppr, blue_tongue, goat_pox, sheep_pox, fmd, hs).

## 1. Command board tiles — /vaccination (Preventive Care · Vaccination)
Endpoint `GET /vaccination/command?park_id&drive_batch_id&drive_park_id&as_of` (handler `backend/internal/vaccinationexecution/adapters/http/handler.go:175`), SQL `commandBoardKPISQL` `backend/internal/vaccinationexecution/adapters/postgres/commandboard_sql.go:137-262`, called `commandboard.go:182`.
Grain: **distinct animals** (one row per target_id after folding its obligations). No date window — every obligation the live animal has ever had (incl. canceled) is in scope; default = all drives (no batch selected).
Per animal flags: any_missed = missed AND no completion; any_awaiting = recorded-unverified AND not accepted; any_rework = rejected AND nothing recorded/accepted; any_verified = accepted; any_overdue = open AND no completion AND not rework AND IST due date < today; any_scheduled = open AND no completion AND due ≥ today.
Priority chain (disjoint, sums to targets): **missed > awaiting > rework > overdue > verified > scheduled > closed_without_dose**.
| Tile (UI label) | Formula | stg 24/09 |
|---|---|---|
| Animals ("Distinct animals in program") | count(distinct live target_id) | 1 562 |
| Missed | any_missed | 0 |
| Verified | any_verified and none of missed/awaiting/rework/overdue | 1 223 |
| Awaiting verification | any_awaiting and not missed | 0 |
| Rework needed | any_rework, not awaiting/missed | 0 |
| Overdue | any_overdue, not rework/awaiting/missed | 0 |
| Scheduled ahead | any_scheduled and NOT any_verified (etc.) | 198 |
| Closed, no dose | residual (all obligations canceled/superseded, nothing given) | 141 |
Filters → SQL: Park (`park_id`) → `EXISTS locations pl WHERE pl.location_id=oi.scope_id AND pl.parent_location_id=$park`; Drive dropdown (`drive_batch_id`) → `oi.batch_id=$batch`; `as_of` → the "today" cutoff.
Traps: an animal with ≥1 verified dose and future open doses is **Verified**, not Scheduled (1 415 live animals hold open doses, only 198 show as Scheduled). A missed obligation with a recorded proof shows as Awaiting, not Missed. Tiles count animals, not doses.
```sql
-- reproduces the tile row exactly (public.* required; no ceo_ai view has completion/verification state)
WITH comp AS (SELECT obligation_id, bool_or(status='accepted') acc,
                     bool_or(status='recorded' AND verified_at IS NULL) rec
              FROM vaccination_completions GROUP BY 1),
s AS (SELECT oi.target_id, COALESCE(c.acc,false) acc, COALESCE(c.rec,false) rec, c.obligation_id IS NULL nocomp,
             oi.status='missed' missed, oi.status IN ('scheduled','due','in_progress','deferred','missed') open,
             (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < (now() AT TIME ZONE 'Asia/Kolkata')::date past
      FROM obligation_instances oi JOIN goats g ON g.goat_id=oi.target_id
       AND g.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu') AND g.merged_into_goat_id IS NULL
      LEFT JOIN comp c USING (obligation_id)),
a AS (SELECT target_id, bool_or(missed AND nocomp) m, bool_or(rec AND NOT acc) aw, bool_or(acc) v,
             bool_or(open AND nocomp AND past) o, bool_or(open AND nocomp AND NOT past) sch FROM s GROUP BY 1)
SELECT count(*) targets, count(*) FILTER (WHERE m) missed,
       count(*) FILTER (WHERE v AND NOT aw AND NOT o AND NOT m) verified,
       count(*) FILTER (WHERE aw AND NOT m) awaiting, count(*) FILTER (WHERE o AND NOT aw AND NOT m) overdue,
       count(*) FILTER (WHERE sch AND NOT o AND NOT aw AND NOT v AND NOT m) scheduled_ahead,
       count(*) FILTER (WHERE NOT (m OR v OR aw OR o OR sch)) closed_no_dose FROM a;
-- -> 1562 | 0 | 1223 | 0 | 0 | 198 | 141   (rework CTE omitted: 0 rejected verdicts on stg; add it from commandboard_sql.go:165 when rejections exist)
```

## 2. Cohort matrix (stage × sex × vaccine: pending / submitted / verified) and Pen × Vaccine
- `GET /vaccination/command/cohort-matrix` → `commandBoardCohortSQL` `commandboard_sql.go:323`. **Obligation grain.** verified = has_accepted; submitted = recorded-unverified and not accepted; pending = open, no completion, no rework; rework counted separately. Canceled obligations appear in no bucket, so pending+submitted+verified ≤ total.
  stg live herd: verified 5 742, submitted 0, pending 4 488 obligations (1 415 animals).
- `GET /vaccination/command/shed-dose-matrix` → `commandBoardShedDoseSQL` `commandboard_sql.go:595`; per obligation state CASE: verified > awaiting > rework > overdue (open, due < today) > scheduled. Red "Goats not done / behind" = animals with a missed or past-due open dose of that vaccine family (broader than the Missed tile, `commandboard_sql.go:720`). stg: 0 pens behind (no past-due open doses).
- Drill-downs: `/vaccination/command/shed-vaccine-animals`, `/closed-without-dose`, `/cohort-exceptions`, `/cohort-days`, `/drives` (SQL `commandboard_drilldown_sql.go`).

## 3. Full vaccine schedule (month) — /vaccination "Full schedule" section
`GET /vaccination/schedule?year&month&park_id` (`handler.go:189`, `:402`) → `vaccinationScheduleWindowSQL` `backend/internal/vaccinationexecution/adapters/postgres/repository.go:937-1210`.
Membership: goat obligations with status in (scheduled, due, in_progress, deferred, completed, missed, waived), category vaccination, not merged, shed+park active; in month if drive day (member assignment → guessed assignment same batch/shed/partition → batch planned_date → due_at) is in the month OR the accepted dose was administered in the month. Includes sold/dead animals (no lifecycle filter).
eff_status: completed (stored completed) | missed/waived/deferred (as-of status events) | in_progress | overdue (drive day < today) | due (= today) | scheduled. Grouped park × shed × partition × stage × protocol.
Columns: overdue/due/in_progress/scheduled/missed/deferred(waived+deferred) counts for due-in-window rows; accepted_count = completed & completion accepted; proof_pending = latest completion 'recorded'; rejected = 'rejected'; animals = distinct goats.
`as_of` rewinds verification (accepted after as_of shows as recorded) and terminal events.
stg Sept 2026 (as of 24/09): animals 290 · overdue 0 · due today 3 · scheduled 58 · deferred 0 · accepted 345 · proof pending 0 · rejected 0 · rows 406.
Sketch (approximate: skips guessed-assignment fallback, as-of rewind and active-location check):
```sql
WITH c AS (SELECT obligation_id,(array_agg(status ORDER BY (status IN ('recorded','accepted')) DESC, administered_at DESC))[1] st,
                  max(administered_at) FILTER (WHERE status='accepted') acc FROM vaccination_completions GROUP BY 1),
r AS (SELECT oi.target_id, oi.status, c.st, c.acc,
        COALESCE(a.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) xd
      FROM obligation_instances oi JOIN goats g ON g.goat_id=oi.target_id AND g.merged_into_goat_id IS NULL AND g.shed_id IS NOT NULL
      LEFT JOIN obligation_batches ob ON ob.batch_id=oi.batch_id
      LEFT JOIN vaccination_drive_assignment_members m ON m.obligation_id=oi.obligation_id AND m.canceled_at IS NULL
      LEFT JOIN vaccination_drive_assignments a ON a.assignment_id=m.assignment_id AND a.shed_id=g.shed_id
      LEFT JOIN c ON c.obligation_id=oi.obligation_id
      WHERE oi.target_type='goat' AND oi.status IN ('scheduled','due','in_progress','deferred','completed','missed','waived')),
w AS (SELECT *, (xd AT TIME ZONE 'Asia/Kolkata')::date d,
        (xd AT TIME ZONE 'Asia/Kolkata')::date BETWEEN '2026-09-01' AND '2026-09-30' diw,
        (acc AT TIME ZONE 'Asia/Kolkata')::date BETWEEN '2026-09-01' AND '2026-09-30' aiw FROM r)
SELECT count(DISTINCT target_id) animals,
  count(*) FILTER (WHERE diw AND status IN ('scheduled','due') AND d < (now() AT TIME ZONE 'Asia/Kolkata')::date) overdue,
  count(*) FILTER (WHERE diw AND status IN ('scheduled','due') AND d = (now() AT TIME ZONE 'Asia/Kolkata')::date) due_today,
  count(*) FILTER (WHERE diw AND status IN ('scheduled','due') AND d > (now() AT TIME ZONE 'Asia/Kolkata')::date) scheduled,
  count(*) FILTER (WHERE status='completed' AND st='accepted') accepted
FROM w WHERE diw OR aiw;   -- -> 290 | 0 | 3 | 58 | 345
```
Simple ceo_ai cross-check ("doses done this month"): `SELECT count(*) FROM ceo_ai.vaccination_obligations_base WHERE status='completed' AND completed_business_day BETWEEN '2026-09-01' AND '2026-09-24'` → 333 (completion date of the obligation, not dose administered date; includes sold/dead goats).

## 4. Live drive tracker — /vaccination/live-tracker
`GET /vaccination/live-tracker?date&park_id&shed_id&partition_label&operator_id&vaccine&status` (`handler.go:185`; handler `live_tracker_handler.go`) → `liveTrackerScopedCTE` `backend/internal/vaccinationexecution/adapters/postgres/live_tracker_repository.go:77-290`, cells `:340`, tiles folded in Go `liveTrackerKPIs` `:1815`.
Grain: **one obligation = one administration** for ONE drive day. Membership = obligations in `vaccination_drive_assignment_members` of assignments with `planned_date = day` (operator = assignment's) UNION unassigned obligations whose override-aware IST due date = day; status NOT IN (canceled, superseded, waived, missed, deferred); category vaccination.
| Tile | Formula | stg 24/09 |
|---|---|---|
| Scheduled | count(*) administrations (split by park) | 3 (Channapatna, Yashoda part 3, ET+TT) |
| Proof videos received | administrations whose goat has a completed `proof_artifacts` video (task_type vaccination) uploaded that IST day | 0 |
| Scans | administrations whose goat has a `sop_task_scan_captures` row that day | 0 |
| Remaining | Scheduled − closed (obligation `status='completed'`), floor 0 — NOT minus proofs | 3 |
| Awaiting close | proofs − closed | 0 |
| Combo animals | goats with ≥2 vaccine families that day | 0 |
| Attention | idle operators / stuck pens (Go rule, idle threshold `LiveTrackerIdleMinutes`) | not SQL-reproducible |
Filters: park → `g.park_id`; shed/partition (normalised) / operator / vaccine family narrow inside the CTE, so all tiles follow them. Status filter only filters table rows.
```sql
WITH cand AS (
  SELECT m.obligation_id, a.operator_id FROM vaccination_drive_assignments a
  JOIN vaccination_drive_assignment_members m ON m.assignment_id=a.assignment_id AND m.canceled_at IS NULL
  WHERE a.planned_date = DATE '2026-09-24'
  UNION ALL
  SELECT oi.obligation_id, NULL FROM obligation_instances oi
  LEFT JOIN vaccination_drive_assignment_members m ON m.obligation_id=oi.obligation_id AND m.canceled_at IS NULL
  WHERE m.obligation_id IS NULL AND oi.target_type='goat' AND oi.status NOT IN ('canceled','superseded','waived','missed','deferred')
    AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date = DATE '2026-09-24')
SELECT count(*) scheduled, count(*) FILTER (WHERE oi.status='completed') closed,
       count(*) - count(*) FILTER (WHERE oi.status='completed') remaining, count(*) FILTER (WHERE c.operator_id IS NULL) unassigned
FROM cand c JOIN obligation_instances oi USING (obligation_id)
WHERE oi.status NOT IN ('canceled','superseded','waived','missed','deferred');   -- -> 3 | 0 | 3 | 0
```

## 5. Operators, capacity, doses to pick
- Operator load per day: `ceo_ai.vaccination_operator_status` (reproduces the tracker's operator rows at **animal** grain: assigned/due/done/overdue; done = every non-dead obligation completed or has recorded/accepted completion; `daily_capacity` = tenant `max_per_day`; `utilization` = operator-day assigned ÷ capacity — never sum/average it). Admin source: `GET /vaccination/drive-assignments` (`handler.go:191`, `driveAssignmentsSQL` `repository.go:599`).
  stg 24/09: Amit Kumar — Yashoda 3, 2 assigned / 2 due / 0 done, util 0.010; Sagar Mahoor — Yashoda Part 3, 1/1/0, util 0.005. 22/09: Naveen 28 done (Godel 2 P4–6), Pramod 30 done (Godel 2 P1–3).
  Trap: same Yashoda partition appears as "3" and "Part 3" (label mismatch between partitions and assignments).
- Capacity config `GET /vaccination/capacity-config` (`handler.go:195`, `capacityConfigSQL` `repository.go:3247`): `public.vaccination_capacity_config` → max_per_day 200, max_buffer_days 7, max_shots_per_animal_per_drive 3, overflow `split_within_safe_window_last_safe_may_exceed_cap`.
- Operator assignment config `GET /vaccination/operator-assignment/config?park_id` (`handler.go:197`, `repository.go:4066`): `vaccination_operator_assignment_config` → Coimbatore 3 active operators/day (3 selected), Channapatna 1 (1 selected). Shifts: `vaccination_operator_shift_config`.
- Doses to pick: `ceo_ai.vaccination_dose_pickup` (per batch day × shed × partition × vaccine). `doses_to_pick` = `obligation_batches.reserved_quantity` (0 on stg for 24/09 — not an animal count); `animals_due` = open (scheduled/due/in_progress) obligations; overdue uses `window_end` < today. 24/09: Yashoda ET+TT, 3 whole-shed rows + 3 partition-3 rows, 1 animal each → 3 animals, not 6.

## 6. Passport — goat drawer / /goats/{id}
`GET /goats/{goat_id}/passport` (`backend/internal/passport/adapters/http/handler.go:33`, `passport/app/service.go:107`); admin-web proxy `apps/admin-web/app/api/goats/[goat_id]/vaccination-passport/route.ts`.
- Open obligations / next due: `ListOpenObligationsByGoat` `backend/internal/obligation/adapters/postgres/sqlc/query.sql.go:383` — status IN (scheduled, due, in_progress, deferred, missed), due = assignment planned date → batch planned date → due_at; next_due = earliest.
- Last accepted: `vaccination_completions WHERE goat_id=$ AND status='accepted' ORDER BY administered_at DESC LIMIT 1` (`vaccination/adapters/postgres/sqlc/query.sql.go:488`) — includes imported history.
- History: all completions for the goat (limit 200), any status.

## 7. Other vaccination numbers in ceo_ai
- `ceo_ai.vaccination_shed_status` (current, per shed + per partition): due = open scheduled/due/in_progress obligations (excludes deferred/missed), done = completed obligations, all-time, **no live-herd filter, obligation grain**. Whole-shed rows sum: due 4 456, done 6 240, animals 324 (eligibility rollup). Do not quote as "animals due"; use §1 for that.
- Pre-arrival history review `ceo_ai.vaccination_prearrival_history_review` (/procurement/source-entry): accepted 426 claims / 418 animal-rows, rejected 4 claims / 2 animal-rows (all-time; distinct_animals not additive across days).
- `ceo_ai.procurement_pipeline.vaccination_pending` = 0 across loads (407 animals in pipeline).

## CEO questions this card answers
- How many animals are fully vaccinated / pending / overdue? · "Kitne goats ka vaccination baaki hai?" → §1 tiles.
- Any missed doses? Which pens are behind? · "Kaunse pen mein vaccine pending hai?" → §1 Missed, §2 Pen×Vaccine.
- How many doses given this month, in-app vs imported? · "Is mahine kitne dose lage?" → §3 accepted 345 (229 app / 116 imported).
- What's planned for this month / next drive? · "Agla vaccination drive kab hai?" → §3, §5 (next assignments 13/10 onward).
- Today's drive progress, remaining? · "Aaj ka drive kitna hua, kitna bacha?" → §4.
- Who vaccinated how many, utilization vs 200/day cap? · "Operator ne kitne kiye?" → §5.
- How many vaccine doses to pick for today? · "Aaj kitni vaccine uthani hai?" → §5 pickup (reserved_quantity may be 0; count animals_due on one grain).
- Proofs awaiting verification / rejected rework? · "Kitne video verify hona baaki hai?" → §1 Awaiting/Rework (0/0).
- When is goat X next due, last vaccinated? · "Is bakri ka agla teeka kab hai?" → §6.
- Pre-arrival vaccination claims accepted vs rejected? · "Supplier ke vaccination record kitne reject hue?" → §7.

## 8. Shed execution drilldown — /vaccination/execution/sheds/[shedId] (Go-only)
- Page `apps/admin-web/features/vaccination-execution/shed-drilldown.tsx`: header (park, shed, partition, animal stages `:115`), total drive rows tag `:128`, status tiles `:132-140`
  (due, overdue, proof_pending, verification_pending, rejected, deferred, missed, blocked, completed), drive chips `:159`, owners (operator/park head/verifier `:179-194`), blockers `:215`, drive-row table `:230-260`.
- Endpoint `GET /vaccination/execution/sheds/{shed_id}?as_of&partition_label` (`vaccinationexecution/adapters/http/handler.go:187`, `:607`) -> `Service.ShedDrilldown` (`app/service.go:209-278`) which reuses the
  `/vaccination/execution` row SQL (`adapters/postgres/repository.go:1303-2200`) filtered to the shed.
- Go-only steps: (1) row grain = execution cohort (park, shed, batch, rule, protocol, dose) with obligation counts as of `as_of`; (2) `work_state` CASE (`repository.go:1957-1985`), first match wins:
  completed (all completed, no rejected/recorded) > rejected > blocked (pen not usable) > deferred (deferred/health-deferred/quarantine/ICU) > missed > blocked (no operator & open) > rejected (task rework)
  > in_progress (proof/recorded with open) > verification_pending > in_progress > overdue (IST due date < IST as_of date) > due > scheduled;
  (3) Go counts rows per work_state into `summary.*` and `summary.total`; drives deduped by lower(drive_name) else drive_id else work_state.
- Trap: tiles count COHORT ROWS (drive rows), not animals or doses. `proof_pending` is a filter chip but the CASE never emits it, so that tile is always 0. For animal/dose counts use §1/§4.
- CEO questions: "Shed X ka vaccination kahan tak pahuncha?" / "What is blocking vaccination in Godel 1?" -> answer from §4 (dose grain) and explain drilldown tiles are drive rows.
