# Logic card: HEALTH (Health > Health Analytics, /health/analytics)

Index: open cases · new cases · recovery % · deaths · unattributed deaths % · never-diagnosed deaths · cases by month · deaths attributed/unattributed by month · disease board (new/open/recovered/died/case-fatality %) · problems by breed / pen type / age · treatment adherence (on-time/late/rework/not-done, awaiting verification) · medicines (doses, animals) · diagnosis engine (observations, confirmed %, declined, pending, superseded, median hours) · engine rules not-taken-up % · death list · ICU / sick now (not on screen)

## Screen -> API -> SQL

- Page: `apps/admin-web/features/health/health-analytics.tsx` (tabs overview/problems/diseases/mortality/treatment/engine). Page does NO arithmetic except two KPI ratios (tsx ~L236, ~L250).
- Endpoint: `GET /health/analytics?park_id=&from=&to=` (`backend/internal/health/adapters/http/analytics_handler.go:29,49`).
- SQL: `backend/internal/health/adapters/postgres/analytics.go`, ONE pgx batch (L566-645). Every query binds `$1 tenant, $2 from, $3 to, $4 park_id ('' = all parks)`.
- Window: inclusive IST dates. Default = 1st of month 5 months back .. today, floored at **2026-08-01** (`domain.HealthAnalyticsDefaultWindow`, floor `HealthAnalyticsFloorDate`). Chips 30/90/120 days = today-(N-1)..today; "all" = 2026-08-01..today; max 1150 days. Bad/missing params silently fall back to default.
- Park chip -> `$4` = park uuid (CBE `00000000-0000-4000-8000-000000003001`, CPT `...3002`). Health cases filter on `health_cases.park_id`; deaths on `goats.park_id`; engine on `goats.park_id` of the run's goat.

| Metric | SQL const (analytics.go) | Formula |
|---|---|---|
| Open cases (KPI) | CaseTotals L54 | `health_cases.status IN ('active','continued','referred')`, **ignores window** (as of now); split by `age_band` adult/kid |
| New cases (KPI) | CaseTotals L54 | `start_date BETWEEN from AND to` |
| Recovery % (KPI) | CaseTotals L54 + tsx | `recovered / closed * 100`; closed = `(closed_at AT TIME ZONE IST)::date` in window; recovered = closed in window AND `status='recovered'` |
| Deaths (KPI, monthly chart) | Months L92 | `goats` with `exit_reason='died'`, `exited_at` NOT NULL, `merged_into_goat_id IS NULL`, IST `exited_at::date` in window |
| Attributed death | Months L92 | a `health_death_causes` row for the goat (recorded cause) OR a `health_cases` row `status IN ('closed_dead','held_death_review')` (legacy inference) |
| Unattributed % (KPI) | Go L681 + tsx | `(deaths - attributed)/deaths*100` |
| Never diagnosed | NeverDiagnosed L953 | died in window AND no `health_cases` row ever (subset of unattributed) |
| Disease board | Diseases L162 | cases with `start_date` in window grouped by `COALESCE(register_rule_id, disease_key)`; open = active/continued/referred; recovered = status recovered; died = `closed_dead AND (is_death_cause OR goat has no health_death_causes)`; CFR = died/new_cases (Go L699). Top-N (`HealthAnalyticsDiseaseLimit`) |
| Problems by breed/pen type/age | Problems L524 | new cases in window; breed = `goats.breed`; pen type = `shed_partitions.shed_type` (alias pen first, else own pen by normalized partition label, else 'unclassified'); age band = `start_date - COALESCE(dob, approx_dob)` buckets 0-7/8-30/31-90/91-180/181-365/>1y/unknown |
| Adherence | Adherence L229 | `health_treatment_sessions` JOIN case (park from case), `business_date` in [from, min(to, today IST)], excluding `canceled, canceled_death, held_death_review`. on_time = completed AND IST `completed_at::date <= business_date`; late = completed otherwise; rework = `status='rework'`; not_done = scheduled/due/in_progress; awaiting verification = completed AND `verified_at IS NULL`; on-time % = on_time/sessions (Go L717) |
| Medicines | Medicines L271 | `health_medicine_administrations`, IST `administered_at::date` in window, doses = rows, animals = distinct goat_id, by (medicine_name, route) |
| Engine | Engine L308 | `health_diagnosis_runs` with `business_date` in window: confirmed/declined/proposed(pending)/superseded by `status`; invalid = `NOT valid`; median hours = `percentile_cont(0.5)` of `confirmed_at - observed_at` for confirmed; confirmed % = confirmed/(confirmed+declined) (superseded excluded, Go L750) |
| Engine rules | EngineRules L353 | per `proposal->'problems'` key: proposed runs vs runs that opened a case with that `register_rule_id`; not-taken-up % = (proposed-opened)/proposed |
| Death list | Deaths L415 | same death set, capped (`HealthAnalyticsDeathListLimit`), newest first; tag = active identifier (animal_identifier_1 first); cause = `health_death_causes.cause_key/cause_kind` else inferred closed_dead case names |

Traps
- Open cases is NOT windowed; everything else is. Recovery % denominator is cases CLOSED in window (not new cases).
- Deaths come from `goats`, not health_cases: a death with no case is still a death (unattributed / never diagnosed).
- Medicines chart has no session join; adherence park comes from the CASE, not the session.
- `ceo_ai.mortality_base` reproduces deaths (`sum(deaths)`) and attributed (`sum(cause_established)`) on STG; health_cases/sessions/runs/medicines have **no ceo_ai view** (use public.*).
- STG 24/09/2026: `health_cases`, `health_treatment_sessions`, `health_diagnosis_runs`, `health_medicine_administrations` are EMPTY -> all case/adherence/engine/medicine numbers are 0. Only deaths + 2 recorded causes exist. Say "no cases recorded", not "herd healthy".
- ICU / sick now is NOT on this screen: it is `goats.health_status` (`icu`, `sick`, `under_treatment`, `quarantine`, `recovering`) for `lifecycle_status='alive'`. Pen-level ICU flag `location_operational_attributes.is_icu` (defers vaccination) is empty on STG.

## Verified SQL (STG, read-only, 24/09/2026)

```sql
-- Deaths / attributed / never-diagnosed, window 2026-08-01..today (screen "all")
SELECT l.name park, count(*) deaths,
  count(*) FILTER (WHERE EXISTS (SELECT 1 FROM health_death_causes dc WHERE dc.goat_id=g.goat_id)
     OR EXISTS (SELECT 1 FROM health_cases hc WHERE hc.goat_id=g.goat_id AND hc.status IN ('closed_dead','held_death_review'))) attributed,
  count(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM health_cases hc WHERE hc.goat_id=g.goat_id)) never_diagnosed
FROM goats g LEFT JOIN locations l ON l.location_id=g.park_id
WHERE g.merged_into_goat_id IS NULL AND g.exit_reason='died' AND g.exited_at IS NOT NULL
  AND (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN '2026-08-01' AND (now() AT TIME ZONE 'Asia/Kolkata')::date
GROUP BY ROLLUP(1);
-- => CBE 5 / 2 / 5 (Aug 2, Sep 3) · CPT 1 / 0 / 1 · total 6 / 2 / 6  -> unattributed 66.7%
-- Same via view: SELECT park_label, sum(deaths), sum(cause_established) FROM ceo_ai.mortality_base
--   WHERE event_date BETWEEN '2026-08-01' AND (now() AT TIME ZONE 'Asia/Kolkata')::date GROUP BY ROLLUP(1);  => 5/2, 1/0, 6/2
-- Recorded causes: SELECT cause_key, cause_kind FROM health_death_causes;  => ACIDOSIS (register_rule), fever (disease_key)

-- Open / new / recovery (returns 0 rows-worth on STG: health_cases empty)
SELECT count(*) FILTER (WHERE status IN ('active','continued','referred')) open_now,
       count(*) FILTER (WHERE start_date BETWEEN :from AND :to) new_cases,
       round(100.0*count(*) FILTER (WHERE status='recovered' AND (closed_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN :from AND :to)
         / NULLIF(count(*) FILTER (WHERE (closed_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN :from AND :to),0),1) recovery_pct
FROM health_cases WHERE (:park = '' OR park_id::text = :park);   -- => 0 / 0 / NULL

-- ICU / sick now (not on screen)
SELECT l.location_code, g.health_status, count(*) FROM goats g JOIN locations l ON l.location_id=g.park_id
WHERE g.merged_into_goat_id IS NULL AND g.lifecycle_status='alive' AND g.health_status IN ('icu','sick','under_treatment','quarantine','recovering')
GROUP BY 1,2;   -- => CBE icu 15, CBE sick 1, CPT icu 3
```

## CEO questions this card answers
- How many animals are sick / under treatment right now? — "Abhi kitne janwar bimaar hain / treatment pe hain?"
- How many new health cases this month / last 30 days? — "Is mahine kitne naye case aaye?"
- What is our recovery rate? — "Recovery rate kya hai, kitne theek hue?"
- How many died and what did they die of? — "Kitni maut hui aur kis wajah se?"
- How many deaths had no diagnosis at all? — "Kitne janwar bina diagnosis ke mar gaye?"
- Which disease is most common / most deadly? — "Sabse zyada kaunsi bimaari hai, kisme sabse zyada maut?"
- Are treatments given on time? — "Treatment time pe ho raha hai ya late?"
- Which medicines are used most? — "Sabse zyada kaunsi dawai lag rahi hai?"
- Which breed / pen type / age group gets sick most? — "Kaunsi breed ya umar ke janwar zyada bimaar padte hain?"
- How many animals are in ICU? — "ICU mein kitne janwar hain?"
- Does the vet confirm the engine's diagnosis? — "Engine ka diagnosis kitni baar confirm hota hai?"

## Health > Config (`/health/config`): treatment protocols, diagnosis registers, diagnosis types (verified 24/09/2026, goatos-stg)
- Page `apps/admin-web/features/health/health-config.tsx` (tabs via `hc_tab`: treatment `:101`, diagnosis `:104`, types `:107`); filters age band + "drafts only" + search (`:240-252` region, WorklistFilters). Config, not herd numbers.
- Treatment tab: `GET /health-config/protocols?age_band&q&draft_only&cursor` (`health/adapters/http/config_handler.go:49`) -> catalog SQL `health/adapters/postgres/protocol_authoring.go:77-141`.
  Columns (adminui `service.go:1019`): display_name, age_band, duration_days, step_count, medication_count (`record_type='medication'`), critical_action_count (`record_type='critical_action'`), published_version, draft_state.
  One row per (disease_key, age_band) = published version FULL JOIN draft. Filters -> `age_band=$`, `display_name ILIKE %q%`, `has_draft`. Page size <=50.
- Drawer/detail: `GET /health-config/protocols/{id}` -> `loadProtocolDetail` (`protocol_authoring.go:186`): header + open_case_count (`health_cases.status IN active,continued,held_death_review` on that version) + steps + history (last 20 versions).
- Diagnosis tab: `GET /health-config/registers` (`register_config_handler.go:30`, SQL `register_authoring.go:54`): one row per active `health_diagnosis_types` x published/draft register; question_count / rule_count = `len(document.questions/rules)` in Go (`:285`).
- Types tab: `GET /health-config/diagnosis-types` (`diagnosis_type_handler.go:25`, SQL `diagnosis_type_authoring.go:37,63`): type + route_count + has_published; routes list with live animals per (age_band, stage_code) = alive goats whose `lower(btrim(management_stage))=stage_code` ('*' = whole age band).
- Medicines picker `GET /health-config/medicines` (`medicine_catalog.go:22`) only loads in the editor.
```sql
SELECT v.age_band, v.status, count(*) versions,
  sum((SELECT count(*) FROM health_protocol_steps s WHERE s.health_protocol_version_id=v.health_protocol_version_id)) steps,
  sum((SELECT count(*) FROM health_protocol_steps s WHERE s.health_protocol_version_id=v.health_protocol_version_id AND record_type='medication')) meds,
  sum((SELECT count(*) FROM health_protocol_steps s WHERE s.health_protocol_version_id=v.health_protocol_version_id AND record_type='critical_action')) crit
FROM health_protocol_versions v GROUP BY ROLLUP(1,2);
-- => adult published 27 / 502 steps / 217 meds / 47 critical; adult draft 1; kid published 27 / 510 / 215 / 45; kid draft 1 (56 versions, 1,068 steps)
SELECT t.type_key, v.status, v.register_label, jsonb_array_length(v.document->'questions') q, jsonb_array_length(v.document->'rules') r
FROM health_diagnosis_types t LEFT JOIN health_diagnosis_register_versions v ON v.tenant_id=t.tenant_id AND v.animal_class=t.type_key AND v.status IN ('published','draft')
WHERE t.status='active' ORDER BY t.sort_order;
-- => adult adult-1 34q/34r · kid_milk kid-milk-7 39q/27r · kid_weaning 37q/27r · kid_fattening 36q/27r (all published v1)
```
- Routes (14): adult buck 38, mother 5, non-pregnant 758 (m0/milking/pregnant 0); kid_fattening f2-male 452, f2-female 192, warmup 58; kid_milk k2 10; kid_weaning k3 32.
- Traps: 27 diseases per age band published; open_case_count is 0 everywhere (health_cases empty). Stage `ICU-Kid` (17 alive) has NO diagnosis route. Counts are config, never "how many sick".
- CEO questions: "How many treatment protocols do we have?" - "Kitne bimaariyon ka treatment protocol bana hai?"; "Which protocol has unpublished changes?" - "Kaunse protocol draft mein hain?";
  "How long is the Acidosis treatment?" - "Acidosis ka ilaaj kitne din ka hai?" (2 days).
