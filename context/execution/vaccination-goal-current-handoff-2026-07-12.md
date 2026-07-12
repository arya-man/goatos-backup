# Vaccination goal handoff — current HEAD truth — 2026-07-12

This is the handoff for the next Codex/Claude session. It replaces the confusing
mid-session status chatter with the current repository truth.

## 0. Start here

Repo:

```text
/Users/ravi/mesha/goatos
```

Current HEAD at handoff time:

```text
d2a7fbcf85a381abdf98fa4b22aee42f7fda8b23
d2a7fbcf test(vaccination): harden kernel-backed E2E smoke
```

Important git fact:

```text
origin/main == HEAD == d2a7fbcf
```

So the latest vaccination/kernel/calendar work is already on `main`. The local
branch still says it is ahead of `origin/agent/vaccination-seed-history-calendar`
because that old feature branch remote is behind; do not confuse that with main.

## 1. Current local dirt

After a fresh verification run, local status was:

```text
M  backend/tests/e2e/report/index.html
?? context/repo-audits/last-35-commits-consolidated-bug-ledger.md
?? context/execution/vaccination-goal-current-handoff-2026-07-12.md
```

Notes:

- `backend/tests/e2e/report/index.html` changed only because the focused E2E was
  re-run for evidence; generated IDs/timestamps drift. Keep it only if you want
  to publish the latest generated local report, otherwise restore it.
- `context/repo-audits/last-35-commits-consolidated-bug-ledger.md` is untracked
  and looks like a broad audit ledger. Treat it as review material, not product
  truth.
- This file is the intended handoff doc.

## 2. What is already landed on main

Plain-English summary:

- Seeded historical vaccinations no longer become fake “late” open obligations.
- Historical accepted vaccination completions are visible as history.
- Future vaccination obligations are created by the kernel from accepted history /
  `vaccination.completed`, not by tests inserting future rows directly.
- Calendar and Action Center were hardened for seeded history + future work.
- The backend/admin-web leadership close path is proven through SOP review:
  operator submits proof, Director can rework, CEO/CXO can verify, obligation
  completes, stock moves, and the next yearly cycle is scheduled.
- Micro-drive / orphan singleton drive is not just “batch exists” anymore; it is
  covered through proof-backed submission, admin verify, SM-5 completion, and
  SM-7 recurrence.
- E2E integrity guard blocks the old fake pattern where tests directly mutate
  derived business state instead of going through generation/SOP/review/kernel.

Recent commits to inspect first:

```text
d2a7fbcf test(vaccination): harden kernel-backed E2E smoke
8725693e docs(handoff): log pending/deferred issues for next session
98361333 fix(calendar): harden drive target projection tests
dc5ab532 fix(calendar): harden catchup drive summaries
ebf33812 fix(vaccination,workforce): review fixes — migration gate, park-scope, task_id optional, contract sync
efb1a910 fix(vaccination): scope seeded history obligations
e2e0e8fc fix(calendar): stabilize history ids and localize android copy
6409d9db fix(vaccinationexecution): complete keyset ScanRoster/operations refactor + real query-bug fixes
9d01c907 Close vaccination calendar and kernel E2E gaps
```

## 3. Verification run on current HEAD

This passed on current HEAD during handoff:

```bash
cd /Users/ravi/mesha/goatos/backend
GOCACHE=/private/tmp/goatos-handoff-gocache go test ./tests/e2e \
  -run 'TestKernelStoryAA_OrphanSingletonShedDrive|TestKernelStoryAJ_LeadershipReviewRoutes|TestKernelStoryAG_RecurringCalendarLifecycle|TestReportCertificationCompletenessRendersAndCountsMissingSurface' \
  -count=1 -v
```

Result:

```text
PASS
ok github.com/vgoats/goatos/backend/tests/e2e 15.207s
```

Also passed:

```bash
cd /Users/ravi/mesha/goatos
bash tools/agent-hooks/check-e2e-kernel-integrity.sh
```

Result:

```text
e2e-kernel-integrity: passed (82 source artifacts scanned)
```

Do not list `tools/agent-hooks/check-server-pagination.mjs` as current-main
evidence: that script is not present at this HEAD.

## 4. What is still pending

### P1 — Android app cannot truly finish a real vaccination drive yet

This is the main remaining product gap.

Current evidence:

- `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/SubmitViewModel.kt`
  still loads `repo.tasks(limit = 1)`.
- Same file explicitly says answers/proof mapping is not wired and submission is
  sent with empty answers.
- The request currently built in `submit()` is only:

```kotlin
SubmitTaskRequestDto(sopVersionId = current.sopVersionId, idempotencyKey = key)
```

Plain English: Android can enqueue a task submission shell, but it does not yet
submit the selected drive's real scanned goats, vaccine lot, cold-chain answer,
administered time, and completed proof refs.

Fix direction:

1. Carry exact `task_id`, `sop_version_id`, `row_version`, `batch_id`, `shed_id`
   from shed/task selection into scan and submit.
2. Load the exact task detail, not first task from `repo.tasks(limit = 1)`.
3. Persist scan draft keyed by task + row version.
4. Submit real `answers.goat_ids`, `vaccine_lot_id`, `cold_chain_verified`,
   `administered_at`, `route_site`, dose fields, and completed `proof_refs`.
5. Add Android unit tests proving the request body is real and task-bound.

### P1 — Android leadership verify/rework screen/action is not implemented

Current backend/admin-web review routes exist:

```text
POST /admin/tasks/{task_id}/verify
POST /admin/tasks/{task_id}/rework
```

Current Android app API does **not** expose app verify/rework endpoints at HEAD.
`contracts/openapi/app-api.yaml` only has:

```text
/app/vaccination/tasks/{task_id}/option-values
```

Current mobile record screen is read-only:

```kotlin
sealed interface RecordEvent {
    data object Close : RecordEvent
}
```

Plain English: Director/CEO/CXO/Park Manager can close through admin-web, but
there is no production Android “accept/rework this vaccination proof” button yet.

Fix direction:

1. Decide route surface:
   - either add app endpoints mirroring admin review:
     `/app/vaccination/tasks/{task_id}/verify` and `/rework`, with scoped grants;
   - or deliberately make mobile open an admin-web review surface.
2. Add AppApi/NetworkModule DTOs and repository functions.
3. Add leadership/record UI actions hidden for operators.
4. Add tests proving operators cannot verify/rework and leadership can.
5. Add kernel E2E or contract test proving app route fans into SOP review fanout
   and `vaccination.completed` exactly once.

### P1 — Multi-task shed → exact task selection → scan is still a mobile UX gap

If a shed has several vaccination tasks, the app needs a clear per-task path. Do
not group drives by vaccine; vaccination drives are shed/task based, and a shed
can contain multiple vaccine obligations.

Fix direction:

- Record/shed drilldown should list executable tasks/drives.
- Selecting one task should navigate to scan with exact task identity.
- Scan should page animals, not fetch 400/1000 rows at once.
- The next session should verify this in `AppNavHost`, `RecordViewModel`,
  `ShedsViewModel`, `ScanViewModel`, and backend execution DTOs.

### P1/P2 — Read-model / scale debt remains

Do not claim the system is million-animal clean yet.

Known shape:

- Some process-integrity / vaccination-execution reads still have baselined
  heavy-query debt.
- The goal is to move hot screens to incremental projections/read models, not
  make request-time god queries bigger.

Use `tools/scale-guard/baseline.txt` and `docs/decisions/scale-anti-patterns.md`
as the starting map.

### P2/P3 — Graph is stale

Understand graph baseline was older than HEAD. Deterministic hook analysis said:

```text
FULL_UPDATE recommended — 116 structural source files changed
```

If the next session needs graph-backed architecture review, run a full
`/understand --full` or equivalent first. For ordinary implementation, direct
git/file/test evidence is enough.

## 5. Do not redo / do not trust blindly

- Do not redo seeded-history backend fixes unless current tests prove a
  regression.
- Do not trust the older committed
  `context/execution/pending-issues-handoff-2026-07-12.md` blindly. It contains
  stale mid-session claims, e.g. the old `RoleManager` compile issue is no
  longer true at current HEAD.
- Treat `context/repo-audits/last-35-commits-consolidated-bug-ledger.md` as the
  one canonical audit queue, and apply
  `context/repo-audits/consolidated-ledger-defect-closure-program.md` before
  changing any row to fixed. Neither file overrides product/medical source
  contracts; they govern defect tracking and proof.
- Do not add E2E by direct SQL insertion of future obligations. It must go
  through production generation/sweeper/SOP/review/completion/consumer paths.

## 6. Suggested next-session order

1. Clean/decide local dirt:

   ```bash
   git status --short --branch
   ```

2. Reconfirm current HEAD and main:

   ```bash
   git fetch origin main
   git rev-parse HEAD origin/main
   git log --oneline --decorate --max-count=12
   ```

3. Pick one of these, in order:

   - Android real selected-drive submit request body.
   - Android leadership verify/rework action + app/backend route decision.
   - Multi-task shed task picker into scan.
   - Read-model/scale debt.

4. For Android submit, start at:

   ```text
   apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/SubmitViewModel.kt
   apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ScanViewModel.kt
   apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/TasksRepository.kt
   apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/sync/SyncRepository.kt
   apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/sync/SyncEngine.kt
   apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/AppApi.kt
   contracts/openapi/app-api.yaml
   ```

5. For backend review authority, start at:

   ```text
   backend/internal/permissions/routes.go
   backend/internal/sop/adapters/http/handler.go
   backend/tests/e2e/story_aj_leadership_review_routes_test.go
   backend/tests/e2e/story_aa_orphan_singleton_shed_drive_test.go
   ```

## 7. Layman status for Ravi

Backend/admin/calendar: mostly done and pushed.

Android app: not done. The phone still does not fully perform “scan goats →
attach proof → submit real vaccination → leadership accept/rework → schedule
next cycle” in production code.

Main next job: make Android stop pretending with empty submissions and wire the
real selected vaccination drive flow.
