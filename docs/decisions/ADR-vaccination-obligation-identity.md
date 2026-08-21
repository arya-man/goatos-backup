# ADR — vaccination obligation identity should not include the protocol version

**Status:** proposed
**Date:** 2026-08-20
**Baseline:** `origin/main` @ `be23d7de5`; staging data via a read-only clone of `goatos-stg`
**Reviewers:** maintainer; a second agent (Codex) independently verified and corrected an earlier
draft of this argument. Both the correction and the surviving claim are recorded below.

---

## Context

A vaccination obligation is identified by a hash that includes the protocol version and the rule
that produced it — `internal/vaccination/app/generation.go:1605`:

```go
key := obligationKey(tenantID, versionID, rule.RuleID, "goat", g.GoatID, keyDueToken,
                     strconv.Itoa(int(rule.Sequence)))
```

Publishing a new plan version creates a new `protocol_version_id`, and its rules are inserted as
fresh rows with new `rule_id`s. Both inputs to the key therefore change on every publish.

The business meaning of the row does not change: *this goat needs this dose of this vaccine,
possibly on a different date*. The identity changes anyway.

### What the current publish path does

`repository.go:908` — publishing a vaccination matrix **atomically retires** any overlapping
published version, in the same transaction, recording audit and outbox rows
(`retirePublishedVaccinationMatrixOverlapsTx`). That machinery is correct and already exists.

What it does **not** do is reconcile the retired version's still-open `obligation_instances`.
No cancellation, no re-pointing, no supersede. Scanning `generateForVersionWithRun` — the
function the `protocol.version.published` handler calls — finds no call to any cancel function.

The only code that supersedes obligations across versions is
`CancelOpenVaccinationObligationsForGoatExceptVersions`
(`internal/obligation/adapters/postgres/repository.go:1640`). Its sole non-test caller is
`generateForGoat` (`generation.go:1930`) — the **per-goat** path, reached from `goat.created` and
from stage / location / health / reproductive rechecks. Never from publish.

Its predicate is well chosen and worth preserving:

```sql
AND oi.status IN ('scheduled', 'due', 'deferred')
AND NOT (oi.protocol_version_id = ANY($3::uuid[]))
```

Terminal and in-progress work is structurally excluded. Completed and canceled obligations can
never be moved by it.

---

## Evidence

Stated separately, because one is an observation and one is an induced experiment. An earlier
draft of this argument blurred the two.

### Observed in staging — no orphans today

| version | status | obligations |
|---|---|---|
| v1 (retired, 21 rules) | completed | 148 |
| v2 (published, 17 rules) | scheduled | 6,852 |
| | deferred | 56 |
| | completed | 5,644 |
| | canceled | 3,049 |

**Zero open obligations sit on a retired version.** v1 holds 148 rows, all `completed`, none
`canceled`.

### Why that is not a safety proof

v1 is clean because it had nothing open to clean. Every one of its 148 obligations was already
`completed` when it was retired. Nothing reconciled anything.

Supporting evidence: cancellation reasons are written by `recordCanceledObligationRows` as
`payload = {"reason": …}` into `obligation_status_events`. The reason histogram in staging:

```
adult_campaign_date_realigned            3802
defer_state                              1453
(blank)                                  1185
recovered_from_defer_state                 40
operator_leave_cancel_aug14_darshan        10
ineligible_after_exit                       7
vaccine_history_outranks_adult_campaign     2
```

`version_no_longer_effective_after_recheck` — the supersede reason — appears **zero times**.
The supersede path has never executed in this database.

> Correction accepted from review: `obligation_instances` itself has no reason column. The
> histogram above comes from `obligation_status_events`, which is where the writer records it.

So staging demonstrates that this case has **never been exercised**, not that it is handled.
The single version transition that has occurred was trivial.

### Induced — the non-trivial case, on an disposable staging clone of staging

Deliberately created, because staging has never produced it. Retire v2, publish v3 with ET+TT
revaccination changed 182 → 91 days, run the real `GenerationService`:

| | before | after |
|---|---|---|
| v2 scheduled / deferred | 6,852 / 56 | **6,852 / 56 — unchanged, now on a retired version** |
| v3 scheduled / deferred | — | 7,209 / 62 |
| completed / canceled | 5,644 / 3,049 | byte-identical |

**6,908 open obligations left behind on the retired version.** 6,232 goat+dose pairs existed
under both versions at once; where the rule had not changed, the duplicate carried an identical
date (`hs_revac` 2027-02-20 twice).

> This 6,908 figure is **induced-risk evidence, not a current staging fact.** An earlier draft
> stated it in a way that read as a production observation. It is what happens if a plan with
> open work is superseded today — and staging currently has exactly 6,908 open rows on v2.

Not user-visible: every read path filters `pv.status='published'`
(`internal/calendar/adapters/postgres/canonical_read.go:199,384,648,1066,1810`) and the sweeper
sweeps only the published version. Operators saw zero duplicates. The rows are invisible,
permanent, and counted by anything that forgets that join.

---

## Decision

**Remove `protocol_version_id` and `rule_id` from obligation identity. Keep
`protocol_version_id` on the row as provenance.**

```
now      key = hash(tenant, VERSION, RULE_ID, goat, due, sequence)
proposed key = hash(tenant, goat, vaccine_code, dose_code, occurrence_anchor)
```

Identity describes *the animal and the dose*. The version records *which plan last produced or
updated this row* — provenance, not identity.

Consequence:

| | today | with stable identity |
|---|---|---|
| vaccine you changed | new rows, old orphaned | same row, date updated |
| vaccines you did not touch | **also new rows** | untouched |
| completed / canceled | safe | safe |
| in-flight drive | safe | safe |
| orphans per publish | up to the open cohort | none |

Supersede-on-publish then becomes unnecessary: there is nothing to supersede if rows persist.

### Rejected — one plan per vaccine

Cross-vaccine rules are statements about the *set*: live→live 28 days, live→killed 14 days,
max 2 vaccines per visit, wave composition. Split the plan into per-vaccine documents and there
is no single coherent answer to "what is the spacing policy right now" — a coordinating document
would be needed, reintroducing the same problem one level up.

**Keep one versioned bundle per scope.** That part of the design is correct.

### Also keep

- Tenant-default plus park-override scoping, most-specific-wins, never both. Guarded by
  `TestListEffectiveVaccinationVersionsForGoatPicksParkOverride`.
- Published-version immutability, the one-live-plan-per-scope exclusion constraint, and
  rules-attach-to-drafts-only. Three guardrails; all three caught bad SQL while this was written.
- The cancel predicate's exclusion of terminal and in-progress states.

---

## Drives are a separate concern

A drive should not care which protocol version created an obligation. It should care about
goat + vaccine + dose + due window + eligibility + spacing.

```
vaccination_drive        drive_id, date, park/shed, status
vaccination_drive_items  drive_id, obligation_id, goat_id, vaccine_code, dose_code
```

Grouping happens at assignment time, against the **currently published** plan's safety rules —
never by merging vaccine obligations into one row. One drive can carry several vaccines for one
goat when the plan permits it.

Worked example of the intended semantics:

```
before      Goat 101 ET+TT due 25 Aug   obligation A
            Goat 101 PPR   due 25 Aug   obligation B

CEO changes the ET+TT interval

after       Goat 101 ET+TT due 30 Aug   obligation A   (same id, date updated)
            Goat 101 PPR   due 25 Aug   obligation B   (untouched)

25 Aug drive → PPR only.  30 Aug drive → ET+TT.
```

Stable identity does not mean ignoring the new plan. It means the business task survives a
republish.

---

## Migration — never rewrite an existing obligation_id

Staging holds **6,908 open scheduled/deferred obligations**. Drive rosters, operator to-do lists,
queued offline completions, proof submissions, dashboard counts and completion-history joins all
reference those rows by `obligation_id`. A wipe-and-regenerate migration would break every one of
them. This is a controlled migration, not a rebuild.

**Rule: `obligation_id` is never rewritten, reassigned, or deleted for non-terminal work.**

That is free, because identity and the generation key are already separate columns:

```
obligation_id    uuid  PRIMARY KEY          ← referenced by drives, mobile, proof. NEVER CHANGES.
idempotency_key  text  UNIQUE (tenant, key) ← what generation matches on. This is what we change.
```

### Two uniqueness paths, not one

Both bake the version into identity, and both must be handled:

```sql
obligation_instances_idempotency_unique  UNIQUE (tenant_id, idempotency_key)
obligation_instances_dup_guard           UNIQUE NULLS NOT DISTINCT
    (tenant_id, protocol_version_id, rule_id, target_type, target_id, due_at)
```

`dup_guard` is why the same goat+dose could legitimately exist twice under two versions — the
version is part of the key, so the rows are not duplicates by its definition. It will also
**block an in-place `due_at` update** if the new date collides with an existing row. Changing
only the hash and leaving `dup_guard` alone will not deliver stable identity.

### Sequence

1. **Add** a nullable `stable_obligation_key text` column. No constraint yet.
2. **Backfill** it for existing rows from goat + vaccine + dose + occurrence anchor.
   `obligation_id` untouched.
3. **Measure duplicates** the new key would collapse, before enforcing anything. Staging's 6,908
   open rows are the population to check. Terminal rows are expected to collide across historic
   versions and must be excluded from any uniqueness rule.
4. **Add uniqueness on open logical work only** — partial, e.g.
   `WHERE status IN ('scheduled','due','deferred')` — once step 3 is clean.
5. **Relax `dup_guard`** to drop `protocol_version_id` and `rule_id`, or replace it with the
   partial constraint from step 4. Without this, in-place date updates can still fail.
6. **Switch generation** to match on `stable_obligation_key` instead of the version-bearing hash.
7. **Change republish behaviour**: update the existing row's `due_at`, `window_start`,
   `window_end` and `protocol_version_id` (provenance) in place. Do not insert a new row.
8. **Terminal rows stay historical.** `completed` and `canceled` are never re-pointed or updated —
   they record what happened under the plan that was live at the time.

### What survives

- Every existing `obligation_id` remains valid. Drive rosters, mobile queues and proof
  submissions continue to resolve.
- Existing scheduled vaccinations keep their identity; only their dates move, and only when the
  plan actually changed them.
- Behaviour changes for **future regeneration**, not for work already in flight.

### Rollback

Steps 1–3 are additive and reversible. The irreversible point is step 6; until then generation
still matches on the old hash and the new column is inert. Land 1–5, verify the backfill against
a clone, then flip 6 and 7 together.

### Mobile

Conceptually no operator-facing change — scan, vaccinate, upload proof, submit. Three real risks:

- anything keying on `protocol_version_id + rule_id` must move to `obligation_id`;
- offline completions queued before the migration must still resolve — they will, since
  `obligation_id` is stable, which is the main reason for the rule above;
- if a drive may carry several vaccines for one goat, the UI must already render multiple rows
  per goat.

Returning `vaccine_code` / `dose_code` explicitly on the API would make identity legible to
clients and is worth doing in the same change.

## Settled: repeat-cycle identity (2026-08-21)

The "Open" item below asking for `occurrence_anchor` is answered. The design was proposed,
counter-reviewed twice, and narrowed against the measured data. Recording it here because the
scope claim is the part most easily overstated.

### Anchor identity to the cause, not the effect

Identity currently includes the DUE DATE (`generation.go` obligation key; `booster.go` repeat
key). For a dose anchored to when the previous one was actually given, that date legitimately
moves, so a re-run inserts beside the existing row instead of moving it.

The fix is to label every repeat obligation with WHAT PREVIOUS VACCINATION CREATED IT, and to
make the database enforce one open successor per cause:

    repeat_cycle_source            completed_obligation | trusted_history | imported_history
    repeat_cycle_source_ref        the completed obligation id, or the history record id
    repeat_cycle_anchor_obligation_id   the completed obligation, when the source is internal
    repeat_cycle_anchor_at         administered_at of the previous dose
    repeat_cycle_due_at            the next due date

    CREATE UNIQUE INDEX CONCURRENTLY obligation_repeat_cycle_open_anchor_unique_idx
    ON obligation_instances (tenant_id, repeat_cycle_anchor_obligation_id)
    WHERE repeat_cycle_anchor_obligation_id IS NOT NULL
      AND status IN ('scheduled', 'due', 'in_progress', 'deferred');

A SECOND index is required, and the first one alone is not enough. Repeat work minted from
accepted or imported history has NO completed obligation to anchor to -- its source is
`trusted_history:<id>` or `imported_history:<id>` -- so the anchor index above skips it
entirely and generation can still duplicate it. Uniqueness must therefore also cover the
source ref:

    CREATE UNIQUE INDEX CONCURRENTLY obligation_repeat_cycle_open_source_unique_idx
    ON obligation_instances (
      tenant_id, protocol_version_id, rule_id, target_type, target_id,
      repeat_cycle_source, repeat_cycle_source_ref
    )
    WHERE repeat_cycle_source_ref IS NOT NULL
      AND status IN ('scheduled', 'due', 'in_progress', 'deferred');

The anchor index is the stricter statement for the internal case (one open successor per
completed dose, regardless of which rule re-derives it); the source index covers every case,
including the history-sourced repeats `generation.go` mints.

ONE OPEN SUCCESSOR PER ANCHOR, not one ever. This is the hinge of the whole design. A global
uniqueness would burn the anchor permanently the first time a successor is cancelled or
superseded, and the cycle could never restart — the same class of silent stoppage the earlier
constant-key proposal was rejected for.

`missed` is OUTSIDE the open set. An earlier draft of this ADR put it inside, on the belief that
recovery-repair reopens missed rows. That was checked and is FALSE:
`ReopenDeferredObligationForKey` gates on `status = 'deferred'` only
(`obligation/adapters/postgres/sqlc/commands.sql:158,214`), and
`docs/protocol-engine/state-machines.md:60` states that `completed`, `waived`, `canceled`,
`superseded` AND `missed` rows are immutable history: "a later policy correction creates new work
or a rework/correction record; it does not rewrite the closed row."

That convention is what makes excluding `missed` correct rather than merely consistent. A missed
successor is closed history, so it must FREE its anchor — the next completed dose then mints new
work, which is exactly what the state machine says a policy correction should do. Had `missed`
stayed inside the index, the first missed dose would have burnt that anchor permanently and the
cycle would have stalled in silence: the very failure this design exists to prevent, reintroduced
through its own predicate.

The open set is therefore exactly `scheduled, due, in_progress, deferred` -- the same four
statuses named in the index predicate above, and nowhere is a different list used. Everything else — `completed`,
`waived`, `canceled`, `superseded`, `missed` — is terminal and outside it.

### Scope, stated honestly

This fixes 207 of the 222 duplicate groups measured in the staging baseline. The other 15 are
DIFFERENT BUGS and must not be counted as fixed by it:

| dose | groups | gap between the pair | shape |
|---|---|---|---|
| `et_tt_revac` | 207 | 0–1 days | `after_previous_completion` + `every_n_days` — the same cycle minted twice across a day boundary. Fixed by the anchor index. |
| `goat_pox_adult_w1` | 14 | 216 days | `manual_campaign`, `repeat = none` — carries no repeat anchor at all. A campaign supersede/realignment defect. NOT fixed here. |
| `et_tt_kid_7w` | 1 | 8 days | `birth_age`, `min_gap_days = 21` — a course-gap duplicate. NOT fixed here. |

The rollout must not claim the system is clean until the manual-campaign and birth-age paths are
repaired too.

### Constraints on the implementation

- KEEP IT OPT-IN. The existing due-date guard stays byte-identical when repeat metadata is
  absent, with a separate branch only for rows carrying `repeat_cycle_source_ref`. Stated
  precisely: TODAY only vaccination inserts through `InsertObligationInstance` — feed completes
  obligations but never creates them, and health does not touch the statement at all. So this is
  architecture-preserving hygiene on a genuinely generic statement, not protection of live
  concurrent callers. An earlier draft of this ADR claimed feed and health share the path; they
  do not.
- BOTH MINTING PATHS. `booster.go` is not the only one that creates repeat work — `generation.go`
  also mints it from accepted and imported history. Fixing only the booster leaves a path that
  still writes bad rows.
- `completed_handler.go` currently discards the completed `obligation_id` and passes only
  `PrevSequence`; the source ref cannot be set without threading it through, and matching the
  current rule by sequence is fragile independently of this change.
- HOT TABLE. `obligation_instances` is registered in
  `backend/tests/integration/validate-hot-index-migrations.sh`, so: nullable columns under a
  bounded `lock_timeout`; `CREATE INDEX CONCURRENTLY`; goose `NO TRANSACTION` (a concurrent index
  cannot run inside a transaction); constraints `NOT VALID` then validated separately; and the
  repair as a bounded job, never an unbounded migration.
- THE BACKFILL IS LOAD-BEARING. Existing rows have NULL anchors, so the partial index does not
  see them: the index prevents new damage, the repair fixes old damage. It must be idempotent and
  report each class separately, including the ones it cannot fix:

      repeat duplicates repaired
      repeat successors created
      campaign duplicates repaired
      course duplicates repaired
      duplicates skipped because they carry no repeat metadata

  That last counter is how the 15 stay visible instead of being hidden by a clean-looking run.

### Mobile

Unchanged. The phone completes a concrete `obligation_id`; the backend emits
`vaccination.completed` and schedules the successor. No repeat-cycle field reaches a client.
Movement is already handled — open future obligations are re-scoped when a goat moves.

### Tests required before trusting it

- `booster_test.go` — a repeat successor stores its source metadata, and a replay does not duplicate.
- `booster_integration_test.go` — complete a repeat dose, dispatch the event twice, get exactly one successor.
- `generation_test.go` — history-driven repeat work carries repeat-cycle metadata too.
- `repository_integration_test.go` — two inserts with the same repeat source cannot create two open
  rows; two DIFFERENT completed anchors can create two different future cycles.
- a move/shift test — a future repeat obligation follows the goat's new shed.

## Open

1. ~~Exact definition of `occurrence_anchor` for repeating doses.~~ Settled above.
2. Whether to keep `effective_from` at all. Future-dating is currently unsafe: `effective_to` is
   immutable on a published row, so a future-dated publish leaves a window with no effective
   plan. Either allow `effective_to` to be closed during the atomic retire, or drop future-dating
   and treat publish as immediate.
3. Whether to fix supersede-on-publish as an interim measure before the identity change lands.
