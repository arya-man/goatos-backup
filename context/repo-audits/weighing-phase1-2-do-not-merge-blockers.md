# Weighing Phase 1 + 2 — DO-NOT-MERGE blockers (maintainer counter-review, 2026-07-31)

Status: **NOT APPROVED. Do not push to main.**
Branch: `weighing-task-flow` in the dedicated weighing worktree
Commits so far: `14cd0b614` (Phase 1), `bc65ceb36` (CEO-assistant coverage exclusions).
`origin/main` = `8c3954ca1` (untouched). Phase 2 was still mid-write and uncommitted
when this list was recorded — verify actual tree state before starting.

The maintainer accepted a counter-review. Every item below must be fixed and proven
before any push. Do not mark an item done without pasted command output.

## Blockers

1. **Migration numbering + duplicate-version guard.**
   Renumber the weighing migrations to the tail after main's highest version, and add a
   guard that fails on duplicate migration version numbers.

2. **Critical-animal-action guard on the weighing submit/observation path (P0, item 9).**
   ICU / quarantine / sick animals must fail closed or return a deterministic
   `unavailable` reason. Wrong handling here is physical-action risk, so this stays P0.
   See `docs/features/critical-animal-action-guardrails.md` and
   `make critical-animal-action-availability-guard`.
   NOTE: this must be implemented WITHOUT breaking weighing free-flow — it is a
   clinical-state availability gate, not a herd-roster/expected-animal validation.

3. **`SubmitIndividualScope` idempotency/retry behavior.**
   - exact replay must NOT enqueue duplicate completed events
   - same key + different payload must return idempotency conflict
   - `ErrIdempotencyConflict` must NOT map to HTTP 500 (map to the correct 4xx)

4. **`closed_by_override` update must not clobber terminal states.**
   Never overwrite `unavailable` / `canceled` / terminal review states; close only open
   states. Add a regression test.

5. **Backend must reject an incomplete individual scope submit.**
   Do not rely on Android visible-page checks for submit readiness.

6. **Roster observations pagination + OpenAPI contract.**
   Fix pagination and the `cursor` / `next_cursor` / `observations` contract, and
   regenerate the OpenAPI + generated clients so backend/contract/clients move together.

7. **Weighing E2E lane honesty.**
   Either make it drive real backend service paths, or stop calling it E2E / merge proof.
   Per AGENTS.md, a seeded readback is not E2E.

   **STATUS (2026-07-31): PARTIALLY ADDRESSED — honest-labelling only, NOT E2E
   coverage added. Do not read this as "blocker 7 done" / "E2E done".**

   Done in this pass (scope: `fixtures/weighing-e2e-2026-07-29/**` renamed,
   `docs/features/weighing/**`, and this annotation only):
   - Renamed `fixtures/weighing-e2e-2026-07-29/` -> `fixtures/weighing-seed-2026-07-29/`
     via `git mv`, and renamed `weighing-seed.test.mjs` ->
     `weighing-seed-validation.test.mjs` to drop the E2E claim from the test
     file name.
   - Rewrote `fixtures/weighing-seed-2026-07-29/README.md` to state plainly
     what the fixture IS (dev seed fixture + schema validator + local DB
     importer + one Go service-level regression input) and what it is NOT
     (not E2E, not production-path proof for capture->proof->verification->
     verdict->completion, not merge evidence).
   - Updated the test's own assertion title/comment to describe it as a schema
     validator, not a behavioral test.
   - Updated every in-scope path reference in `docs/features/weighing/` to the
     new directory name.
   - Wrote `docs/features/weighing/e2e-coverage-gap.md`, naming the six
     specific unproven production paths (mobile capture, proof upload,
     verification queue, verdict, completion, roll-forward/delay handling)
     and what a real E2E test must drive.

   Still owed (real follow-up work, NOT done here):
   - **The actual E2E test does not exist.** No test in this repo drives
     Weighing capture -> proof-upload -> verification-queue -> verdict ->
     completion through real backend APIs, mobile app, or durable consumers.
     `grep -rn weighing backend/tests/e2e` still returns nothing.
   - `make check-e2e-kernel-integrity` still passes only vacuously (no
     weighing E2E test exists for it to check).
   - Three references to the OLD directory name could NOT be updated in this
     pass because they sit in scope-locked forbidden paths (backend/, tools/):
     - `tools/dev/validate-weighing-fixture.mjs:156` — default fixture
       path `../../fixtures/weighing-e2e-2026-07-29/weighing-seed.json`.
     - `backend/cmd/seed-weighing-fixture/main.go:21` — `defaultFixturePath =
       "../fixtures/weighing-e2e-2026-07-29/weighing-seed.json"`.
     - `backend/cmd/seed-weighing-fixture/main.go:231-232` — fallback path list
       includes `fixtures/weighing-e2e-2026-07-29/weighing-seed.json` (two
       `filepath.Join` calls).
     - `backend/cmd/seed-weighing-fixture/main_test.go:18,41,69` — three
       `filepath.Join(..., "fixtures", "weighing-e2e-2026-07-29", ...)` calls.
     **The rename is INCOMPLETE until these files are updated to
     `fixtures/weighing-seed-2026-07-29/...` (or the fixture directory is
     symlinked/duplicated, which is not recommended).** Until then, running
     `go run ./cmd/seed-weighing-fixture` without an explicit `-fixture` flag will
     fail to find the fixture at its old default path, and
     `backend/cmd/seed-weighing-fixture/main_test.go` will fail because it
     constructs a path to a directory that no longer exists.
   - The tool filename `tools/dev/validate-weighing-fixture.mjs` and the
     command name `seed-weighing-fixture` still carry "e2e" in their own
     identifiers even though they are a schema validator and a seed importer,
     respectively. Renaming those is also out of scope-lock for this pass.

8. **W-06 stays P1** — the PR body used it as validation proof.

9. **W-02 stays P0** — ICU/quarantine handling (see item 2).

10. **Tests required** for: duplicate event replay, unavailable-state preservation,
    partial-submit rejection, idempotency conflict, and page/contract behavior.

## Required gates after the fixes (all must be green, on the exact final SHA)

```bash
cd "$(git rev-parse --show-toplevel)"
make validate-migrations
make validate-sqlc-plans
make guardrails
cd backend && GOFLAGS=-buildvcs=false go build ./... \
  && go test ./internal/weighing/... ./internal/notificationbridge/...
GOATOS_RUN_POSTGRES_TESTS=1 go test ./internal/weighing/adapters/postgres/
cd ../apps/goatos-android && ./gradlew :core:core-data:testDebugUnitTest --tests "*Weighing*" --rerun-tasks
npm --prefix apps/admin-web run check:mock-fidelity
cd "$(git rev-parse --show-toplevel)" && make ci-local
```

Push only after all of the above are green: `zsh -ic 'git mesha-push main'`.

## Known PRE-EXISTING failures on `origin/main` (8c3954ca1) — not caused by this branch

Verified by running the same targets in a throwaway worktree at `8c3954ca1`:

- 11 weighing Postgres integration tests fail, incl.
  `TestFreeFlowAnimalObservationUpdateAndProofReplacementAreAudited`,
  `TestUpdateCampaignCancelsDeselectedSheds`,
  `TestCompletedWeighingShedEnqueuesSubmissionEventInSameTransaction`
- `make validate-migrations` fails on `000022` / `000031`
- `gofmt -l`: `verification/app/service_test.go`, `tests/e2e-hrms/harness_test.go`
- `scale-guard` is a RATCHET PASS and reports **NOT SCALE CERTIFIED**
  (26 known offenders, zero new from this branch)
- `vaccination-hrms-seed-fixture-guard` failure was NOT yet attributed — prove it at
  base before blaming or claiming it

## Standing rules for whoever picks this up

- Weighing stays free-flow: `scanned_identifier` always stored, `animal_id` nullable,
  no herd/vaccination/expected-animal/shed-ownership validation on the submit path.
- Vaccination stays strict. Do not loosen it.
- One bucket = exactly one operator.
- Role gates stay backend-owned via `/app/bootstrap`; never infer from role-name strings.
- Do not touch the other session's dirty files in the primary goatos checkout
  (including its uncommitted CEO-spreadsheet section in
  `docs/runbooks/current-active-rbac-roles.md`).
- **Verify every agent claim against the files.** During this session agents fabricated a
  work session, reported three unbounded reads as fixed when they were not, shipped a
  guard that printed failure but exited 0, and "fixed" failing tests by relaxing the
  limits under test.

---

## ADDENDUM 2026-07-31 — Phase 2 landed (uncommitted) + TWO NEW Phase 1 blockers

Phase 2 kernel work is now in the worktree, UNCOMMITTED and UNVERIFIED by the
coordinator (context exhausted). Treat the agent's report as a claim to audit.
New files: `backend/internal/kernelstages/weighing_kernel.go`,
`backend/internal/weighing/{domain,adapters/postgres}/kernel.go`,
`backend/migrations/postgres/000059_weighing_kernel_work_items.sql`,
`tools/agent-hooks/check-weighing-kernel-phase2-guard.mjs`, plus tests.
Claimed: registered in existing kernel-worker operational cadence (5 min), biztime
business-day only, keyset + FOR UPDATE SKIP LOCKED with partial index, publish
idempotent via UNIQUE(tenant_id, campaign_shed_id) + ON CONFLICT DO NOTHING inside
the publish txn, roll-forward preserves planned_business_date with
CHECK (due_business_date >= planned_business_date), 68 guards registered.

### NEW BLOCKER 11 — `india-date-guard` FAILS, caused by Phase 1
`backend/internal/weighing/adapters/postgres/verification_verdict.go:359`
uses `DecidedAt: time.Now().UTC()`. Goat OS business meaning must derive from
Asia/Kolkata via biztime, never UTC. Fix with the biztime helpers and add a test.

### NEW BLOCKER 12 — `vaccination-hrms-seed-fixture-guard` FAILS, caused by Phase 1
Trigger is `backend/migrations/postgres/000058_weighing_close_and_verification_state.sql`.
Its `ALTER TABLE` lines are not excusable by `seed-fixture-guard:ignore:` (that marker
only covers migrations whose added DDL is exclusively CREATE TABLE). Either update the
seed companion docs/commands per the seed-migration coupling runbook, or make a recorded
decision about the marker rule. Do not silently widen the guard.

Both proven pre-existing to Phase 2 by a three-way run:
`8c3954ca1` exit=0 · `14cd0b614` (Phase 1) exit=1 · current worktree exit=1.
So `make guardrails` currently FAILS on these two and nothing else.

### Phase 2 boundaries explicitly NOT done
1. Calendar/Control Tower SQL not merged into the shared queries. Phase 2 exposed
   `GET /weighing/process-state` (grain `weighing_work_item`, disjoint buckets,
   whole-filter summary, `weighing.monitor`) instead, to avoid surgery on the
   vaccination-shaped shared calendar query. Folding it in is follow-up (TRD §6.1).
2. No admin-web/Android renderer consumes `/weighing/process-state` yet.
3. Escalation fires once per transition, not daily while delayed (anti-noise, documented).
   Daily re-escalation would need a maintainer decision.
4. `make validate-migrations` still fails on `000022`/`000031` (baseline).

### Still owed before any push
Blockers 1-12 fixed; then validate-migrations, validate-sqlc-plans, guardrails,
focused backend + Android tests, check:mock-fidelity, judge/lens vs the do-not-reopen
ledger, and `make ci-local` on the exact final SHA. Squash `14cd0b614` + `bc65ceb36`
+ Phase 2 into one commit. Push only via `zsh -ic 'git mesha-push main'`.

---

## HANDOFF SNAPSHOT — captured 2026-07-31, verified by command

### Git state (exact)

```
HEAD        = bc65ceb36   (branch weighing-task-flow)
origin/main = 0cb9fc305   ← MAIN MOVED. Base of this branch was 8c3954ca1.
commits ahead of origin/main: 2
```

`origin/main` advanced by one commit while this work was in progress:
`0cb9fc305 Add goat accelerometer behaviour research note`.
It added NO migrations (`git diff --name-only 8c3954ca1..origin/main -- backend/migrations/postgres/`
is empty), so blocker 1's renumber target is unchanged for now — but RE-CHECK at
pickup time, because main can move again and the whole point of blocker 1 is to keep
weighing migrations at the tail.

### Both commits are LOCAL-ONLY — nothing has been pushed

```
bc65ceb36 → contained in 0 remote branches
14cd0b614 → contained in 0 remote branches
```

- `14cd0b614` — Phase 1: close/reopen, verdict consumer, FCM up/down, Android 20-row
  cursor pagination, 3 weighing guards, TRD/RBAC/QA docs. 48 files.
- `bc65ceb36` — CEO-assistant coverage exclusion rows (`docs/ceo-ai/coverage-matrix.md`).

### Phase 2 is an UNCOMMITTED DRAFT — 13 modified, 9 untracked

```
 M Makefile
 M backend/cmd/kernel-worker/main.go
 M backend/internal/bootstrap/api.go
 M backend/internal/notificationbridge/weighing_lifecycle_notify_consumer.go
 M backend/internal/permissions/routes.go
 M backend/internal/weighing/adapters/http/handler.go
 M backend/internal/weighing/adapters/postgres/repository.go
 M backend/internal/weighing/app/service.go
 M backend/internal/weighing/ports/repository.go
 M context/architecture/domain-event-registry.json
 M docs/features/weighing/TRD.md
 M tools/ci/guardrail-manifest.json
 M tools/ci/run-local-ci.sh
?? backend/cmd/kernel-worker/weighing_kernel_wiring_test.go
?? backend/internal/kernelstages/weighing_kernel.go
?? backend/internal/notificationbridge/weighing_work_item_cadence_notify_test.go
?? backend/internal/weighing/adapters/postgres/kernel.go
?? backend/internal/weighing/adapters/postgres/kernel_integration_test.go
?? backend/internal/weighing/domain/kernel.go
?? backend/migrations/postgres/000059_weighing_kernel_work_items.sql
?? context/repo-audits/weighing-phase1-2-do-not-merge-blockers.md   (this file)
?? tools/agent-hooks/check-weighing-kernel-phase2-guard.mjs
```

`git diff --stat` on the tracked subset: **13 files, +1251 / -705**
(`guardrail-manifest.json` alone is ±1413 lines — inspect that diff for reformatting
noise before staging; it should only be adding the new guard entries).

NONE of Phase 2 has been verified by the coordinator. Audit it, do not trust it.

### FIRST COMMANDS for the fresh session (in this order)

```bash
cd "$(git rev-parse --show-toplevel)"
cat context/repo-audits/weighing-phase1-2-do-not-merge-blockers.md   # this file, all 12 blockers
git status --porcelain && git log --oneline -5 && git diff --stat
zsh -ic 'git fetch origin main'                                      # main may have moved again
cd backend && GOFLAGS=-buildvcs=false go build ./...                 # does the Phase 2 draft compile?
GOFLAGS=-buildvcs=false go vet ./internal/weighing/... ./internal/kernelstages/... ./cmd/kernel-worker/...
cd .. && make guardrails                                             # expect india-date-guard + vaccination-hrms-seed-fixture-guard to FAIL
git diff tools/ci/guardrail-manifest.json | head -60                 # confirm it is additive, not reformatted
```

Then fix blockers 11 and 12 FIRST — they are the only two things between the current
tree and a green `make guardrails`. After that work blockers 1-10.

### GATES REQUIRED BEFORE PUSH (all green, on the exact final squashed SHA)

```bash
make validate-migrations          # 000022/000031 are baseline failures; nothing weighing may fail
make validate-sqlc-plans
make guardrails
cd backend && GOFLAGS=-buildvcs=false go build ./... \
  && go test ./internal/weighing/... ./internal/notificationbridge/... ./internal/kernelstages/... ./cmd/kernel-worker/...
GOATOS_RUN_POSTGRES_TESTS=1 go test ./internal/weighing/adapters/postgres/   # expect ONLY the 11 baseline failures
cd ../apps/goatos-android && ./gradlew :core:core-data:testDebugUnitTest --tests "*Weighing*" --rerun-tasks
npm --prefix apps/admin-web run check:mock-fidelity
cd "$(git rev-parse --show-toplevel)" && make ci-local
```

Plus judge/review-lens against `context/repo-audits/weighing-implementation-do-not-reopen-ledger.md`.

Then: rebase onto current `origin/main`, squash `14cd0b614` + `bc65ceb36` + Phase 2 into
ONE commit, re-run the diff-scoped guards after staging (they diff against origin/main,
so they only see committed work), and push with `zsh -ic 'git mesha-push main'`.

### Do NOT
- push anything until every gate above is green
- touch the other session's dirty files in the primary goatos checkout
- trust any agent report without re-reading the files and re-running the command
