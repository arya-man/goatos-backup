# Local CI and the Landing Path

How `make ci-local` and `make land-main` actually work, what the push receipt
does and does not attest, and where the wall-clock goes.

---

## 1. Gate semantics (read this first)

A push to `main` is authorized only by a green `make ci-local` on the **exact**
commit SHA being pushed. The receipt lives in the worktree git dir
(`.git/goatos-ci-local-receipt.json`), is never committed, and is checked by the
pre-push hook.

- **Exact SHA.** The receipt's `sha` must equal the SHA being pushed.
- **Scoped receipts** additionally record the remote-main `base`, the
  component-rule hash, and the complete classifier-selected job list. At push
  time the coverage is recomputed; a changed base, changed rules, a missing
  component, or a newly-full diff is rejected.
- **Partial `JOB=` runs never authorize a push.** `tools/ci/run-local-ci.sh backend`
  and friends deliberately write no receipt.
- **`GOATOS_FAST_LOCAL_CI=1` never writes a receipt.**

There is no local-CI bypass for `main`: speed comes from the scoped classifier,
not from skipping evidence. A push to `main` without a matching green exact-SHA
receipt is blocked.

---

## 2. Landing path end to end

`make land-main` →

1. refuse a dirty worktree (`--untracked-files=all`);
2. fetch fresh `origin/main`, rebase the candidate **before** CI;
3. run `make ci-local` (the full affected-component gate);
4. fetch `origin/main` **again**;
5. if main moved: rebase again, and reuse the receipt only when the rebased
   patch-id is identical **and** the classifier selection is unchanged;
   otherwise **rerun the whole gate**;
6. push the exact certified SHA.

The loop runs up to `GOATOS_LAND_MAX_ATTEMPTS` (default 3). **A contended main
can pay the gate up to three times.** That is the single largest known
contributor to a long landing, and it is not fixed by anything in this round.

Measured counterfactual (`node tools/ci/analyze-rebase-scope-widening.mjs 30`,
read-only, not a gate): over the last 29 adjacent-commit pairs on `origin/main`,
the classifier selection **changed across the base advance in 11 of 29 cases
(38%)**. Reuse requires exact job-set equality, so a *narrowing* refuses reuse
too. Per-attempt facts are now logged to `.git/goatos-land-main-attempts.log`.

---

## 3. Screenshot policy (Paparazzi)

**Screenshots are OFF by default, including in the run that writes the push
receipt.** Run them on demand:

```bash
make ci-local-screenshots     # GOATOS_RUN_ANDROID_SCREENSHOTS=1 + a COMPLETE auto-scoped ci-local
```

`ci-local-screenshots` is a **complete** run, not a screenshots-only one: it is
`make ci-local` with the Paparazzi opt-in ON. `MODE` is **clamped** to the two
receipt-writing modes (`$(if $(filter all,$(MODE)),all,auto)`): `MODE=all`
forces the full suite, and anything else — including `MODE=android` — resolves
to `auto`. There is deliberately no `JOB=` hook. Passing `MODE` through raw
reopened the hole below, because `MODE=android` became an explicit-job partial
run that writes no receipt. That is deliberate. It used to run the explicit `android` job, which is a
*partial* run — and partial runs write no receipt by design, so the command the
block message told you to run could never clear the block; the only working
exits were the undocumented `GOATOS_RUN_ANDROID_SCREENSHOTS=1 make ci-local` before the broad local-CI bypass was removed. A remediation that cannot remediate trains bypass
habits. It now writes a real receipt carrying `screenshots: "yes"`, and
`tools/ci/check-screenshot-remediation.sh` (in `run_common`) proves that
end-to-end on every run: it drives the real block, reads the `make` target out
of the message, executes it, and fails unless the blocked push is then allowed.

Run it when your diff touches:

- `apps/goatos-android/**/ui/**`
- the design system / theme
- `apps/goatos-android/app/src/test/snapshots/**`

`ci-local` will now shout at you: when your diff touches Android UI or
snapshots and screenshots did not run, the summary prints a banner, the receipt
records `screenshots="skipped-with-ui-diff"`, **and that receipt does not
authorize a push to `main`** (see §4).

**UI-diff detection is content-aware, not path-only.** The old detector matched
`^apps/goatos-android/.*(/ui/|/snapshots/)`, which missed ~37 of ~63
`@Composable` files — every `feature/feature-*/` screen,
`core/core-designsystem/`, and `app/src/main/.../feature/weighing/` have no
`/ui/` path segment. The detector now lives in `tools/ci/android-ui-diff.sh`: it
scopes by path and then discriminates by content (does the changed file declare
or use `@Composable`?), and treats goldens and Compose-visible resources
(`res/values*`, `res/drawable*`, `res/font*`, `res/mipmap*`) as UI
unconditionally. It over-detects rather than under-detects — a non-UI edit inside
a Compose file counts as UI, because the safe direction is "prove it". Its
self-test is `tools/ci/check-android-ui-diff.test.sh` (run by `run_common`).

> ### ⚠️ SEQUENCING BLOCKER — read before landing a UI change
>
> **`make ci-local-screenshots` is currently RED** (~15 golden failures in
> `ScanEdgeCaseScreenshotTest` (10) and `ScanAutoProofFlowScreenshotTest` (5)).
> Since `skipped-with-ui-diff` now *blocks* a main push, the documented way out
> is a command that is wired correctly but cannot pass today **on golden
> content**. (The separate defect — the remediation being a partial run that
> wrote no receipt and so could never clear the block whatever the goldens did —
> is fixed; see §3.) Until the goldens are resolved, an Android UI diff must fix
> or deliberately update the goldens before it can land.
>
> **Root cause is a wall-clock-dependent fixture, not toolchain drift.** Both
> failing classes pass `lastSyncedAt = 0L` (the Unix epoch) into
> `SyncStatusIndicator`, whose default `nowMillis` is a live
> `System.currentTimeMillis()`. The rendered label is therefore
> `(now - 0) / 86400s` days — it increments by one **every day**
> (`Updated 20663d ago` recorded vs `Updated 20670d ago` actual = 7 days later).
> Every other screenshot class passes because it uses `lastSyncedAt = null`
> ("Up to date") or `System.currentTimeMillis()` ("just now") — both
> time-invariant. This is the same defect class AGENTS.md already names under
> the business-day time rule.
>
> **Re-recording alone is wrong**: it buys exactly one day of green. The fixture
> must be made deterministic FIRST — preferably by threading the existing
> `nowMillis: () -> Long` seam at `SyncStatusIndicator.kt:45` through the scan
> screen call sites (`app/src/main/kotlin/sg/mesha/goatos/ui/Overlays.kt:572`
> and `:686`) so the test pins both sides — and only then re-recorded once, as a
> deliberate consequence of that change, reviewing all 15 deltas.
> `ScreenshotTest.kt:444` carries the same latent `lastSyncedAt = 0L` and should
> be fixed in the same pass. Re-recording goldens is a **maintainer** action;
> agents must not do it.

`GOATOS_SKIP_ANDROID_SCREENSHOTS` has been **removed**. It was a receipt hole:
it wrote a full `mode=all` receipt for a run whose screenshot proof never
executed, with nothing on the receipt saying so.

The anti-narrowing guard moved to `tools/ci/check-android-screenshot-proof.sh`
(the old in-file version grepped the script it lived in and matched its own
function body, so it was inert). Its negative self-test is
`tools/ci/check-android-screenshot-proof.test.sh`, wired into
`make land-main-self-test`. Intent is unchanged: **when** screenshots run they
must run the full `:app:verifyPaparazziDevDebug`, never narrowed with `--tests`.

---

## 4. Receipt schema

```json
{
  "sha": "<40-hex>",
  "mode": "all" | "scoped",
  "screenshots": "yes" | "skipped" | "skipped-with-ui-diff" | "not-applicable",
  "result": "green",
  "base": "<40-hex>",          // BOTH modes — the diff base the run used
  "jobs": ["common", "..."],   // scoped only
  "rulesHash": "<sha256>"      // scoped only
}
```

**`base` is recorded for `all` receipts too, and push-time validation checks it
against real remote main.** `mode: "all"` says every job ran; it does NOT say
every job saw the real diff. Both the job classifier and the Android UI-diff
detector diff against the base, so a base that is not behind remote main makes
the diff EMPTY: the `skipped-with-ui-diff` banner never fires and a genuinely
green `all` receipt records `screenshots: "skipped"` over a real UI change.
`GOATOS_CI_BASE=HEAD make ci-local` did that deliberately; an unresolvable
`origin/main` (bad network) did it accidentally via the `HEAD~1` fallback.

At push time `computeBaseAncestry` requires `receipt.base` to be an ancestor of
— or equal to — the remote main being pushed to. A base at-or-behind remote main
can only OVER-detect, which is safe. A missing base, or a base ahead of/diverged
from remote main, BLOCKS. Scoped receipts keep their stricter exact-match rule
on top of this.

The `HEAD~1` fallback is now loud everywhere and **fatal** on the two
receipt-writing modes (`auto`, `all`): `run-local-ci.sh` exits `4` before any job
runs. Still working, on purpose: `GOATOS_FAST_LOCAL_CI=1` (offline dev loop,
writes no receipt), explicit `JOB=...` partial runs (write no receipt), and
detached HEAD (the base is a ref, not the current branch).

Behavioural proof: `bash tools/ci/check-ci-base-provenance.test.sh` (wired into
the `ci-tooling` self-tests and the `local-ci-evidence` guardrail selfTest).

**Anything other than `screenshots: "yes"` does NOT attest screenshot
coverage, and `skipped-with-ui-diff` now BLOCKS a push to `main`.** The field
used to be recorded and displayed only; `evaluatePush` never read it, so a
receipt saying "this diff touches UI and the proof did not run" authorized main
exactly like `"yes"`. It is now evidence:

| `screenshots` | push to `main` |
|---|---|
| `yes` | allowed |
| `skipped` | allowed — the authorized default (Paparazzi is opt-in) |
| `not-applicable` | allowed — the android job never ran |
| `skipped-with-ui-diff` | **BLOCKED** — run `make ci-local-screenshots` |
| missing/unknown on an android-covering receipt | **BLOCKED** — pre-fix receipt |

The check runs before the mode branches, so it applies to `all` and `scoped`
receipts alike and to receipts carried through `--reuse-after-rebase` (which
still forwards the value verbatim, never upgrading it).

> **Migration note:** the last row invalidates every receipt written *before*
> this change that covers the android job. The first `make land-main` after this
> lands must re-run CI. This is intended.

The screenshot gate never relaxes the exact-SHA, base, or job-coverage checks. Recording refuses (exit 2) if the run
covered the android job but reported no screenshot state, and
`--reuse-after-rebase` carries the value forward verbatim in both branches; it
can never be upgraded from `skipped` to `yes`.

---

## 5. The iteration loop

Do not re-run the whole gate to re-check one guard.

| Situation | Command | Receipt |
|---|---|---|
| Iterating on a change | `GOATOS_FAST_LOCAL_CI=1 make ci-local` | none |
| One job failed, re-check it | `tools/ci/run-local-ci.sh <job>` | none |
| Certify for landing | `make ci-local` (once) | yes |

A RED run now prints the exact single-job re-check commands for the jobs that
failed. Fast and partial runs say in plain words that no receipt was written and
that they cannot authorise a push.

---

## 6. Reading the timings

Every `step`/`optional_step` is timed. Each run appends
`epoch<TAB>sha<TAB>step<TAB>status<TAB>seconds` to:

```
.git/goatos-ci-local-timings.tsv
```

The summary also prints the run's **slowest 10 steps**. Use it before proposing
any further speedup — the long poles are currently unmeasured, with `govulncheck`
(whole-module SSA, not covered by the Go test cache, runs every attempt) and the
admin-web production build the leading suspects alongside the cold Gradle
bootstraps.

---

## 7. `telemetry-guard` moved to `common`

`make telemetry-guard` used to run inside both `run_admin_web` and
`run_android_guards`. It now runs once in `run_common`.

Honest effect:

- For **receipt-writing** modes this is a *strengthening*: `common` is seeded
  unconditionally by the classifier and both `all` and `guardrails` call
  `run_common`, so the guard is now unconditional rather than
  component-conditional.
- For the **non-receipt partial modes** `tools/ci/run-local-ci.sh admin-web` and
  `... android` it is a *narrowing*: those two currently run it and now will
  not. They write no receipt, so no commit can reach main uncertified.

---

## 8. Flags that look like cargo cult but are not

Do not "discover" and delete these:

| Flag | Verdict | Named cause |
|---|---|---|
| `--rerun-tasks` (Paparazzi) | **KEEP** | Added in `6569960b5` with `./gradlew --stop` + `rm -rf */build`; `8d1f97b4f` removed the `rm -rf`, leaving this as the sole staleness protection. Failure mode is `UP-TO-DATE` — the gate passes without running. |
| `--no-daemon`, `--no-configuration-cache`, `-Dkotlin.daemon.enabled=false`, `-Dkotlin.compiler.execution.strategy=in-process`, `-Pkotlin...` | **KEEP** | `f2fb96a1b` "Stabilize Android CI Kotlin tasks", `aa6104a3b` "Stabilize Android lint in local CI", plus the Firebase Perf ASM vs. unit-test Flow-fake issue. `gradle.properties` enables the configuration cache; CI overrides it deliberately (Paparazzi alpha, Hilt+KSP, google-services, Crashlytics, Firebase Perf, Baseline Profile — a textbook CC-blocker set). |
| `--max-workers=1` | **KEEP, NEEDS-PROOF** | Blame is silent. Not the same as "no reason". Measure its cost via the timings TSV before proposing removal. |

---

## 9. Landing this work

`tools/ci/ci-scope.mjs` forces the full suite for any `tools/ci/**` or
`Makefile` change, so changes to the landing path itself must land as **one
commit**; splitting them costs a full-suite landing per split.

---

## 10. Reachability, attribution, and speed (2026-08-05)

### `GOATOS_CI_TRACE_ONLY=1` — a reachability probe, not a bypass

`tools/ci/check-android-screenshot-proof.sh` used to assert the Paparazzi opt-in
was reachable with `grep -Fq 'GOATOS_RUN_ANDROID_SCREENSHOTS'`. That string also
appears inside the SKIP banner's `echo`, so deleting the `1|true|TRUE|True)` arm
that actually runs Paparazzi left the guard green. The guard now drives
`run-local-ci.sh` in trace mode instead: `step` prints `CI-TRACE <name> :: <cmd>`
and returns without executing, and the guard asserts the branch is entered with
the opt-in ON and not with it OFF (`off` *unsets* the variable, so flipping the
`:-0` default to `:-1` is caught too).

Three properties keep trace mode from being a bypass, and the self-test asserts
them: `step` executes nothing; the script `exit 3`s **before** the receipt block
(that ordering is load-bearing — moving the check after the receipt write turns
trace mode into a receipt forger); and `check-local-ci-evidence.mjs --record`
independently refuses when the variable is set.

### RED re-run hints point at a lane that can re-run the failure

`run_android_guards()` no longer hardcodes `current_job="android"`. It is shared
by the `android` job (which builds `:app` with Gradle) and the Gradle-free
`guardrails` lane, so a `mobile-guard` failure under `guardrails` used to tell
you to run `... run-local-ci.sh android` — a full `:app` compile you never asked
for. Callers now own attribution, and an unattributed caller fails loudly.
Proof: `tools/ci/check-run-local-ci-attribution.test.sh`.

### One Gradle invocation instead of three

The non-FAST android leg now runs `:app:compileStgReleaseKotlin
:app:testStgReleaseUnitTest :app:lintStgRelease` in a single `./gradlew` call
with **every flag byte-for-byte unchanged**. Measured on a 12-core / JDK-21
box: three `--no-daemon` invocations pay JVM start + configuration +
up-to-date checking three times (~30 s of fixed overhead on an up-to-date tree)
versus 12.7 s paid once — **~17 s saved per android leg**. Gradle reports the
union of the task graphs (712 actionable tasks), not the sum-with-repeats.
Failure semantics are unchanged (Gradle stops at the first failing task, exactly
as the three sequential steps did).

`--no-daemon` is deliberately **kept** on the receipt path: it is the one flag
with a recorded reason (mirror GitHub's ephemeral runner, `2926c7de5`), and
`GOATOS_FAST_LOCAL_CI=1` already offers daemon + combined tasks for non-landing
runs. `--no-configuration-cache` and `--max-workers=1` have **no recorded
reason** in blame — that is "unproven", not "unnecessary"; they stay. The
in-process Kotlin strategy is load-bearing on `testStgReleaseUnitTest` (Firebase
Perf ASM instrumentation has corrupted unit-test Flow fakes here before,
`f4a63345`) and must not be touched.

Enabling the configuration cache once failed with four
`Starting an external process 'git ...' during configuration time is
unsupported` problems, all from a then-uncommitted `gitOutput(...)` helper in
`apps/goatos-android/app/build.gradle.kts`. **That has since been rewritten as a
`ValueSource`** (`GitOutputValueSource` / `gitOutput(...)` in that file, still
uncommitted at the time of writing and owned by another change), which is the
sanctioned escape hatch — Gradle re-obtains it when reusing a cached entry, so
`git status --porcelain` stays honest. Hilt, KSP and Paparazzi produced zero
problems. CI still passes `--no-configuration-cache` deliberately; enabling it
is a separate, unmeasured decision.

### Jobs run in parallel, accounted through the filesystem

`tools/ci/parallel-dispatch.sh` runs the selected jobs concurrently (default
width 3, `GOATOS_CI_LOCAL_JOBS`, clamped 1..4; width 1 degrades to sequential).
This changes **how** jobs run, never **which** — no gate is skipped and no new
bypass exists.

The obvious hazard is that a job runs in a subshell, so a child's `fail=1` is
invisible to the parent. It is eliminated by construction: the parent never
reads a child variable. Each job writes `<job>.status` **last and atomically**,
so a job killed by a signal or OOM leaves no status file, and a missing or
non-numeric status scores 97 = FAILED. The parent additionally asserts
`accounted == launched == selected`. `screenshots_ran` crosses the boundary the
same way, and its absence on the android job fails the run so a receipt can
never claim wrong screenshot coverage. Jobs contending on the same resource are
grouped and never overlap (`android` → `gradle`; `backend` + `query-plans` →
`docker`), scheduled by the parent so a killed child cannot leave a stale lock.
Job logs are captured per job and replayed in **selection** order, not finish
order, so output stays deterministic.

`job_group()` cannot see a SECOND WORKTREE. It is in-memory and per-process, so
two checkouts both entered the Gradle region. In the recorded `ci-local`
timings the `:app compile+unit+lint` step cost 413 s and 212 s in a two-way
overlap, and 340 s / 251 s / 361 s in a three-way one, against 84-181 s for runs
nothing else overlapped. `tools/ci/gradle-worktree-lock.sh` closes that with a
machine-wide advisory `mkdir` mutex keyed on `realpath(GRADLE_USER_HOME)`,
acquired once in `run_android` after the cheap static guards and the toolchain
check. It is FAIL-OPEN on every path: it can never fail a step, skip the android
job, or wait unboundedly, and it changes **when** the android job starts, never
which steps run or how any of them is judged. It does **not** make a single pass
faster and it saves nothing on a solo landing.

One behavioural consequence to know before you press Ctrl-C: the android lane
now exits 130/143 on INT/TERM instead of falling through into `android benchmark
compile` — and the re-raise is aimed at the acquiring subshell, so TERMing the
lane no longer takes `run-local-ci.sh` down with it. Guard:
`tools/ci/check-gradle-worktree-lock.sh` (16 behavioural cases, ~45 s, no
Gradle) with `check-gradle-worktree-lock.test.sh` driving it against 19 mutated
copies of the library (17 min 16 s measured). Both are diff-scoped in `run_common` — the
guard to a lock-library diff, the self-test to the lock, guard, harness or
`run-local-ci.sh` — because charging 15 minutes to every `tools/ci/**` commit
would be a wall-clock regression inside a wall-clock fix. `make guardrails` and
`make gradle-worktree-lock-guard` still run the guard unconditionally. Flags and
the full property list: `docs/runbooks/local-ci-performance.md` §2 and §5.

Proof: `tools/ci/check-run-local-ci-parallel.test.sh`, wired into `run_common`.
It asserts the failure semantics directly *and* drives the real script in a
throwaway git repo to prove a RED parallel run exits non-zero and writes **no
receipt** — with an all-green positive control so the test cannot pass
vacuously.
