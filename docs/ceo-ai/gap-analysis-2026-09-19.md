# CEO AI chat ("Ask Mesha") — what it answers today vs. what the app shows

Audited against clean `origin/main` @ `f90e43c72` (2026-09-19). Three parallel research
agents (+7 sub-agents) swept `backend/internal/**`, every `apps/admin-web` route,
`docs/ceo-ai/**`, the coverage guards and `pages/`.

## 1. Recent ceo-ai commits (what just landed)

| SHA | Change |
|---|---|
| `b4bfee0e2` | Live answers from STG reads: `natural_sql.go` deterministic pre-planner, `sales_overview` API reader, SQL fallback via `MESHA_MCP_DB_DSN`, Gemini 3.8 Flash |
| `64b3e0efc` / `f4c5f422f` / `dff2bc546` | Operational counts: weighing / mortality / farm-born routed to SQL before generic census; `origin_type` on `animal_current_scope`; kid/adult/first-week/cause columns on `mortality_base` (migration 000358); human sentences |
| `b6d7e9942` / `31e3e30bf` | `feed_weight_band_summary` API reader (Growth Director feed-by-weight-band) |
| `957e8ffee` / `afca995a9` | Grain proofs for `mortality_base` |
| `8c42a6e6f` | Coverage-matrix row for Farm born sales |

## 2. What the chat can answer TODAY (live in STG)

**Only 9 deterministic SQL intents + 8 wired API readers actually work.** Everything
routed to Cube or MCP Toolbox (~40 % of the planner catalog: operator load/overdue/capacity,
dose pickup, pre-arrival, shifting, SOP status, notifications, inventory, shed capacity,
feed direction) dead-ends because `MESHA_CUBE_URL` and `MESHA_MCP_TOOLBOX_URL` are not
set in `infra/envs/stg/cloud_run_services.tf:177-230`.

| Works now | Source | Chart | Time filter |
|---|---|---|---|
| Sales: sold count, revenue, goats/sheep, top buyers | `sales_overview` API (`GET /sales/overview`) | bar from monthly rows | farm CBE/CPT; "this month" only |
| Active animals by species / breed / pen / park; farm-born; kid/adult | SQL `animal_current_scope` | bar | none (needs a park in text or memory, else falls to Vertex) |
| Mortality: deaths, kid/adult, cause established, first-week, rate, by month | SQL `mortality_base` | bar/line | today / month / all-time |
| Weighing: animals weighed (lump/individual), avg weight by pen, lowest pens | SQL `weighing_capture_activity` | bar | **none — all campaigns ever** |
| Feed by weight band summary | `feed_weight_band_summary` API | none | from/to/sex/origin never parsed from text |
| Feed variance (blocked / under-fed pens) | SQL `feed_adherence` | bar | as-of day only |
| Source-entry health issues per load | trusted SQL | none | none |
| Ops risk / action center / exceptions | SQL `action_center_current`, `operations_kernel_health` API | none | none |
| Vaccination operator overload | SQL `vaccination_operator_status` | bar | none |
| Vaccination due/overdue/missed (by shed) | fallback alias → `vaccination_shed_summary` API | bar | as-of, 45-day window |
| Counts breakdown by park/shed/breed/sex/stage | `counts_breakdown` API | bar | none |
| Procurement open loads, roster coverage, verification queue, audit summary | API readers | none | none; **no park scoping** |

Frontend limits (`apps/admin-web/features/ceo-ai/`): charts are `bar` or `line` only,
single series, no y-axis/legend, no max-point thinning; charts are **lost when a thread is
resumed**; no markdown rendering; no copy/feedback/trace link; starters are 12 hard-coded
questions in `backend/internal/ceoai/adapters/http/conversations.go:62-75`.

## 3. Gap matrix — every page a human can see vs. the chat

Legend: ✅ answerable now · ⚠️ partial (wrong grain/time/scope) · ❌ not reachable.

### Sales (8 routes, every one has KPIs + charts)
| Page view | Chat |
|---|---|
| `/sales/sold` revenue, animals, ₹/kg, manure; **revenue/animals/manure by month**; ₹/kg by breed; buyers table; deals ledger | ⚠️ current month + top buyers only. No month-by-month series, no breed price bands, no deals ledger, no weight-band split (data IS in `GET /sales/overview`: `monthly[]`, `price_bands[]`, `sold_weight_bands`) |
| `/sales/farm-value` total farm value, meat kg, over-35 kg, by category bucket | ❌ (`farm_valuation` already in overview payload) |
| `/sales/loads` purchased/sold/mortality/remaining per load, purchase vs sold value, P&L, avg weight in/out, ₹/kg landing vs sale, fattening days | ❌ (`GET /procurement/loadwise-sales`) |
| `/sales/farm-born` on-farm, sold, earned, avg price; by breed/sex/stage/pen | ⚠️ headcount only; no revenue/sold breakdown (`GET /procurement/farm-born-sales`) |
| `/sales/buyer-analytics` buyers, repeat %, repeat revenue, outstanding, per-buyer | ❌ (`GET /procurement/buyer-analytics`) |
| `/sales/market-analytics` prices by city × question, trend over 30/90/180/365 d | ❌ (`GET /market/analytics`) |
| `/sales/vendors`, `/sales/config` | excluded (registers/writes) — correct |

### Weighing
| Page view | Chat |
|---|---|
| `/weighing/weights` & `/weighing/analytics` headline ADG, total/avg weight, >30/>35 kg, ADG by breed/sex/stage/pen/load, farm-born vs purchased, elevated vs ground, weight bands, weekly growth, gain thresholds, kids losing weight, load purchased vs latest | ❌ **entire ADG/growth surface** (`GET /weighing/leadership/growth`, `/weighing/weight-demographics`, `/growth-director/weights`). Only raw capture counts + avg-by-pen work, and with no period filter |
| Growth Director road-to-sale bands, fair fight, slow growth | ❌ |

### Counts
| Page view | Chat |
|---|---|
| `/counts/breakdown` totals, K0–K4/fattening/bucks/ICU, by pen/breed/stage×sex, purchased loads | ✅ mostly (stage buckets not in composer vocabulary) |
| `/counts/analytics` births, deaths, sold, net change **by month**; breed/tag/age/sex mix | ⚠️ deaths only; no births/sold/net-change series (`GET /counts/herd-analytics`) |
| `/counts/mortality` rate by stage/breed/load/pen/sex/species; by cause/season/age; cross-tabs | ⚠️ totals + by-month; no by-cause / by-load / by-pen / season |
| `/counts/herd` register KPIs | ✅ |
| `/counts/milk-preparation` | ❌ (operational; low priority) |

### Feed
| Page view | Chat |
|---|---|
| `/feed/analytics` Consumption: directed kg/day, animals fed, g/head, execution verified %, ₹/animal/day; spend 7d/month/3m/year; daily expenditure; spend share by item; feed mix | ❌ (`GET /feed-analytics/directed`, `/stock`) |
| Stock: days left per farm×item, low stock, 7-day forecast & cost | ❌ (`/feed-analytics/stock` — Toolbox `inventory_stock` is dead) |
| Execution: packing/distribution verified %, packed vs directed variance | ❌ (`/feed-analytics/execution`) |
| Feed by pen directed vs verified | ❌ |
| `/procurement/feed-purchases` kg bought, ₹ spent, per-kg, payment pending | ❌ |

### Health / Vaccination
| Page view | Chat |
|---|---|
| `/health/analytics` open cases, new cases, recovery %, deaths attributed, cases by disease, deaths by month, treatment adherence, medicines given, diagnosis engine | ❌ **entire health surface** (`GET /health/analytics`; coverage-matrix says "open decision", not excluded) |
| `/vaccination` command KPIs: animals, missed, verified, awaiting, rework, overdue, scheduled ahead | ⚠️ due/overdue by shed via alias only; no verified/awaiting/rework/missed counts (`GET /vaccination/command`) |
| Pen×vaccine matrix, cohort matrix, coverage % per protocol | ❌ (`/vaccination/command/*`, `/app/vaccination/coverage`) |
| `/vaccination/live-tracker` today's drive progress, operator idle | ❌ (`GET /vaccination/live-tracker`) |
| `/protocol-adherence` adherence %, open gaps | ❌ Cube dead; alias returns shed summary, not a % |
| `/control-tower`, `/action-center` | ✅ |

### Procurement / ops / people
| Page view | Chat |
|---|---|
| `/procurement/source-entry` loads by status | ✅ but no park filter |
| `/procurement/animal-purchases` awaiting/accepted/rejected | ❌ (`GET /procurement/animal-purchases/review`) |
| `/tasks` leadership tasks open/overdue by assignee | ❌ (matrix marks Leadership Tasks EXCLUDED as "private correspondence" — worth revisiting for the CEO's *own* tasks) |
| `/work-board` lanes, `/routines` due/delayed/done | ❌ |
| `/verify` backlog, oldest unreviewed, reject rate, verifier activity | ⚠️ queue items only; no `oversight-analytics` KPIs |
| `/alerts` today's alerts/critical | ❌ (`GET /alerts/rows`) |
| `/people` clocked-in/out/flagged, `/leave` waiting | ❌ (HR — matrix excludes attendance ledger; leadership-level counts arguable) |
| `/operations/audit`, `/dlq` | ✅ summary only |
| `/herd-signals` BLE tags moving/quiet/weak/battery | ❌ (matrix EXCLUDED) |

## 4. Recommendation — what to add, in order

Each item = one API-tier reader (existing service, no new SQL) + planner rule + composer
sentence + coverage row + golden Q. The recipe is fixed (see §5). Grouped by value to a CEO
asking "any question about weighing, sales, procurement".

**Tier A — biggest gaps, existing single-call endpoints (do first)**
1. **Growth/ADG reader** → `weighingService.GetLeadershipGrowthADG` + `GetWeightDemographics` (sections param). Unlocks headline ADG, ADG by breed/sex/pen/load, weekly trend (line), >30/>35 kg, losing kids, farm-born vs purchased. Also give the existing weighing SQL a `from/to`.
2. **Sales overview — full payload**: expose `monthly[]` (revenue/animals/manure by month → line chart), `price_bands[]` (₹/kg by breed), `sold_weight_bands`, `farm_valuation`. Same reader, more facts.
3. **Load-wise P&L reader** → `procurement.LoadwiseSales` (per-load purchased/sold/mortality/remaining, purchase vs sold value, profit/loss, ₹/kg in vs out, fattening days).
4. **Health analytics reader** → `health.Analytics` (open/new cases, recovery %, deaths by month, by disease, adherence, medicines).
5. **Feed analytics reader** → `feedanalytics` directed + stock + execution (kg/day, ₹/animal/day, spend windows, days-left per item, verified %).
6. **Herd analytics reader** → `counts.HerdAnalytics` (births/deaths/sold/net change by month).
7. **Mortality breakdowns** → extend the mortality SQL/reader with by-cause, by-load, by-pen, season (data is in `GET /counts/mortality`).

**Tier B — leadership KPIs that are cheap**
8. Vaccination command KPIs (`/vaccination/command` kpis + coverage %) — replaces the lossy Cube→shed-summary alias.
9. Buyer analytics (repeat %, outstanding, per buyer) and market prices (`/market/analytics`).
10. Verification oversight analytics (backlog, oldest, reject rate).
11. Alerts today / critical; animal-purchase review counts; feed purchases spend.
12. Leadership tasks (CEO's own open/overdue) and routines due/delayed — needs a coverage-matrix decision to un-exclude.

**Tier C — platform fixes that multiply everything above**
13. **Time ranges for SQL/API paths**: today only Cube paths parse "last 30 days / this quarter / Aug vs Sep"; add a shared date-range extractor and thread `from/to` into every reader.
14. **Park scoping** on procurement/roster/kernel-health/audit readers (Spec comments admit it's ignored).
15. **Cube + Toolbox in STG** (set the two env vars) OR delete the dead intents so questions don't dead-end.
16. **Chart UX**: multi-series, legend/axes, point thinning >12, persist `chart` on stored messages so resumed threads keep graphs, markdown rendering, "view trace" link.

## 5. How each addition lands (the guard chain)

`backend/internal/ceoai/adapters/readtools/toolexecutors.go` (Spec+Execute+`SetXxxReader`)
→ `backend/internal/bootstrap/ceoai_readers.go` (closure returning `[]Fact{Label,Value,Scope}`)
→ `bootstrap/api.go:1218-1245` wire → `keywordplanner/planner.go` rule (or `natural_sql.go`
regex, above broader patterns) → `app/fallback.go` alias → `app/composer.go` sentence →
`docs/ceo-ai/coverage-matrix.md` row naming tool/Spec/Execute/Set* → golden Q in
`tools/ceo-ai/eval/golden/*.json`. Gates: `go test ./internal/ceoai/... ./internal/bootstrap`,
`make leadership-assistant-coverage-guard assistant-route-closure-guard
aggregate-projection-guard ceo-ai-boundary-guard ceo-ai-eval-selftest`, then `make land-main`.

Open coverage-matrix gaps to reference: G5 mortality rate (now largely closed by 000358),
G6 breeding (scoped refusal), G7 feed adherence, G9 scale certification, G10 thread titles.

## 6. Late additions from the feed/workforce backend sweep

- `GET /feed-analytics/stock` already returns `spend{last_7_days,this_month,three_months,this_year}`, `items[].days_left/low_stock` and a 7-day `forecast[]` with `shortfall_kg`/`required_cost` — one reader covers "how much did we spend on feed this month / which feed runs out first / what do we need to buy this week".
- `GET /admin/workforce/people` rows carry per-person `proof_uploads / approved / rejected / rejection_pct` and `clock_in_today` — a leadership-grade "who is clocked in / whose proofs get rejected most" read that the matrix currently excludes under HR; worth a deliberate decision.
- `GET /verification/oversight-analytics` (backlog, oldest pending, verdicts/day, est. days to clear, reject rate 30d, per-verifier watch integrity) has no params and is a single call — cheapest Tier B item.
- `feeddirection` has a `ListAlerts` read with **no HTTP route registered** (`domain/alerts.go:12`); the `/alerts/rows` module is the right source for "what's off today".
