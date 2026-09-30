# 15-minute landing: Paparazzi proof (2026-09-28)

Goal: any PR validates and lands in 15 minutes or less.

## Problem

PR #451's landing took 5156 s. `android screenshots` alone took 2333 s.

- `:app:verifyPaparazziDevDebug` ran the whole `testDevDebugUnitTest` task: 216 classes, 1567 tests. Only 17 classes (134 tests) are screenshot tests. The rest duplicate `testStgReleaseUnitTest`, which already ran in the compile lane.
- `forkEvery = 1` (602a32711) started one JVM per class, one after another.
- The full path added `--rerun-tasks --max-workers=1`. That recompiled every module on one worker.

## Change

- `app/build.gradle.kts`: a Paparazzi run includes only `sg.mesha.goatos.ui.*ScreenshotTest`. It keeps `forkEvery = 1` and adds `maxParallelForks` (default 4; override with `GOATOS_PAPARAZZI_FORKS`) and `maxHeapSize = 2g`.
- `run-local-ci.sh`: both screenshot steps use task-level `--rerun`, `--build-cache` and `$(android_gradle_workers)`. `--rerun` still forces the screenshot task to execute, so the gate can never pass `UP-TO-DATE`. That was the staleness risk `--rerun-tasks` guarded.
- `check-android-screenshot-proof.sh`: now fails if either of these returns:
  - `--rerun-tasks`
  - `--max-workers=1`
  - a missing `--rerun`
  - the build file no longer limits the run to screenshot classes
  - the build file no longer sets `maxParallelForks`

  The self-test has three new negative cases.

## Measured (10-core Mac, load average ~120, swap ~16 of 17 GB used)

| Run | Before | After |
|---|---|---|
| Full proof, cold worktree (includes `:app` devDebug compile) | 2333 s | 377 s, 134/134 pass |
| Full proof, warm | — | ~161 s of work (824 s wall, 663 s of it waiting for the machine Gradle lock) |
| Broken golden (one PNG flipped) | — | `FeedPurchasesListScreenshotTest > longFeedNames FAILED`, BUILD FAILED |

## Still open

- Five CI jobs run at once on a machine that is already swapping. On 2026-09-28, admin-web lint took 677 s (normally 39 s) and `go vet` took 466 s.
- The Codex app-server leaked about 150 `node_repl` processes.
- Spotlight indexes the worktrees' build folders.
- The compile lane builds the stgRelease variant, and Paparazzi builds devDebug, so libraries compile twice.

## Follow-up (same day): Android gets the CPU first; budget is 15 min

- `run_job` in `run-local-ci.sh` renices every non-Android job to nice +10, and its children inherit it. Android, the critical path, gets the cores first. Width, launch order and verdicts are unchanged. Turn it off with `GOATOS_CI_NICE_OTHERS=0`.
- Proven by `check-run-local-ci-parallel.test.sh` layer 4, which runs the shipped `run_job`:
  - children of the common job: nice 10
  - android and the parent: nice 0
  - with the opt-out: nice 0

  Replacing the renice with a no-op turns the test red, and only that check fails.
- `land-main` budget warning lowered from 1200 s to 900 s. It still only warns.
- Not done here, because each needs an `app/build.gradle.kts` edit, which runs every Android module at landing:
  - devDebug instead of stgRelease for the `:app` landing lane (removes the double library compile)
  - excluding `*ScreenshotTest` from plain `testDevDebugUnitTest`
- Not done: Kotlin daemon instead of in-process compile. The gain is small on a warm build cache, and the flag has a named stability cause (`f2fb96a1b`).
