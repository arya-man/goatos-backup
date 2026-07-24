# Scale Anti-Patterns

Status: active guardrail.

> **Current release target: the 5k-to-50k operational-kernel envelope.** The
> accepted ADR `docs/decisions/operational-kernel-5k-50k-scale-envelope.md` is
> the authority for operational-kernel deployment scale and worker topology. It
> makes the 5,000-to-50,000-animal envelope the current release target and
> narrows one-million-animal deployment topology to future scale work and
> regression/research material. The anti-patterns below apply in full at the
> current envelope EXCEPT the five explicitly exempted, plan-tested envelope
> reads defined in the "5k-to-50k envelope: exempted canonical read paths"
> section below (Calendar, process-integrity, vaccination shed, execution, and
> operations): they are cheap compute-on-write correctness discipline, not a
> million-animal-only concern, so they hold whether the target is 50k today or a
> future 1M. Where this document still reasons about "1M" it is
> naming the future scale ceiling the shape must survive, not a present
> deployment requirement.

Goat OS keeps future million-animal scale as a design horizon, not a present
release invariant. New request paths, workers, importers, projectors,
dashboards, and reporting paths must be tenant scoped, indexed, bounded,
resumable, and measurable.

Seed/provisioning drift is a scale anti-pattern too: do not create extra
parallel business roles for the same authority. CEO/CXO full access is a single
`ceo_internal` grant with a `cxo` workforce hint; adding an additional admin
person role creates duplicate authorization branches, duplicated test matrices,
and stale UI labels.

Vaccination seed scope drift is the same class of bug. A CPT-only rehearsal
source must not pull in CBE/Coimbatore because a broad fixture once covered both
parks. Shed partition labels such as `Gandhi 1` or `Godel 1 - Part 3` are not
new buildings; source audits, fixture guards, seeders, read APIs, and frontend
tables must aggregate owner/count totals at physical-shed grain and carry the
partition only as drive-assignment detail.

Operator-role drift is part of the same failure mode. In CPT operator-drive
rehearsals, Amit, Darshan, and Sagar are all manager-tier vaccination operators;
none of them is support-only, backup-only, or park-head-only. Their week-offs
must come from HRMS/timetable seed data and reduce the available operator pool
for that business date before animal-cap splitting runs.

## Sub-500ms serving-read budget

Every operator-facing API, SSR page load, dashboard read, schedule/calendar
view, and drawer/list bootstrap is a hot serving read unless this document names
it as an offline/export/background exception. A hot serving read must stay under
500ms at the enforced percentile budget; seconds-class responses are a
production bug, not an acceptable "slow path." The executable policy lives in
`tools/perf/api-latency-policy.mjs`: default p90 <= 300ms and p95/p99 <= 500ms.

Do not hide a seconds-class read by raising a timeout, increasing a limit,
adding a spinner, doing a frontend-only cache, or saying "the data is small
today." Change the shape:

- use the narrow endpoint that owns the screen's exact grain/window;
- add or fix the natural indexed predicate/keyset cursor;
- batch N+1 reads into one set-based query;
- use the accepted projection/read-model pattern where the ADR requires it; or
- split a broad "everything calendar" contract from a narrow schedule/worklist
  contract instead of making every screen pay for the broad union.

Concrete recurrence: a month-wise Full Schedule screen must not call a broad
Calendar events endpoint that reconstructs unrelated event sources and then
filters/massages them into schedule rows. The screen should read the canonical
vaccination schedule contract for the selected month/window; the Calendar screen
can keep its broader endpoint only when it truly renders calendar-specific event
types. Reusing a broad endpoint for a narrow UI and producing >500ms hot loads is
a scale anti-pattern even if both endpoints are "correct."

Vaccination date-source drift is the same performance and correctness bug. Once
`vaccination_drive_assignments` rows exist, the operator-day assignment is the
canonical scheduled execution source for every command lens: Full Schedule,
Calendar L1/L2/L3, Action Center, Protocol Adherence, Control Tower, Workflows,
execution pages, and leadership/operator reads. Those reads must prefer
assignment `planned_date` (or the override-effective assignment date in the
schedule ledger) before `obligation_batches.planned_date` or
`obligation_instances.due_at`. A screen that rebuilds membership from stale batch
or obligation dates can show the right aggregate and an empty roster, or mark
today's moved work as yesterday's overdue work. `make
vaccination-schedule-canonical-guard` blocks the known recurrence in Calendar
targets, process-integrity rows, and the schedule ledger.

`make scale-guard` blocks new static offenders for the highest-risk patterns:

- compute-on-read god CTEs on request paths
- N+1 database calls inside loops (raw driver calls)
- N+1 fan-out: a ctx-taking call to an injected I/O dependency
  (repo/reader/port/client/roster/ownership) inside a loop — the driver call is
  one adapter layer down, invisible to the raw-driver N+1 check. The "small data,
  still slow" class (one round trip per row): a 25-row page becomes 51 serial
  reads. Fix by batching to a single `*ByIDs` / `= ANY($1)` read, as `ShedSummary`
  now does with `ShedOwnerships`.
  Concrete recurrence: vaccination operator availability/capacity must be
  loaded once per sweep session at `(tenant, park, business_date,
  cap_per_operator)` grain and reused by date scoring, `ConductedBy` selection,
  effective-cap calculation, and drive-assignment splitting. Do not call
  `AvailableVaccinationOperatorsForDrive` independently from date loops,
  preflight loops, assignment distribution, and combo alignment; that re-creates
  park x candidate-date x helper fan-out and hides the cost behind "only three
  operators."
  Operator capacity itself is HRMS-authored per active execution seat
  (`workforce_positions.vaccination_daily_animal_cap`) with
  `vaccination_capacity_config.max_per_day` only as the fallback default. Do
  not multiply a hardcoded/default cap by operator count in UI or backend once
  per-seat caps are available. Operator assignment config is a point lookup
  scoped to `(tenant, park)` with bounded ≤N operators, never a scan or fan-out.
  `active_operators_per_day` and `default_operator_code` are scheduler-consumed
  seed/admin config, not saved-for-display metadata. Treating them as
  config-only is a false-green seed anti-pattern: CPT's N=1/default-Darshan
  drive must plan one operator per business date, with the PM shift label used
  only to identify the fallback operator when the default is off/on leave. Shift
  labels must never create morning/afternoon vaccine sessions; the drive cap is
  day-grained unique animals.
- infinite paging loops without cursor/progress proof
- deep `OFFSET` pagination where keyset pagination is required
- tenant-wide projection delete/reinsert rebuilds
- non-sargable `lower(col) LIKE '%...'` search predicates
- capped read-time rollups that fetch a larger raw slice, aggregate it in
  app/service/frontend state, then hide pagination/truncation and present the
  collapsed result as business truth
- admin-web route prefetch for operational links whose target performs
  authenticated SSR/API reads; navigation must be user-triggered, not fired by
  link visibility or hover in the background
- business-calendar hardcodes that derive operational month/year/window state
  from the server/browser default timezone instead of the Goat OS business
  timezone

`make calendar-endpoint-grain-guard` blocks the adjacent endpoint-grain
recurrence across backend/admin-web/mobile/contracts/DB: a narrow vaccination
schedule/full-schedule/read-model surface must not call, alias, or derive from
the broad `/calendar/vaccination/events` Calendar presentation endpoint. The
Calendar endpoint remains valid for Calendar event presentation; narrow screens
must use `/vaccination/schedule` or another grain-owned API/read path with its
own contract and latency evidence.

Vaccination operator-day sync is a shared-source rule, not a UI convention.
`vaccination_drive_assignments` is the canonical operator-day source for a
published/moved vaccination drive: planned business date, operator, park,
physical shed, partition, assigned animals, dose summary, and capacity state are
read from that table or a set-based read model whose membership starts there.
Calendar, Protocol Adherence (PA), Action Center (AC), Workflows (WF),
vaccination execution, and any L1/L2/L3 calendar drilldown must agree on that
same source. When a drive moves to a new business date, every surface must show
the moved operator-day assignment or explicitly show no row because the
assignment does not exist; none may fall back to stale
`obligation_batches.planned_date`, `window_start`, `obligation_instances.due_at`,
or a frontend-local date filter and present that as current work.

`make vaccination-shared-source-sync-guard` blocks the highest-risk recurrences:
backend vaccination/calendar/process-integrity reads that combine operator-day
date semantics with `obligation_batches` date fields without also joining
`vaccination_drive_assignments`, and admin-web server page code that fans out
Calendar/PA/AC/WF/vaccination fetches inside `Promise.all(...map(...))` or
`await` loops during page switches. The permanent fix is one grain-owned,
bounded endpoint/read model per screen transition, backed by set-based SQL and
shared operator-day assignment membership.

## Indexed predicates and guard-authoring safety

Two recurrence classes are prohibited on every hot path:

- **Column-side type casts in predicates.** Do not write `indexed_uuid::text =
  ANY($1::text[])` (or the equivalent cast on another indexed column). Casting
  the stored column can prevent PostgreSQL from using its ordinary index. Keep
  the column bare and bind a typed array: `indexed_uuid = ANY($1::uuid[])`.
  The query must have a natural `EXPLAIN` proof on a realistic row count; an
  `EXPLAIN` that forces `enable_seqscan = off` is not sufficient.
- **Unbounded configuration-block matching.** A Terraform/HCL guard must bind
  related fields to the same parsed block. A file-wide regex may combine a bad
  value from one `env {}` block with a good value from a later sibling and go
  green. Every guard must include an adversarial self-test for that bypass and
  the self-test plus the real check must run in `ci-local`.
- **A literal-token guard sold as an architectural boundary.** A guard that
  greps for the exact string from the original incident enforces that string,
  not the rule. State the guard's blind spots in its own header comment and add
  a self-test fixture for each one, or narrow the guard's advertised claim.
  Concrete failure: `tools/agent-hooks/check-ceo-ai-core-boundary.mjs:39-42`
  is two literal `ceo_ai.` SQL regexes plus a path-prefix classifier
  (`:78-96`). It is blind by construction to a dynamic schema name
  (`fmt.Sprintf("SELECT %s.fn(x)", os.Getenv("REPORTING_SCHEMA"))`), to
  `SET search_path TO ceo_ai, public` followed by an unqualified call, and to a
  core package importing a `backend/internal/ceoai/**` helper that runs the SQL
  internally — so the coupling that caused the CT/AC 500s can return green.
  When the rule is "package A must not depend on package B", the guard must
  check the **import graph**, not call-site strings; when the rule is about
  runtime schema resolution, a static scan cannot close it and the guard must
  say so.

These are recurrence rules, not one-off review advice: `make scale-guard`,
`make validate-sqlc-plans`, and deployment-guard self-tests are the
machine-enforced backstops. Review skills for both Claude and Codex must still
require the same evidence when a new hot query or configuration guard is added.

Existing debt is tracked in `tools/scale-guard/baseline.txt`. Do not add a new
baseline count for new work. Fix the query, batch the writes, add a real
projection/read model, use keyset cursors, prove loop progress, or add a narrow
inline `scale-guard:ignore` with a concrete boundedness reason.

## Source-seed validation anti-patterns

Source ingestion is also a scale and correctness boundary. One bad spreadsheet
row can fan out into obligations, drives, owners, calendars, alerts, and mobile
worklists. Do not import vaccination drive assignments as source data or count
assignment rows as animals. Operator-capacity planning must remain derived,
set-based, and bounded at operator/business-date/unique-animal grain so
partitioned sheds cannot multiply read-model counts or leak work across
operators.
proof work. The following are banned:

Vaccination drive assignment persistence must stay set-based. The planner may
produce one row per physical shed/partition/operator/date, but those rows are
written with one batched upsert for the generated batch, never one database
round trip per partition. Operator capacity is counted as unique animals per
available operator per business date; multi-vaccine animals do not multiply the
assignment write volume.

Vaccination operator availability is the same scale boundary on the read side:
one sweep/preflight session may probe many dates and may call separate helpers
for capacity, operator choice, and partition assignment. Those helpers must share
the session-scoped `(tenant, park, business_date, cap_per_operator)` availability
cache. A direct repo/port call from any one of those helpers is a recurrence of
the N+1 fan-out bug, even when the seed fixture has only three operators.

Do not model raw partition-bearing shed names as separate canonical buildings.
`Gandhi 1/2/3` are partitions under `Gandhi`; `Godel 1 - Part 3` is partition
`Part 3` under physical shed `Godel 1`. Splitting them into separate `locations`
rows multiplies work, breaks operator ownership, and makes UI grouping lie.

Derive a person's role from their AUTHORITATIVE current position, not a frozen
snapshot taken earlier in the seed. `seed-roster-real` recasts a rehearsal
operator's resolved seat into a `vaccination_operator_<name>` position via the
operator-roster overlay, but it computes `bestCode`/`bestTier` during the earlier
June-seat mapping pass, BEFORE that overlay runs. Deriving `primary_role_hint`
from that stale `bestCode`/`bestTier` labelled field executors by their old seat —
manager tier → `supervisor`, or a leftover `park_head` seat → `park_head` — and
the mobile scan gate (`operatorAllowed = primary_role_hint == "operator"`)
silently dropped every scan for Amit/Darshan/Sagar on STG. The fix keys the hint
off the member's actual post-overlay positions (any active `vaccination_operator_*`
⇒ `operator`), locked by `TestDeriveRoleHintVaccinationOperatorAlwaysOperator`.
The general rule: when a later pass mutates the authoritative record, recompute
derived identity from the final record, never from a snapshot frozen upstream.

Do not report drive assignment rows as the complete future schedule until the
missing-obligation audit is clean. Adult ET+TT dose 1 history must always have
same-goat adult ET+TT dose 2 work; otherwise the assignment table is just a
partial projection.

Do not map source health case-log status `Closed` or explicit healthy status
`Fine` to canonical `recovering`. `Closed` is resolved history and normalizes to
`healthy`; `Fine` also normalizes to `healthy`; `Open` normalizes to `sick`, and
`Extended` normalizes to `under_treatment`. Treating any populated source health
cell as a defer state is a seed anti-pattern because it silently removes
eligible animals from vaccination drives.

- connecting to the database or writing grants/roster/config before the exact
  selected source directory passes a DB-free preflight;
- seeding a private/raw sheet directly instead of a sanitized committed fixture
  with manifest hashes and a correction ledger;
- changing vaccination dates or converting them to `NA`/`Pending` merely to
  make contradictory mock DOB, species, or terminal metadata look valid;
- silently inventing DOBs, staff owners, backups, Park Heads, shed mappings, or
  maternal relationships;
- committing payroll, salary, bank, IFSC, tax, payment, advance, DOJ, real staff
  names, or other out-of-scope HRMS data;
- adding a source column, migration, config/SOP rule, owner role, or importer
  branch without adding the corresponding validator rule, failing fixture,
  deterministic transform decision, manifest refresh, and documentation;
- leaving an active one-time vaccination obligation live after accepted source
  history for the same goat/rule has been imported; that creates duplicate work
  while hiding that the goat already has accepted history;
- a guard with no adversarial self-test, or a guard not run by full local CI;
- validating only the default fixture while a source-path override can be
  written before that exact override is checked.

The recurrence protection is `make vaccination-hrms-source-audit`,
`make vaccination-hrms-seed-fixture-guard`, the guardrail registration
manifest, and the exact-source first step in `make
seed-vaccination-source-full`. The complete contract is
`docs/runbooks/source-seed-data-validation.md`.

Vaccination matrix schedule metadata is part of that same contract.
`route_site` must be authored as protocol metadata (`subcutaneous` for the
current seed matrix), not as an operator/SOP form field and not as a
frontend-only default. If a future PR changes route-site semantics, it must
update the seed source policy, source validator, fixture manifest, docs, and
self-test in the same patch or local CI must fail.

Calendar/Action Center/operator worklists need an extra explicit warning here:
if the product wants one park-drive row instead of hundreds of goat/protocol
rows, that grouped row must come from a projector/read model. It is not
acceptable to:

- bump the raw request-path limit (for example to 5000),
- aggregate those raw rows in a service/helper or frontend component,
- clear `NextCursor` or otherwise hide truncation,
- and then show shed/vaccine/goat counts as if they were complete business truth.

That pattern is still compute-on-read, still partial when the raw slice is
truncated, and still unsafe at 1M animals even if it looks fine on a local
fixture. If a temporary UI collapse is needed for a mock/demo, it must be
clearly partial/debug-only and must not invent authoritative totals.

## Business-calendar timezone anti-pattern

Goat OS operational dates are business-calendar facts, not server-local display
facts. Any schedule, calendar, freshness window, SLA bucket, or month/year
filter must use the declared business timezone explicitly (`Asia/Kolkata` for
the current Mesha/VGoats operating model). It is a hardcoded correctness
anti-pattern to compute those buckets with runtime-local extraction such as:

- JavaScript `new Date(iso).getMonth()` / `getFullYear()` for business month
  checks;
- Go `time.Now()` / `t.Month()` without converting to `biztime.DefaultLocation()`
  for operational windows;
- SQL `date_trunc` without the intended business timezone when the source value
  is `timestamptz`;
- frontend filtering that uses the browser/server timezone and then displays
  the result as the business schedule.

The failure mode is subtle: `2026-08-01T00:00:00+05:30` is August in IST but
July on a UTC server. A Full Schedule, Calendar, or Action Center month filter
that uses server-local month extraction can drop or mis-bucket exactly the
boundary rows operators care about.

Local seed fixtures used by emulator E2E are also contract state when mobile or
workers depend on them. The vaccination trigger fixture's `CBE-RFID-0001`
primary RFID must stay synthetic, reviewed, and synchronized with the fixture
validator/runbooks; never generate ad-hoc RFID values in runtime code.

Fixes must normalize through the business timezone at the layer where the
bucket is computed. In admin-web, use `Intl.DateTimeFormat(..., { timeZone:
"Asia/Kolkata" })` or an equivalent shared helper for business month/year
checks. In Go, use `biztime.DefaultLocation()` before deriving dates. In SQL,
use `AT TIME ZONE 'Asia/Kolkata'` intentionally and cover the expression with
tests or plan evidence when it is on a hot path. Every new date-window fix needs
a boundary test for an IST-midnight value that crosses the UTC day/month.

## STG stale-content anti-pattern

A staging environment running mixed commits is not a valid E2E target. It is the
deployment equivalent of a stale read model: API, admin-web, workers, jobs,
migrations, seeds, and database-generated rows can each be correct in isolation
while the combined system lies. This is especially dangerous for Calendar,
Action Center, Protocol Adherence, vaccination execution, and Full Schedule,
where frontend screens read backend-owned contracts and derived operational
state.

Before calling any deployed-STG check green, prove all of these at the same
time:

- local `HEAD` equals current `origin/main`;
- API Cloud Run service image matches that exact SHA;
- admin-web Cloud Run service image matches that exact SHA;
- kernel worker service image matches that backend SHA;
- migration, DLQ, analytics, seed, and any other feature-involved Cloud Run jobs
  match that exact backend/migration SHA;
- database migrations and generated/seeded rows were produced after that code
  reached the environment, or were explicitly rerun from that SHA;
- browser proof uses `https://stg.dashboard.mesha.sg`, not localhost.

If `origin/main` moves while a deploy is building, rolling out, or being tested,
the environment is stale again. Create a new Cloud Deploy release for the new
main SHA, wait for rollout success, and re-run service/job image parity before
continuing. Do not debug UI symptoms, performance, grants, or data correctness
against mixed frontend/backend/job/database state.

The normal staging authority is Cloud Deploy. For an explicitly authorized
break-glass local repair, `tools/deploy/stg-clouddeploy-release.sh` must wait
for the rollout and verify image parity for API, admin-web, kernel worker,
migrate, DLQ, and analytics. Skipping rollout wait or image parity invalidates
the handoff.

The narrow Calendar exception is accepted vaccination completion history: a
read-only timeline behind the explicit `status=completed` path. That history is
not active work, not a park-drive rollup, and not a second projection row. It
may be derived at read time from canonical accepted completions plus completed
obligations when the query stays tenant-scoped, date-bounded, keyset-paginated,
and honest about truncation. Default month/date-marker responses may include
read-only completed markers so users can see recent completed days in the same
calendar surface, but open-work pages must not turn those markers into
aggregated operational totals, suppress pagination truth, or blur them into the
authoritative active-work list.

## 5k-to-50k envelope: exempted canonical read paths

The accepted ADR `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`
serves five operator screens directly from canonical tables through bounded,
indexed SQL for the 5,000-to-50,000-animal release envelope: **Calendar,
process-integrity, vaccination shed, vaccination execution, and vaccination
operations**. That per-request canonical read is the `compute-on-read` / god-CTE
shape this document bans and that `make scale-guard` blocks mechanically.
Narrowing the deployment scale target does NOT disable the guard, so these five
paths are reconciled with the guard explicitly rather than by weakening it:

- Each of the five named reads carries a scoped, sanctioned annotation on the
  exempted read:
  `// scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md`
  with a matching `tools/scale-guard/baseline.txt` entry where the guard
  requires one. The guard is **NOT** globally disabled: it stays fully active for
  every other path in `backend/internal/**`, and compute-on-read remains banned
  everywhere else. Only these five specific, named reads are exempted, and only
  under this envelope.
- The exemption is valid ONLY for a read that is query-plan-tested per the ADR —
  both the keyset-paginated list shape and the indexed summary-aggregate shape,
  the aggregate proven against the upper-bound obligation-row count (up to ~500k
  obligation rows at 50k animals), not just the 5k list case. An exempted read
  with no query-plan test is a defect, not an exemption.
- The annotation is **removed** when a screen later earns its own projection (the
  ADR scale-out ladder): that read then returns under full guard enforcement. The
  exemption is a measured, temporary envelope allowance, never a standing licence
  to compute-on-read.

This keeps the machine gate honest: the anti-pattern rule is suspended only for
these specific, measured, plan-tested envelope reads, never blanket-disabled.
This document backs `make scale-guard`; the guard code itself is unchanged by
this reconciliation note.

## Vaccination Drive Planner Anti-Patterns

Vaccination scheduling is not "DOB + offset = one animal drive." Per-animal
`due_at` rows are canonical obligation truth, but they are only inputs to the
drive planner. A scheduler that groups by exact due date and emits one drive per
animal/day is a defect even if every individual obligation date is medically
correct.

Forbidden:

- using exact `due_at` / exact `window_start` / exact `window_end` as mandatory
  drive boundaries before the planner scores compatible work;
- creating 1-2 animal micro-drives while compatible same-park animals are due or
  ready nearby and can be delayed within their own authored medical window plus
  the one-time batching hold;
- treating shed count as a park-batching constraint. Sheds are execution/proof
  breakdown, not the optimizer target. A one-shed park drive is valid when it
  maximizes animal output safely, and a cross-shed drive is required when that
  yields more safe animals;
- ranking by obligation/vaccine rows before distinct animals;
- hiding bad drive fragmentation in Calendar by merging rows in the frontend;
- repeatedly rolling a held animal forward to chase larger future drives;
- leaving any live seed/import goat without `park_id`, `shed_id`, and
  `current_location_id = shed_id`;
- creating park-scoped or tenant-scoped goat vaccination obligations. Park is
  the drive/visit grouping scope after obligations exist, not a fallback animal
  obligation scope;
- treating screenshots after generation but before sweeper closeout as
  vaccination proof. Generation creates due obligations; the sweeper creates
  clubbed drive batches.

Required behavior: maximize compatible distinct animals per same-park visit
inside the selected animals' safe windows. Default policy allows one batching
hold of up to seven days (`max_batching_hold_days=7`,
`max_batching_hold_count=1`). Micro-drives are valid only when no compatible
same-park work can be clubbed between the group due/ready date and binding
safe-until date. Normal per-drive animal caps are soft on the last safe day;
per-animal shot caps are hard. Backfills and proof sweeps must not reuse the
eligibility horizon as "today": keep `asOf` for planner date math and
`dueBefore` for loading candidates. Batched read models must use batch
`planned_date` as the drive date; using the earliest member animal `due_at` for
execution/control-tower rows is the same micro-drive leak in projection form. CI guard:
`make vaccination-drive-clubbing-guard`. Post-reseed DB proof:
`make vaccination-drive-clubbing-db-proof`.

Seed/import proof is part of the same contract. During this build phase, missing
source placement is completed deterministically into an explicit seed-intake
shed; do not skip goats and do not invent vaccination dates/history to make
output look clean. `tools/dev/seed-closeout.sh` must run the goat-shed integrity
proof after seed, generation, and sweeper. Static guard:
`make goat-shed-scope-guard`. Post-seed DB proof:
`make goat-shed-integrity-db-proof`.

## Projection rebuild anti-patterns

The `full (stop-the-world) MV refresh` rule above bans the delete+reinsert
*mechanism*. This section bans the *rebuild trigger and availability* failures
that surfaced in the Calendar/CT/AC/PA/Vaccination projection rebuild (Codex
"Fix vaccination calendar rules", 2026-07-13). A projection that reads correctly
at 1k rows can still take the whole surface down or lie about freshness at 1M.
Root cause under all four: **rebuild should be read-through, dirty-scoped, and
honest about what it actually recomputed — never take the read model offline,
re-scan the whole tenant on a timer, rebuild twice, or blanket-stamp fresh.**

1. **Rebuild-by-unavailability (availability regression).** A rebuild that flips
   the read model to `unavailable`/stale (`ErrProjectionUnavailable`, 503, blank
   wall) *before* recomputing, so Control Tower / Action Center / Protocol
   Adherence / Calendar / Vaccination fail to load even though the last
   successful projection is still valid. Rebuild must be **read-through /
   version-swap**: the previous projection version keeps serving until the new
   version is built and atomically swapped in. Only a genuine cold start (no
   prior successful version for that scope) may serve `unavailable`. Never
   degrade a warm surface to rebuild it.

2. **Scheduled whole-tenant rebuild on a timer.** A cron that unconditionally
   recomputes the *entire tenant* projection every N minutes regardless of what
   changed (the whole-tenant `RecomputeProjection(tenantID)` shape). Cheap at 1k
   rows, a full-herd re-read every cycle at 1M. Replace with **event/dirty-set-
   driven incremental maintenance scoped to the changed unit** — per-shed /
   per-park dirty projections keyed off outbox deltas. Whole-tenant recompute is
   allowed ONLY as an explicit, off-request, manual/seed/backfill path (labeled
   as such), never as the steady-state refresh.

3. **Double / duplicate rebuild per cycle.** The same projection rebuilt more
   than once per cycle — two schedules, or a projector AND a sweeper both
   rebuilding the same rows. One projection = one owner = one trigger. A
   duplicate rebuild schedule is a defect, not a safety margin.

4. **False-freshness "incremental" worker.** A worker *labeled* incremental that
   actually copies the whole tenant and stamps freshness/watermark on scopes it
   did not recompute (e.g. marking mixed-age sheds `fresh`). The freshness
   envelope (`as_of` / `last_success_at` / `freshness_status` / source watermark
   / projection version) must reflect ONLY what was actually recomputed. Never
   blanket-stamp fresh. An "incremental recovery" path that is really a
   whole-tenant copy with a false freshness claim is a band-aid — reject and
   remove it, do not ship another unsafe rebuild on top of it.

Corollary (compute-on-read), scoped by scale target: under the 5k-to-50k
envelope the Calendar day/month-marker and completion-history read is served
directly from canonical obligations/batches/SOP/proof through bounded, indexed
SQL under the scoped `// scale-guard:ignore: 5k-50k-envelope` exemption — see the
"5k-to-50k envelope: exempted canonical read paths" section above and the
accepted ADR `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, which
removes `calendar_event_projections` and frames serving Calendar from canonical
tables as the correct choice that "deletes the entire projection-drift bug
class" (citing exactly the rebuild-trigger anti-patterns in this section). At
that envelope this is the sanctioned day-1 read, not a banned one. At future/1M
scale — or the moment this screen earns its own projection on the ADR scale-out
ladder, at which point the annotation is removed — that same Calendar
completion-history and month/date-marker read must instead be served from a
materialized projection rather than from canonical joins run live per request:
at that ceiling "measured" is not "safe" and a timed god-join is still
compute-on-read and still 1M-unsafe. Either way the accepted narrow Calendar
history exception above stays read-only, tenant-scoped, date-bounded, keyset,
and truncation-honest, and outside the named envelope exemption it does not
license live joins on the open-work path.

These four are **review-caught, not statically caught** — `make scale-guard`
blocks the delete+reinsert mechanism (`full-mv-refresh`) but cannot see rebuild
cadence, a serving-state flip, or a false freshness stamp. Reviewers must name
them; see `.agents/skills/goatos-code-review/references/kernel-and-scale.md`.

When an E2E run, scale audit, or feature proof is generated while fixing one of
these issues, commit the report and publish it through the GitHub Pages report
site. Local-only proof must say that it is local-only and must not be described
as staging or production certification.

## Bounded Worker and Cursor Progress Invariants

Every sweeper, processor, scheduler, and worker that is designed to terminate or
scan a scope must follow these non-negotiable rules:

1. **Bounded workers must never restart at zero.** A worker designed to scan a
   finite set (for example, all due-within-30-days rows) and exit must resume
   from its last-scanned checkpoint, never restart from offset 0 on the next
   invocation. An unbounded/infinite worker that polls for new work may restart
   its polling loop; a bounded worker that scans a defined window must not. Use a
   keyset cursor stored durably in the result or state row to resume from exactly
   where the prior scan ended — no re-scanning, no rolling back the cursor.

2. **Cursors and progress markers must be monotonic.** Forward progress is
   guaranteed only when the cursor or `last_seen_key` always increases or at
   minimum never goes backwards. A cursor reset to an earlier value, a
   re-generated keyset that omits processed rows, or a scan loop that resets its
   `offset` to 0 on error violates monotonicity. Consequence: the same row can be
   processed twice, or work scheduled twice for the same due date window. Always
   track the cursor durably so recovery or a manual `--resume` starts exactly
   where processing left off.

3. **Filter before limit, never after.** A query or loop that LIMIT's N rows and
   then filters in application code silently drops valid rows that do not match
   the app-side predicate. It is not possible to know whether all matching rows
   were processed unless the match is part of the query predicate itself. When a
   due-window scan limits 100 rows per batch and later app code filters to a
   subset (for example "only rows with status = pending"), the first batch of 100
   is fetched, filtered to 50, and then the batch exits — but the next batch
   starts fresh at offset 100, never at the 50 matching rows from the prior
   batch. If there are 1000 eligible rows total but the filter reduces each batch
   to <N result rows, a forward sweep still hits the full 1000 and the work gets
   done; if the filter is applied *before* the LIMIT in SQL, the sweep is bounded
   to the actual match set. Gotcha: a list API endpoint that accepts both a filter
   and a limit parameter must apply the filter to the query, not filter in the
   calling service/controller, otherwise a paginated consumer gets incomplete
   results on each page.

4. **Page size must not alter business completeness.** Changing the per-request
   limit (for example 10 vs 20 vs 100 rows per page) must never result in a set
   of rows that are ultimately processed being different. A change in page size is
   a performance and latency tuning; it is not a correctness dimension. If a
   feature works correctly with 20-row pages and breaks with 100-row pages — or
   if a "fix" that changes page size accidentally changes which rows are included
   — the feature is not properly bounded. Consequence: do not use page size to
   work around incomplete filtering or missing retry/resume logic.

## Admin-web SSR full-table request reads

`make scale-guard` scans Go (`backend/internal/**`) only. The same compute-on-read
disease crosses into `apps/admin-web/**`: a Next.js server component or
`lib/api/server.ts` helper that **drains a paginated backend endpoint cursor-by-
cursor into one big array** to compute a KPI on the request path. That is exactly
the `searchAllGoats` full-herd walk removed in commit `810bc1b3` — it looks fine
against a 1k-goat fixture and melts at 1M:

```ts
// BANNED — full-herd SSR walk
export async function searchAllGoats(params: Omit<HerdSearchParams, "limit" | "cursor">) {
  const items = [];
  let cursor;
  for (;;) {
    const page = await searchGoats({ ...params, limit: 100, cursor });
    items.push(...page.data.items);           // accumulate every page into memory
    if (!page.data.next_cursor) break;
    cursor = page.data.next_cursor;           // drain the whole cursor
  }
  return { ok: true, data: items };           // then filter/count in the component
}
```

The fix is a **projection/summary endpoint** that returns pre-aggregated counts;
the request does one indexed lookup, never a row walk:

```ts
// CORRECT — read the read model
export async function getHerdRegisterSummary(params: HerdRegisterSummaryParams) {
  return request(() => client.request("/herd-register/summary", { query: params }));
}
```

`make admin-web-request-reads-guard`
(`tools/agent-hooks/check-admin-web-request-reads.mjs`) blocks the `cursor-drain-
loop` shape: a `for`/`while` whose body both accumulates (`.push(...)` / `.concat`)
and advances a cursor from `next_cursor`. It is **diff-scoped** (a commit with no
admin-web TS passes instantly), skips `'use client'` modules and test/mock/seed
files, and offers an inline `// scale-guard:ignore: <reason>` escape hatch for a
genuinely-bounded, small-cardinality read. The `Omit<Params, "limit" | "cursor">`
signature alone is **not** flagged — a projection/summary reader legitimately takes
no page bound (e.g. `getOperationsAuditSummary`); only the actual drain loop is.
`make admin-web-request-reads-guard-audit` runs the whole-tree audit; the legacy
`OutboxDao.observeAll` and any other pre-existing whole-set reader surface there
and should migrate to a projection/keyset read.

## Admin-web route prefetch request reads

Next.js `Link` prefetch is also a request-path scale anti-pattern for Goat OS
admin-web. It can start a server navigation before the operator clicks a link:
when a link becomes visible or is hovered, Next.js may load the target route in
the background. That is useful for mostly-static pages. It is harmful for
authenticated operational screens such as Full Schedule because the target route
can execute SSR helpers and backend API reads while the user is still looking at
the current screen.

For example, a Vaccination page that renders Full Schedule/month/year links must
not silently fire expensive schedule reads for those targets in the background.
Those reads should happen only after the operator chooses the schedule view.

Admin-web code must import `Link` from
`@/components/no-prefetch-link`, never directly from `next/link`, and must not
set `prefetch={true}`. `make admin-web-prefetch-guard`
(`tools/agent-hooks/check-admin-web-prefetch.mjs`) enforces that globally for
`apps/admin-web/**`, with `apps/admin-web/components/no-prefetch-link.tsx` as
the only allowed direct `next/link` import.

## Seeder-only contract drift

A seed/importer patch that changes vaccination schedule, SOP proof, HRMS owner,
or stage/DOB behavior without updating the committed fixture, validator, and
runbooks is an anti-pattern. It creates a false-green local seed: the code may
compile, but the next developer's source bundle still lacks the new invariant.

Example: vaccination matrix `route_site=subcutaneous` is required protocol
metadata for publishability, but route/site must not reappear as an operator SOP
form field. The seed fixture guard must fail a seeder-only route/site change
until the fixture manifest, source policy, validator, and docs describe the same
rule.

The same coupling applies to vaccination proof grain. Moving the SOP from
per-goat video to shed-level video is not just a UI switch: the fixture
manifest, source validator, backend proof gate, Android form runner, and
verifier bridge must all agree that scans remain per goat while video proof is
one-to-five shed-level artifacts. A seeder-only or client-only proof-grain
change is a false-green seed and is blocked by the seed fixture guard.
Client screens must not fetch all target animals just to prove this point; shed
proof upload stays shed-grain while animal evidence stays the paged scan roster.

## Config-present must fail closed, never fall back to defaults

A critical anti-pattern at any scale: when a config/policy/capacity row EXISTS
for a scope but resolution yields an EMPTY result, code must **fail closed**
(defer/flag/zero-capacity/reject) rather than silently falling back to the
unconfigured/default behavior.

This pattern guards against two distinct failure cases:

1. **Silent misconfiguration**: A park/operator/stage config exists but is
   incomplete or empty. The code must not treat "no active results" as "no config
   present" and fall through to base defaults. The presence of the config row
   itself signals that the operator intended to override the default; an empty
   result means the override failed or is incomplete.

2. **Data inconsistency**: A scheduler, filter, or policy read returns zero
   candidates where the config explicitly claims there should be results. This
   mismatch is a data integrity red flag, not a "gracefully degrade to defaults"
   moment.

**Concrete failure case (vaccination operator assignment):**
`visit_shot_lock.go` filters the global operator-capacity candidate set by
applied `active_operators_per_day` config. If the config row exists for the park
but the filter yields **zero executable operators**, the sweeper must NOT treat
`len(operators)==0` as "no config present" and fall back to base operator
capacity. Instead, it must:

- Defer the work (flag as pending config resolution)
- Log the mismatch (capacity available but no permitted operators for this drive)
- Fail the assignment or escalate to leadership review

The seeded fixture and database proof must always distinguish:
- "No config for this park" → use base capacity
- "Config exists but filter returned empty" → FAIL CLOSED (do not use base capacity)

Code pattern (Go pseudocode):
```go
// WRONG: silently degrades to defaults when config is present but empty
config := findVaccinationOperatorConfig(tenant, park, date)
if config == nil {
  return baseCapacity  // correct: no config, use default
}
operators := filterActiveOperators(config)
if len(operators) == 0 {
  return baseCapacity  // WRONG: config exists but is empty, should fail closed
}
```

```go
// CORRECT: fails closed when config is present but filter returns empty
config := findVaccinationOperatorConfig(tenant, park, date)
if config == nil {
  return baseCapacity  // correct: no config, use default
}
operators := filterActiveOperators(config)
if len(operators) == 0 {
  return nil, ErrConfigPresentButEmpty  // CORRECT: config exists but is incomplete
}
```

This pattern is enforced at review time by the code-review skill
(`.agents/skills/goatos-code-review/references/`) and must appear in every
pull request that reads config and filters results. Regression test: any query
that reads a config row must have an E2E case where the config exists but the
filter yields zero results, and that case must document the expected fail-closed
behavior (deferral, escalation, or explicit rejection).

`make scale-guard` catches the raw N+1 and compute-on-read anti-patterns; this
one requires human review and regression test coverage.

## Operator-cascade wiring anti-patterns

A config/roster change must travel an unbroken chain before a planned drive is
re-planned:

```text
write path mutates scheduling-relevant state   (must EMIT its cascade event)
  -> outbox relay dispatches onto the domain bus (handler must be on the DURABLE bus)
    -> sweeper re-selects work under the operator cap (must NET session-reserved load)
```

All three links broke at once on `origin/main` f9a44b84 while every isolated
unit and E2E test stayed green, so all three are named anti-patterns and all
three are machine-enforced by `make cascade-event-wiring-guard`
(`tools/agent-hooks/check-cascade-event-wiring.mjs`).

### A. Session-reserved capacity ignored at the selection layer

A per-actor/per-day capacity read from the database is only net of load
committed by **prior** runs. Whatever the **current** sweep session has already
reserved lives in memory (`SweepSession.vaccinationOperatorLoads`), and the
underlying DB snapshot is cached per session
(`SweepSession.cachedVaccinationOperators`). Summing the DB-reported remaining
capacity without subtracting the in-session reservation is an over-selection
bug, not a rounding error.

Concrete failure: `totalVaccinationOperatorCap` summed each operator's
DB-reported remaining capacity for `(tenant, park, date)` and fed it into
`operatorCapacityPlanner` / `effectiveOperatorAnimalCap`, which is what bounds
how many obligations a due-group may attach to a batch. A later rule-version in
the same sweep (e.g. `sheep_pox` after `blue_tongue`) re-read the same cached
pre-session snapshot, saw the operator's full un-reserved capacity again, and
selected on top of what an earlier due-group in that same sweep had already
committed. A real CPT reseed produced 221-223 animals against a 200
animals/operator/day cap.

The assignment-split layer (`planVaccinationDriveAssignments`) already did the
subtraction. Fixing only the later layer is a band-aid: the **selection** layer
is what decides how much work enters the batch at all.

Rule: any function that consumes a DB-reported per-actor capacity inside a sweep
must subtract the same session's reserved load for that exact
`(tenant, park, date, actor)` key before treating it as headroom. A cheap
in-memory reservation ledger is mandatory whenever one pass re-reads a cached
snapshot that an earlier pass in the same run already spent against.

### B. Domain-event handler registered on a bus nothing real dispatches to

Goat OS has four places that look like a domain bus. Only two are durable
dispatch paths:

| Bus | Reality |
|---|---|
| `backend/internal/kernelstages/bus.go` (`BuildDomainBus`) | **DURABLE** — backs the in-process outbox-relay publisher and the continuous domain-consumer stage |
| `backend/cmd/domain-event-consumer/main.go` (`buildDomainBus`) | **DURABLE** — the standalone Pub/Sub consumer binary |
| `backend/internal/bootstrap/api.go` | the API process's own in-process bus; for several event types its producers are dead code |
| `backend/internal/domainconsumer/wiring/bus.go` | wired into **no** `cmd/*` binary — tests only |

Concrete failure: `OperatorConfigReplanHandler` was registered in
`bootstrap/api.go` and `domainconsumer/wiring/bus.go` only. A green isolated E2E
test built the wiring bus itself and proved the handler logic end to end, while
production dispatched every `vaccination.capacity.changed` /
`vaccination.roster.changed` / `vaccination.operator_leave.changed` event onto a
bus that had no subscriber and silently dropped it. Planned drives kept the
stale cap and roster forever.

Rule: every type exposing `Register(bus eventbus.Bus)` must be registered on
**every** durable bus. Registering on a test/API-only bus is not integration.
A test that constructs its own bus proves handler logic, never wiring — the
wiring assertion must name the production bus builder
(see `backend/internal/kernelstages/bus_test.go`). A deliberate omission must be
declared in `DURABLE_BUS_EXEMPTIONS` in the guard with a written reason and the
file that really does register it; the guard re-verifies that claim, so an
exemption cannot decay into "registered nowhere".

### C. Write path mutates scheduling-relevant state without emitting its cascade

A mutation is "scheduling-relevant" when a consumer's already-materialised
future work would be wrong if it never heard about the change. Such a write must
enqueue its cascade event to `outbox_messages` **in the same transaction** as
the state change — the emit is part of the write, not a follow-up.

Concrete failures:

- workforce `UpdatePosition` changed a vaccination operator seat's
  `vaccination_daily_animal_cap`, `week_off_weekday`, `status`, or `valid_to`
  directly on `workforce_positions` and emitted nothing. The only wired
  producers were `UpsertOperatorAssignmentConfig` (N / default operator) and
  `ApplyLeave` — so HRMS-side seat edits were invisible to vaccination.
- protocol `publishVersion` synced the tenant-wide `vaccination_capacity_config`
  transactionally but emitted only `protocol.version.published`, which is
  consumed by obligation **regeneration**, not by capacity replan. Every park
  with an operator-assignment config kept planning against the old capacity.

Rule: enumerate the columns/tables that feed a scheduler, and treat any writer
of them as a registered producer with a cascade event, per
`context/architecture/domain-event-integration-contract.md`. "Another endpoint
already emits this event" is not coverage — coverage is per **write path**. The
guard currently pins three: `workforce_positions` cap/week-off,
`vaccination_capacity_config`, and `vaccination_operator_assignment_config`;
extend `CASCADE_WRITE_RULES` when a new scheduler input appears.

Seed CLIs under `backend/cmd/` are deliberately out of scope: a seed run is
always followed by an explicit generate/replan step.

### D. Cascade payload written, propagated, and read by no consumer

An event payload field that no consumer parses is not "captured for later" — it
is a silent accept-and-discard, and it reads to every future author as though
the fact is already honored. A payload key belongs in an event only when a named
consumer reads it, or when the decision to ignore it is written down.

Concrete failure: procurement writes supplier-claimed prior vaccination history
into the `goat.created` payload
(`backend/internal/procurement/adapters/postgres/goat_created_outbox.go:34-43`,
key `trusted_vaccination_history`). `GoatCreatedHandler`
(`backend/internal/vaccination/app/generation_handler.go:11-75`) reacts to the
event type only and recomputes from the goat's persisted row; a repo-wide grep
outside `procurement` finds no reader. `AdminGoatCreateRequest.VaccinationHistory`
(`backend/internal/identity/domain/types.go:193`) is declared and never
referenced. A procured adult with genuine ET+TT/PPR coverage is therefore
scheduled from scratch on the full adult primary course.

Rule: every payload key must either be consumed by a handler named in
`context/architecture/domain-event-registry.json`, or be deleted along with the
struct field that fed it and replaced by an explicit ignore decision (for
example "unverifiable supplier claims are never canonicalized") recorded in the
registry entry. "Written for a future consumer" is not an accepted state.

## Read-model grain is not the grain the consumer assumes

**This is an adherence failure, not a coverage gap.** AGENTS.md has required for
a long time that every aggregate "identify the canonical membership source, use
the same stable group key on producer and consumer, prove every join is 1:1 or
deduplicate/pre-aggregate the many side". Five instances shipped on
`origin/main` anyway. A sixth restatement of the principle would change
nothing, so this section does not restate it — it converts it into a mechanical
proof obligation that a reviewer can demand and an author can discharge in
writing.

### The proof obligation (state it in the PR, next to the `projection-review:` marker)

For any query that binds a fact to a plan/aggregate row, or that puts a
numerator and a denominator side by side, write these three lines out:

1. **Group key** — the exact column list the producer is unique on, and the
   exact column list the consumer matches/groups on. Put them side by side. If
   they differ by even one column, that difference is the bug until proven
   otherwise.
2. **Row multiplicity of each joined side** — for every join, `1:1`, `1:N`
   pre-aggregated, or semijoin. A column you left out of the key is a column
   you are asserting cannot vary; say so explicitly or add it.
3. **Numerator and denominator over the same key set** — when a ratio, cap
   check, or coverage percentage is computed, name the key set each side ranges
   over and show they are identical. "Both read the same `cohort_ids`" is not
   sufficient if one side then collapses a dimension the other keeps.

If those three lines cannot be written, the query is not reviewable and is not
approved.

### Sub-shape A — missing dimension in the WHERE (predicate form)

`vaccination_drive_assignments` is an aggregate plan row, not per-goat
membership. Its uniqueness grain is batch + planned date + park + shed +
physical shed + `partition_label` + operator
(`backend/migrations/postgres/000022_vaccination_drive_assignment_operator_grain.sql`),
and each row additionally carries `vaccine_rule_ids`. Selecting on
`tenant_id + batch_id + shed_id` and taking `LIMIT 1` is not a near-miss; in a
mixed-vaccine or partitioned batch it is deterministically wrong.

Four independent live defects on `origin/main` f9a44b84, in four modules that
each re-derived the lookup by hand:

| Site | Missing dimension |
|---|---|
| `backend/internal/vaccinationexecution/adapters/postgres/repository.go:745-756` (repeated at `:1624-1634`, `:1914-1930`, `:2728-2737`) | `vaccine_rule_ids` — a moved vaccine's date becomes the execution date for its siblings |
| `backend/internal/processintegrity/adapters/postgres/repository.go:637-655` | `vaccine_rule_ids` **and** `partition_label`; the date surfaces as public `due_at` at `:746-747` into Action Center / Protocol Adherence / Control Tower |
| `backend/internal/calendar/adapters/postgres/targets.go:127-139` | `vaccine_rule_ids` — the drive drawer lists animals for sibling vaccines that never moved |
| same-partition operator/date splits from `backend/internal/vaccinationexecution/app/operator_drive_planner.go:207` | no dimension can disambiguate — two rows are legitimately equal at every column the reader has |

Passport and the Calendar list are the reference implementations and already
carry the rule guard: `cardinality(assignment.vaccine_rule_ids) = 0 OR
oi.rule_id = ANY(assignment.vaccine_rule_ids)`
(`backend/internal/obligation/adapters/postgres/sqlc/query.sql:48`,
`backend/internal/calendar/adapters/postgres/canonical_read.go:743`). Two
surfaces being right while four are wrong is the signature of a hand-copied
predicate, and it is the same shape as the effective-date duplication already
recorded in this repo: the fix is one shared primitive plus a parity test, not
four more hand-edits.

### Sub-shape B — missing column in the GROUP BY key (aggregation form)

The predicate can be complete and the query still wrong, because the defect
moved from the `WHERE` to the `GROUP BY`. An author who reads sub-shape A and
adds `vaccine_rule_ids` to their `WHERE` has **not** complied if their
aggregation then collapses a dimension the other half of the same expression
keeps.

Concrete failure (BUG-027,
`backend/internal/processintegrity/adapters/postgres/repository.go`). The split
cohort at `:900-915` deliberately matches siblings on
`batch_id`/`shed_id`/`partition_label`/`vaccine_rule_ids` and deliberately
**excludes** `planned_date`, so a cohort may legitimately span several dates.
The numerator at `:920` respects that — `SUM(a.animal_count)` over every row in
the cohort, all dates. The denominator at `:938-948` does not:

```sql
SELECT a.operator_id, MIN(a.planned_date) AS planned_date
FROM vaccination_drive_assignments a
WHERE ... AND a.assignment_id = ANY(drive_split.cohort_ids)
GROUP BY a.operator_id            -- collapses to ONE operator-day per operator
```

`operator_cap` then sums one daily cap per operator. A same-operator, two-date
split therefore compares two operator-days of assigned animals against one
operator-day of capacity, fabricating `over_cap_required` on legitimately
distributed work — and hiding a real over-cap in the inverse case. The fix is
`GROUP BY a.operator_id, a.planned_date`: the real operator-day grain, matching
the date set the numerator ranges over.

Rules for both sub-shapes:

- Before writing the lookup, name the grain of the table you are joining to and
  list every column in it — then check the `WHERE`, the `GROUP BY`, **and** the
  key set of anything the result is divided by or compared against. All three
  must agree.
- A deliberate exclusion in a match key (here, `planned_date` out of the cohort
  key) is a contract, and it propagates: every aggregate computed over that
  cohort must either preserve the excluded dimension or state why collapsing it
  is safe. A `MIN(...)` used as a display representative is not a licence to
  aggregate capacity at that collapsed grain.
- When the planner can legitimately emit two rows indistinguishable to the
  reader (a partition split across operators or dates), `ORDER BY ... LIMIT 1`
  is not a tie-break, it is a fabricated answer. Fix it with an exact
  membership source (goat-level assignment membership or an equivalent
  deterministic ledger), not a better ranking.
- A new consumer of `vaccination_drive_assignments` must be added to the shared
  predicate/helper, and a parity test must assert that every consumer resolves
  the same obligation to the same row.

Mandatory fixture for any change in this area: a mixed ET+TT/PPR batch where
PPR alone is date-overridden, a shed with two `goat_shed_partitions` values,
**and** one cohort split across two dates for the same operator. A
single-vaccine, single-partition, single-date fixture proves nothing here and is
a false green — and the single-date case is what let sub-shape B through a
review that had already learned sub-shape A.

## Cross-surface count parity (web = mobile = API must show the same number)

**The same business fact must produce the SAME number on every surface that
shows it — admin-web, the mobile app, and the API. If two surfaces disagree,
one of them is a bug, even when each query is internally "correct".**

The concrete incident: a vaccination drive on one day showed **200 doses** on the
admin-web operator drive schedule and **400 doses** on the mobile calendar for
the identical drive. Neither the phone nor the web was wrong to render what it
got — the two BACKEND read models counted different things:

- Operator drive schedule → `count(DISTINCT oi.target_id)` from
  `vaccination_drive_assignments` = **200 distinct animals** (correct).
- Calendar park-drive marker → `GREATEST(ob.estimated_targets, 1)` =
  `obligation_batches.estimated_targets` = **400**, which is the obligation-ROW
  estimate (200 animals × 2 dose rows), not distinct animals.

The mobile app was NOT at fault — it faithfully rendered a wrong backend number.
"Frontend is fine, mobile is broken" was actually "two backend read models of the
same fact disagree", and the phone happened to consume the wrong one.

Rules (Claude AND Codex):

1. **Pick ONE authoritative source per business count and reuse it.** For "how
   many animals in this drive/day", that is `count(DISTINCT target_id)` over the
   actual `vaccination_drive_assignments`/obligations — the same grain the
   operator schedule uses. Never let one surface read an *estimate* column
   (`estimated_targets`, `estimated_*`, `*_quantity`, a cached rollup) as a
   user-facing count while a sibling surface reads the actuals.
2. **Estimate columns are not display counts.** `estimated_targets` is a
   pre-split planning estimate and can legitimately differ from realized work
   (row-grain, pre-move, pre-cap-split). It may drive capacity planning; it must
   never be rendered as the drive's animals/doses on a user surface.
3. **When you add or change a count that appears on more than one surface, prove
   parity in the same change**: state, next to the `projection-review:` marker,
   that the web/mobile/API paths for that fact resolve to the same source and
   grain, and add a test that asserts `calendar_marker_count ==
   operator_schedule_count == raw_assignment_distinct_animals` for a drive.
4. Different *labels* are fine (one surface may say "animals", another "doses"),
   but the underlying **number for the same fact must match**. A single-injection
   vaccine is 1 dose per animal; if "doses" ≠ "animals" for such a vaccine, the
   count is double-counting dose rows.

Fixed at `backend/internal/calendar/adapters/postgres/canonical_read.go` by
switching the park-drive `target_count` from `ob.estimated_targets` to
`count(DISTINCT oi.target_id)` — the same grain the single-shed drive path and
the operator schedule already use.

## Documented gate with no executable enforcement

A rule that exists only as prose in a runbook or validation doc is not a gate —
it is a hope. If a document states an automatic-failure condition or a required
post-step, either an executable check enforces it or the document must be
rewritten to describe what the pipeline actually does.

Concrete failures:

- `fixtures/vaccination-cpt-operator-drive-2026-07-23/LOCAL_DB_RESEED_VALIDATION.md`
  lists "the checkout SHA is not latest `origin/main`" as an automatic-failure
  condition, and no target, guard, or seed command checks git ref state before
  mutating the DB (`Makefile: seed-vaccination-source-full`). A stale checkout
  can produce a "clean reseed proof" from out-of-date rule code — precisely the
  scenario the doc's own failure list exists to prevent.
- CPT expected-drive validation is documented as part of seed closeout but is
  not invoked by the closeout path, so the documented assertion never runs.

Rule: when a doc says "must", grep for the code that enforces it in the same
review. If there is none, the finding is the missing check, not the missing
sentence. Enforcement belongs in the `make` target that performs the mutation,
so it cannot be skipped by running the underlying command directly.

## Unknown keys in a contract fixture, silently dropped

Go's `encoding/json` discards keys with no matching struct field without an
error. A fixture that carries a block the loader has no field for therefore
seeds nothing, reports success, and contradicts its own validation doc.

Concrete failure: `operatorRosterContract`
(`backend/cmd/seed-roster-real/main.go:561-589`) declares only `Schema`,
`OperatorCapacity`, `SourceScope`, `Operators`, and
`DefaultOperatorAssignment`. The fixture's top-level `directors` (Chandrakant)
and `leadership_full_access` (five CXO emails) in
`fixtures/vaccination-cpt-operator-drive-2026-07-23/cpt-operator-roster.json`
have no fields and are dropped. The documented CPT reseed seeds neither the
director nor the CXO grants, and the "director has zero execution capacity"
property holds only incidentally — because he is never touched — rather than as
an enforced invariant.

Rule: every loader of a committed fixture or seed contract must use
`Decoder.DisallowUnknownFields()` (or an explicit schema validation pass), so an
unconsumed block fails loud at load time. This is mechanically checkable: a
guard can enumerate `json.Unmarshal`/`json.NewDecoder` call sites under
`backend/cmd/**` that read a path under `fixtures/` and require
`DisallowUnknownFields`. Adding a field to make the key consumed is the fix;
leaving the key present and unread is not.


<!-- Coupling review 2026-07-20: the counts (approval, department_module_grants) and feed_direction migrations 000009-000015 plus the seed-roster-real department-module-grants write were reviewed against the vaccination HRMS seed source. They are orthogonal to it (counts/feed tables, not the vaccination roster source), so no fixture/source-data change is required. Recorded in fixtures/vaccination-hrms-source-full/manifest.json -> seed_contract_coupling_reviews. -->
<!-- Coupling review 2026-07-22: adult ET+TT dose-2 post-seed invariant and shed partition name-pattern normalization do not change raw fixture bytes. They change transform/generation validation: partition-bearing shed labels normalize to physical shed + partition metadata, and accepted et_tt_adult_w1 must have same-goat et_tt_adult_w2 work before handoff. -->
<!-- Coupling review 2026-07-22: ceo_ai reporting migrations 000024-000027 create read-only SQL views (ceo_ai.vaccination_operator_status, vaccination_shed_status, vaccination_dose_pickup, action_center) that query canonical vaccination/obligation/workforce tables. They do not modify the seed source data, HRMS schema, vaccination protocol rules, or SOP contracts. The reported reads stay tenant-scoped, indexed, and bounded by the 5k-50k envelope exemption for canonical-read screens; they are not full-tenant recomputes or projection-drift anti-patterns. -->
<!-- Coupling review 2026-07-22: CPT-only operator-drive rehearsal role mapping does not change raw fixture bytes. It changes seed interpretation and validation: Amit, Darshan, and Sagar must seed as equal manager-tier vaccination_operator_* positions with execute duty and source week-offs, and drive splitting must use DB-backed availability for the planned date. -->

<!-- Coupling review 2026-07-23: the seed-roster-real operator-roster overlay does not alter scale posture. It is a bounded per-park recast of a fixed set of resolved roster seats into per-person vaccination_operator_<name> positions during seeding (no per-row I/O, no request-path query, no new read model); drive splitting continues to read DB-backed operator availability. No scale anti-pattern is introduced or relaxed. -->
<!-- Coupling review 2026-07-23: vaccination operator assignment config is scheduler-consumed, not config-only. The bounded point lookup filters the already-loaded daily operator-capacity candidate set by active_operators_per_day/default_operator_code and shift fallback identity; it does not introduce tenant-wide scans, per-animal reads, or time-of-day drive splitting. -->

<!-- 2026-07-23 operator-config auto-cascade: migration 000036 adds obligation_operator_config_replan_watermarks, an operational idempotency-watermark table (no seed data / no HRMS-source rows; consumer-only). No fixture bytes change. -->

<!-- Coupling review 2026-07-24: CPT operator Android login provisioning is a seed/auth contract, not a scale-path change. Every executable vaccination operator must receive a distinct Firebase/Auth email-password identity from cpt-operator-roster.json operators[].email_hint after DB seed; shared operator credentials, founder/CXO Android execution shortcuts, and committed plaintext passwords are forbidden. No new request-path query, projection, or tenant scan is introduced. -->
<!-- Coupling review 2026-07-24: CPT no-PPR 2026 seed publication is a bounded seed-packet rule filter, not a direct obligation mutation or request-path scale change. Canonical source/history mapping remains available; the CPT operator-drive target simply does not publish PPR rules into the 2026 packet proof. -->
<!-- Coupling review 2026-07-24: CPT adult campaign coalescing and seed_catchup_overrides do not introduce a request-path scale pattern. Adult same-partition grouping is in generation/packing, and the date-scoped capacity override is a bounded operator/date lookup for an explicit seed catch-up row, not a tenant-wide recompute or general cap escalation. -->
<!-- Coupling review 2026-07-24: CPT clean-DB roster bootstrap is seed-time bounded work over source-present centers only. Adult same-business-day history cutoff changes generation evidence timing, not request-path reads. No new tenant-wide request scan, projection workaround, or general capacity escalation is introduced. -->
