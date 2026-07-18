# Vaccination Handoff - 2026-07-19 00:37 IST

This handoff exists because the vaccination clustering thread kept compacting and
repeating old status. Treat this file as the source of truth for the next
session. Do not infer current status from screenshots or compacted chat.

## Current live state

- Repo: `/Users/ravi/mesha/goatos`
- Branch: `fix/vaccination-event-guardrails-80842179`
- Base commit: `00b88d1ae1a0e9164a78e9bbbf291ba525d9ea5b`
- Base commit title: `fix: close vaccination terminal and capacity drift`
- Worktree: dirty
- Pushed: no
- Reseeded after this dirty work: no
- Tests: red
- Latest explicit user instruction: stop implementation and create a handoff
  document with every bug/fix/partial state.

Do not claim this is done. Do not reseed before the code compiles, focused tests
pass, guards pass, the branch is pushed to `main`, and a final review pass is
clean.

## Dirty files at handoff start

Tracked changes:

```text
.claude/settings.json
apps/admin-web/features/config/rule-dsl.ts
backend/cmd/domain-event-consumer/main.go
backend/cmd/obligation-sweeper/main.go
backend/internal/bootstrap/api.go
backend/internal/domainconsumer/wiring/bus.go
backend/internal/kernelstages/bus.go
backend/internal/kernelstages/obligation_sweeper.go
backend/internal/obligation/adapters/postgres/combo_align_defer_finalize_integration_test.go
backend/internal/obligation/adapters/postgres/combo_batch_projection_review_integration_test.go
backend/internal/obligation/adapters/postgres/repository.go
backend/internal/obligation/adapters/postgres/visit_shot_lock.go
backend/internal/obligation/app/combo_align.go
backend/internal/obligation/app/combo_align_test.go
backend/internal/obligation/app/drive_planner.go
backend/internal/obligation/app/drive_planner_config.go
backend/internal/obligation/app/drive_planner_test.go
backend/internal/obligation/app/park_consolidation.go
backend/internal/obligation/app/preflight.go
backend/internal/obligation/app/rv_guard_test.go
backend/internal/obligation/app/sweep_session.go
backend/internal/obligation/app/sweep_session_test.go
backend/internal/obligation/app/sweeper.go
backend/internal/obligation/app/sweeper_test.go
backend/internal/obligation/app/visit_shot_lock.go
backend/internal/obligation/domain/types.go
backend/internal/vaccination/app/generation.go
backend/internal/vaccination/app/generation_test.go
context/architecture/domain-event-registry.json
context/execution/vaccination-edge-case-code-coverage.md
tools/agent-hooks/check-domain-event-architecture.mjs
```

Untracked changes:

```text
.claude/settings.json.bak
.claude/skills/debug-issue/
.claude/skills/explore-codebase/
.claude/skills/refactor-safely/
.claude/skills/review-changes/
.cursor/mcp.json
.gemini/
.opencode.json
.qoder/
backend/internal/calendar/app/obligation_lifecycle_handler.go
backend/internal/calendar/app/obligation_lifecycle_handler_test.go
backend/migrations/postgres/000002_vaccination_capacity_overflow_policy.sql
```

Important partial edit after the status snapshot: `backend/internal/obligation/domain/types.go`
has `Reason string` added to `ObligationRef`, but the reason is not fully wired
from SQL into generation. Finish this deliberately or revert it deliberately.
Do not leave it half-wired.

## Non-negotiable vaccination rules

These are product rules, not suggestions.

1. A vaccination drive is park/date level. Shed count is display/context only.
2. `capacity.max_per_day` means total vaccine administration cells for one
   park/date drive across all sheds, vaccines, batches, and rules.
3. One goat with two vaccines consumes two capacity cells.
4. The cap is not per shed, not per animal, not per obligation row if rows can
   represent different cell counts.
5. Last-safe-day overflow may exceed cap only when moving later would cross that
   goat's own medical safe window or the one-time due+7 batching hold boundary.
6. The planner must maximize same-park animals within each goat's medical safe
   window and due+7 hold. Avoidable 1-2 animal drives are bugs.
7. A micro-drive is valid only when every compatible same-park clubbing option
   inside the legal window/hold has failed.
8. Normal eligible goats must be scheduled. No manual-review escape hatch for
   ordinary planning.
9. Sick, ICU, recovering, or pregnant-hold goats that become eligible again must
   get fresh schedulable work from the return/requalification point.
10. Dead, sold, culled, transferred, or lost goats are terminal and must not get
    future work.
11. Live/in-care goats must have park and shed. Seed/import must fill missing
    park/shed. Do not normalize "goat without shed" as a planning case.
12. Reseed proof must include source seed, HRMS owner mapping, stages/eligibility,
    generation, and sweeper. If any stage is skipped, proof fails.
13. Event registry/guards must prove a real producer and real deployed runtime
    subscriber. A JSON entry, string mention, or no-op handler is not proof.

## Subagent/reviewer truth

- A direct subagent spawn failed with `agent thread limit reached`.
- Visible sidebar names such as Raman, Hume, Euclid, Mendel, Arendt, and
  Poincare were stale slots and not proof of active work.
- Two separate review worktrees/tasks did return actionable results. Their
  findings are captured below as "Lifecycle review" and "Batching/capacity
  review".
- Do not say "agents are working" unless the actual tool returns live thread IDs
  or final results.

## Tests already run

From `/Users/ravi/mesha/goatos/backend`:

```bash
go test ./internal/obligation/app ./internal/vaccination/app ./internal/calendar/app ./cmd/domain-event-consumer ./internal/kernelstages ./internal/domainconsumer/wiring
```

Result:

```text
ok   github.com/vgoats/goatos/backend/internal/obligation/app 0.554s
FAIL github.com/vgoats/goatos/backend/internal/vaccination/app
ok   github.com/vgoats/goatos/backend/internal/calendar/app (cached)
ok   github.com/vgoats/goatos/backend/cmd/domain-event-consumer 0.615s
ok   github.com/vgoats/goatos/backend/internal/kernelstages 1.238s
?    github.com/vgoats/goatos/backend/internal/domainconsumer/wiring [no test files]
```

Failing test:

```text
TestGenerateReplayTerminalObligationDoesNotDelayCoDueVaccine/canceled:
Goat Pox due = 2026-08-28, want 2026-07-31.
A canceled PPR must not delay it.
```

Meaning: the dirty successor patch is too broad. "Canceled" can mean different
things. A canceled row because a goat left and came back needs a successor. A
canceled terminal/history row must not become a spacing anchor and must not
delay a co-due vaccine.

The event guard self-test passed once before the latest `Reason` edit:

```bash
node tools/agent-hooks/check-domain-event-architecture.mjs --self-test
```

Re-run it after all event/guard edits.

## Dirty work already attempted

### Capacity and batching

Files involved:

```text
backend/internal/obligation/app/drive_planner_config.go
backend/internal/obligation/app/visit_shot_lock.go
backend/internal/obligation/adapters/postgres/visit_shot_lock.go
backend/internal/obligation/app/sweep_session.go
backend/internal/obligation/app/sweeper.go
backend/internal/obligation/app/park_consolidation.go
backend/internal/obligation/app/preflight.go
backend/internal/obligation/app/combo_align.go
backend/internal/obligation/adapters/postgres/repository.go
backend/internal/obligation/domain/types.go
```

Intent:

- Add a shared park/date drive-capacity ledger.
- Count dose cells, not animals.
- Make sweeper, park consolidation, combo alignment, preflight, and DB batch
  writer use the same capacity semantics.
- Return exact attached obligation IDs from the DB writer so hold/cap metadata
  is recorded only for rows actually attached.

Current risk:

- This is not verified.
- Reviewer found more capacity bugs in the dirty diff. See VAXCAP list below.
- `planned_quantity` changes may still use wrong unit or wrong SQL casts.

### Returning goat successor work

Files involved:

```text
backend/internal/vaccination/app/generation.go
backend/internal/vaccination/app/generation_test.go
backend/internal/obligation/domain/types.go
```

Intent:

- If a goat requalifies after prior canceled work, create a deterministic
  successor obligation instead of reusing the old idempotency key and no-oping.

Current risk:

- Dirty patch is too broad.
- It needs cancellation reason/evidence, not just `status = canceled`.
- It must support both scheduled and clinically deferred successors.
- It must not turn terminal canceled vaccine history into a spacing anchor.

### Event guard and runtime wiring

Files involved:

```text
context/architecture/domain-event-registry.json
tools/agent-hooks/check-domain-event-architecture.mjs
backend/internal/calendar/app/obligation_lifecycle_handler.go
backend/internal/calendar/app/obligation_lifecycle_handler_test.go
backend/internal/bootstrap/api.go
backend/internal/domainconsumer/wiring/bus.go
backend/internal/kernelstages/bus.go
backend/cmd/domain-event-consumer/main.go
```

Intent:

- Stop accepting producer-as-consumer registry lies.
- Stop accepting event string mentions as proof.
- Wire real runtime subscribers in API and worker buses.
- Remove or correct the fake `vaccination.manual_campaign.requested` event.

Current risk:

- Lifecycle review says the new handler is effectively no-op and guard still
  accepts fake consumers.
- Manual campaign still appears as an orphan runtime event in some paths.

### Frontend stale enum

Files involved:

```text
apps/admin-web/features/config/rule-dsl.ts
```

Intent:

- Replace retired `split_within_safe_window_then_mark_needs_review` with
  `split_within_safe_window_last_safe_may_exceed_cap`.

Current risk:

- Review says this is mostly fixed.
- Need grep and real publish/config test.

### Forward migration

Files involved:

```text
backend/migrations/postgres/000002_vaccination_capacity_overflow_policy.sql
```

Intent:

- Existing dev/stg DBs must migrate from old overflow enum to new enum.

Current risk:

- Latest batching review says migration updates rows before dropping old check
  constraint, so upgrade aborts. Fix order.

## Open bugs from latest lifecycle review

### LIFE-001 / P0: canceled work still gets no successor when returning goat is clinically deferred

Where:

```text
backend/internal/vaccination/app/generation.go:1445
backend/internal/vaccination/app/generation.go:1499
```

Why:

Successor creation is gated by `!deferred`, and helper forces `scheduled`.
A requalified sick/ICU goat keeps only its canceled row instead of receiving
fresh deferred work.

Fix:

Create a successor using the newly calculated status, either `scheduled` or
`deferred`, preserve the canceled predecessor, and record lineage/status
history.

Guard:

Real Postgres park A -> B -> A and ineligible -> eligible round trips covering
normal -> scheduled and clinically held -> deferred successors.

### LIFE-002 / P1: batched roster rows use planned date for lookup but return old obligation date

Where:

```text
backend/internal/calendar/adapters/postgres/targets.go:41
backend/internal/calendar/adapters/postgres/targets.go:68
apps/admin-web/features/calendar/calendar-event-drawer.tsx:457
```

Why:

A held drive moved from D to D+7 appears on D+7, but every animal row displays
`oi.due_at` D. Ordering and deduplication also use that obsolete date.

Fix:

Return batched `scheduled_at = planned_date`, falling back to obligation
`due_at` only for unbatched work.

Guard:

DB-backed D/D+7 test asserting event date, roster membership, returned target
timestamp, and rendered date all equal D+7.

### LIFE-003 / P1: event guard still accepts fake no-op consumer

Where:

```text
backend/internal/calendar/app/obligation_lifecycle_handler.go:25
backend/internal/calendar/app/obligation_lifecycle_handler.go:30
tools/agent-hooks/check-domain-event-architecture.mjs:144
```

Why:

Canceled/rescoped events are subscribed, but `HandleEvent` immediately returns.
The guard finds event token plus `Subscribe(` and passes.

Fix:

Register an effect-bearing consumer and deployed wiring, or stop claiming
canonical SQL reads are event consumers. Validate exact subscribed event and
measurable consumer effect.

Guard:

Adversarial no-op, wrong-event subscription, orphan handler, and
producer-to-deployed-consumer E2E cases.

### LIFE-004 / P1: manual campaign remains orphan runtime event

Where:

```text
backend/internal/vaccination/app/generation_handler.go:52
backend/internal/vaccination/app/generation_handler.go:190
backend/internal/vaccination/adapters/http/handler.go:199
```

Why:

HTTP directly invokes generation; no runtime path publishes
`vaccination.manual_campaign.requested`. Runtime bus builders still subscribe
its handler.

Fix:

Preferred: remove event constant, handler, registrations, and direct-handler
tests. Alternative: publish the event transactionally and restore registry plus
producer-to-consumer proof.

Guard:

Every runtime subscription must correspond to a registered event with a proven
durable producer and deployed consumer.

### LIFE-005 / P2: completed drive rosters still labeled as scheduled doses

Where:

```text
backend/internal/calendar/adapters/postgres/canonical_read.go:632
backend/internal/calendar/adapters/postgres/canonical_read.go:983
```

Why:

`scheduled_count` is filtered, but `summary_primary` uses total roster
`target_count` and always calls it scheduled. Completed work can still appear as
scheduled through API/mobile contract.

Fix:

Derive copy/counts from status-specific buckets. Keep `target_count` only as
total roster size.

Guard:

Completed-only, completed+deferred, and canceled+deferred response assertions,
including summary copy.

## Open bugs from latest batching/capacity review

### VAXCAP-001 / P0: upgrade migration violates old constraint before removing it

Where:

```text
backend/migrations/postgres/000002_vaccination_capacity_overflow_policy.sql:5
```

Why:

Existing DBs still constrain `overflow_policy` to retired value. Migration
writes new value before dropping old constraint, so deployment aborts.

Fix:

Drop old constraint first, update rows, then add new constraint.

Guard:

Upgrade test from pre-`00b88d1a` schema with old-policy row.

### VAXCAP-002 / P1: new capacity and combo SQL cannot execute

Where:

```text
backend/internal/obligation/adapters/postgres/visit_shot_lock.go:220
backend/internal/obligation/adapters/postgres/repository.go:2107
backend/internal/obligation/adapters/postgres/repository.go:2240
```

Why:

`planned_quantity` is numeric, but dirty queries evaluate
`COALESCE(planned_quantity, '')`. PostgreSQL rejects empty string as numeric.

Fix:

Operate on numeric value directly, for example
`COALESCE(planned_quantity, 0)::int`, with explicit validation for non-integral
quantities.

Guard:

Real Postgres tests for `CountDriveCellsForParkDate` and both combo-list methods
with null and non-null quantities.

### VAXCAP-003 / P1: mixed-rule batches persist wrong administration-cell total

Where:

```text
backend/internal/obligation/adapters/postgres/repository.go:2531
backend/internal/obligation/adapters/postgres/repository.go:2644
backend/internal/obligation/adapters/postgres/repository.go:2684
```

Why:

Writer divides total planned cells by selected row count, rounds up, then
multiplies that average by every attached row. A batch with one-cell and
two-cell rules is over- or under-counted.

Fix:

Carry exact cell count per obligation and sum only successfully attached IDs.
Never reconstruct cells from an average row quantity.

Guard:

Postgres tests for `[2,1,1]` mixed-rule creation, partial attachment, and
same-batch retry. Assert exact totals after each operation.

### VAXCAP-004 / P1: started/completed administrations disappear from daily capacity

Where:

```text
backend/internal/obligation/adapters/postgres/visit_shot_lock.go:235
```

Why:

Park/date counter includes only `planned` batches. Changing a batch to
`in_progress` or `completed` frees its cells, allowing later over-planning.

Fix:

Count every non-canceled/non-superseded batch. Completed work must use canonical
administered-cell totals and continue consuming that date capacity.

Guard:

Status-matrix integration test covering planned, in_progress, completed,
canceled, and superseded batches across multiple vaccines.

### VAXCAP-005 / P1: park date selection maximizes before capacity, producing avoidable split drives

Where:

```text
backend/internal/obligation/app/park_consolidation.go:112
backend/internal/obligation/app/park_consolidation.go:145
backend/internal/obligation/app/park_consolidation.go:352
```

Why:

Candidate dates are ranked using uncapped animals. Once the chosen date retains
`minMergeTargets` after capacity, search stops even when a later safe date fits
the entire group.

Fix:

Score every feasible date after persisted park/date cell capacity and use
earliest date only as tie-breaker. Mirror in preflight.

Guard:

D1 has two free cells, D2 has ten; ten safe animals must produce one D2 drive,
not 2+8 drives.

### VAXCAP-006 / P1: movable work can consume capacity before last-safe work

Where:

```text
backend/internal/obligation/app/park_consolidation.go:438
backend/internal/obligation/app/sweeper.go:870
```

Why:

Rows are admitted in existing rule/priority order. A movable row can fill cap
first, forcing a last-safe row into overflow even though moving the first row
would keep all goats legal without exceeding capacity.

Fix:

Reserve capacity for immovable safe/hold-boundary cells first, then fill
remaining capacity with movable cells. Overflow only when immovable cells
themselves exceed the cap.

Guard:

Mixed movable/last-safe test at cap one, plus all-last-safe case that
legitimately exceeds cap.

### VAXCAP-007 / P2: preflight forgets earlier capacity claims

Where:

```text
backend/internal/obligation/app/preflight.go:302
backend/internal/obligation/app/preflight.go:354
backend/internal/obligation/app/visit_shot_lock.go:160
backend/internal/obligation/app/sweep_session.go:198
```

Why:

Preflight records claimed cells, but the next group's refresh overwrites session
total with persisted DB cells. Since preflight is write-free, earlier claims are
not persisted and vanish.

Fix:

Seed each park/date baseline once during preflight and accumulate claims without
resetting them.

Guard:

Two versions/groups individually fit but jointly exceed cap; preflight must
match real sweep selection.

### VAXCAP-008 / P1: reseed clubbing proof never proves capacity

Where:

```text
tools/dev/check-vaccination-drive-clubbing-proof.sh:66
tools/dev/check-vaccination-drive-clubbing-proof.sh:103
tools/dev/check-vaccination-drive-clubbing-proof.sh:111
```

Why:

Proof counts distinct animals in planned batches. It never reads `max_per_day`,
sums administration cells across vaccines/batches, includes started/completed
work, or validates last-safe overflow.

Fix:

Add park/date cell-ledger query against effective published capacity and require
per-goat boundary evidence for every overflow cell.

Guard:

Adversarial fixtures for multi-vaccine, multi-dose, multi-batch over-cap failure
and legitimate last-safe overflow success.

## Earlier review findings and current status

### Codex VAX30-001: requalified goats lose vaccination work

Status: partially fixed and still open.

Dirty patch mints successors for some canceled replay cases. It is not correct
yet because:

- deferred returning goat still does not get fresh deferred successor;
- generic canceled terminal row can become a spacing anchor;
- `ObligationRef.Reason` was started but not fully wired.

### Codex VAX30-002: published daily capacity not enforced

Status: partially fixed and still open.

A shared park/date ledger exists in dirty code, but latest VAXCAP review found
the ledger nonfunctional/incomplete in SQL, persisted quantity, status counting,
date scoring, immovable priority, preflight, and proof.

### Codex VAX30-003: retired frontend overflow enum

Status: likely fixed, verify.

Dirty patch changed frontend default/fallback to
`split_within_safe_window_last_safe_may_exceed_cap`. Need grep and publish test.

### Codex VAX30-004: domain-event guard false green

Status: partially fixed and still open.

Producer-as-consumer registry issue was addressed in dirty edits, but lifecycle
review found no-op consumer and manual-campaign orphan event still remain.

### Codex VAX-1: held drives resolve roster on wrong date

Status: original finding stale, but remaining target date bug open.

Membership lookup now prefers `planned_date`. Returned roster row timestamp still
uses old obligation due date.

### Codex VAX-2: mixed batch retries double-count hold

Status: likely fixed, verify.

Dirty DB writer returns exact attached IDs and should record hold metadata only
for newly attached IDs. Needs regression test.

### Codex VAX-3: cap walking stops before enough animals

Status: original version stale, but maximum-output capacity scoring still open.

The walk continues past first under-threshold date, but VAXCAP-005 says it still
scores dates before capacity and can choose avoidable split drives.

### Codex VAX-4: completed batches make deferred-only work appear scheduled

Status: status bucket likely fixed, presentation still open.

`scheduled_count` no longer counts completed/canceled, but summary copy can still
say scheduled doses from total roster target count.

### Claude capacity findings

Status:

- Park consolidation cap wrong unit: partially fixed, VAXCAP still open.
- Combo align missing capacity: partially fixed, VAXCAP still open.
- Baseline migration edited without forward migration: forward migration exists
  but is broken, VAXCAP-001.
- Multi-park actor sees only one park: pre-existing auth UX/API issue, not part
  of current vaccination clustering closure unless explicitly scoped.
- Dead combo clamp: low/confusing but harmless if publish rejects non-2.

## Exact next steps

Do these in order. Do not reseed or push until tests/guards pass.

1. Run status and review diff.

```bash
cd /Users/ravi/mesha/goatos
git status --short
git diff --stat
git diff --check
```

2. Fix generation successor semantics.

- Wire cancellation reason/evidence into `ObligationRef`.
- Mint successor only for requalification/shift/return reasons.
- Use newly calculated status: scheduled or deferred.
- Do not let terminal canceled history rows affect spacing.
- Add real Postgres guards for normal return, deferred return, and terminal
  canceled non-spacing.

3. Fix capacity semantics end to end.

- Rename or comment `MaxGoatsPerDrive` if it carries cells. Prefer a clearer
  `MaxDriveCells` semantic if possible.
- SQL must use numeric `planned_quantity` correctly.
- Persist exact cell totals per successfully attached obligation.
- Count planned, in_progress, and completed capacity, excluding canceled and
  superseded.
- Score candidate dates after capacity, not before.
- Reserve capacity for immovable/last-safe cells before movable cells.
- Preflight must accumulate claims and match real sweep.
- Reseed proof must check capacity cells and last-safe overflow evidence.

4. Fix calendar target timestamp and summary label.

- Return batched `scheduled_at = planned_date`.
- UI drawer should display scheduled date for batched rows.
- Summary copy must come from status buckets, not total target count.

5. Fix event registry and runtime truth.

- Remove `vaccination.manual_campaign.requested` as an event unless a real
  durable producer is added.
- No no-op consumers in registry.
- Guard must verify exact subscription to the registered event and measurable
  effect, not string presence.
- Runtime buses must match registry.

6. Fix migration.

- Drop old constraint first.
- Update rows.
- Add new constraint.
- Add upgrade test if harness exists.

7. Format and run focused checks.

```bash
cd /Users/ravi/mesha/goatos
gofmt -w backend/internal/obligation backend/internal/vaccination backend/internal/calendar backend/internal/bootstrap backend/internal/domainconsumer backend/internal/kernelstages backend/cmd
node tools/agent-hooks/check-domain-event-architecture.mjs --self-test
node tools/agent-hooks/check-domain-event-architecture.mjs
rg "split_within_safe_window_then_mark_needs_review" apps backend context tools contracts
rg "vaccination.manual_campaign.requested" backend context tools
cd backend && go test ./internal/vaccination/app -run 'TestGenerate.*Canceled|TestGenerate.*Successor|TestGenerateReplayTerminal' -count=1
cd backend && go test ./internal/obligation/app ./internal/vaccination/app ./internal/calendar/app ./cmd/domain-event-consumer ./internal/kernelstages ./internal/domainconsumer/wiring -count=1
```

8. Only after green: commit, push to `main`, run another latest-30-commits review
   against pushed `main`, fix any real findings, then reseed/proof locally.

## Reseed/proof requirement after push

After all code/guards are green and pushed:

1. Sync canonical checkout to pushed `main`.
2. Start backend/frontend from canonical checkout, not temp worktree.
3. Run full seed chain: source seed, HRMS/owner mapping, animal stages, eligibility,
   vaccination generation, sweeper.
4. Run capacity-aware drive-clubbing proof.
5. Query small-drive report. Remaining <=2 animal drives must include evidence
   that no same-park compatible clubbing exists inside each goat's legal window
   and due+7 hold.
6. Verify Chrome local frontend on `http://127.0.0.1:3300/`, not staging.

## What not to do

- Do not push the current dirty state.
- Do not reseed this dirty state.
- Do not say all agents are active based on stale sidebar slots.
- Do not accept a guard that only finds strings.
- Do not treat "manual review" as a normal vaccination planning outcome.
- Do not allow goats without sheds as normal data.
- Do not count capacity by animal, shed, row, or batch. Count dose cells for the
  whole park/date drive.
- Do not let compaction restart old loops. Read this file, run `git status`, and
  continue from live state.
