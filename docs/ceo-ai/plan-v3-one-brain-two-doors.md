# Ask Mesha v3 — one brain, two doors (judged plan)

Baseline `origin/main` @ `f90e43c72`. Supersedes v2 after three independent code-level
reviews (SQL-tier safety, view/page parity + MCP unification, 60-question CEO coverage).
v2's Appendix A (full endpoint → coverage map) still applies; corrections below override it.

## What the judges killed in v2

| v2 said | Verdict | Why (file:line) |
|---|---|---|
| Relax sqlguard to allow JOIN inside `ceo_ai` | **REJECT** | Validator is a tokenizer, binds only the *first* `tenant_id` literal (`sqlguard/executor.go:142-166`); views are not tenant-filtered internally; `mesha_ceo_readonly` has SELECT on all of `public` (baseline `:16455`) — the validator is the **only** barrier. Cross-grain joins also fan out silently. |
| Schema card generated from `information_schema` at boot | **REJECT** | Grain lives in `--` migration comments, not `COMMENT ON`; date/park columns are inconsistent across the 28 views (`event_date`, `feed_day`, `business_date`, `due_at`, none…). Card must be repo-owned. |
| `growth_adg_pairs` view feeds headline ADG | **REJECT** | Headline = animal-weighted mean of per-animal *period* medians + whole-shed spans (`weighing/adapters/postgres/growth.go:625-724`); 339/791 kids are lump-sum pens with no pairs. `AVG(pairs)` reproduces the exact "133 g vs 200 g" bug killed on 2026-08-26. |
| `vaccination_command_kpis` at park × vaccine | **REJECT** | KPI tiles fold **per animal** with precedence missed > awaiting > rework > overdue > verified (`commandboard_sql.go:81-105`); a park×vaccine pre-aggregate double-counts. |
| `feed_stock_position` as a view | **REJECT** | `StockRateOverrides` / `StockFamilyMerge` are Go constants bound as `$3..$8` (`feeddirection/.../analytics.go:1667-1680`). |
| Move `apiReadTools()` into shared executors | **REJECT** | They are HTTP path builders in a *separate Cloud Run binary* with no DB pool; auth is email-allowlist + forwarded bearer (`cmd/mcp/main.go:2096-2258`) vs in-app `domain.Actor`. Share the **spec**, not execution. |
| `ask_goatos` "already proxies" | **FALSE** | `naturalLanguageReadRoute` (`main.go:1822-1882`) regex-routes to typed tools *before* proxying — a third planner. `proxyAskGoatOS` (`:2163-2185`) drops chart/citations/facts. |
| "Keep regex fast paths" | **INSUFFICIENT** | `weighingQuestion` matches bare `weight`, `mortalityQuestion` matches `deaths`, `breedQuestion` matches `breed` (`app/natural_sql.go:12-22`) and short-circuit Vertex — new views would never be reached; the CEO gets a **wrong** number, not "can't". |
| LIMIT ≤ 200 | **REJECT** | `MaxRowLimit=100` (`validator.go:28`), eval `aggregate_first` ≤ 8 record tokens. Keep 100. |
| Multi-series charts | **BLOCKED** | `Fact{Label,Value,Scope}` (`domain/types.go:106`) is single-valued; SQL contract is `label,value,scope`. |
| 40 s watchdog | doesn't exist | real bounds: 15 s SSE keepalive, 25 s step, 20 s × 2 Vertex, 8 s statement timeout. |

---

## D0. Tenant isolation — non-negotiable at every layer (shared system)

This is a multi-tenant product: several companies, each with its own CEO(s). A CEO sees
everything inside their tenant and **nothing** outside it. Every layer enforces it
independently; no layer trusts the one above.

| Layer | Rule | Test (named, CI-required) |
|---|---|---|
| DB role | `mesha_ceo_readonly` has SELECT on `ceo_ai.*` only (**landed: migration 000359**). `mesha_cube_readonly` is the one deliberate `public.*` reader (Cube governed metrics; Cube is not wired in STG) — tracked here as remaining blast radius, to be narrowed when Cube is either wired or deleted (P1b); **revoke** `SELECT ON ALL TABLES IN SCHEMA public` (baseline `:16456`). Blast radius, not CEO restriction. | `TestAssistantRoleCannotReadPublic` |
| Views | every `ceo_ai.*` view exposes `tenant_id` as a column and is never queried without a tenant predicate; new views without `tenant_id` fail `ceo-ai-schema-card-guard` | `TestEveryCeoAiViewHasTenantID` (pg-gated, `information_schema`) |
| sqlguard (model SQL) | single relation; `tenant_id = '<session literal>'` required **structurally, not by literal equality** (P1a, tokenizer-grade, no parser). `Validate` bans every set operator (`UNION`/`INTERSECT`/`EXCEPT`) and the `TABLE`/`VALUES` statement forms as whole tokens, requires exactly one depth-0 `FROM` over one `ceo_ai.<view>` (no `JOIN`, no comma join, a clause keyword must follow the relation/alias), balanced parentheses, exactly one depth-0 `WHERE` with no depth-0 `OR`, and — `checkTenantConjunct` — exactly one `tenant_id` token in the whole statement, sitting in a depth-0 AND conjunct of that WHERE (optionally parenthesised once) of the exact form `[alias.]tenant_id = '<literal>'[::cast]`. `NOT tenant_id = …`, `tenant_id = … IS FALSE`, `(tenant_id = …) = false`, `CASE WHEN tenant_id = …`, `(tenant_id = … OR …)`, `bool_or(tenant_id = …)` in HAVING, and a second `AND tenant_id = '<victim>'` are all rejected by shape before any literal is read. `ExtractAllTenantPredicates` then reads the literal that conjunct binds on the raw statement and the executor requires it to equal the session tenant byte-for-byte (a following literal or operator is rejected too). UUID literals against other columns (park_id, load_id, goat_id, buyer_id…) are legitimate same-tenant filters and pass — single relation + the positive tenant conjunct confine them. The PR #318 judge proved the earlier "every literal equals the session" rule leaked live through negation, parenthesised disjunction and a `UNION ALL TABLE` arm; the shape rule is what actually confines the scan. Parser-grade (P1d) replaces it; it does not gate P1a. | `TestTenantBypassCorpusAllRejected` (static, ≥120 shapes incl. the judge's six), `TestStructurallyScopedOracle` + `FuzzTenantPredicateBypass` (invariant: whatever the guard accepts is structurally scoped per an independent oracle), `TestModelSQLNoForeignRowsEver` (pg-gated: two seeded tenants on real `ceo_ai.*` views, every corpus shape run as tenant A, zero tenant-B rows or an error — never B rows with err=nil), `TestExtractAllTenantPredicates_*` |
| Trusted SQL (server-authored) | Today `ExecuteTrustedReadOnlyForTenant` re-extracts a string literal from the SQL text (`executor.go:177`) and `healthIssueSQL` `fmt.Sprintf`s the tenant in. **New API in P1a**: `ExecuteTrustedReadOnlyForTenant(ctx, tenantID, sql, args ...any)` — tenant is always `$1`, callers pass `args` for everything else; the executor rejects trusted SQL that contains any string literal in a `tenant_id` predicate or fewer than one `tenant_id = $1` occurrence. Old signature deleted; every trusted query (`healthIssueSQL` and any added later) is rewritten to `$1` and gets an A/B fixture test. | `TestTrustedSQLRejectsTenantLiteral`, `TestTrustedSQLRequiresTenantParam`, `TestTrustedSQL_<name>_TenantScoped` per query |
| API/Go readers | `domain.Actor.TenantID` from session only; every reader passes it; readers that call services without a tenant arg are rejected by `catalog_consistency_test` | `TestReadersRequireTenant` |
| Cache | key = tenant \| **user** \| conversation \| IST day \| normalized text — the conversation id is client-supplied and only proven to be the caller's after the cache lookup, so the user must be in the key (review finding on PR 318: same-tenant colleague replaying another user's conversation id got their cached follow-up) | `TestCacheKeyTenantFirst`, `TestCacheCrossTenantMiss`, `TestCacheSameTenantCrossUserMiss` |
| Memory / resume | park/shed/metric memory and conversation resume are scoped by (tenant, user); resuming a conversation id from another tenant → 404 | `TestConversationResumeCrossTenant404`, `TestMemoryRecallTenantScoped` |
| Chart / facts | `Fact.TenantID` (typed field, D4) set from the actor by executor/reader; composer/cache/chart builder reject mixed or foreign `TenantID`; a rejection is an invariant violation upstream and is **observable**: logged at error level, counted on `ceoai_tenant_gate_reject_total{reason=unstamped|foreign|mixed|no_actor_tenant}` and audited as a `refused` request whose trace carries a `tenant_gate` step (`Assistant.tenantGateRefusal`) before the generic refusal; stored `chart jsonb` lives under the tenant-scoped message row | `TestComposerRejectsMixedTenantFacts`, `TestFactTenantSetFromActorNotResult`, `TestAskRefusesForeignTenantFacts` (audit row + counter) |
| MCP | tenant from **verified identity → tenant mapping** (allowlist entry = `{email, tenant_id}`), never from `X-GoatOS-Tenant-ID`. Remove client tenant-header forwarding from **every** MCP upstream path — `proxyAskGoatOS` (`main.go:2144`) **and** the typed-tool API reads (`main.go:~2252`) **and** `get_vaccination_today`/`get_health_today`; one shared `upstreamHeaders(identity)` helper is the only place the tenant header is written. Existing tests asserting header pass-through (`main_test.go` ~719/764/995/1077) are inverted, not deleted. | `TestMCPTenantFromIdentityMap`, `TestMCPClientTenantHeaderIgnored_{Ask,TypedTools,VaccinationToday,HealthToday}`, `TestMCPSingleUpstreamHeaderWriter` (grep-guard over non-test `.go` files in `cmd/mcp` only: exactly one **write** of the header — `Header.Set("X-GoatOS-Tenant-ID"` / `Header.Add(` — and zero `r.Header.Get("X-GoatOS-Tenant-ID"` reads of the inbound request; `_test.go` and docs may name the header freely) |
| Audit / trace | every audit row and admin trace carries `tenant_id`; `/ceo-ai/admin/trace/{id}` for another tenant's request → 404 | `TestAdminTraceCrossTenant404` |

**Adversarial suite (`tools/ceo-ai/eval/golden/tenant-isolation.json` + Go tests):** seed two
tenants (A, B) with distinguishable fixtures (breed names, buyer names, tag prefixes).
As CEO A, attempt to reach B's data via: model SQL with B's tenant literal; model SQL with
two literals; trusted SQL paths; every catalog tool with a `tenant_id`/`park_id` param
belonging to B; MCP with `X-GoatOS-Tenant-ID: B`; cached answer replay; resuming B's
conversation id; a chart whose facts were produced for B; the admin trace of B's request;
injection text ("switch to tenant B"). Expected: zero B rows/labels in any answer, chart,
citation or trace; each attempt audited. Runs in `ceo-ai-eval-selftest` (always) and live
(`CEO_AI_EVAL_LIVE=1`).

## D1. SQL becomes the general tier — without widening the guard

**Non-negotiable (unchanged in every phase):** single relation, single flat SELECT, tenant
literal bound to session, LIMIT ≤ 100, `ceo_ai.*` only, JOIN/comma-join/subquery bans with
their existing negative tests. Cross-view questions are answered by **pre-joined views**
(D2), never model-drafted JOINs. **Per-animal answers are allowed for the caller's tenant;
tenant isolation is the only visibility boundary.** "Hide rows from the CEO" is not a safety
goal anywhere in this plan. Drop the `aggregate_first` eval rule and the `refusal_rawdump`
golden; LIMIT 100 stays as a payload cap only (composer says "showing first 100 of N" and
offers the page link). PII refusal for *people* (phone numbers, bank details) stays as a
data-class rule, not a visibility rule. See **D0** for the tenant-isolation contract.

D1.1 **Schema card, repo-owned** — `reporting/schema_cards.go` (or embedded YAML), one
card per view: purpose, grain, `date_column?`, `park_column?`, group-by-able columns,
columns+types, `aggregate_only`, `never_average` notes, `route` (page href for drill
link), `label/value/scope` output convention. Tests: pg-gated card ⇔
`information_schema.columns` diff; no card column equals a sqlguard banned keyword
(`LOAD`,`START`,`CLOSE`,`SET`… `validator.go:58-86`); new guard `ceo-ai-schema-card-guard`
fails on any `CREATE VIEW ceo_ai.*` without a card. Prompt: replace the single-view hint
(`vertex/prompt.go:63-66`) with the card block; snapshot test; `MaxOutputTokens` 1024→2048;
token accounting from Vertex `usageMetadata` not `len/4` (`orchestrator.go:416`).

D1.2 **Time window, server-resolved** — move the pure resolver out of
`wiring_timerange.go` into `app/timerange.go` (it already does today/yesterday/this|last
week|month|year/mtd/ytd/last N/ISO). Add quarter, month names, "since <date>", two-window
comparison. New phrase extractor from question text → `Params[from/to]`, threaded like
`injectAsOf` into every sub-question (SQL, API, Cube) — but as **server truth**: `injectWindow`
overwrites any `from/to/compare_*` a model-drafted plan seeded when a window resolved, and
strips them when none did, so the guard never validates a model-chosen period
(`TestModelSuppliedWindowIgnored`). A two-window comparison stamps `compare_from/compare_to`
on every sub-question too; the planner drafts one sub-question per window and
`validateModelSQL` accepts a draft bound to EITHER window, the repair prompt offers both,
and the composer renders both (`TestCompareWindowsThreadedAndValidated`). **Concrete guard, not a promise:**
new `sqlguard.ValidateWindow(sql, card, window)` — when a window was resolved and the
card has a `date_column`, the draft must contain `<date_column> >= '<from>'` and
`<date_column> < '<to_exclusive>'` (whole-token match, same tokenizer as `Validate`); when
the card has **no** `date_column` (current-state views) and the question carried a window,
the planner must route to a dated view or answer "as of now" explicitly — the composer
prints `Window: as of <now>` and the reviewer flags a mismatch. Named tests:
`TestValidateWindowRequiredDateLiterals`, `TestValidateWindowCurrentStateViewRefusesPeriod`.
Composer prints the window (this is D4's citation rule; once).

D1.3 **Repair loop** — on sqlguard reject or PG error, re-prompt once with error + card;
second failure → honest partial. Telemetry `ceoai_sql_reject_total{reason}`,
`pg_error_code`; validator reason in admin trace.

D1.4 **Regex pre-planner narrowed, then shadowed** — `natural_sql.go`:
`weighingQuestion` → only capture vocabulary (`weighed`, `lump sum`, `per animal`,
`campaign`), drop bare `weight`/`avg weight`; `mortalityQuestion` excludes `disease|case`;
`breedQuestion` requires a count verb; `feedQuestion` requires `blocked|variance|under-fed`;
`farmBornQuestion` excludes `gain|grow|adg`. Golden Qs W3/W7/W9/H3/F1 must pass **through
Vertex** (`forbid_tools_any_of` in the eval harness). Order unchanged (regex still
short-circuits). `MESHA_AI_SQL_SHADOW=1`: for regex answers also draft/validate/execute
model SQL and log agreement; flip per intent only at ≥ 90 % agreement.

D1.5 **D0 lands first.** P1a starts with the D0 grant revoke, the all-literal tenant
binding in the executor, the `tenant_id`-on-every-view test, and the two-tenant
adversarial suite. Trusted server-authored reads that need `public.*` (`healthIssueSQL`)
move to dedicated `ceo_ai.*` views. The declared-join allowlist / parser-grade validator
(pg_query_go) stays a separate later item (P1d).

Exit: 65 goldens unchanged; ≥ 20 new goldens on existing views ≥ 90 % grounded; eval p50
within one Vertex round-trip of a recorded baseline (record it first — none exists).

---

## D2. Views where the page is SQL, Go readers where it is not

Rule: a view ships only if plain `SELECT … GROUP BY` over it reproduces the page's number
under the page's own filters. Period-scoped medians, `as_of`, tolerances, Go constants →
keep the page's Go reader, mounted in the catalog. Every view: migration header shaped
like 000111/000358, `projection-review:` line, `NULLIF(partition_label,'whole')`,
`partition_label` **and** `operational_location_display` on location-bearing rows,
**five named grain proofs** (OneToMany, PageBoundary, DateShift, ParkScope, StatusBuckets)
in `reporting/reporting_views_test.go` — ~15 views × 5 = ~75 tests; budget it.
Boundary guard (`check-ceo-ai-core-boundary.mjs`) forbids non-ceoai code touching
`ceo_ai.*`, so "materialise from Go" = a reader in `internal/ceoai`, i.e. option G.

### Views (V)
| View | Grain | Must carry | Answers |
|---|---|---|---|
| `load_economics` | load | prior outcomes folded; `price_basis`; NULL cost ⇒ NULL P&L; `park_code` agree-or-bare; `served_rank` (page = newest 60); **+ breed, species, source_label, purchase_date, sold_out_date** | load P&L, ₹/kg in vs out, fattening days, mortality % by load, "did the June Sirohi loads make money" |
| `sales_deal_lines_closed` | deal **line** (UNION legacy no-line deals) | `status='Deal Closed'`, `is_priced_live`, `farm`, month key, buyer_key, payments rollup | revenue/animals/manure by month, ₹/kg by breed, top buyers, outstanding |
| `sales_buyer_summary` | buyer | deals, animals, ₹, first/last sale, outstanding, repeat flag | repeat %, buyer cadence (no subquery needed) |
| `farm_valuation` | valuation bucket | rates from one shared `ceo_ai.farm_valuation_rates` source used by the page SQL too; `verified_only` flagged | farm value, meat kg |
| `weighing_latest_individual_weight` | canonical animal | identity CTE inlined (`identity_scope.go:153-222`), latest-ever accepted individual weigh, band, campaign pen + partition, origin/sex/breed | >30/>35 kg (sale-readiness definition), band counts, avg by pen **with date** |
| `growth_adg_pairs` | consecutive pair | animal_key, both dates/obs ids, g/day, partition, breed/sex/origin/load; card says **"never AVG for a headline"** | losing kids, gain thresholds, by-dimension lists, worst pens |
| `mortality_events_base` | death | date, park, pen, breed, sex, species, age band, load, recorded_cause, inferred_cause, season, days_on_farm, days_since_vaccination | top causes, worst pens, by breed/load/season — everything `/counts/mortality` shows |
| `herd_movement_monthly` | month × park | births, deaths, sold, other exits, moves, net change | herd analytics series in one view |
| `feed_directed_pen_day` | pen × day | heads (only here), directed kg, verified kg **NULL** when unverified, packing/distribution stage cols | kg/day, g/head, directed vs verified, verified % |
| `feed_directed_item_day` | park × item × day | kg, ₹ at latest-reached-load rate (lateral from `analytics.go:416-431`), workflow | spend windows, spend share, feed mix, expenditure/day |
| `feed_conversion_monthly` | pen × month | feed kg, feed ₹, heads, kg gained, FCR, ₹/kg gained | "feed cost per kg gained" without a cross-grain JOIN |
| `feed_purchases_base` | purchase | kg, ₹, per-kg, vendor, delivery, payment balance | feed bought/spent/unpaid |
| `intake_review_base` | candidate × decision | load, vendor, decision, reason, date | rejected at intake and why |
| `health_cases_base` | case | `died_of_this` column (`health/.../analytics.go:174-186`), rule key; card: deaths-by-month is `mortality_events_base` | open/new/recovered by disease/month |
| `health_treatment_sessions_base` | session | bucket col (on_time/late/rework/not_done/awaiting); card: ≤ today IST | adherence |
| `health_medicine_administrations_base` | administration | medicine, route, animal | medicines given |
| `vaccination_animal_status` | live animal | precedence bucket at `now()`, `any_*` booleans, park, shed (no partition — shed-grain) | command KPIs = COUNT by bucket |
| `vaccination_pen_vaccine_status` | pen × vaccine | state, behind/verifying/rework counts | pen × vaccine matrix ("which pens have PPR gaps") |
| `market_prices_daily` | city × question × day | price, unit | market trends |
| `mortality_base`, `animal_current_scope`, `counts_movement_daily` (extend) | — | stage-bucket alias col; verify births/sold exist | stage mix, herd flows |

### Go readers (G) mounted in the catalog (page parity is only possible here)
`weighing_growth` (headline ADG, prev-period delta, weekly gain, shed leaderboard, weight-band
distribution, by park — sections param) · `growth_director` (fair fight, road-to-sale) ·
`feed_stock` (days-left, forecast, low stock) · `vaccination_command` (batch-scoped KPIs,
coverage % per protocol) · `vaccination_live` (today's drive, minute-fresh) ·
`oversight_analytics` (no-param) · `alerts_today` (rules run in Go) · `work_board_summary`.
All get park + from/to params where the service has them (fixes the four readers that
ignore park today).

Decisions needed from Ravi before P2: which "≥ 35 kg" the chat quotes (recommend
sale-readiness = latest-ever individual); `operational_location_display` emitted by the
view via one `ceo_ai` SQL helper (recommend) vs composed in Go.

---

## D3. One catalog **spec**, two executors, one planner

- New `internal/ceoai/catalog` package: `ToolSpec{Name, Description, InputSchema, Route,
  Source GET path, CoverageMarker}` consumed by `app.Registry` (chat, in-process executors)
  and by `cmd/mcp` `tools/list` (HTTP proxy execution, as today). Auth models stay separate.
  `get_vaccination_today` and `get_health_today` (currently outside `apiReadTools()`) join it.
- **Delete** `naturalLanguageReadRoute` / `vaccinationScheduleQuestion` from `cmd/mcp/main.go`;
  `ask_goatos` proxies `/ceo-ai/ask` unconditionally → one planner, one grounding review,
  one trace, one chart. `proxyAskGoatOS` returns `structuredContent{answer, citations[],
  chart{}, facts[], request_id, conversation_id, window}`.
- Typed MCP tools also return `{facts, chart, citations, request_id}` via the composer, so
  Claude and the panel say the same number. Trace shows `door: mcp|web`.
- MCP tenant resolution per **D0**: config moves from `AllowedEmails authallow.EmailSet` +
  `MESHA_MCP_TENANT_ID` global fallback (`main.go:79,106,118`) to
  `MESHA_MCP_ALLOWED_PRINCIPALS='email=tenant_uuid,email=tenant_uuid'`. **Fail closed at
  startup**: if `MESHA_MCP_ALLOWED_EMAILS` or `MESHA_MCP_TENANT_ID` is still set, or any
  principal lacks a tenant, or a tenant is not a UUID, the process exits non-zero with a
  message naming the offending entry — no silent fallback to a global tenant, no
  "allowed but tenantless" state. STG env change (`cloud_run_services.tf`) lands in the
  same PR. Tests: `TestMCPConfigRejectsLegacyAllowlist`, `TestMCPConfigRejectsLegacyTenantID`,
  `TestMCPConfigRejectsPrincipalWithoutTenant`. Tenant-header forwarding removed from every
  upstream path (see D0 MCP row). All of the above are a **blocker** for P1c.
- `ask_goatos_sql` ships in **P3** (Ravi: MCP is for the 4 CEOs, give them the SQL tier).
  It calls the same `/ceo-ai/ask` SQL path with the same single-relation sqlguard, schema
  card, tenant literal, grounding review and audit row — no separate guard in `cmd/mcp`.
  Only the JOIN relaxation (P1d) stays deferred, and that is a tenant-safety item, not a
  CEO-permission item.
- Dead tiers: delete Cube/Toolbox planner rules in STG (SQL + views + readers cover them);
  new test fails when a planner route targets a tier whose `MESHA_*_URL` is absent in
  `infra/envs/stg/cloud_run_services.tf`.
- Guards that must change in the same PR: `check-assistant-route-closure.mjs` reads the
  catalog package instead of grepping `main.go` literals; `catalog_consistency_test.go`
  becomes a compiled registry check instead of source-grep of `SetXxxDataReader`;
  `external MCP:` matrix markers and `external-mcp-integration.md` entries generated from
  the catalog; `TestCatalogConsistency` asserts MCP tool set == chat tool set.
- Eval parity: run the golden set through MCP (`MESHA_EVAL_MCP_URL`) and score
  `answer_equivalent` vs the web run. Parity is a metric, not a claim.

---

## D4. Answer quality — page-parity contract

- **Fact model** (P1a, with D0): `Fact{TenantID, Label, Scope, Value, Values map[string]float64,
  Unit}`. `TenantID` is set by the executor/reader from the session actor (never from the
  SQL result); composer, cache and chart builder reject a fact set containing more than one
  `TenantID` or any fact whose `TenantID` ≠ actor — this is the typed field
  `TestComposerRejectsMixedTenantFacts` enforces. SQL contract `label, scope,
  value[, series_<name>…]`; readers emit `Values` for multi-metric rows.
- **Chart types** matching the pages: `bar` (≤ 24), `grouped_bar` (dimension × ≤ 3 series,
  own unit per series), `stacked_bar`, `line` (≤ 52, even-sampled not truncated — today
  rows 13+ are silently dropped), `kpi` (1–4 tiles with delta chip), `table` (≤ 25 × 8).
  No pie; share = bar + %. No heatmap in v3 (pen × vaccine → table).
- **Derived, grounded**: Δ abs+% when exactly two period labels; share-of-total when asked;
  unit-aware formatting (₹ lakh, kg, g/day); "derived from N facts" note.
- **Window + source line** on every answer: `Window: 1–19 Sep 2026 (IST) · Source:
  ceo_ai.growth_adg_pairs`; grounding review fails without it.
- **Drill link** `Chart.Href` from the card's `route` → "Open in page".
- **Persistence**: `chart jsonb` on messages, `StoredMessage.chart`, `parseChart` on resume.
- **Markdown** safe subset in the panel; **starters** generated from the catalog.
- **Composer vocabulary**: stage labels (K0–K4, fattening, bucks, ICU), species, ₹.
- Gates: goldens carry `chart_type_any_of` + `series_min`; chart-geometry fixtures for
  grouped/stacked/table; visual-regression screenshot per chart type.

---

## D5. Eval + guards
- Golden set: 65 → ≥ 85 Qs / ≥ 60 classes (judge 3's 20 additions incl.
  `weighing-adg-headline-month`, `weighing-adg-by-breed` (forbid `animal_current_scope`),
  `sales-revenue-month-vs-last`, `procurement-load-pnl`, `feed-cost-per-kg-gained`,
  `mortality-top-causes-quarter`, `refusal-pii-operator-deaths`).
  Oracles on `public.*` per `eval.md`.
- Harness: `forbid_tools_any_of`, `chart_type_any_of`, `series_min`, MCP parity run.
- Existing chain unchanged + `ceo-ai-schema-card-guard` + STG tier-reachability test.
  Land only via `make land-main`.

---

## D6. Stays current automatically (proposed, pending Ravi's yes)
- **Page-contract drift guard**: every KPI tile / chart the admin-web contract declares
  (`adminui/app/service.go` page contracts) must map to a schema-card view+column or a
  catalog Go reader; a new tile on a page fails CI until the bot can answer it.
- **Typed exclusions**: `EXCLUDED:config|write|pii|detail` only; a read the page shows
  cannot be excluded.
- **Nightly coverage eval**: one golden Q generated per contract KPI, run on STG, failures
  posted to Slack (same pattern as the dashboard nightly).

## Phases (each = one PR, ≤ 3 builders + 1 judge, screenshots as proof)

| Phase | Scope | Exit |
|---|---|---|
| **P0** | Record eval baseline (p50/p90, grounded %) on STG; decide the two D2 questions | baseline artefact in `outputs/` |
| **P1a** | **D0 tenant isolation** (grant revoke, `ExtractAllTenantPredicates` + fuzz corpus, tenant_id-on-every-view, `Fact.TenantID`, cache/resume/trace tests, two-tenant adversarial suite) → schema cards + prompt + `ValidateWindow` + timerange + repair loop (D1.1–D1.3); `Fact.Values` + SQL contract; eval harness flags (`forbid_tools_any_of`, `chart_type_any_of`) | 65 goldens unchanged; +20 goldens ≥ 90 %; `mesha_ceo_readonly` cannot read `public.*`; p50 within one Vertex round-trip of P0 baseline. **No regex, MCP or guard-rewrite changes in this PR.** |
| **P1b** | Regex narrowing + shadow mode (D1.4); delete dead Cube/Toolbox rules + STG reachability test | hijack goldens (W3/W7/W9/H3/F1) pass **through Vertex**; shadow agreement logged; zero golden regressions |
| **P1c** | MCP: `{email=tenant}` principals config fail-closed + tenant header removed from **all** upstream paths (blocker) → delete `naturalLanguageReadRoute`/`vaccinationScheduleQuestion` → full-answer proxy → catalog spec package → route-closure guard + `catalog_consistency_test` rewrite | **MCP parity gate in this PR**: every existing golden Q that today routes via the MCP regex router answers `answer_equivalent` to the web run at ≥ 90 % *before* merge; MCP tool set == chat tool set |
| **P2** | Views wave 1: `load_economics`, `sales_deal_lines_closed`, `sales_buyer_summary`, `farm_valuation`, `weighing_latest_individual_weight`, `growth_adg_pairs`, `mortality_events_base`, `herd_movement_monthly` + `weighing_growth` reader + D4 chart types/persistence/window line | sales/weighing/counts rows of the 60-Q matrix ✅ |
| **P3** | Views wave 2: feed ×4, health ×3, vaccination ×2, `market_prices_daily`, `intake_review_base` + `feed_stock`, `vaccination_command`, `vaccination_live`, `oversight_analytics`, `alerts_today` readers + park/time params on all readers + MCP typed-tool composer parity + starters | full 60-Q matrix ≥ 90 % ✅, MCP `answer_equivalent` ≥ 90 % |
| **P4** | Matrix decisions: leadership tasks (CEO's own), people clock/proof stats, herd signals summary — EXCLUDED rows or views | matrix rows landed either way |
| **P1d** (parallel, separate owner, nothing waits on it) | parser-grade sqlguard (pg_query_go) → enables declared JOINs later; also closes judge MINOR R3-1 (attribute-notation calls `alias.fn` invisible to the `ident (` detector — harmless today, no query-executing single-composite-arg functions exist) | adversarial SQL test set green |

## Open questions for Ravi
1. "≥ 35 kg" definition the chat quotes (recommend sale-readiness).
2. ~~MCP tenant header~~ → resolved by D0: verified identity → tenant map, multi-tenant, tested (P1c blocker).

Decided 2026-09-19: per-animal answers allowed within the caller's tenant (no
aggregate-only rule); tenant isolation is the only visibility boundary (D0); MCP gets the
SQL tier via `ask_goatos_sql` in P3.

## Landing record — P1a (PR #318)
Three judge rounds (`docs/ceo-ai/reviews/pr318-judge-findings.md`): round 1 found six live
cross-tenant leaks through a literal-equality tenant check (fixed by a structural
positive-conjunct rule + set-operator ban + single-FROM); round 2 found query-executing
functions (`schema_to_xml` etc.) reading through string-literal targets (fixed by a
function allow-list); round 3: no leak, MERGEABLE. Corpus: 170 shapes, pg-gated
`TestModelSQLNoForeignRowsEver` run as `mesha_ceo_readonly`.
