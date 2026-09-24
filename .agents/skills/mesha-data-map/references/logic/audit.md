# Audit logic cards

Index: A1 Actions in view | A2 Awaiting verification | A3 Proof coverage % | A4 Flagged anomalies | A5 Operation family tiles | A6 Operator rail | A7 Audit list + filters | A8 ceo_ai.audit_activity_summary

Screen: Admin / Data Ops > Audit Log, route `/operations/audit` (`apps/admin-web/app/(admin)/operations/audit/page.tsx`, UI `features/operations-audit/audit-log.tsx`).
Backend: `backend/internal/operationsaudit` — `GET /operations/audit` and `GET /operations/audit/summary` (`adapters/http/handler.go:37-38`). Source table: `public.audit_log` (one row per recorded action).
Verified read-only on goatos-stg, 24/09/2026; day totals re-run 25/09 after the day closed (IST day 24/09 = 5193 rows; table holds 474,172 rows since 25/07/2026).

Shared rules:
- Default window: no `from`/`to` -> `to = now`, `from = to - 24h` (rolling 24 h, NOT the IST day) (`app/service.go:56-61`). The page passes `from`/`to` only from the URL.
- Time column: `recorded_at` (`repository.go:126-132`). `ceo_ai.audit_activity_summary` uses `created_at`; they differ on ~11% of rows (578 of 5193 on 24/09, none crossing an IST date), so day totals can drift slightly.
- Park top-bar scope is NOT applied: no park predicate exists in `listArgs` (`repository.go:123-215`). Audit counts are tenant-wide.
- Noise: `app.device.heartbeat` = 3055 of 5193 rows on 24/09 (59%). Exclude it for "what did people do" questions.

---
## A1 Actions in view (KPI)
- Endpoint: `GET /operations/audit/summary` with the page filters. Code: `adapters/postgres/repository.go:85-111`.
- Formula: `count(*)` of audit_log rows in window matching every active filter.
- SQL (verified: 5193 for IST 24/09 closed day; rolling 24 h at 24/09 run time 5197):
```sql
SELECT count(*) FROM audit_log
WHERE recorded_at >= '2026-09-24 00:00+05:30' AND recorded_at < '2026-09-25 00:00+05:30';
```
- Questions: "How many actions were logged today?" / "Aaj kitne actions record hue?"

## A2 Awaiting verification (KPI)
- Code: `repository.go:368-370` (awaitingVerificationSQL); tab filter `status=verification_pending` -> `metadata->>'status' = 'verification_pending'` (`repository.go:178-181`).
- Formula: (action ILIKE '%verification%' AND lower(coalesce(status,result)) IN (awaiting, awaiting_verification, pending, proof_pending, verification_pending)) OR lower(coalesce(status,result)) IN (awaiting_verification, verification_pending).
- Traps: this counts audit EVENTS whose metadata says pending at the time of writing, not items still pending now. Actions like `feed.packing.pending_verification` (188 on 24/09; the same 188 recurs for packing completed / quantities_recorded and distribution pending_verification because each of the 188 pen-sessions writes one row per action, distinct resource_ids, not duplicates) do NOT match (no "verification" in metadata status). For real queues use `ceo_ai.verification_queue_status`.
- SQL (verified: 0 on 24/09): replace the FILTER in A1 with the formula above.
- Questions: "How many things are waiting for verification?" (route to verification_queue_status) / "Verify hona kitna baaki hai?"

## A3 Proof coverage %
- Code: `repository.go:95` (proof_events) and `104-106`.
- Formula: `round(100 * proof_events / actions)`, capped 100; proof_events = resource_type='proof' OR action ILIKE '%proof%' OR '%sop%'. 0 when actions = 0.
- Traps: denominator is ALL actions (heartbeats, sign-ins), so the % is near 0 by construction; it is not "share of tasks with proof". Proof-gap tab uses a different predicate (`repository.go:372-377`: proof/sop/vaccination actions or proof_required flag, with no proof_id/proof_ref_id/media_proof_id/proof_url/evidence_id).
- SQL (verified: 1 proof event / 5197 in rolling 24 h -> 0%).
- Questions: "What is our proof coverage?" / "Proof kitne actions pe laga?"

## A4 Flagged anomalies (KPI + "Anomalies only" toggle)
- Code: `repository.go:264-266` (anomalySQL), toggle `repository.go:208-210`.
- Formula: action ILIKE any of fail/reject/rollback/delete/skip/mismatch/rework, OR metadata has key `error` or `anomaly_reason`, OR metadata.anomaly in (true,1,yes), OR lower(result|status) in (failed, rejected, rollback, rework, mismatch, skipped, deleted), OR lower(category) in (stock_mismatch, delete, deleted, skip, silent_skip, rework).
- Traps: keyword match on action text: a legitimate `...delete...` admin action counts as an anomaly. "Rejected" KPI in summary = action ILIKE '%reject%' OR result='rejected' (`repository.go:96`).
- SQL (verified: anomalies 0, rejected 0 on 24/09).
- Questions: "Any failures or rejections today?" / "Aaj koi gadbad/reject hua?"

## A5 Operation family tiles
- Code: web `audit-log.tsx:49-60` (families) and `560-566` (one summary call per family, value = `actions`); backend `repository.go:268-282` (auditDomainSQL).
- Formula: domain = metadata.domain (pc+vaccination -> vaccination) else fallback from action/resource prefix: vaccination., feed., weighing., health./clinical./treatment., milk., procurement./source./goat.created, goat./counts./census. -> counts, sop./protocol./auth./app.device./notification./calendar. -> admin, else other.
- Traps: rows whose metadata.domain is a value with no tile (calendar 690, toxin 12, pen_visits 6, configuration 2 on 24/09) appear under "All" only; the "Other" tile shows just `domain='other'` (2). Admin is dominated by heartbeats.
- SQL (verified 24/09 with the full Go CASE: admin 3393, feed 1073, calendar 690, toxin 12, milk 9, pen_visits 6, other 2, configuration 2, procurement 1; vaccination/weighing/health/counts 0):
```sql
SELECT CASE WHEN metadata->>'domain'='pc' AND metadata->>'module'='vaccination' THEN 'vaccination'
  WHEN coalesce(metadata->>'domain','')<>'' THEN metadata->>'domain'
  WHEN action LIKE 'feed.%' OR resource_type LIKE 'feed_%' THEN 'feed'
  WHEN action LIKE 'sop.%' OR action LIKE 'protocol.%' OR action LIKE 'auth.%' OR action LIKE 'app.device.%'
    OR action LIKE 'notification.%' OR action LIKE 'calendar.%' THEN 'admin'
  ELSE 'other' END d, count(*)            -- full CASE: repository.go:268-282
FROM audit_log WHERE recorded_at >= '2026-09-24 00:00+05:30' AND recorded_at < '2026-09-25 00:00+05:30'
GROUP BY 1 ORDER BY 2 DESC;
```
- Questions: "How much feed activity was logged today?" / "Aaj feed mein kitne actions hue?"

## A6 Operator rail
- Code: `audit-log.tsx:533-547`.
- Formula: groups the CURRENT PAGE of rows (max 100) by actor_id (else actor_type), count desc. Name from metadata.operator_name/actor_name, else short id.
- Traps: counts are per visible page, not the whole window. For real per-person totals group audit_log by actor_id and join `workforce_members.user_id` -> display_name.
- SQL (verified whole day by actor_type 24/09: human 3096, system 690, verifier 652, operator 430, user 325).
- Questions: "Who did the most actions today?" / "Sabse zyada kaam kisne kiya?"

## A7 Audit list + filters
- Endpoint: `GET /operations/audit?limit=100&cursor=` (`repository.go:35-83`), order recorded_at DESC, audit_id DESC; keyset cursor.
- Filters->SQL (`repository.go:123-215`): actor_type, actor_id, action (exact), resource_type, resource_id, scope_type, scope_id; domain -> auditDomainSQL; module -> metadata.module or fallback (`284-304`); category -> metadata.category; result -> coalesce(metadata.result, metadata.status); status -> metadata.status; q -> lower LIKE across action/actor/resource/scope/domain/module/category/result/status/operator_name/target_label; anomalies_only; proof_gaps.
- Top actions 24/09 (verified): app.device.heartbeat 3055, notification.sent 495, auth.sign_in 261, feed.distribution.completed 197, feed.packing.completed 188.
- Questions: "Show what happened to load X" / "Is goat ka audit dikhao" / "Kisne feed packing complete kiya?"

## A8 ceo_ai.audit_activity_summary (agent shortcut)
- Definition (view): per IST `created_at` date x area (coalesce(resource_type, scope_type, 'general')) x actor_label (actor_type, not a name) x action x metadata.result; `count`, `last_activity_at`.
- Traps: IST calendar day on `created_at` vs screen's rolling 24 h on `recorded_at`; area is resource_type, not the screen's family; no person names.
- SQL (verified: 5193 for 24/09 without the heartbeat filter; the created_at vs recorded_at difference nets to 0 for this day):
```sql
SELECT sum(count) FROM ceo_ai.audit_activity_summary WHERE business_date = '2026-09-24'
  AND action_label <> 'app.device.heartbeat';   -- 2138 with heartbeat excluded (verified)
```
- Questions: "What were the main activities yesterday?" / "Kal kya kya hua system mein?"

## A9 Audit row drawer
- Code: `features/operations-audit/audit-log-local-drawer.tsx:100-130`; values are the list row (A7), no extra call: operation label/detail (action + metadata), actor, time (`recorded_at`), target (resource_type/resource_id, linked when a route exists), raw metadata.
- SQL: `SELECT * FROM audit_log WHERE audit_id = :id;`
- Questions: "Who did this and when?" / "Yeh kisne aur kab kiya?"
