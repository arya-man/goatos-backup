# 15-minute landing: Paparazzi proof (2026-09-28)

Goal: any PR validates and lands in 15 minutes or less.

## Problem

PR #451's landing took 5156 s. `android screenshots` alone took 2333 s.

- `:app:verifyPaparazziDevDebug` ran the whole `testDevDebugUnitTest` task: 216 classes, 1567 tests. Only 17 classes (134 tests) are screenshot tests. The rest duplicate `testStgReleaseUnitTest`, which already ran in the compile lane.
- `forkEvery = 1` (602a32711) started one JVM per class, one after another.
- The full path added `--rerun-tasks --max-workers=1`. That recompiled every module on one worker.

## Change

- `app/build.gradle.kts`: a Paparazzi run includes only `sg.mesha.goatos.ui.*ScreenshotTest`. It keeps `forkEvery = 1` and adds `maxParallelForks = 4` (Gradle caps it at `--max-workers`) and `maxHeapSize = 2g`.
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

## Review round (2026-09-30, four review agents)

Reviewers found five problems. All are fixed in the same commit:

1. **The dev/debug-only tests ran nowhere.** `app/src/testDev` and `app/src/testDebug` (9 tests) only ever ran inside `testDevDebugUnitTest`. The screenshot filter dropped them. The Paparazzi run now includes them. The class names are read from the sources, because `DefaultRfidInputTransformTest.kt` declares `DevDefaultRfidInputTransformTest`.
2. **`--rerun` did not force the tests to run.** It reruns only the thin `verifyPaparazziDevDebug` wrapper, so the cacheable test task could be replayed from the build cache. The Paparazzi test task now sets `outputs.upToDateWhen { false }` and `outputs.cacheIf { false }`. Compiles stay cached.
3. **The renice follow-up (6dab81712) did more harm than good.** It lowered every non-Android job to nice 10 on a laptop shared with other agents and apps, so those jobs yielded to every process on the box, not only to Android. It also stacked in nested runs: `check-ci-base-provenance` case (e) took 198–244 s against 14–40 s with the renice off, and hit its 60 s watchdog. The renice is reverted, and so are `GOATOS_CI_NICE_OTHERS` and its test layer. The 900 s budget stays.
4. **Guard gaps.** The guard now checks:
   - `maxParallelForks` is at least 2 (it used to check only that the word appears).
   - `--max-workers=1` is caught at the end of a line.
   - The Paparazzi block keeps the screenshot filter, the testDev/testDebug classes and the no-cache lines.
   - No Paparazzi test outside `sg/mesha/goatos/ui/*ScreenshotTest.kt` exists (the filter would silently skip it).

   Every check has a self-test mutation.
5. **Stale docs and no skill rule.** Stale lines about the 20-min budget, `--rerun-tasks` and `--max-workers=1` are updated in the runbooks. `.agents/skills/goatos-build/SKILL.md` now states the screenshot proof contract.

The optional knob `GOATOS_PAPARAZZI_FORKS` was removed as well. Developers set nothing new: `make land-main` and `make ci-local-screenshots` are unchanged.
