# Goat OS ledger-cleanup continuation handoff — 2026-07-12

This is the single restart document for the next Codex or Claude session. It
combines repository cleanup, accepted fixes, rejected WIP, preserved branch/stash
evidence, and the remaining ledger work. Do not reconstruct state from chat.

## 1. Continuation branch

Use this branch; do not start from a scratch branch:

```text
integration/ledger-cleanup-handoff-20260712
```

The branch is intentionally a continuation branch, not `main` or `stg`. It must
be reviewed and completed in small ledger-fix slices before a PR is raised.
Never merge PR #3 (`main -> stg`) as part of repository cleanup.

At the time this handoff was consolidated:

```text
origin/main: c239fbc5 perf: enforce API latency ceilings
branch anchor before this handoff commit: 695cfbb0
```

Always re-run `git status`, `git fetch origin main`, and `git log` because the
exact branch HEAD will include the handoff commit itself.

## 2. Canonical bug queue and proof rule

The one bug queue is:

```text
context/repo-audits/last-35-commits-consolidated-bug-ledger.md
```

The required closure proof is:

```text
context/repo-audits/consolidated-ledger-defect-closure-program.md
```

The ledger still says `40 open` because counts were frozen before the cleanup
fixes below. Do not casually decrement it. For each candidate closure:

1. show the failing-before regression;
2. show the root-cause code change;
3. run the required unit/integration/E2E/scale/mobile proof;
4. get independent counter-review;
5. then update the canonical row and totals.

## 3. Accepted work on the continuation branch

### Repository/audit state

- `a113d49e` — canonical consolidated ledger, closure program, repo routing, and
  initial handoff were committed.
- `23d798f1` + `09e5ce9a` — branch/stash/worktree disposition is recorded in
  `context/execution/repository-cleanup-2026-07-12.md`; better code wins, not
  newer code.

### Safe product fixes

- `b96e54df` — Android authentication errors traverse wrapped causes and never
  display raw Firebase/provider exception text. Unit regressions were added.
- `728f8db1` — three peer-confirmed ledger residuals were fixed:
  - park-scoped grants authorize `/app/vaccination/**` without broadening admin
    routes (FIXCHK-001 candidate closure);
  - FEFO SQL ranking and DTO disabled state share one India business date
    (FIXCHK-002 candidate closure);
  - scan-roster test asserts real `next_cursor` and rejects stale camelCase
    `nextCursor` (FIXCHK-003 candidate closure).

Validation run for `728f8db1`:

```text
go test ./internal/platform/httpmiddleware                         PASS
go test ./internal/vaccinationexecution/adapters/http             PASS
go test ./internal/vaccinationexecution/adapters/postgres
  -run TestBusinessDateUTCUsesIndiaCalendarAtUTCBoundary          PASS
full postgres adapter suite                                       NOT PROVEN
```

The full postgres adapter package did not complete locally because its Docker
availability path hung. Do not convert that limitation into a passing claim.

### Guardrails

- `081b2377` is patch-equivalent to `origin/main` commit `c239fbc5` and enforces
  the API policy `p90<=300ms`, `p95<=500ms`, `p99<=1000ms` in manifests,
  tests, CI wiring, and docs. `make guardrails` passed, including the seven
  latency-policy tests. The live 1-minute/5-minute environment certification is
  still separate work; this commit prevents threshold relaxation, not runtime
  proof.
- `695cfbb0` adds the durable Android review lens and fix-quality audit to the
  Goat OS `/code-review` skill. It explicitly covers Room SSOT, Paging 3 /
  RemoteMediator, L0-L3 navigation, lifecycle, memory, logout wipe-all,
  contract consumer blast radius, regression tests, and false-green gates.

### Deliberately reverted Android WIP

- `9c6e80b2` attempted task-bound routes plus 20-row scan cursor loading.
- `0b8da256` reverts it in full.

Reason: although it compiled and its focused tests passed, it accumulated all
continuation pages into one growing JSON blob and kept scan actions only in
ViewModel memory. That violates the accepted mobile law: per-row/bounded Room
state, PagingSource/RemoteMediator, process-death-safe draft, and Room-backed
outbox. Keep `9c6e80b2` only as source material for route-identity and cursor
tests; do not resurrect its repository design wholesale.

## 4. Mobile truth at handoff

Android is not complete. Specifically:

- `ScanViewModel` still asks for 1,000 rows and does not persist actual scan
  actions as a task+row-version Room draft.
- scan roster DTO/repository/UI do not implement production Paging 3 +
  `RemoteMediator` + bounded `PagingSource`.
- `SubmitViewModel` now remains on current-main behavior after the WIP revert:
  it chooses the first task and submits empty answers/proofs.
- logout is not a clean slate: Room read caches/outbox, DataStore,
  SharedPreferences, WorkManager, files/media, SavedState, singleton/bootstrap
  state, Firebase credentials, and backend device/FCM binding are not erased by
  one fail-closed coordinator.
- whole-tree `make mobile-guard-audit` still reports three known patterns after
  the reverted WIP: two `LeadershipViewModel` 50-row reads and the unbounded
  outbox DAO read. The ledger contains the broader mobile backlog.

The later dedicated mobile PR must implement this chain, not a partial UI patch:

```text
network page (~20, keyset)
  -> RemoteMediator writes normalized principal-scoped Room rows
  -> bounded PagingSource powers UI
  -> RFID/manual action writes durable task+row-version draft to Room
  -> Submit loads exact task + Room draft + real form/proof values
  -> Room outbox queues idempotent request
  -> ACK deletes/archives draft
  -> logout wipes every app-owned/server-device surface
```

The “latest 100 scan entries” idea is UI windowing only; it must never be the
business-data retention rule. Actual scans/drafts/submissions stay durable.

## 5. Separate completed candidate: analytics taxonomy

A clean separate worktree/branch exists:

```text
/Users/ravi/mesha/.worktrees/goatos-analytics-rebuild
agent/analytics-taxonomy-rebuild
origin/agent/analytics-taxonomy-rebuild
f9a4affb feat(android): mobile analytics event taxonomy + principal identity (noop-backed)
```

It rebuilds the parked analytics WIP against the real `AnalyticsPort`, keeps the
runtime binding no-op, and adds taxonomy/identity tests. It is pushed for
preservation but not merged into the continuation branch. Review `f9a4affb`
independently under the mobile,
privacy, lifecycle, and external-egress gates; then cherry-pick only if approved.
Firebase egress/setup remains a separately gated action.

## 6. Preserved unpublished evidence

No git stashes remain, but all nine recovered stash commits are pinned against
garbage collection:

```text
cleanup-review/stash-0-temp-kernel-validation     b42d2ca9
cleanup-review/stash-1-autostash                  8edb3adf
cleanup-review/stash-2-leadership-copy            9c302c21
cleanup-review/stash-3-e2e-report-noise           31b075cf
cleanup-review/stash-4-local-artifacts             3ca95f0f
cleanup-review/stash-5-partial-analytics           94b92c44
cleanup-review/stash-6-adminui-vaxexec             adc423bf
cleanup-review/stash-7-cmdsurface                  10efaf2b
cleanup-review/stash-8-shell                       4dae4c38
```

Do not delete these refs until the selective-port matrix is complete.

Important remaining source branches:

- `agent/finish-vaccination-closure` (`310b8969`): 299-file mixed WIP; do not
  merge wholesale. Useful source: exact task/form/proof flow, draft model,
  cache purge, benchmark/leak scaffolding, guard scripts. Its logout is still
  incomplete and incorrectly ordered.
- `m4-android-finish` / `worktree-agent-a9ecbea17e46cefa7`: telemetry/Firebase
  ports. Compare against `f9a4affb`; do not enable Firebase by accident.
- kernel-audit branches: current docs are generally stronger; retain until the
  last quality comparison is written, then delete.
- `gh-pages`: required live GitHub Pages source (`build_type=legacy`,
  `gh-pages:/`). Never delete it as scratch.
- `stg`: deployment branch and PR #3 base. Never delete or merge during cleanup.

See `context/execution/repository-cleanup-2026-07-12.md` for the full
branch/stash decision record.

## 7. Live repository inventory at consolidation

Registered worktrees: 3.

```text
/Users/ravi/mesha/goatos
  integration/ledger-cleanup-handoff-20260712
/Users/ravi/mesha/.worktrees/goatos-staging-vaccination-clean-slate
  main at 68ed9069 (clean but behind origin/main)
/Users/ravi/mesha/.worktrees/goatos-analytics-rebuild
  agent/analytics-taxonomy-rebuild at f9a4affb
```

Local branches: 20 after the analytics branch was added. No scratch branch has
yet been deleted after the quality-review correction. Open GitHub PRs: only PR
#3, `main -> stg`, titled “Deploy main to staging”. GitHub Pages still serves
from `gh-pages:/`.

The next cleanup session should remove branches/worktrees only after the handoff
records one of: merged/patch-equivalent, selectively ported with proof, or
rejected with concrete correctness/architecture evidence.

## 8. Next-session order

1. Check out/pull `integration/ledger-cleanup-handoff-20260712` and verify it is
   clean.
2. Fetch `origin/main`; merge/rebase only with explicit conflict review.
3. Independently counter-review `b96e54df`, `728f8db1`, `c239fbc5`, and
   `695cfbb0`; update ledger rows only with the closure proof packet.
4. Finish the branch/stash quality matrix and remove only proven-disposable
   refs/worktrees.
5. Fix ledger bugs in small branches/PR-sized commits. Priority starts with the
   two P0 rows: Android logout clean slate and partial clinical-defer canceling
   sick work; then the P1 sweeper/scale/ordinary-PR mobile gates.
6. Build the full Android Room/Paging/draft/form/proof flow as its own dedicated
   branch/PR. Do not mix it into repository cleanup.
7. Run the current-SHA closure program, raise the PR to `main`, and leave PR #3
   untouched until staging deployment is explicitly authorized.

## 9. Useful validation commands

```bash
git status --short --branch
git fetch origin main
git log --left-right --cherry-pick --oneline origin/main...HEAD
make guardrails
make mobile-guard-audit
export JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home
cd apps/goatos-android && ./gradlew :app:compileDevDebugKotlin
```

Do not treat compile, screenshots, seeded UI data, generated HTML, or a
diff-scoped guard as end-to-end proof. Backend/DB/business-chain evidence must
match the row being closed.
