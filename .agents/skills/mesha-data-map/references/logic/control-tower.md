# Logic card: HOME ("/") and CONTROL TOWER (/?lens=control-tower)

Index: R1 What "/" shows · C1 Process intact tile · C2 Critical tile · C3 Open gaps (at risk) tile · C4 Evidence (verification backlog) tile · C5 Config/SOP blocker band · C6 Critical alert band + chips · C7 Open gaps table + drawer

## R1 Home route "/" (root-route)
- `apps/admin-web/app/(admin)/page.tsx:62-77`. "/" shows NO numbers of its own. It redirects:
  1. no `?lens=control-tower` and the caller holds page `weighing-analytics` -> `/weighing/analytics` (ADG Analytics) with its default window (`landingHref` :34-60) -> see `adg-analytics.md` (§0 page scope, §1 General tab ... §7).
  2. `?lens=control-tower` and the caller holds page `control-tower` -> Control Tower (below). It is hidden from the nav (`permissions/capability_pages.go:78`, HiddenFromNav).
  3. no landing page but the caller holds `control-tower` -> redirected to `/?lens=control-tower` (:71-73).
  4. otherwise first enabled published nav page, `/verify` preferred, else `/vaccination`.
- So "what is on the home page" for most users = ADG Analytics. Control Tower is vaccination-only; it shows no weighing, feed, sales or herd numbers.

## Shared: endpoint and grain
- UI `apps/admin-web/features/control-tower/index.tsx`; API `GET /control-tower/vaccination` (`lib/api/server.ts:3512`) -> `processintegrity/adapters/http/handler.go:63,140` -> `processintegrity/app/service.go:130` ControlTower.
- Query: park from top-bar scope (backend enforces park for park-bound users, handler.go ~:200), `as_of` default now, horizon `due_before = as_of + 30 days`, category vaccination (handler.go:233), `OnlyBrokenOrAtRisk = true`, completed excluded, limit 10 (page size 10/25/50/100).
- Rows = the Action Center pen/batch/dose grain (`work-board.md` §2), filtered to `work_state IN (rejected, blocked, missed, overdue, proof_pending, verification_pending)` (`processintegrity/adapters/postgres/repository.go:1626-1629`).
- Summary counts (`service.go:143-172`): taken from the same filtered rows, or re-counted without the `ct_state`/`ct_severity` filters (`CountByWorkState`) when those filters are set, so the tiles never shrink with the chips.
- **Go-only**: the row set is ~1,300 lines of SQL plus drive/operator logic; not reproducible as one query. Obligation-level floor below.

## C1 Process intact (KPI 1, index.tsx:187-193)
- `summary.process_intact = open_gap_count == 0`. Label "intact" / "not intact" (critical > 0) / "at risk" (only warnings).
- CEO: "Is the vaccination process intact?" — "Vaccination ka process theek chal raha hai?"

## C2 Critical (KPI 2, :194)
- `critical_count` = rows with work_state `rejected` + `blocked`.
- Traps: `blocked` = pen not usable for vaccination (config/SOP gap), not a field failure. `missed` is NOT critical here although its severity is broken.

## C3 Open gaps / at risk (KPI 3, :195)
- `warning_count` = rows `overdue` + `missed` + `proof_pending` + `verification_pending`. `open_gap_count` = critical + warning (not shown as a tile).
- Trap: the tile is labelled "open gaps" but shows warning_count only.

## C4 Evidence / verification backlog (KPI 4, :196-202)
- `verification_backlog` = rows `verification_pending` (pen grain, SOP submitted not yet verified). Not the per-dose queue on Action Center (`work-board.md` §7), which counts `vaccination_completions.status='recorded'`.

## C5 Config/SOP blocker band (:246-265)
- Shown when `config_or_sop_blockers` (= `blocked` rows) > 0; links to `/vaccination/plan`.

## C6 Critical alert band + chips (:219-321)
- Band = first 5 rows of the page, sorted by severity chip order (broken first). Tag on the band header = `critical_count · warning_count`.
- Severity chips (`ct_severity`): severity from `repository.go:1458-1471` (verification_pending -> watch; proof_pending/overdue -> at_risk; rejected/blocked/missed -> broken). State chips (`ct_state`) -> `work_state = $6`.
- Row text: title/detail/next_action built in Go (`service.go` alertTitle/alertDetail); capacity line `driveCapacityLabel` (:53-67): over_cap_required "N animals over operators×cap slots", medical_defer, terminal_animal_closed.
- Owner = operator name else park head name else "unassigned" (:49-51).

## C7 Open gaps table + drawer (:330-440)
- Columns gap (work_state) / severity / detail / owner / next_action (contract `adminui/app/service.go:479`); `start–end of total_count`, keyset cursor paging.
- Drawer (`control-tower-local-drawer.tsx:98-125`): gap, severity, scope_label, detail, evidence/proof summary, next_action; links to Action Center (`ac_row`), `/workflows/{row_id}`, Protocol Adherence, Vaccination.

## Verified (STG, 25/09/2026 00:15 IST, obligation-level floor; screen rows are Go-only)
```sql
SELECT oi.status, count(*),
  count(*) FILTER (WHERE oi.due_at < now()) past_due,
  count(*) FILTER (WHERE oi.due_at <= now() + interval '30 days') in_horizon
FROM obligation_instances oi
JOIN protocol_versions pv USING (tenant_id, protocol_version_id)
JOIN protocol_definitions pd ON pd.protocol_id=pv.protocol_id AND pd.category='vaccination'
WHERE oi.target_type='goat' AND oi.status IN ('scheduled','due','in_progress','deferred','missed','waived')
GROUP BY 1;
-- deferred 32 (0 in horizon) · scheduled 4,456 (119 in 30-day horizon, 3 past due) · missed 0
-- vaccination_completions status='recorded' = 0
```
Reading: no missed obligations and no recorded doses awaiting review. The 3 past-due rows are the CPT 24/09 drive (3 animals, IST due day 24/09, still `scheduled` with no completion although its SOP task is `accepted`), so at obligation level they are overdue as of 25/09; whether the screen shows them as overdue or verification_pending is decided in Go (submission-aware). Any Critical can only come from `blocked` (pen unusable) or `rejected` pen grains, which need the Go path.

## Traps
- Vaccination only. Feed-direction exceptions (`count_projection_exceptions`, 72 open on STG) are the same endpoint family but only when `category=feed_direction`; Control Tower never asks for it.
- Horizon is 30 days from as_of; a gap due later is invisible.
- `ceo_ai.action_center_current` is obligation grain with no horizon; do not use it to reproduce these tiles.

## CEO questions this card answers
- What does the home page show? — "Home page pe kya dikhta hai?" (ADG Analytics; Control Tower only via its link)
- Is vaccination on track across parks? — "Sab park mein vaccination sahi chal raha hai?"
- How many critical vaccination gaps are there? — "Vaccination mein kitne critical gap hain?"
- What is waiting for verification in vaccination? — "Vaccination ka kitna kaam verification ke liye ruka hai?"
- Which pen is blocked by config or SOP? — "Kaunsa pen config ya SOP ki wajah se ruka hai?"
- Who owns each open gap and what is the next step? — "Har gap ka owner kaun hai, agla kadam kya hai?"
