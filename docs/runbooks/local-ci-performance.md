# Local CI Performance: Job Model, Flags, and Wall-Clock

**Scope.** How `make ci-local` decides what to run, every environment flag that
changes its behaviour or its duration, which command to run when, and the
measured cost of each part.

**Companion docs — read together, they do not repeat each other:**

- `docs/runbooks/local-ci-and-landing.md` — the landing path end to end, the
  receipt schema, the iteration loop, and the screenshot policy.
- `docs/runbooks/local-release-evidence.md` — the exact-SHA push evidence gate
  and the pre-push hook (authoritative for gate semantics).
- `docs/runbooks/local-ci.md` — guardrail registration.

> Main landing has no local-CI bypass. Speed comes from accurate job selection
> and timing evidence, not from skipping the exact-SHA receipt gate.

---

## 1. The job model

`tools/ci/run-local-ci.sh` runs one or more **jobs**. The job list is either
auto-selected by the classifier, forced to everything, or named explicitly.

| Job | What runs (source: `run-local-ci.sh`) |
|---|---|
| `common` | `git-identity-guard`, `guardrail-registration-guard`, `local-stack-service-guard`, `local-ci-evidence-guard`, `domain-event-architecture-guard`, `operational-read-model-contract-guard`, `critical-animal-action-availability-guard`, `leadership-assistant-coverage-guard`, `assistant-route-closure-guard`, **`telemetry-guard`**, `ai-doctor`, `stg-promotion-guard`, boundaries (+ its self-test), refresh-binding, UI vaccine labels, vaccination shared-source sync, calendar endpoint grain, contract-drift, **`screenshot-remediation-guard`**, **`push-hook-freshness-guard`**, **`parallel-dispatch-cleanup-guard`**, the **`gradle-worktree-lock` guard and its 19-mutant self-test** (both diff-scoped — see §5), large-file guard, whitespace `git diff --check`, plus the `tools/ci/**` self-test suite **only when the diff touches `tools/ci/`** (`ci_tooling_changed`, `run-local-ci.sh:209`) |
| `backend` | Go build/vet/tests, backend foundations + scale/kernel/idempotency/read-model guards, migration + sqlc static checks, `govulncheck`, CEO-AI eval self-test, and (opt-in only) the Postgres/Docker E2E chain |
| `query-plans` | `make validate-sqlc-plans` — required for every backend diff; index regressions must fail the local gate |
| `admin-web` | `npm ci` (only if `node_modules` is missing), lint, typecheck, unit tests, **production build + token-leak check**, request-reads guard, prefetch guard, local-overlay guard, mock fidelity |
| `android` | All Android static guards (`mobile-guard`, navigation-stack, bounded-memory, room-migration, screenshot-proof coverage guard, …), then **one** Gradle invocation for `:app:compileStgReleaseKotlin :app:testStgReleaseUnitTest :app:lintStgRelease` (`run-local-ci.sh:522`), optional Paparazzi screenshots, then `:benchmark:compileDevNonMinifiedBenchmarkKotlin`. Default = **2** `--no-daemon` Gradle bootstraps (3 with the screenshot opt-in) |
| `guardrails` | The Gradle-free compatibility lane: `run_common` + **the full `backend` job** + the Android *static* guards (`run_guardrails`, `run-local-ci.sh:558`). No Gradle, no npm build. **Writes no receipt.** |

**Jobs run in parallel.** `tools/ci/parallel-dispatch.sh` dispatches the selected
jobs concurrently (default width 3, `GOATOS_CI_LOCAL_JOBS`, hard-clamped 1..4;
width 1 degrades to sequential). Contending jobs are grouped and never overlap
(`android` → `gradle`; `backend` → `docker`; `query-plans` → OCI tunnel /
`GOATOS_SQLC_PLAN_ADMIN_DSN`). This changes how
jobs run, never which. Details and the failure-accounting contract:
`docs/runbooks/local-ci-and-landing.md` §10.

### How auto-scoping picks jobs

`tools/ci/ci-scope.mjs` diffs `${GOATOS_CI_BASE:-origin/main}...HEAD` and maps
each changed path through `tools/ci/component-paths.json`:

- `common` is **always** selected (`selectedJobs` is seeded with it
  unconditionally).
- `query-plans` is selected only for SQL, migrations, Postgres repository code, or broad API contract fanout.
- High-risk shared files in `forceFull` force backend + admin-web + android; scoped CI helper files run common self-tests plus the originally affected components.
- An **unmapped** path also forces the full suite — the classifier fails safe.
- No diff paths at all ⇒ full suite.

`full=true` (a `forceFull`/unmapped reason *and* all three components) records
`mode=all`; otherwise the run records `mode=scoped` plus the exact base and the
selected job list.

**Consequence for anyone editing CI itself:** CI helper changes are scoped through
`ciCommonOnly` when their own self-tests cover the behavior. High-risk shared
workflow/Makefile changes still force a full-suite landing.

---

## 2. Every flag that affects local CI

Defaults below were read from the code, not from a report. `Receipt?` answers
the only question that matters for landing: *can a run with this flag set
authorise a push to `main`?*

| Flag | Default | What it changes | Wall-clock | Receipt? |
|---|---|---|---|---|
| `GOATOS_FAST_LOCAL_CI` | `0` (`run-local-ci.sh:44`) | Android uses the Gradle **daemon** and one combined `:app:compileStgReleaseKotlin :app:testStgReleaseUnitTest :app:lintStgRelease` invocation; the benchmark compile is skipped unless the diff touches Android build files | Large saving on the Android job (daemon reuse + one configuration phase instead of three) | **NO — never writes a receipt.** This is the inner-loop flag |
| `GOATOS_RUN_ANDROID_SCREENSHOTS` | `0` (`run_android`) | Opt-**in** for the Paparazzi proof. Governs both the fast and the normal Android path — one knob, not two. Known UI diffs may use the guarded targeted mapper (`tools/ci/android-screenshot-scope.sh`); unknown UI/resource diffs fall back to full `:app:verifyPaparazziDevDebug` | Adds the single-threaded `devDebug` screenshot build. Targeted feed mapping avoids unrelated screenshot methods, but still pays the app test build. Full fallback measured floor ≈100 s; on a cold worktree, materially more | Yes (it does not itself suppress the receipt). Sets `screenshots=yes` on the receipt |
| `GOATOS_RUN_POSTGRES_TESTS` | `0` (`:121`) | Opt-in for the Docker/Postgres integration + E2E chain (`e2e-image-build`, `e2e-parity`, `e2e-smoke`, `e2e-business-chain`) and `go test ./...` with Postgres enabled. When `0`, the backend job still runs `go test ./...` with Postgres disabled. This flag does not control required SQL query-plan validation; that gate uses `GOATOS_SQLC_PLAN_ADMIN_DSN` against the OCI tunnel. | Very large when on (image build + compose stacks) | Yes. Default `0` is the normal landing posture |
| `CEO_AI_EVAL_LIVE` | `0` (`:229`) | Runs the **live** CEO-AI answer-quality eval against a real assistant endpoint (Vertex/Gemini) + Postgres oracle. Also needs `MESHA_ASSISTANT_URL`, `GOATOS_EVAL_DATABASE_URL`, `GOATOS_EVAL_TENANT_ID`. The cheap structural self-test always runs | Adds network-bound eval time | Yes. Default `0`; the skip is a loud `SKIP`, never a silent pass |
| `GOATOS_CI_BASE` | `origin/main` (single resolver: `run-local-ci.sh` `resolve_ci_base`) | The diff base for the classifier AND the Android UI-diff detector. An unresolvable ref falls back to `HEAD~1` **loudly**, and that fallback is FATAL (exit 4) on the receipt-writing `auto`/`all` modes | Indirect — a narrower base selects fewer jobs | Yes: the resolved base is recorded on EVERY receipt (`all` and `scoped`) and must be an ancestor of real remote main at push time |
| `MODE=all` (make var) | unset | `make ci-local MODE=all` forces every job | Maximum | Yes, `mode=all` |
| `JOB=<job>` (make var) | unset | `make ci-local JOB=common\|backend\|query-plans\|guardrails\|admin-web\|android` runs exactly that job. Does NOT enable `GOATOS_FAST_LOCAL_CI` — for the iterate loop use `GOATOS_FAST_LOCAL_CI=1 tools/ci/run-local-ci.sh <job>`, or `make ci-local JOB=android` silently takes the slow `--no-daemon` path and also pays the benchmark compile, while the RED banner's script-shaped command does neither. The dominant saving on a landing is the Gradle daemon on `:app compile+unit+lint`: **84–288 s** without the daemon vs **17–50 s** with it. The benchmark compile itself is modest: **2–24 s** | Minimal | **NO — partial runs never write a receipt** |
| `GOATOS_BYPASS_LOCAL_CI` | unsupported | Rejected by `land-main`; ignored by the pre-push evidence path | None | **NO.** Main requires a green exact-SHA receipt. |
| `GOATOS_LAND_MAX_ATTEMPTS` | `3` (`land-main.sh:90`) | How many rebase→full-CI→re-fetch attempts `land-main` will make when `main` moves under it | A contended `main` can pay the **entire** gate up to 3×. This dominates a bad landing | n/a. Lowering it is a failure mode, not a speedup |
| `GOATOS_LAND_TEST_MODE` / `GOATOS_LAND_TEST_CI_COMMAND` | `0` / unset (`land-main.sh:67`, `:124`) | Used by `tools/ci/land-main.test.sh` to drive `land-main` with a fake CI command | n/a | Test harness only |
| `GOATOS_CI_LOCAL_JOBS` | `3` (`parallel-dispatch.sh:43`, hard-clamped 1..4) | Parallel job-dispatch width. `1` degrades to sequential. Never changes WHICH jobs run; contending jobs (`android`→gradle, `backend`→docker, `query-plans`→OCI tunnel / `GOATOS_SQLC_PLAN_ADMIN_DSN`) are grouped and never overlap regardless of width | Wall-clock only | Yes — receipt semantics are untouched by dispatch width |
| `GOATOS_CI_TRACE_ONLY` | unset (`check-local-ci-evidence.mjs:264`) | Reachability probe used by `check-android-screenshot-proof.sh`: `step` prints `CI-TRACE <name> :: <cmd>` and executes nothing, and the script `exit 3`s **before** the receipt block | Near-zero (nothing executes) | **NO — `--record` independently refuses while it is set.** Not a bypass |
| `GOATOS_CI_GRADLE_LOCK` | `1` (`gradle-worktree-lock.sh`) | Machine-wide advisory mutex around the Gradle region of the `android` job, so two worktrees QUEUE instead of both crawling. `0` opts out entirely and touches no filesystem | Saves nothing on a solo run (an uncontended `mkdir` succeeds instantly). On an overlapping pair it replaces two mutually-slowed legs (recorded 413 s + 212 s) with two sequential solo-speed ones (recorded solo range 84-181 s), so the second worktree WAITS rather than thrashing — the lock makes no single pass faster | Yes — the lock changes start time only |
| `GOATOS_CI_GRADLE_LOCK_TIMEOUT` | `1800` | How long `gradle_lock_acquire` waits before it gives up and PROCEEDS UNLOCKED, loudly. It never fails, never skips the job, never returns non-zero | Bounds the worst-case wait | Yes — the lock changes start time only |
| `GOATOS_CI_GRADLE_LOCK_STALE_SECONDS` | `1500` | Age at which a lock whose owner cannot be VERIFIED (a foreign hostname, a `hostname` flap on this same box, or a pid that is alive but is a different process) expires. Must stay BELOW `_TIMEOUT`, or the branch is unreachable at default settings and the only escape is the fail-open path. A same-host owner that is provably dead is reclaimed immediately, regardless of age | Bounds recovery from an unverifiable lock | Yes — the lock changes start time only |
| `GOATOS_CI_GRADLE_LOCK_DIR` | `$TMPDIR` | Directory holding `goatos-gradle-lock.<md5 of realpath(GRADLE_USER_HOME)>`. Exists so the guard can run in a sandbox; not a knob for normal use | n/a | Yes — the lock changes start time only |
| `JAVA_HOME` | `/opt/homebrew/opt/openjdk@21` (`:456`) | JDK for all Gradle steps | n/a | n/a |
| `ANDROID_HOME` | `~/Library/Android/sdk` (`:457`) | Android SDK; also exported as `ANDROID_SDK_ROOT` | n/a | n/a |

Both `GOATOS_CI_GRADLE_LOCK_TIMEOUT` and `GOATOS_CI_GRADLE_LOCK_STALE_SECONDS`
are sanitised for **shape and magnitude** before use, and a rejected value is
announced on stderr. This is not defensive decoration: `[ "$waited" -ge "$timeout" ]`
exits with status 2 on every poll for a non-numeric *or* an all-digit-but-oversized
value, so the fail-open branch can never fire and `acquire` hangs forever — the
exact unbounded-wait class the fail-open contract exists to prevent. Guard case (o).

**Removed flags — do not reintroduce:**

- `GOATOS_SKIP_ANDROID_SCREENSHOTS` — **deleted.** It let a run skip the
  Paparazzi proof and still write a full `mode=all` receipt with no record of
  the omission. That is the receipt hole this work closed.
  `tools/ci/check-android-screenshot-proof.sh` fails if the string reappears.
- `GOATOS_FORCE_ANDROID_SCREENSHOTS` — **renamed** to
  `GOATOS_RUN_ANDROID_SCREENSHOTS` so the fast path and the normal path share
  one knob.

`GOATOS_REQUIRE_DOCKER=1` and `GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token`
appear in the script but are **set by the script itself** for specific steps.
They are not user-facing knobs.

---

## 3. Which command do I run?

```
Am I iterating on a fix?                     -> GOATOS_FAST_LOCAL_CI=1 tools/ci/run-local-ci.sh <job>
Did one job just go red and I fixed it?      -> tools/ci/run-local-ci.sh <job>        (no receipt)
Am I iterating on goldens / Compose UI?      -> GOATOS_FAST_LOCAL_CI=1 GOATOS_RUN_ANDROID_SCREENSHOTS=1 tools/ci/run-local-ci.sh android   (Gradle daemon; writes NO receipt)
Am I ready to land a UI change?              -> make ci-local-screenshots             (ONCE, on the final SHA; see §4)
Am I ready to land?                          -> make land-main                        (runs make ci-local for you)
Do I only want to certify, not push?         -> make ci-local
Do I distrust the classifier?                -> make ci-local MODE=all
```

**The loop that actually costs the hour** is: run the full gate → one guard
fails → fix one line → run the *entire* gate again. Do not do that. Iterate
with `GOATOS_FAST_LOCAL_CI=1` or a single `JOB=`, and run the full
`make ci-local` **once**, at the end, to certify. A red run now prints the exact
single-job re-check commands for the jobs that failed.

Every fast/partial run prints, in plain words:
`fast/partial run — NO receipt written; this cannot authorise a push.`

---

## 4. Running the full Paparazzi screenshot proof on demand

Screenshots are **opt-in**. The default landing run — including the run that
writes the push receipt — does not run them and says so in a banner.

```bash
make ci-local-screenshots
# == GOATOS_RUN_ANDROID_SCREENSHOTS=1 bash tools/ci/run-local-ci.sh auto
# MODE=all  -> ... run-local-ci.sh all
```

**`MODE` is CLAMPED here** (`Makefile`, `ci-local-screenshots`:
`$(if $(filter all,$(MODE)),all,auto)`). Only `all` is honoured; anything else
— including `MODE=android` — resolves to `auto`. Passing `$(MODE)` through raw
reopened the exact hole this target exists to close, because `MODE=android`
became an explicit-job PARTIAL run, and partial runs write no receipt. There is
deliberately **no `JOB=` hook** on this target.

It is a **complete** run, not a screenshots-only partial. That matters because
only a complete auto-scoped/full run writes a push receipt: while this target
passed the explicit `android` job it was partial, wrote nothing, and therefore
could not clear the `skipped-with-ui-diff` push block it is advertised as the
fix for. `make screenshot-remediation-guard` now proves the link by execution.

Because it is complete and writes the receipt, running `make ci-local` first and
this second pays the entire suite twice for one landing — **run exactly one of
them.** And prove screenshots **once, on the SHA you intend to push**: proving
them on every intermediate commit costs a full pass each time and certifies
nothing you will push, since a rebase or amend changes the SHA and voids the
receipt anyway (measured: five `android screenshots` steps, 109-208 s each, on
five DIFFERENT SHAs inside one 42-minute block).

This runs `:app:verifyPaparazziDevDebug`. For known feed UI/golden diffs, the
runner adds a guarded allowlist of seven feed screenshot filters: feed role
chrome, feed direction, feed distribution, feed transport, and feed packing.
Unknown Android UI/resource diffs still run the full task with no filters.
`tools/ci/check-android-screenshot-proof.sh` (wired into the `android` job and
into `make land-main-self-test`) fails the build if any screenshot invocation
uses raw ad-hoc `--tests`, loses the task name, or if the opt-in becomes
unreachable. It has negative self-tests proving both the proof guard and the
targeted mapper are live, not inert.

**Run it whenever you intentionally change UI:**

- anything under `apps/goatos-android/**/ui/**`
- the design system, theme, typography, or colours
- `apps/goatos-android/app/src/test/snapshots/**`
- any Compose screen you expect to look different

`ci-local` now helps: if the diff could change a screenshot and screenshots did
not run, it prints a distinct banner and records
`screenshots=skipped-with-ui-diff` on the receipt instead of the quiet
`skipped` — **and that receipt is refused at push time**
(`check-local-ci-evidence.mjs:240`). Detection is content-aware
(`tools/ci/android-ui-diff.sh`): it scopes by path, then asks whether the
changed file declares or uses `@Composable`, and treats goldens and
Compose-visible resources (`res/values*`, `res/drawable*`, `res/font*`,
`res/mipmap*`) as UI unconditionally. It over-detects by design.

> **Known state:** `make ci-local-screenshots` is currently **RED** (~15 golden
> failures in `ScanEdgeCaseScreenshotTest` and `ScanAutoProofFlowScreenshotTest`).
> The root cause is identified and is NOT render noise: those classes pass
> `lastSyncedAt = 0L` while `SyncStatusIndicator`'s default `nowMillis` is a live
> `System.currentTimeMillis()`, so the rendered label drifts by one day per day.
> Re-recording goldens is forbidden (and would buy one day of green); the fixture
> must be made deterministic first, and re-recording is then a maintainer action.
> Analysis: `docs/runbooks/local-ci-and-landing.md` §3. Until it is resolved, the
> on-demand hatch does not give you a green screenshot proof. Do not present it
> as if it does. To be explicit about what the opt-in default means: `ci-local`
> no longer PAYS for screenshots by default. It **does not mean screenshot
> coverage currently exists** — the proof is red for the known fixture reason
> above. Hash-keyed reuse of a Paparazzi proof across SHAs is rejected outright,
> not deferred: it turns the receipt's `screenshots:"yes"` from "proven on this
> SHA" into "believed equal to some earlier SHA", where the belief is a
> hand-written hash of Paparazzi's full Gradle input set.

The old path-only pattern missed `feature/feature-*/…Screen.kt` and
`core/core-designsystem/`; the content-aware detector above now catches them.
It is still a trigger for the proof, not the proof itself — keep following the
"whenever you intentionally change UI" rule above, which does not depend on the
detector.

---

## 5. Measured wall-clock

**Separate the measured from the estimated.** Very little of the Android leg
has been measured, and the largest number in this document is a floor, not a
result.

### Measured

Source: `.git/goatos-ci-local-timings.tsv`, written by the new step-level timing
instrumentation, plus standalone guard runs.

| Item | Measured |
|---|---|
| `telemetry-guard` (standalone) | 5.5 – 7 s per invocation |
| `assistant-route-closure-guard` | 25 s (single slowest `common` step observed) |
| `admin-web production build + token leak` | 19 s (warm `node_modules`, warm `.next`) |
| `admin-web lint` | 10 s |
| `local-ci-evidence-guard` | 8 – 11 s |
| `ci-local JOB=common` (at the `telemetry-guard` hoist) | ≈26 s — **superseded**: measured before the `tools/ci/**` self-test suite and the three new guards existed. It is not a current figure for `common` |
| `ci-local JOB=admin-web` (after) | ≈33 s |
| `telemetry-guard` de-duplication (it ran twice; now once) | **≈6 s saved per full suite** |
| `tools/ci/**` self-test suite when it ran unconditionally | ≈59 s added to `JOB=common` (of which the screenshot-proof suite alone ≈62 s guarding a 0.18 s check) — source: the measurement recorded in `run-local-ci.sh:304-309`. Now diff-scoped, so it is ≈0 on a commit with no `tools/ci/**` diff |
| `android screenshots` (Paparazzi) | Observed **109, 116, 123, 148, 208 s** — five runs on five different SHAs across a single 42-minute session. Range: **109–208 s**. These observations are not in the git-tracked timings TSV because Gradle runs were not profiled during this work. Do not cite numbers from memory or previous sessions; this range is the only measured evidence on this machine |
| android legs running CONCURRENTLY in separate worktrees | `:app compile+unit+lint` cost **413 s and 212 s** in a two-way overlap (epochs 1785995260 / 1785995293) and **340 s / 251 s / 361 s** in a three-way one (epochs 1785967083-1785967590), against **84-181 s** for runs no other recorded run overlapped. This is what the Gradle worktree lock exists for; queuing is faster than racing. Honesty note: two solo-recorded runs sit at 288 s and 413 s with no overlapping run to explain them — most likely a maintainer's manual `./gradlew`, which is the KNOWN LIMIT the lock does not cover |
| `gradle-worktree-lock guard` (`check-gradle-worktree-lock.sh`) | **44-46 s** wall over five runs, of which case (h) — 8 racers x 3 rounds proving a concurrent stale break admits exactly one — is **≈23 s**. Diff-scoped in `run_common` to a `tools/ci/gradle-worktree-lock.sh` **or `run-local-ci.sh`** diff — cases (g)/(g3) assert properties of the latter — so **≈0 s** on every other commit |
| `gradle-worktree-lock guard self-test` (23 mutants) | **17 min 16 s** measured (~52 s/mutant; the mutants that HANG cost the harness's 150 s budget each). Diff-scoped to the lock library, the guard, the harness, or `run-local-ci.sh` — deliberately NOT to all of `tools/ci/**`, or every CI-tooling commit would pay 15 minutes for a change to an unrelated file |
| `:app` compile+unit+lint collapsed into one Gradle invocation | **≈17 s saved per android leg** — three `--no-daemon` invocations pay ~30 s of fixed JVM-start/configuration/up-to-date overhead vs 12.7 s paid once, on a 12-core/JDK-21 box (`run-local-ci.sh:505-511`) |

### Estimated — not results

| Item | Estimate | Why it is only an estimate |
|---|---|---|
| `ci-local JOB=common` before | ≈19 s | Derived by subtracting the newly-hoisted `telemetry-guard` |
| `ci-local JOB=admin-web` before | ≈38.5 s | Derived by adding the removed duplicate `telemetry-guard` back |

### Not measured at all

The Android job was **never profiled** — no Gradle run was performed in this
work, and `.git/goatos-ci-local-timings.tsv` contains no android row. The
following are the leading suspects for a long landing and **none of them has a
number**:

- the remaining cold `--no-daemon` Gradle bootstraps in the Android job — now
  **two** by default (combined `:app` compile+unit+lint, then the benchmark
  compile), three with the screenshot opt-in. The collapse from four is measured
  above; the absolute cost of what remains is not
- `govulncheck` (whole-module SSA analysis; not covered by the Go test cache)
- a cold `npm ci` + cold `next build` in an isolated landing worktree
- the `land-main` multi-attempt rebase loop (up to 3× the entire gate)

### Honest summary

> Reproducible *engineering* speedups now on the receipt path:
> **≈6 s** (`telemetry-guard` de-duplication), **≈17 s per android leg**
> (the three `:app` Gradle invocations collapsed into one), and **≈59 s off
> `JOB=common` on any commit that does not touch `tools/ci/**`** (the CI-tooling
> self-tests are diff-scoped). Parallel dispatch shortens the wall clock of a
> multi-job run further, but by an amount **nobody has measured** — do not quote
> a number for it.
>
> `JOB=common` is still **≈7 s slower** than before the `telemetry-guard` hoist,
> because that guard now runs there unconditionally, and it carries three
> additional guards (`screenshot-remediation`, `push-hook-freshness`,
> `parallel-dispatch-cleanup`) whose individual cost is unmeasured (the
> screenshot-remediation guard's header claims ~2 s; that is an author estimate,
> not a timing).
>
> Everything else that got faster got faster by **not running the Paparazzi
> proof**, which is a coverage trade (see §6), not an optimisation.

### Reading your own timings

```bash
column -t -s$'\t' "$(git rev-parse --git-path goatos-ci-local-timings.tsv)" | tail -40
```

Columns: `epoch`, `sha`, `step`, `status`, `seconds`. Every `ci-local` run also
prints a **slowest steps (top 10)** block at the end of its summary. The file is
inside `.git/`, is append-only, and is never committed.

### Cross-worktree Gradle contention

`parallel-dispatch.sh`'s `job_group()` serialises `android` within **one**
`run-local-ci.sh` process. It is entirely in-memory, so a second worktree is
invisible to it, and the builds crawl: **413 s and 212 s** in a recorded
two-way overlap, **340 s / 251 s / 361 s** in a three-way one, against
**84-181 s** for runs nothing else overlapped.

`tools/ci/gradle-worktree-lock.sh` is a machine-wide advisory mutex around the
Gradle region of the `android` job, keyed on `realpath(GRADLE_USER_HOME)` — the
contended resource is the shared Gradle caches and the cores, not the checkout.
It is an atomic `mkdir` lock (macOS has no GNU `flock(1)` and this tree has no
lock primitive). It is acquired **once**, after the cheap static guards and the
toolchain check, and released at both exits of `run_android`. The wait is
recorded as `android gradle lane wait` in the timings TSV, so the contention is
measurable next session instead of invisible.

**It can never fail a run.** No path returns non-zero; a timeout prints
`PROCEEDING WITHOUT THE LOCK` and continues; the wrapped command's exit status
is returned verbatim in every lock state; nothing in the file touches `fail`,
`RESULTS`, `screenshots_ran`, `receipt_mode`, or the receipt. A green push
receipt still means the full suite was green on that exact SHA — the lock
changes **when** the android job starts, never what runs or how it is judged.

Properties worth knowing before you press Ctrl-C or read a wait banner, each
carried by a named guard case:

- A SIGKILLed holder (what `_dispatch_cleanup`'s KILL escalation produces)
  cannot release itself, so a dead same-host owner is reclaimed within one poll.
  Age expiry applies only to an owner whose liveness cannot be *verified*, and a
  live pid is checked against its recorded start time — `kill -0` proves
  liveness, never identity.
- The stale break is serialised and re-validated. A bare atomic `mv` is not
  enough: `rename(2)` is atomic with respect to the path, not the inode the
  decision was made about, so a losing racer's `mv` just moves the winner's
  fresh lock (measured: three processes inside at once).
- Ctrl-C or a TERM on the android lane now exits 130/143 instead of falling
  through into `android benchmark compile` — worth knowing before you press it.
  The re-raise targets the **acquiring shell**, not `$$`: inside
  `dispatch_jobs`' `( … ) &` the lane's `$$` is `run-local-ci.sh` itself, so
  signalling it would kill the whole suite mid-dispatch and orphan
  `GradleWorkerMain` JVMs holding build locks.

**Stated limits, rather than hidden:** a maintainer's manual `./gradlew` run
does not take this lock and still contends; worktrees with genuinely different
`GRADLE_USER_HOME` values (or different users) do not share a lock and get
today's behaviour; and the lock saves **nothing** on a solo landing.

---

## 6. What is still on the table (not done)

| Item | Value | Status |
|---|---|---|
| Collapse `:app` compile + unit + lint into one Gradle invocation | Removes 2 cold JVM starts and 2 configuration phases | **DONE** (`run-local-ci.sh:522`), every flag byte-for-byte unchanged; measured ≈17 s saved per android leg. Failure semantics unchanged (Gradle stops at the first failing task) |
| Replace `--rerun-tasks` with real Gradle input tracking | Large on repeat runs | Not attempted. Only acceptable with proof that staleness protection survives |
| `--max-workers=1` on the Android steps | Potentially large | **KEPT, marked NEEDS-PROOF** — `git blame` shows no cause, and "no cause found" ≠ "no cause". Measure with §5 before touching |
| Shared `.next/cache` outside the landing worktree, `npm ci --prefer-offline`, shared `GRADLE_USER_HOME` | Unknown | Blocked on real numbers from the timings TSV |
| Parallel job lanes | Wall-clock on multi-job runs | **DONE** (`tools/ci/parallel-dispatch.sh`, default width 3 via `GOATOS_CI_LOCAL_JOBS`, clamped 1..4). The feared failure mode — a green receipt for a red run — is eliminated by construction, not by avoidance: verdicts cross the process boundary only as atomically written status FILES, a missing/non-numeric status scores 97 = FAILED, and the parent asserts `accounted == launched == selected`. `screenshots_ran` crosses the same way and its absence on the android job fails the run. Proof: `tools/ci/check-run-local-ci-parallel.test.sh` + `check-parallel-dispatch-cleanup.sh`. **Speedup unmeasured** |

### Flags that look like cargo cult and are **not** — do not "discover" and delete these

| Flag | Named cause |
|---|---|
| `--rerun-tasks` (Paparazzi) | Added in `6569960b5` alongside `./gradlew --stop` + `rm -rf */build`; `8d1f97b4f` removed the `rm -rf`, leaving this as the **sole** staleness protection. Failure mode is `UP-TO-DATE` — the gate passes without running |
| `--no-daemon`, `--no-configuration-cache`, `-Dkotlin.daemon.enabled=false`, `-D/-Pkotlin.compiler.execution.strategy=in-process` | `f2fb96a1b` "Stabilize Android CI Kotlin tasks", `aa6104a3b` "Stabilize Android lint in local CI", plus the Firebase Perf ASM vs. unit-test Flow-fake issue. `gradle.properties` enables the configuration cache; CI overrides it **deliberately** — the plugin set (Paparazzi alpha, Hilt+KSP, google-services, Crashlytics, Firebase Perf, Baseline Profile) is a textbook config-cache blocker list |

---

## 7. What speed may never come from

- Making the exact-SHA gate weaker. `evaluatePush`, `normalizedJobs`,
  `computeScopedCoverage`, the exact-SHA match, the scoped base match, and the
  job-coverage check are frozen.
- New bypass environment variables.
- Deleting or narrowing any guard that `ci-local` runs.
- Making a fast/partial run write a receipt.
- Re-recording Paparazzi goldens to turn the proof green.
