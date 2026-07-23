# Mesha CEO Bot Analytics Context

Status: target context for the leadership-only Mesha assistant. This file
defines how the finished agent must reason about Mesha data. It does not mean
the ADK/Vertex runtime, persisted memory, MCP Toolbox service, complete
`ceo_ai.*` schema, or safe SQL fallback are implemented yet.

Implemented now: dashboard bubble, server-side leadership gate, safe read-only
API demo routing, and guardrails requiring future assistant coverage updates.

Not implemented yet: Google ADK runtime, production Vertex planner, Cube Core
metric service, persisted memory/session store, MCP Toolbox runtime, complete
reporting schema, SQL fallback executor, retry/fallback orchestration, and
assistant audit/metrics persistence.

This file is the concise source map for the CEO bot. It tells the model and the
tool layer which Mesha data surfaces are safe to use, which domains are
covered, and how common leadership questions should map to Cube metrics,
existing read APIs, MCP tools, or read-only SQL.

## Access Model

The CEO bot is enabled only for CEO/CXO leadership surfaces.

The bot may answer from three tiers, in this order:

1. Governed metrics through Cube when the metric exists.
2. Existing Mesha read APIs and read models.
3. MCP Toolbox business tools over curated reporting views.
4. Read-only Postgres SQL against allowlisted operational tables when no API,
   MCP tool, or governed metric exists yet.

The bot must not run writes, mutations, repair actions, DLQ replays, imports,
status changes, approvals, or proof verdicts. Read-only SQL must always be
tenant-scoped and bounded.

## Backend Stack

Mesha backend uses Go with `pgx`/`pgxpool` and typed SQL/sqlc-style adapters.
It does not use GORM.

For the CEO bot, MCP should expose safe read tools and selected read-only SQL
views. Do not wrap every application API one-by-one. Existing APIs remain normal
Mesha APIs; MCP is useful as a tool protocol for the model and for direct
database read tools where a specific API does not exist.

Cube is the governed metric service. It owns official formulas and queries
Postgres or BigQuery using read-only credentials. Vertex/Gemini chooses when a
question should use Cube, but Cube calculates the official number.

## Tooling Pattern

```text
CEO/CXO chat
  -> Mesha chat route
  -> Vertex AI / Gemini planner
  -> allowlisted tool
  -> Cube, Mesha read API, MCP Toolbox, or read-only SQL
  -> concise answer with source and freshness
```

Gemini chooses the tool and extracts arguments such as shed, park, date, status,
or domain. Mesha executes the tool. Gemini must not directly execute arbitrary
SQL.

For official KPI questions, Gemini should plan a Cube metric call first. Cube is
the route for governed numbers such as active animals, vaccination due/overdue,
vaccination compliance, mortality, feed cost, procurement cost, and operator
completion. If Cube has no metric yet, the assistant can use APIs, MCP tools, or
SQL fallback and mark the answer as non-governed/exploratory.

For direct SQL, the server generates and validates `SELECT`-only SQL, blocks
comments/multiple statements/DML/DDL, injects `tenant_id = $1`, applies a small
limit, and returns totals/aggregates by default.

## Always-On Coverage Rule

The assistant context must cover all leadership-relevant Mesha features, not
only the first dashboard screens. Every current and future module must expose an
assistant read path through one of:

1. Mesha read API.
2. Cube governed metric.
3. MCP Toolbox business tool over a curated reporting view.
4. Governed read-only SQL fallback over approved `ceo_ai.*` views.
5. Explicit exclusion saying why leadership should not see it.

When a developer or coding agent adds a new read API, backend module, reporting
view, dashboard route, mobile workflow, operational table, or domain event, the
same PR must update this context and the MCP/toolbox plan. Silent gaps are not
allowed.

## Safe Domains

### Counts / Herd Census

Primary use:
total animals, animals by park/shed/stage/breed/sex/status, named shed counts,
capacity comparisons, current stock snapshot.

Preferred source:
`GET /herd-register/summary` for exact current summary counts.
`GET /counts/breakdown` for grouped census and filters.

Read-only tables when API is not enough:
`goats`, `locations`, `shifting_events`, `shifting_event_impacts`.

Common questions:

| Question | Source | Expected answer shape |
| --- | --- | --- |
| How many animals do we have now? | `/herd-register/summary` or `/counts/breakdown` | One total count plus scope/freshness. |
| How many animals in Castro 1? | `/counts/breakdown` or SQL join `goats` to `locations` | One total count. Do not list animal rows. |
| Count by shed in Channapatna | `/counts/breakdown` | Table or compact top-N by shed. |
| Count by breed and sex | `/counts/breakdown` | Grouped totals. |
| Which sheds look over capacity? | SQL/read model over counts + shed capacity config if available | Shed, count, capacity, variance. |

Default response rule:
For count questions, return aggregate totals first. Never list individual
animals unless the user explicitly asks for animal IDs or records.

### Vaccination / Preventive Care

Primary use:
due/overdue work, planned sessions, shed workload, completion status, capacity,
proof and verification gaps, adherence, and OPERATOR drive load / capacity /
utilization.

Vaccination has TWO reporting grains — use the right one:
- SHED grain (which sheds are due/overdue, doses to pick): `vaccination_shed_status`
  / `vaccination_dose_pickup` views + `vaccination_due`/`vaccination_overdue` Cube
  metrics.
- OPERATOR grain (operator-based drive model: drives planned by operator animal
  capacity, work assigned at operator grain, shed-level video proof): the
  `ceo_ai.vaccination_operator_status` view + `operator_vaccination_load` /
  `operator_vaccination_overdue` / `operator_vaccination_capacity` /
  `operator_vaccination_utilization` Cube metrics (kpi_vaccination_operator).
  Vaccination is NOT purely shed-driven — an operator dimension exists and
  leadership can ask about it directly.

Preferred source:
`GET /vaccination/execution` for shed execution rows.
`GET /vaccination/operations` for operations cohorts.
`GET /vaccination/schedule` for calendar/month view.
`GET /vaccination/execution/sheds/{shed_id}` for shed drilldown.
`GET /vaccination/action-center` and `GET /vaccination/action-center/counts`
for action-center summaries.
`GET /vaccination/adherence` for adherence.
`GET /control-tower/vaccination` for leadership control-tower state.

Read-only tables when API is not enough:
`vaccination_eligibility_rollups`, `vaccination_completions`,
`vaccination_generation_runs`, `vaccination_capacity_config`,
`vaccination_drive_assignments`, `obligation_instances`,
`obligation_batches`, `sop_tasks`, `sop_submissions`,
`sop_submission_items`, `verification_items`, `workforce_members`,
`locations`, `goats`.

Assistant coverage note:
operator workload/capacity questions are answered from the vaccination
execution, schedule, action-center, or control-tower read APIs first. If SQL
fallback is needed, read generated drive-assignment rows at exact
park/business-date/operator/physical-shed/partition grain and aggregate unique
animals; do not repeatedly query workforce availability per candidate date or
treat assignment rows as dose counts. The `/vaccination/execution` read API
exposes `physicalShed` and `partition` separately so leadership answers can
group `Gandhi 1/2/3` as one physical shed with partition-level detail.
The admin operator schedule is one visible row per
planned-date/operator/park. Its move-date action is not frontend state: it posts
a vaccine-level date override to `/vaccination/schedule/drive-date-overrides`.
The planner/sweeper consumes that override to recalculate the affected vaccine
assignment dates while preserving vaccine spacing, combo, buffer, and
operator-capacity rules. Leadership answers about postponed vaccination drives
should therefore mention the recorded override and the regenerated assignment
rows, not treat the old inline schedule as authoritative after a move.
When a persisted assignment row contains multiple vaccine rules, moving one
vaccine must physically split `vaccination_drive_assignments`: sibling vaccines
remain on the original business date, and the moved vaccine gets its own
assignment membership on the override date. SQL fallback answers must not count
stale mixed rows from the original date after an override has been accepted.
Mixed-vaccine operator rows must also preserve per-vaccine original dates in the
read API. Leadership assistant and read API consumers must use
`vaccineOriginalDates[vaccineCode]` when explaining or replaying a vaccine move;
the visible row date is only the current effective drive date and can belong to
another vaccine already moved into the same operator row.
Date-override verification must cover the full round trip: the original month
shows the vaccine before the override, the original month loses only the moved
vaccine after the override, the target month gains that vaccine, and clearing
the override restores the original month. The admin drawer labels and validation
copy for this flow are backend-contract owned, with frontend fallback copy only
for resilience.
Calendar read API answers for vaccination drives must also use
`vaccination_drive_assignments.planned_date` as the effective drive date when
assignment rows exist. `obligation_batches.planned_date` is only a fallback for
older unsplit batches. Leadership assistant read API and SQL fallback answers
must not report "no drive today" from a stale batch date when operator-capacity
assignment rows have moved the real execution date.
Control Tower, Protocol Adherence, Action Center, Workflows, Calendar, and
Android calendar surfaces must agree on vaccination drive dates and statuses.
The shared source order is `vaccination_drive_assignments.planned_date` for
operator-capacity planned drives, then canonical obligation/batch rows only for
legacy or unassigned work; audit/history tables are evidence trails, not the
live scheduling source. Same India business-day vaccination drive rows are not
overdue during that day: overdue starts only when the effective drive business
date is before today's Asia/Kolkata date. Assistant coverage, read APIs, and UI
presenters must also translate matrix vaccine rule identifiers such as
`et_tt_adult_w2` into human labels such as `ET+TT Adult course 2 weeks`; raw
rule codes are allowed as IDs but not as leadership-facing copy.
For CPT operator timetable answers, the backend can label the center as
`Channapatna` while the position code is `vaccination_operator_*`. Assistant
coverage and frontend read consumers must treat those rows as CPT vaccination
operators with 200-animal daily cap and their seeded week-off days; do not
filter them out just because the display center string is not literal `cpt`.

Common questions:

| Question | Source | Expected answer shape |
| --- | --- | --- |
| What is due today? | `/vaccination/execution` or `/vaccination/schedule` | Due total, overdue total, top sheds. |
| Which sheds are overdue? | `/vaccination/execution` | Shed list sorted by overdue/due count. |
| Why is Gandhi 2 overdue? | Shed drilldown + SOP/proof state | Cause summary: due, done, proof, verification, owner. |
| What is vaccination adherence this week? | `/vaccination/adherence` | Percentage, numerator/denominator, period. |
| Where are proof gaps? | `/vaccination/verification-queue` or `verification_items` | Count by shed/owner/status. |
| Are vaccination operators overloaded tomorrow? | `/vaccination/execution`, `/vaccination/schedule`, or assignment SQL fallback | Operator totals by date and shed/partition, with over-cap markers. |
| Which operators are behind on vaccination? | `operator_vaccination_overdue` (kpi_vaccination_operator) | Operator list sorted by overdue assigned animals. |
| Who is overloaded / operator capacity? | `operator_vaccination_utilization` / `operator_vaccination_capacity` | Operators with utilization > 1.0 (assigned vs daily cap), per planned day. |
| Operator drive assignments today? | `operator_vaccination_load` (filter planned day = today) | Assigned animals per operator × park × shed. |
| How many animals is <operator> assigned? | `operator_vaccination_load` (group by operator_label) | Total assigned animal slots for that operator. |

Default response rule:
Explain operational causes, not only numbers: due, done, stale, proof missing,
verification pending/rejected, or capacity blocked.

### Feed Direction

Primary use:
daily feed direction, packing worklist, ration gaps, blocked feed cells,
session split, feed completion.

Preferred source:
`GET /feed-direction/preview`.
`GET /feed-packing/worklist`.
`GET /feed-config/ration-rates`.
`GET /feed-config/shed-factors`.
`GET /feed-config/session-templates`.
`GET /feed-config/schedule`.
`GET /feed-config/experiment`.

Read-only tables when API is not enough:
`feed_direction_completions`, `feed_direction_issues`,
`feed_direction_issue_rows`, `feed_ration_groups`, `feed_shed_tags`,
`feed_item_catalog`, `feed_ration_rates`, `feed_shed_factors`,
`feed_session_templates`, `feed_session_template_items`,
`feed_schedule_config`, `feed_experiment_config`.

Common questions:

| Question | Source | Expected answer shape |
| --- | --- | --- |
| What feed is needed today? | `/feed-direction/preview` | Totals by feed item/session and blocked count. |
| What is the packing list? | `/feed-packing/worklist` | Shed/session/feed item/kg rows or aggregate. |
| Which ration config is missing? | `/feed-direction/preview` + issue tables | Blocked cells grouped by shed/ration/feed item. |
| What changed in feed config? | `feed_config_write_log` if available | Recent changes by kind/actor/outcome. |

Default response rule:
Blocked feed is not zero. If a feed quantity is blocked/null, say missing
configuration, not `0 kg`.

### Shifting / Movement

Primary use:
authorized movements, pending execution, completed/canceled movement, movement
impact on counts and downstream vaccination.

Preferred source:
`GET /app/counts/shifting-events/pending-execution`.
`GET /app/counts/shifting/destinations` for destination catalog.
Counts projection/read APIs for resulting stock impact.

Read-only tables when API is not enough:
`shifting_events`, `shifting_event_impacts`, `goats`, `locations`,
`obligation_instances`, `vaccination_eligibility_rollups`.

Common questions:

| Question | Source | Expected answer shape |
| --- | --- | --- |
| What shifts are pending? | pending-execution API | Count and top pending shifts by source/destination. |
| Did shifting affect vaccination? | movement tables + vaccination rollups | Movement count, rescoped vaccination work if visible. |
| Which sheds gained animals today? | shifting events + impacts | Destination shed totals for the date. |

Default response rule:
Do not treat approval as completed movement. Report requested, authorized,
completed, and canceled separately.

### Procurement

Primary use:
source-entry loads, expected vs received count, arrival review, source health,
HF vaccination evidence, accepted intake.

Preferred source:
`GET /procurement/source-entry/loads`.
`GET /procurement/source-entry/loads/{load_id}`.

Read-only tables when API is not enough:
`procurement_loads`, `procurement_load_goats`,
`procurement_hf_vaccination_evidence`, `procurement_source_health_checks`,
`procurement_pc_handoffs`, `goats`, `locations`.

Common questions:

| Question | Source | Expected answer shape |
| --- | --- | --- |
| What procurement loads are open? | `/procurement/source-entry/loads` | Open loads by status/source/count. |
| Expected vs received this week? | procurement load tables | Totals and variance by load/source. |
| Which intake is blocked? | load detail + health/evidence tables | Blocked count and reason buckets. |

Default response rule:
Separate expected, received, accepted, rejected, and holding counts.

### Workforce / Action Center

Primary use:
operator ownership, backup assignments, coverage, task queues, overdue work,
verification backlog, audit activity.

Preferred source:
`GET /admin/roster/positions`.
`GET /admin/roster/backup-config`.
`GET /admin/roster/coverage`.
`GET /action-center/obligations`.
`GET /operations/audit`.
`GET /operations/audit/summary`.
`GET /verification/queue`.

Read-only tables when API is not enough:
`workforce_members`, `workforce_positions`, `workforce_roster_assignments`,
`workforce_absences`, `workforce_capabilities`,
`workforce_member_capabilities`, `department_module_grants`, `sop_tasks`,
`verification_items`, `obligation_instances`.

Common questions:

| Question | Source | Expected answer shape |
| --- | --- | --- |
| Who owns overdue vaccination work? | action-center/control-tower + roster | Owner, backup, due/overdue counts. |
| Which staff has no backup? | roster/backup APIs | Staff/position list with missing backup. |
| What is pending verification? | `/verification/queue` | Count by status/category/park/owner. |
| What happened today? | `/operations/audit/summary` | Actions by domain/operator/result. |

Default response rule:
Do not expose sensitive staff details unless directly needed for operational
ownership. Prefer position/name, role, active status, and responsibility.

## SQL Safety Contract

Read-only SQL is allowed only for CEO analytics gaps where an existing API does
not provide the answer. The SQL tool must enforce:

- `SELECT` or `WITH ... SELECT` only.
- One statement only.
- No comments.
- No DML/DDL/maintenance keywords.
- Tenant filter on tenant-scoped tables.
- Bounded `LIMIT`.
- Small result payload.
- Timeout.
- Query logging with user, question, source tables, latency, and row count.

Allowed SQL should prefer aggregates over detail rows. Detail rows are allowed
only when the user explicitly asks for records.

## Source And Freshness Footer

Every answer should include or internally carry:

- source tier: governed metric, Mesha read API, or read-only SQL.
- data source: endpoint/table/tool name.
- freshness: business date or generated timestamp when available.
- scope: company, park, shed, or filtered domain.
- caveat: exploratory/raw if not a governed metric.

## Required Tool Catalog

These are the minimum always-on tools the assistant should know. Future modules
must add a business tool or documented read path in the same change that adds
the feature.

| Tool | Type | Backing source |
| --- | --- | --- |
| `counts_summary` | read API | `/counts/breakdown`, `/herd-register/summary` |
| `vaccination_shed_summary` | read API | `/vaccination/execution`, shed summary/read model |
| `vaccination_action_center` | read API | `/vaccination/action-center`, `/control-tower/vaccination` |
| `vaccination_dose_pickup` | MCP/read API | vaccine names, doses to pick, sheds affected, due/overdue |
| `operator_vaccination_load` / `_overdue` / `_capacity` / `_utilization` | Cube (kpi_vaccination_operator) | operator drive load, overdue, per-day capacity, utilization over `ceo_ai.vaccination_operator_status` (`vaccination_drive_assignments`) |
| `feed_direction_summary` | read API | `/feed-direction/preview`, `/feed-packing/worklist` |
| `shifting_pending_summary` | read API | `/app/counts/shifting-events/pending-execution` |
| `procurement_load_summary` | read API | `/procurement/source-entry/loads` |
| `workforce_coverage_summary` | read API | `/admin/roster/positions`, backup, coverage |
| `inventory_stock_summary` | MCP/read API | stock on hand, reorder gaps, last reconciliation |
| `verification_queue_summary` | read API | `/verification/queue` |
| `action_center_summary` | read API | `/action-center/obligations` |
| `operations_audit_summary` | read API | `/operations/audit/summary` |
| `sql_analytics` | read-only SQL | allowlisted tables above |

## Implementation Notes

Start with specific tools for common CEO questions. Use SQL only as fallback for
cross-domain questions or missing API coverage. When a SQL pattern becomes
frequent or business-critical, promote it into a named Mesha read API or governed
Cube metric.

Leadership assistant read API coverage for vaccination drive date changes:
CEO/CXO "move vaccine date" commands persist a
`vaccination_drive_date_overrides` row keyed by park, vaccine, and original
drive date. The vaccination schedule read model applies that override
immediately: sibling vaccines that remain on the original day stay visible
there, while the moved vaccine appears under the override date so the assistant
and UI do not keep offering the old vaccine/date pair in a loop.
The admin schedule move drawer uses a themed local date picker; weekday labels
must keep stable unique keys because the drawer can be opened without any move
being submitted, and render-only warnings must not surface as operator errors.

---

## Integration status pointer (2026-07-22)

The leadership assistant is WIRED end-to-end and PROVEN on the live local path
(Vertex planner → Cube governed metrics → grounded answers matching a SQL
oracle: active animals 1308, goats 975 / sheep 336, vaccination overdue 168 /
due 836; adversarial refusals; SSE streaming; 2-turn persistence; cache hit;
audit + admin trace with no identity leak). It is NOT deployed to staging/prod.

PENDING (not done): conversation/feedback/starters HTTP routes are unregistered
(only `POST /ceo-ai/ask` + admin trace are); API-tier read executors
(feed/procurement/workforce) are not built; Cube reads `public.*` not `ceo_ai.*`
yet; `mesha-cube-stg` + Cloud Run toolbox + Agent Engine deploy, prod secrets,
and BigQuery/dbt marts + prod-scale certification are future work.

Canonical, detailed status: `docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md`
→ "Integration Status — 2026-07-22".
