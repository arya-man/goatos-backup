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

### CD-WEB-RESPONSIVE-REVIEW — admin-web reviews require laptop + mobile UI/UX proof
- STATUS: **LOCKED** (maintainer decision 2026-09-11)
- INVARIANT: any review or code change touching admin-web/web-rendered UI, CSS,
  route/page contracts, charts, tables, or visible copy must be reviewed on both
  laptop and mobile. The review must include affected nested page tabs, sidebars,
  drawers/modals/popovers, dynamic detail pages, charts, tables, legends, KPI
  cards, and horizontal-scroll regions. A build/typecheck or single desktop
  screenshot is not enough.
- PROOF: `apps/admin-web/scripts/smoke-visual-live.mjs` + `responsive:guard`
  are the local guard path; `smoke-visual-route-coverage.test.mjs` must be
  updated when adding pages/tabs/dynamic routes so the guard cannot silently
  miss them.
- ENFORCED-BY: `responsive:guard`, `smoke-visual-route-coverage.test.mjs`, and
  manual screenshot inspection.
- DO-NOT: approve web/admin UI work that lacks mobile evidence, skips nested
  tabs/drawer/dynamic-route coverage, or presents screenshots not visually
  validated by the reviewer.

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

### CD-ADDITIVE-PUBLISH — a publish only touches the rules it changed
- STATUS: **LOCKED** (maintainer decision 2026-08-24)
- INVARIANT: a plan version holds every vaccine, so publishing is routine. Adding a 6th vaccine
  to a plan of 5 leaves those 5 **operationally untouched** — same `obligation_id`, same
  `due_at`, same status, same task/batch/proof attachment — with only `protocol_version_id`
  moving. Editing 1 of the 5 changes that 1 and nothing else. Blast radius equals the edit.
- MECHANISM: a rule is recognised across versions by `protocol_rules.identity_key`
  (`vaccine|dose|sequence`) + `content_fingerprint` (sha256 over every field that decides what
  is owed and when). Generation **carries over before it supersedes**: matching rules are
  rebound in place (UPDATE, id preserved), and only what is left is cancelled.
- PROOF: `docs/preventive-care-vaccination/additive-publish.md` (6 numbered guardrails);
  unit tests in `internal/protocol/domain/rule_lineage_test.go`; ordering test
  `TestGenerationCarriesOverBeforeItSupersedes`.
- DO-NOT: re-introduce a version-wide `protocol_version_replaced` sweep for unchanged rules;
  add `due_at` or `status` to the carry-over UPDATE; reorder carry-over after generation (it
  would collide with `obligation_instances_dup_guard`); treat `sort_order` as rule content.
- WATCH-FOR (the failure mode this lens exists to catch): cancel-and-re-mint dressed up as
  something else — regenerating "just to be safe", recomputing due dates for rules whose
  content did not change, rebuilding tasks/assignments because the version moved, or detaching
  proof/completion state from a carried-over obligation. Cause-anchored identity prevents
  DUPLICATES; it does not by itself prevent CHURN, and the two are routinely confused.

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

### CD-EXISTENCE-ASSERTION — an existence check may not be called coverage (2026-09-23)
- STATUS: **LOCKED** (maintainer decision 2026-09-23)
- INVARIANT: a check earns the word "covered" only when it compares a produced value to an
  independently expected value AND goes red when the fix it covers is reverted. `visible`,
  `text contains`, `count >= 1` and `absent` assert that a surface exists, not that it is
  right. A commit with a check's name written next to it is a mapping, not coverage.
- PROOF: 2026-09-23 audit — all 928 web assertions in the automation lanes were
  existence-shaped and zero compared a number to an expected number; `6fbc825f2` ("Spend share
  only shows the feeds the farm buys") was "covered" by an assertion on the chart's heading.
  The prior ledger called 99 partition commits covered while no check ever read a pen label.
- ENFORCED-BY: manual (`references/verification-and-coverage.md#assertion-strength`,
  `#covered-means-revert-fails`). No machine guard measures assertion strength yet — GAP.
- DO-NOT: add a coverage row, receipt line, or ledger status backed only by a name match; do
  not close a "covered" claim without the revert-goes-red demonstration.

### CD-UNEARNED-VERDICT — a check that did not run renders no verdict (2026-09-23)
- STATUS: **LOCKED** (maintainer decision 2026-09-23, contract §4)
- INVARIANT: in BOTH directions. A check must never report pass without attempting the thing
  it claims to prove, and must never report a finding from a path that never ran. The only
  correct output of an unattempted check is `not-attempted`, naming the blocker. Silence is a
  verdict too: a run where everything skipped must say so where the findings would have gone,
  because "no alert" reads as "fine". A lane whose only liveness probe is unauthenticated
  (`/version`) must prove authentication against real authorization-gated data before it is
  allowed to report an empty result — an expired bearer returns 200.
- PROOF: five instances in one night — a lane reporting "People are signed out at random" when
  the app never launched; two tests passing on an error screen; a lane accusing the product of
  losing a plan nothing asked it to save; three journeys reporting a business rule holds while
  never attempting the operation it forbids. Plus a stale bearer producing a full page of false
  negatives behind a 200 from `/version`.
- ENFORCED-BY: manual (`references/verification-and-coverage.md#no-unearned-verdict`,
  `#silence-is-a-verdict`, `#auth-before-no-findings`).
- DO-NOT: emit pass/fail from a branch that returned early; do not treat an empty findings list
  as a green result without an authenticated real-data read in the same run.

### CD-CHECK-WEAKENING — noise is fixed at the measurement, never at the threshold (2026-09-23)
- STATUS: **BANNED** (contract §3)
- INVARIANT: a false positive is repaired by fixing what the check measures. Raising a
  threshold, deleting an assertion, or adding a page/route/selector to an exemption list is
  banned. A check that fires on a correct page is worse than no check; a check that stopped
  catching the real thing is worse still, because it reports green. Related: a `--self-test` or
  dry-run gate proves nothing about code its early return never reaches, and a gate that can
  only enter one branch must say so out loud.
- PROOF: `GOATOS_DASHBOARD_SLACK_DRY_RUN=1` returns before any upload, so three separate
  crashes shipped through a green gate (`2ccbc5bb7`, `3db2925c4`, `83182e472`). A search input
  measured 196x18 on the inner rect was reported "too small to tap" while the padding — and the
  real target — is on the wrapper.
- ENFORCED-BY: manual (`references/verification-and-coverage.md#never-weaken-a-check`,
  `#self-test-branch-blindness`; `references/frontend-rendering.md#effective-hit-area`).
- DO-NOT: silence a finding by widening a tolerance or exempting a surface; do not present a
  dry-run self-test as proof of a code path it returns before reaching.

### CD-RAW-ANCHOR-INTERNAL-ROUTE — in-app navigation must not reload the document (2026-09-23)
- STATUS: **BANNED**
- INVARIANT: a raw `<a href="/internal/route">` used as an in-app navigation control — a tab, a
  back link, a row-click target, a breadcrumb — is a full document navigation: it re-downloads
  the page, re-runs every Server Component and discards client state. Route changes use
  `useRouter`/`Link`; same-page overlays use `LocalOverlayLink` plus a local controller; `<a>`
  is for external links only. Applies to every tab, sub-tab, modal, drawer, row action and
  inline editor, not to the page the report named.
- PROOF: `apps/admin-web/features/health/health-config.tsx` builds its Treatment/Diagnosis tabs
  as two `<a className="btn" href={href(...)}>`, so every tab switch re-downloads the page,
  while roughly fifty other admin-web files navigate correctly through `useRouter`.
- ENFORCED-BY: `admin-web-local-overlays` + `admin-web-interaction-patterns` cover the overlay
  half; no guard yet rejects a raw anchor to an internal ROUTE — GAP, review-time only
  (`references/frontend-rendering.md#no-raw-anchor-internal-routes`).
- DO-NOT: build an in-app tab or back control from an `href()` helper and a native anchor; do
  not accept "the guard is green" as evidence, because this shape is outside it.

### CD-EXAMPLE-IS-NOT-SCOPE — a reported bug is one instance of a class (2026-09-23)
- STATUS: **LOCKED** (maintainer decision, contract §0, stated four times)
- INVARIANT: the deliverable for any reported defect is every place in the product the class
  can occur, checked every run: every route at 1440 AND 390, every tab and sub-tab, every
  modal/drawer/sheet/popup, every Edit/row-action/inline editor, L1+L2+L3 (list, detail, and
  the screen reached from the detail), and the Android screens for the same journey. A check
  that runs only on the page the example came from is not done. Unreachable parts are named,
  never silently dropped. Deep-linking every route does not exercise L2/L3: journeys must
  navigate the way a person does, or controls that appear only after an interaction are never
  seen. Expectations come from the database or the rendered product — a code comment is not a
  contract.
- PROOF: pen/partition labels treated as "partitions" instead of every label on every screen;
  flicker treated as "the Tasks filter bar" instead of every overlay on every page; full page
  reload treated as "Health Config" instead of every in-app navigation control.
  `apps/admin-web/lib/operational-location.ts` documents shed "Yashoda" as unpartitioned while
  the database gives it partitions 1-10. `.navback` exists only after a row tap, so a
  deep-linking sweep had never seen it.
- ENFORCED-BY: `operational-location` + `operational-partition-identity` cover the location
  schema half only; the sweep itself is manual
  (`references/verification-and-coverage.md#example-is-never-the-scope`).
- DO-NOT: scope a fix or a check to the reported page; do not derive an expected value from a
  comment, a constant name, or fixture prose.

---

## Part B — Review-Lens Index (trigger-keyed routing table)

**This is a routing table, not a reading list.** List the target's changed paths, match them
against the `triggers:` line of each lens, and apply ONLY the matched lenses plus the always-on
ones. A one-file CSS change should select two or three lenses, not the whole file.

```bash
# from the goatos checkout root — the changed paths this review must route on
git diff --name-only origin/main...HEAD      # branch/PR target
git diff --name-only HEAD                    # working-tree target
```

Always-on, whatever changed: **evidence** (below) and this ledger's Part A.

`covers:` is the number of `fix`/`revert` commits on `origin/main` since 2026-08-01 whose
SUBJECT matches that class. One commit can match several lenses, so the numbers rank the
classes — they are not a partition. Re-measure rather than trusting a stale number. A class
with a high count AND a guard is the important case: the guard is not catching what its name
suggests, and the review is the only catch.

Apply matched lenses in the SKILL.md priority order (kernel → scale → security → architecture
→ business-rule → observability → UI-contract → maintainability).

### Always-on

**LENS-EVIDENCE** · covers: 199 (`guard-false-green`)
- triggers: every review, and hard-on for `tools/ci/**`, `tools/agent-hooks/check-*`,
  `tools/dashboard-automation/**`, `apps/admin-web/scripts/**`, `**/*.test.*`, `**/*_test.go`,
  `**/*.test.mjs`, any receipt/coverage/ledger artifact
- rule: an existence assertion is not coverage; "covered" means red on revert; a check that did
  not run renders no verdict in either direction; silence is a verdict; noise is fixed at the
  measurement, never the threshold; a dry-run self-test proves only the branch it reaches; a
  loader that cannot find its input fails loudly; a restore is proved by content fingerprint,
  not row count; "no findings" needs an authenticated real-data read first
- guard: `guardrail-registration`, `local-ci-evidence`, `test-execution-integrity`,
  `dashboard-automation` — none of which measures assertion STRENGTH (gap)
- detail: `references/verification-and-coverage.md` · Part A: CD-EXISTENCE-ASSERTION,
  CD-UNEARNED-VERDICT, CD-CHECK-WEAKENING

**LENS-SCOPE-OF-CLASS** · covers: all of them
- triggers: every fix, every new check
- rule: the reported page is one instance; sweep every route at 1440 and 390, every tab,
  overlay, row action, L1/L2/L3 and the Android twin; derive expectations from the database or
  the rendered product, never a comment
- guard: none (manual) — `operational-location` covers only the location schema half
- detail: `references/verification-and-coverage.md#example-is-never-the-scope` · Part A:
  CD-EXAMPLE-IS-NOT-SCOPE

### Ranked lenses

**LENS-PROOF-MEDIA** · covers: 332
- triggers: `apps/goatos-android/**` proof/capture/media/video/photo code,
  `backend/internal/**/proof*`, `backend/internal/verification/**`,
  `apps/admin-web/**` proof preview or media read, any signed-URL or GCS path
- rule: proof grain is SOP-owned and flows backend-config → API → Android; no post-upload
  hidden remote preview/probe/download; preview identity is stable and not signed-URL-keyed; an
  optional capture never blocks submit and a required one is never satisfied by absence
- guard: `android-proof-media-egress`, `backend-proof-media-egress`,
  `admin-web-proof-media-egress`, `android-proof-video-pipeline`,
  `android-camera-only-proof-capture`, `proof-capture-authorization`, `android-screenshot-proof`
- detail: `references/mobile.md` + `references/backend.md` +
  `docs/runbooks/goatos-stg-proof-media-egress-2026-09-08.md`
- note: seven guards and still the largest class — they cover egress and pipeline, NOT proof
  grain or capture completeness. Review the grain by hand.

**LENS-ANDROID-PHONE-SCALE** · covers: 331
- triggers: `apps/goatos-android/**` list/picker/loading/layout code; any Compose screen over
  state or domain collections
- rule: no unkeyed `items()`, no nested scrollable inside a list row, no unbounded
  `.forEach` over domain data in a scrollable, no chips as the picker for an unbounded
  dimension, no full-screen spinner that discards cached content
- guard: `android-compose-lists`, `android-bounded-memory`, `android-ui-foundations`,
  `android-ui-copy-layout`, `mobile-list-fetch`
- detail: `references/mobile.md` + `apps/goatos-android/docs/phone-scale-ui.md` · Part A:
  CD-PHONE-SCALE-UI

**LENS-PEN-LABEL** · covers: 326
- triggers: any diff that renders a location/shed/pen/partition/park label;
  `apps/admin-web/lib/operational-location.ts`, `backend/internal/**/location*`,
  location-bearing schema, any screen showing pen names
- rule: when a partition exists (`Castro 1` beside `Castro 2`), every surface renders the
  partition label, grouped by `shed_id` + park, never collapsed to the parent unless the
  aggregate is explicit; goats never move between parks
- guard: `operational-location`, `operational-partition-identity`, `goat-shed-scope`,
  `goat-shed-integrity-db-proof`
- detail: `docs/decisions/operational-location-convention.md` +
  `docs/decisions/partition-is-operational-shed.md` + `references/frontend.md` · Part A:
  CD-NO-CROSS-PARK-MOVE
- note: guarded on schema/identity, still 326 fix commits — the RENDERED label on every screen
  is the uncovered half. `operational-location.ts` also carries a comment the database
  contradicts; read the database.

**LENS-PERMISSION-SCOPE** · covers: 181
- triggers: any route/page/tab/module gate, `packages/rbac/**`, `person_module_access`,
  grant-role or `ceo_internal` code, nav composition, `backend/internal/**` tenant-scoped query
- rule: every scoped query filters `tenant_id`; a permission check is scope-specific, never a
  coarse module tick; CEO/CXO business visibility never depends on a later manual HRMS tick
- guard: `role-scoped-ui-contract`, `nav-composition`, `org-boundary`, `stg-operator-scope`,
  `android-row-action-scope`, `weighing-operator-scope`
- detail: `references/backend.md` + `references/frontend.md` · SKILL.md "Founder visibility
  invariant" · Part A: CD-R50-019-SCOPE

**LENS-VERIFICATION-SIGNOFF** · covers: 180
- triggers: `backend/internal/verification/**`, verifier/sign-off UI, any review queue,
  `apps/goatos-android/**` verifier screens
- rule: verdict + evidence + self-verify ban + scope + close + idempotency; verification is a
  separate task from execution; a mismatch is fixed at ingestion, never by a runtime
  review/reconcile queue
- guard: `no-mismatch-review-queue`, `leadership-verifier-surface-separation`,
  `android-verifier-detail-scope`
- detail: `references/backend.md` + `references/business-rules.md` · Part A: CD-R50-VERIFICATION,
  CD-STAGE-REVIEW

**LENS-TOTALS-RECONCILE** · covers: 163 (plus 8 explicit `double-count`)
- triggers: any summary/card/KPI/rollup, `backend/internal/counts/**`, a query combining `JOIN`
  with aggregation and pagination, any projection or read model
- rule: grain-explicit counts, declared disjoint buckets, page-independent totals, canonical
  membership, stable group key, proven join cardinality — a screen-local fix that hides a
  mismatch is a finding
- guard: `aggregate-projection-review`, `operational-read-model-contract`,
  `atomic-readmodel-sync`, `admin-web-sectioned-aggregate-reads`
- detail: `references/aggregates-and-projections.md` +
  `docs/architecture/operational-read-model-contract.md`

**LENS-NAV-ROUTE** · covers: 117
- triggers: nav rendering, sidebar/bottom-bar, tabs, breadcrumbs, deep links, route
  registration, `apps/goatos-android/**` navigation stack
- rule: command lenses are top-level only and fed by `?domain=`/`?category=`; a hosted route
  must also be a supported root destination or every deep link lands on home; in-app navigation
  must not reload the document
- guard: `nav-composition`, `nav-entry-point-placement`, `android-navigation-stack`,
  `module-alerts-tab`, `admin-web-nav-icon-coverage`
- detail: `references/frontend-rendering.md#no-raw-anchor-internal-routes` +
  `docs/decisions/role-module-nav-composition.md` + `.agents/skills/nav-composition/` · Part A:
  CD-RAW-ANCHOR-INTERNAL-ROUTE

**LENS-NOTIFICATION-DELIVERY** · covers: 108
- triggers: `backend/internal/notificationbridge/**`, outbox → channel code, Slack/FCM/push
  adapters, reminder and escalation paths
- rule: notifications, reminders and escalations are durable rows written in the same
  transaction, never logs or in-memory state, and fail closed; the subject and the recipient set
  are specific and bounded; delivery is proved end of channel, not end of enqueue
- guard: `notification-specificity`, `fcm-recipient-routing`, `cascade-event-wiring`,
  `domain-event-architecture`
- detail: `references/kernel-and-scale.md` + `references/backend.md` · Part A: CD-R50-033

**LENS-MIGRATION-SCHEMA** · covers: 102
- triggers: `backend/migrations/postgres/**`, any Room migration, a new or altered column or
  index, a hot-table query
- rule: hot-table indexes are CONCURRENT in their own migration; add-nullable → backfill →
  constrain are separate, resumable, idempotent phases; a migration that touches a seed-owned or
  app-visible table updates the seed/projector in the same change
- guard: `migration-duplicate-versions`, `seed-migration-coupling`, `room-migration-safety` ·
  *manual:* `validate-migrations`, `validate-sqlc-plans`, `validate-hot-index-migrations`
- detail: `references/backend.md` + `.agents/skills/db-migration-safety/` · Part A: CD-R50-015

**LENS-IDEMPOTENCY-OUTBOX** · covers: 90
- triggers: any write path, server action, mutation adapter, outbox producer/consumer,
  `ON CONFLICT`, retry or lease code
- rule: a stable idempotency key backed by a MATCHING unique index; state + audit + outbox in
  one transaction; claiming or leasing work is not a delivery attempt; a same-key replay returns
  the original result and fires no side effect twice
- guard: `idempotency-writes`, `atomic-readmodel-sync`, `domain-event-architecture`,
  `domain-event-envelope-enum`
- detail: `references/kernel-and-scale.md` + `references/backend.md` · Part A:
  CD-IDEMPOTENCY-UNIQUE-INDEX, CD-PEND2-R50-022

**LENS-OVERLAY-PAINT** · covers: 72 (`flicker-overlay-paint`)
- triggers: `apps/admin-web/app/mesha-theme.css`, any scrim/veil/drawer/modal/sticky rule, any
  overlay open/close controller, any `z-index`, `backdrop-filter`, `transform` or `position`
  change
- rule: an ordinary open/close never navigates or requests an RSC payload; an opaque panel holds
  its own stacking context for the whole transition and sits clearly above its dimmer;
  `position:sticky` plus `backdrop-filter` tears on mobile GPUs; a click costs only what it
  changes
- guard: `admin-web-local-overlays`, `admin-web-interaction-patterns`, `overlay-motion`
- detail: `references/frontend-rendering.md` + `docs/decisions/admin-web-interaction-patterns.md`
- note: flicker evidence is a GIF or filmstrip. A still cannot show it (contract §7).

**LENS-OFFLINE-SYNC** · covers: 70
- triggers: `apps/goatos-android/core/**` Room/outbox/sync code, any device-side queue, replay,
  or WorkManager job
- rule: Room is the single source of truth and reads come from it; the device outbox has a
  declared lifecycle with bounded retry; a restarted unit keeps every synced capture of its
  current round; nothing derives a terminal state the server never confirmed
- guard: `offline-first-reads`, `android-outbox-lifecycle-policy`, `room-migration-safety`,
  `mobile-list-fetch`
- detail: `references/mobile.md` + `docs/decisions/android-offline-first.md` · Part A:
  CD-R50-008-010

**LENS-SCHEDULER-SWEEPER** · covers: 68
- triggers: `backend/internal/obligation/**`, `backend/internal/vaccination*/**`, any sweeper,
  planner, drive-batching or cron path
- rule: keyset-chunked with forward progress, `FOR UPDATE SKIP LOCKED`, lease/cursor and
  idempotency — never a restart at offset zero; drive planning maximizes compatible distinct
  animals per park visit inside the authored window; exact-due-date micro-drives are a bug
- guard: `sweeper-deployment`, `deployed-job-flags`, `worker-stage-budgets`,
  `vaccination-drive-clubbing-db-proof`
- detail: `references/kernel-and-scale.md` + `references/business-rules.md`

**LENS-PUBLISH-VERSION-LOCK** · covers: 67
- triggers: any config/protocol/SOP publish path, draft vs active version code,
  `backend/internal/protocol/**`, `backend/internal/configuration/**`
- rule: a publish only touches the rules that actually changed — adding a 6th vaccine leaves the
  other 5 operationally untouched (same obligation id, due date, task/batch/proof); an authored
  value the engine cannot honor is REJECTED at publish, never silently ignored
- guard: `additive-publish`, `config-validate-or-reject`, `vaccination-schedule-canonical`
- detail: `docs/preventive-care-vaccination/additive-publish.md` + `references/business-rules.md`
  · Part A: CD-ADDITIVE-PUBLISH, CD-PEND3

**LENS-SQL-BIND** · covers: 66
- triggers: any dynamically built SQL, `backend/internal/**` repository code, a changed
  parameter list
- rule: every dynamic statement binds through the repo's bind contract; parameter counts are
  never hardcoded in a caller or a guard
- guard: `postgres-bind-contract`, `commandboard-query-plan`
- detail: `references/backend.md` + `docs/engineering/backend-go-postgres-quality.md`

**LENS-SEED-FIXTURE** · covers: 60
- triggers: `backend/cmd/seed-*/**`, committed fixtures/manifests, importers,
  `tools/dev/seed-closeout.sh`
- rule: no fake business truth — a missing source becomes a reviewed mapping, a labelled
  provisional fixture a preflight can reject, or a blocker; committed loaders fail loud on
  unknown keys; a documented DB comparison is executed, not described; source dates are history
  anchors, not open work
- guard: `vaccination-hrms-seed-fixture`, `seed-migration-coupling`,
  `vaccination-shared-source-sync`
- detail: `goatos-build/SKILL.md` "Must" + `docs/runbooks/source-seed-data-validation.md`

**LENS-CONTRACT-DRIFT** · covers: 56
- triggers: `contracts/openapi/**`, event/JSON-schema payloads, shared DTOs, generated clients,
  `apps/admin-web/lib/**` contract readers
- rule: backend owns nav, labels, columns, filters, chips, copy and disabled reasons; the client
  renders them; a contract change pulls EVERY consumer lens (CRG `callers_of`), because the
  diff-scoped guards see no consumer file
- guard: `contract-drift`, `mobile-contract-ownership`, `ceo-ai-page-contract-drift`,
  `role-scoped-ui-contract`
- detail: `references/frontend.md` + `references/mobile.md` + SKILL.md "Consumer auto-pull"

**LENS-FALSE-EMPTY** · covers: 43
- triggers: any data route's empty/error/loading branch, any `catch` that returns `[]`, any
  bootstrap/contract-unavailable path
- rule: a swallowed backend failure rendered as an empty array or a collapsed page is a
  merge-blocking product-truth bug — it lies to an operator about herd state. Distinct loading,
  empty-success, permission, contract-unavailable and unexpected-error surfaces, plus an
  `error.tsx` boundary
- guard: none for the empty-vs-error distinction (GAP); `dashboard-automation` catches the
  known strings (`backend_down`, "The board could not be loaded", "Weights could not be loaded")
- detail: `references/frontend.md` "Error, Loading, and Accessibility"

**LENS-AUTH-SESSION** · covers: 42
- triggers: bearer/token handling, Firebase auth, session and logout paths,
  `apps/goatos-android/core/**` auth, any lane or script holding a stored token
- rule: a stored bearer expires silently and an unauthenticated endpoint still answers 200 —
  prove authentication by reading real authorization-gated data before trusting any result;
  logout clears device state; never commit a service-account JSON or a plaintext password
- guard: `secret-accessors`, `stg-promotion` (deploy half only) — no guard covers stale-token
  false negatives (GAP)
- detail: `references/verification-and-coverage.md#auth-before-no-findings` +
  `references/backend.md`

**LENS-CHART** · covers: 40
- triggers: any chart, legend, axis, series, tooltip, or KPI tile under `apps/admin-web/**`
- rule: membership is a contract decision, not a rendering one — a membership change must be
  proved by the new member being VISIBLE in frame at 1440 and 390, never by a page that merely
  rendered; labels must survive the phone width
- guard: none specific; `responsive:guard` + `smoke:visual:baseline` are the evidence path
- detail: SKILL.md "Web/UI visual proof" (PR #370 is the worked example) +
  `references/frontend.md`

**LENS-LATENCY** · covers: 31
- triggers: any page data read, hot-path query, work-board or dashboard slice, new endpoint
- rule: operator-facing routes hold a sub-500ms hot-load budget (p90 <= 300ms, p95/p99 <=
  500ms); a skeleton, spinner, prefetch or client cache does not fix a seconds-class read;
  fixed-URL warm-cache timings are not serving-cost evidence; measure before AND after against
  the same actor/tenant/park/date/page size
- guard: `commandboard-query-plan`, `commandboard-query-plan-wiring`, `admin-web-request-reads`,
  `admin-web-sectioned-aggregate-reads`, `worker-stage-budgets`
- detail: SKILL.md "Work Board latency evidence" + `references/kernel-and-scale.md`

**LENS-COPY** · covers: 31
- triggers: any user-visible string, `backend/internal/adminui/**`, UI config entries
- rule: operator-facing copy is plain English and backend-owned; findings and alerts name the
  page, the device and what a person sees — never a selector, property name, element tag, field
  path, status code, SQL or check code (contract §7)
- guard: `herd-signals-language`, `ui-title-case`, `ui-vaccine-labels`, `notification-specificity`
- detail: `references/frontend.md` "User-facing copy firewall"

**LENS-PAGINATION** · covers: 12
- triggers: any cursor, keyset, page-size or "load more" path
- rule: the cursor is monotonic and the next page cannot regress; page size never silently
  changes the business completeness of a read; a paginated reminder loop reaches every candidate
  or is explicitly marked partial
- guard: `mobile-list-fetch`, `admin-web-request-reads`
- detail: `references/aggregates-and-projections.md` + `references/mobile.md`

**LENS-CAPACITY** · covers: 9
- triggers: operator capacity, `workforce_positions.vaccination_daily_animal_cap`, shift config,
  assignment config
- rule: capacity is HRMS-owned per position; the tenant default is a fallback, never a coercion;
  clearing a cap to null restores the default and must not silently keep the old custom cap; the
  cap fails closed
- guard: `operator-cap-fail-closed`
- detail: `goatos-build/SKILL.md` "Must" + `references/business-rules.md`

**LENS-BUSINESS-DATE** · covers: 8
- triggers: any date-only business value — due, missed, recovery window, eligibility, overdue
- rule: India/local operational timezone, never UTC; a pinned clock, never `now()` at the call
  site
- guard: `india-business-date`
- detail: `references/business-rules.md`

**Currency:** new critical fixes get a Part A CD-entry at land time; new machine guards and ADRs
get cited under their lens. The `review-lens-ledger` guard fails a push if a CD block is missing
STATUS/INVARIANT/PROOF. Re-measure `covers:` when the ranking is used to argue priority.
