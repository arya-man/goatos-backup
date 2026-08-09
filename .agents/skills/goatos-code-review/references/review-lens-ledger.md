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
- STATUS: **OPEN** (validator regression; closure-pending under F0)
- INVARIANT: a forward migration on a hot/populated table must be lock-safe: CHECK re-adds use
  `ADD CONSTRAINT ... NOT VALID` + `VALIDATE` in a separate transaction/migration;
  index swaps use `CREATE ... CONCURRENTLY`
  under a `-- +goose NO TRANSACTION` migration, CREATE-new-then-DROP-old (never a window with no
  unique index → no 42P10 for ON CONFLICT writers); long DML is split out of the DDL transaction;
  dedup DELETEs are bounded/scoped.
- HISTORICAL-PROOF: migrations `000003/000004/000006` established the safe
  source pattern.
- CURRENT-GAP: `validate-hot-index-migrations` accepts same-transaction
  add-not-valid plus validate and is absent from ordinary CI. F0 must repair the
  rule, adversarial self-test, hot-table inventory, and CI wiring before this
  returns to CLOSED. Do not restore the stale 141 floor that false-greened
  migrations 1-140.
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

### CD-R50-008-010 — Android roster memory + mobile-list-fetch guard
- STATUS: **CLOSED** (re-verified against code 2026-07-20; the earlier OPEN was a stale carry-over
  from the 2026-07-19 handoff, not a code gap — R50-008/010 had already landed by then)
- INVARIANT: (R50-008) the SCAN/execution roster is fetched in bounded ~20-row keyset pages on
  BOTH the network fetch and the observed Room read, with a cursor-non-advance guard +
  `MAX_ROSTER_SYNC_PAGES` cap (no cycle, no silent truncation) and a per-row SSOT for indexed
  lookup; every JSON-blob cache DAO honors the shared `JsonBlobCacheDao` governance
  (`enforceCacheBounds`: TTL + row/byte cap). (R50-010) `mobile-list-fetch` guard has a
  `--self-test` and an empty diff falls through to a full-tree audit (never a vacuous pass).
- PROOF: `ExecutionRepository` keyset paging + cursor guard + `ExecutionRepositoryPaginationTest`
  (landed `d437cf42`, predates this ledger); `check-mobile-list-fetch.mjs --self-test` green +
  empty-diff→full-tree branch. Closing gap fixed here: `RosterTimetableCacheDao`/
  `RosterCoverageCacheDao` were the only blob caches NOT implementing `JsonBlobCacheDao` — now
  they do, `RosterRepository.refresh*` calls `enforceCacheBounds()`, proven by `RosterCacheBoundsTest`.
- ENFORCED-BY: `mobile-list-fetch`, `android-bounded-memory`, `room-migration-safety` guards.
- DO-NOT: re-flag R50-008/010 as open from the stale handoff doc; verify against code first. Do not
  add a screen-facing blob cache that skips `JsonBlobCacheDao` governance.

### CD-PHONE-SCALE-UI — banned Android phone-scale UI anti-patterns (2026-08-04)
- STATUS: **BANNED**
- INVARIANT: real park cardinality (~100 sheds x ~70-90 animals/shed, ~7-8k rows/park) never
  renders unbounded. Three named anti-patterns are banned repo-wide: (1) `LazyColumn`/`LazyRow`/
  `LazyVerticalGrid` `items()`/`itemsIndexed()` with no stable `key`, or a nested scrollable
  (another Lazy* or a `verticalScroll`/`horizontalScroll` Column/Row) placed directly inside a
  list's items() row lambda; (2) unbounded `.forEach { ... Composable ... }` rendering inside a
  scrollable Column/Row over state/domain data (sheds/animals/operators/dates) instead of a
  windowed `LazyColumn`/`LazyRow` with ~20-row keyset paging; (3) chips used as the picker for an
  unbounded dimension (sheds/animals/operators/dates) instead of a searchable selector — use the
  `FilterSelectorRow` + `SearchablePickerDialog` shape in
  `apps/goatos-android/feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeightHistoryChartScreen.kt`;
  (4) a full-screen spinner (`if (loading) CircularProgressIndicator() else content`) that
  discards already-rendered content on refresh instead of a skeleton/shimmer cold-load state plus
  an in-place sync annotation (existing rule, `docs/mobile/android-ui-quality.md`).
- PROOF: this ledger entry + `apps/goatos-android/docs/phone-scale-ui.md` (rulebook, one
  correct/incorrect example per rule) + `.agents/skills/mobile-anti-patterns/SKILL.md` (agent-facing
  summary) + `tools/agent-hooks/check-android-compose-lists.mjs` extended with
  `nested-scroll-in-lazy-items`, `column-foreach-unbounded`, `chip-row-unbounded-dimension`, and
  `spinner-replaces-cached-content` rules (self-test green, 6 rules total in the guard).
- ENFORCED-BY: `android-compose-lists-guard` (machine, all 4 rules — deliberately narrow shapes
  for rules 2 and 4: `chip-row-unbounded-dimension` only catches a state/domain-keyword
  `.forEach { FilterChip/AssistChip }`; `spinner-replaces-cached-content` only catches a bare-flag
  `when {}` branch next to a proven cache-rendering sibling branch. Any other code shape for the
  same anti-pattern is a false negative by design — review-time via the skill + this doc is still
  the authority for the general rule, not just these two shipped shapes.
- DO-NOT: add a new Android list/picker/loading-state screen without reading
  `apps/goatos-android/docs/phone-scale-ui.md` first; do not treat a guard PASS on a
  differently-shaped chip row or `if/else` spinner as proof the anti-pattern is absent — those
  problem (static-text heuristics for these two were tried and rejected as too noisy).

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
| **mobile-android** | `references/mobile.md` | offline-first-reads, mobile-list-fetch, android-bounded-memory, android-navigation-stack, room-migration-safety | CD-R50-008-010 |
| **observability-telemetry** | inline (SKILL.md priority #6) | *manual:* telemetry-guard | — |
| **event-integration** | `context/architecture/domain-event-integration-contract.md` | domain-event-architecture | CD-R50-014 |

**Currency:** new critical fixes get a CD-entry here at land time; new machine-guards/ADRs get
cited under their lens. The `review-lens-ledger` guard fails a push if a manifest guard is
uncited or a CD block is missing STATUS/INVARIANT/PROOF. Writing a new lens's checklist stays a
human/AI judgment — the guard enforces that an entry exists, not that it's complete.
