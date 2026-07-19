# Review-Lens Ledger — Closed Decisions + Review Lenses

**Load this on EVERY Goat OS review (Claude, Codex, or human) before flagging anything.**

Two jobs:
1. **Part A — Closed-Decisions Registry (do-not-reopen).** What critical fixes are already
   made + the invariant they enforce + that they are LOCKED. Before you re-flag a bug or
   re-propose a design, check here: if it's `CLOSED`/`BANNED`/`LOCKED`, do not re-open,
   re-fix, or rebuild it. Verify the PROOF is not actually stale first; if it is, say so
   explicitly rather than silently re-filing.
2. **Part B — Review-Lens Index (what-to-check).** The lenses to apply, each mapping to its
   enforcing machine guard + ADR + example bugs. This is a map layer — the deep checklists
   live in the sibling `references/*.md` chapters; this points at them.

Status vocabulary: **CLOSED** (fixed + proven — do not re-fix) · **BANNED** (anti-pattern —
do not rebuild) · **LOCKED** (maintainer rule — do not change) · **OPEN** (known unresolved —
safe to work, not a new finding).

---

## Part A — Closed-Decisions Registry

### CD-STAGE-REVIEW — vaccination stage/age-mismatch "review queue"
- STATUS: **BANNED**
- INVARIANT: vaccination stage is a pure function of age (DOB → age-weeks → kid cutoff). A
  "kid tag but adult age" is impossible with clean data. Fix at INGESTION (seed auto-corrects
  the mock tag from age + reports; real ingestion corrects-and-surfaces) — never a runtime
  review/reconcile/repair queue.
- PROOF: feature purged (code + API + admin tab + `vaccination_stage_review_items` table +
  handoff doc) in commit `c444e23d`; landed `66a71c9d`.
- ENFORCED-BY: `no-mismatch-review-queue` guard + ADR `docs/decisions/ingestion-validation-not-runtime-review.md`.
- DO-NOT: re-propose, enrich, or rebuild a stage-review/reconcile queue; do not re-flag its
  absence as a gap. The guard fails your push if you re-add the pattern.

### CD-NO-CROSS-PARK-MOVE — goats never move between parks
- STATUS: **LOCKED** (maintainer decision 2026-07-19)
- INVARIANT: shed moves exist only within one park; leaving a park is a terminal
  transferred/sold exit, never a move. Initial placement is exempt.
- PROOF: runtime `identity.MoveGoat` returns `ErrCrossParkMove`; seed rejects cross-park in
  `checkNoCrossParkMoves`. Source: `context/source-findings/goats-and-parks-source-findings.md`.
- ENFORCED-BY: manual review + the seed guard. (Gap: no standing guard on the counts/shifting
  path — see PR #11 review; a shifting flow MUST assert `source_park == destination_park`.)
- DO-NOT: add a shifting/relocation path that permits a cross-park destination.

### CD-PEND1 — obligation `in_progress` reachability
- STATUS: **CLOSED**
- INVARIANT: `in_progress` is set on a reachable production path (the first completion in a
  multi-obligation drive marks its still-open siblings `in_progress`), so `MarkMissedBefore`'s
  in-progress guard protects a running drive. A writer placed after the terminal transition is
  dead-on-arrival — prove reachability with a test on the real path.
- PROOF: sibling-marking in `MarkCompleted` AND the vaccination accept path
  (`RecordAndAcceptCompletionAtomic`); `pend1_sibling_inprogress_integration_test.go`.
- ENFORCED-BY: manual (reachable-lifecycle-writer rule in `references/backend.md`).
- DO-NOT: re-wire `in_progress` at SOP-submit (submit = completion; dead there).

### CD-PEND2-R50-022 — cancel-by-key batch repair
- STATUS: **CLOSED**
- INVARIANT: a single-key cancel repairs the batch `estimated_targets`/`planned_quantity`/
  `cell_ledger` + reserved-stock `release_qty`, under a `FOR UPDATE` batch lock (no lost
  update on concurrent cancels).
- PROOF: `batch_lock` CTE in `CancelOpenObligationByIdempotencyKey`; cancel-by-key integration tests.
- DO-NOT: repair batch quantities without locking the batch row.

### CD-PEND3 — capacity_scope enforce-or-reject
- STATUS: **CLOSED**
- INVARIANT: an authored config value that the engine can't honor must be REJECTED at publish,
  not silently ignored. `capacity_scope` other than tenant is rejected until the planner enforces it.
- PROOF: `parseVersionedCapacity` rejects non-tenant scope. ENFORCED-BY: `config-validate-guard`.

### CD-PEND5-6-7 — seed cross-park / sweeper business-day / biztime zero-clamp
- STATUS: **CLOSED**
- INVARIANT: seed rejects a silent cross-park move (CD-NO-CROSS-PARK-MOVE); the LIVE sweeper
  (`internal/kernelstages/obligation_sweeper.go`, not just the retired CLI) defaults `due-before`
  to the IST business-day boundary, not a wall-clock instant; `ClampFutureAsOf(zero)` returns now.
- PROOF: landed `66a71c9d`. DO-NOT: reintroduce a wall-clock due-before default or fix only the CLI binary.

### CD-R50-011 — successor suffix is O(1), not an unbounded loop
- STATUS: **CLOSED**
- INVARIANT: the next free successor suffix is computed in one bounded query
  (`NextSuccessorSuffix`, max 2000), never an unbounded attempt-by-attempt DB probe loop.
- PROOF: NextSuccessorSuffix + bounded-successor Postgres test; landed `66a71c9d`.
- DO-NOT: replace with a `for attempt:=1;;attempt++` round-trip probe.

### CD-R50-VERIFICATION — verdict/evidence/self-verify/scope/close/idempotency
- STATUS: **CLOSED** (R50-014/016/017/018/019/020/021)
- INVARIANT: `verification.item.closed` is in the domain-event schema enum; a verdict cannot be
  reversed (`AND status='pending'`); missing/unsigned evidence fails CLOSED; the submitting
  operator cannot verify their own item; a park-scoped grant for a different permission cannot
  escape to tenant scope (the R50-019 scope-escalation class); per-item close excludes
  submission-owned items; the write path honors a server-backed Idempotency-Key.
- PROOF: verification suite green w/ Postgres (`GOATOS_RUN_POSTGRES_TESTS=1`); landed `66a71c9d`.
- DO-NOT: re-flag these as open; verify the tests first if you suspect a regression.

### CD-R50-019-SCOPE — permission checks are scope-specific
- STATUS: **CLOSED** / general rule
- INVARIANT: authorization must check "has a grant FOR THIS permission at the required scope",
  never "has ANY grant". Same class recurs in the counts approval flow (PR #11) — a Park Head
  must not approve a shifting/birth/death outside their park scope.
- PROOF: RecordVerdict operator/scope predicate + mixed-grant exploit test; landed `66a71c9d`. Counts variant flagged in PR #11 review.
- DO-NOT: extract only role names and drop scope; do not gate approval by permission-type alone.

### CD-R50-033 — notification subject consistency + bounded exhausted set
- STATUS: **CLOSED**
- INVARIANT: `notification.*` events use `subject_type` consistent with `subject_id` (the
  notification_request id, not a calendar-event id); exhausted (max-attempts) rows transition
  to `exhausted` and LEAVE the active-claim set (bounded memory), counted as deadletters.
- PROOF: `ClaimDue` two-phase transition; notification suite green.

### CD-R50-015 — forward-migration lock safety
- STATUS: **CLOSED**
- INVARIANT: a forward migration on a hot/populated table must be lock-safe: CHECK re-adds use
  `ADD CONSTRAINT ... NOT VALID` + separate `VALIDATE`; index swaps use `CREATE ... CONCURRENTLY`
  under a `-- +goose NO TRANSACTION` migration, CREATE-new-then-DROP-old (never a window with no
  unique index → no 42P10 for ON CONFLICT writers); long DML is split out of the DDL transaction;
  dedup DELETEs are bounded/scoped.
- PROOF: migrations `000003/000004/000006` + `validate-postgres-migrations.sh` + hot-index guard.
- ENFORCED-BY: `validate-hot-index-migrations` (floor recalibrated post-squash — do not restore
  the stale 141 floor that false-greened migrations 1-140).
- DO-NOT: `ADD CONSTRAINT` on a hot table without `NOT VALID`; drop-then-create a live unique index.

### CD-IDEMPOTENCY-UNIQUE-INDEX — ON CONFLICT needs a matching unique index
- STATUS: **CLOSED** (InsertDeferredObligation 42P10)
- INVARIANT: `INSERT ... ON CONFLICT (cols) DO ...` requires a UNIQUE constraint/index on exactly
  those columns or it errors 42P10 at runtime (this shipped broken). The index must exist before
  the code deploys.
- PROOF: obligation_status_events idempotency index made UNIQUE (baseline + `000004` concurrent swap); landed `66a71c9d`.
- ENFORCED-BY: `idempotency-writes-guard`.

### CD-R50-002 — recipe-coupling test targets the real script
- STATUS: **CLOSED**
- INVARIANT: the migrate↔seed coupling test asserts on the ACTUAL recipe
  (`run-local-stack-supervised.sh`: migrate && grant && seed-closeout), so removing a flag fails it.
- PROOF: `dev_local_recipe_coupling_test`; landed `66a71c9d`.

### CD-R50-008-010 — Android complete-roster memory
- STATUS: **OPEN** (the one unresolved follow-up — safe to work, not a new finding)
- GAP: the complete-roster refresh accumulates the whole roster in memory + one JSON blob
  (unbounded at scale), and the mobile-guard baseline exempts the responsible helper (false green).
- NEXT: bound the complete-roster memory (cap/stream) + tighten the mobile-guard baseline.

---

## Part B — Review-Lens Index

Apply these in priority order (kernel → scale → security → architecture → business-rule →
observability → UI-contract → maintainability). Each lens → its deep chapter + enforcing guard(s)
+ example closed-decisions.

| Lens | Deep chapter | Guards | Examples |
|---|---|---|---|
| **kernel-obligation** | `references/kernel-and-scale.md` | sweeper-deployment, deployed-job-flags, worker-stage-budgets · *manual:* scale-guard, check-e2e-kernel-integrity.sh | CD-PEND1, CD-R50-011 |
| **db-migration** | `references/backend.md` | seed-migration-coupling, room-migration-safety · *manual:* validate-migrations, validate-sqlc-plans, validate-hot-index-migrations | CD-R50-015, CD-IDEMPOTENCY-UNIQUE-INDEX |
| **scale-aggregate** | `references/aggregates-and-projections.md` | aggregate-projection-review, atomic-readmodel-sync, admin-web-request-reads · *manual:* scale-guard | CD-PEND2-R50-022 (grain/fan-out); counts PR #12 |
| **vaccination-rule** | `references/business-rules.md` | clinical-defer-states, vaccination-schedule-canonical, goat-shed-scope | CD-STAGE-REVIEW, CD-NO-CROSS-PARK-MOVE |
| **backend-hexagonal / idempotency / atomic** | `references/backend.md` | idempotency-writes, atomic-readmodel-sync, config-validate-or-reject, domain-event-architecture · *manual:* check-boundaries.sh, check-contract-drift.sh | CD-PEND3, CD-IDEMPOTENCY-UNIQUE-INDEX |
| **ingestion-validation** | ADR `ingestion-validation-not-runtime-review.md` | no-mismatch-review-queue | CD-STAGE-REVIEW |
| **security-rbac-scope** | inline (SKILL.md priority #3) | *(no standing scope guard yet — gap)* | CD-R50-019-SCOPE |
| **frontend-admin-web** | `references/frontend.md` | admin-web-request-reads, admin-web-prefetch, nav-composition, mock-clicks | — |
| **mobile-android** | `references/mobile.md` | offline-first-reads, mobile-list-fetch, android-bounded-memory, android-navigation-stack, room-migration-safety | CD-R50-008-010 (OPEN) |
| **observability-telemetry** | inline (SKILL.md priority #6) | *manual:* telemetry-guard | — |
| **event-integration** | `context/architecture/domain-event-integration-contract.md` | domain-event-architecture | CD-R50-014 |

**Currency:** new critical fixes get a CD-entry here at land time; new machine-guards/ADRs get
cited under their lens. The `review-lens-ledger` guard fails a push if a manifest guard is
uncited or a CD block is missing STATUS/INVARIANT/PROOF. Writing a new lens's checklist stays a
human/AI judgment — the guard enforces that an entry exists, not that it's complete.
