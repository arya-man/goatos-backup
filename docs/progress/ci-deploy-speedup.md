# CI + STG deploy speed-up (branch `perf/ci-deploy-speedup`)

Status: DRAFT, in progress. Base `origin/main` bc90250aa.

## Problem

- `make land-main`: typically 30-60 min. Target: 15 min or less.
- STG deploy (backend + web + Android): about 65 min (backend+web about 31, Android separately about 34). Target: 15-20 min.
- Constraint: no guard or check is removed or weakened.

## Measured baseline

**Local `ci-local`** (167 runs in `.git/goatos-ci-local-timings.jsonl`):
- A quiet full pass takes 5.2 min at p50 and 8.8 min at p90.
- Under contention it takes 30-50 min. On 09-24 20:06: android 40.5, backend 14.7, admin-web 12.2, 50.6 min wall clock.
- Contention makes each step 5-15x slower. 16% of runs overlapped another run, with bursts on 23-24 Sep.
- A warm cache makes `go test` about 30x faster and the Android compile 7-10x faster.
- Android runs with `--no-daemon --no-configuration-cache --max-workers=1` and in-process Kotlin. The full paparazzi run uses `--rerun-tasks`. (Superseded 2026-09-28: see `docs/progress/ci-15min-landing-20260928.md`.)

**STG backend+web build** (dd268f77 / b7a209c2, 30.4 / 31.3 min):
- Four image builds run one after another (no `waitFor`), with no layer cache, on the default machine:

  | Image | Time (min) |
  |---|---|
  | backend | 5.4-5.8 |
  | migrate | 0.5 |
  | admin-web | 4.8-5.0 |
  | ask-mesha-agent | 2.0 |

- The release step takes 15.4-15.7 min. The rollout inside it takes 9.5 min:

  | Rollout phase | Time |
  |---|---|
  | task image setup | about 1.7 min |
  | kernel-worker pre-migration | 0.8 min |
  | migration | 13 s |
  | API + cutover | 0.5 min |
  | 5 services, one after another | about 2.5 min |
  | Grafana/observability | about 4.3 min |

**Android build** (0cf96274, 34.6 min):
- Gradle took 28m56s: 676 tasks, 563 executed, 113 cached.
- Flags: `--max-workers=1`, 2.5 GB heap, default machine.
- The JDK is installed with apt on every run.
- Android starts only after backend/web has finished, so it adds its full time on top.

## Changes (deploy) — commit d97e427cc

| # | Change | Why | Estimated saving |
|---|---|---|---|
| a | The four image builds depend only on `configure-docker-auth`, so they run at the same time. The release step waits for all four. | They were independent but ran one after another. | about 7-8 min (13 min becomes the longest single image, about 5-6) |
| b | `machineType: E2_HIGHCPU_32` | 4 Docker builds plus Gradle now run together. 8 vCPU/8 GB is too small for Gradle's 4 GB heap, Kotlin, a Next build and a Go build at once. | Needed for (a) and (d) |
| c | `tools/deploy/stg-image-build.sh`: buildx with `--cache-from/--cache-to type=registry,ref=<repo>/<image>:buildcache,mode=max,ignore-error=true`. Falls back to plain `docker build` if buildx is missing. | The Dockerfiles already copy go.mod/go.sum and package-lock before the source, so the dependency layers can be reused. | 1-3 min per image once the cache is warm |
| d | Android is split into two steps. **`android-mobile-build`** (`waitFor: ['-']`) runs alongside the images and the rollout. **`android-mobile-distribution`** (publish) waits for the build, release tag bookkeeping and the ask-mesha deploy. | Removes about 30 min of Android time that ran after backend/web. | about 30 min off the combined deploy |
| d | The deploy-only Gradle run uses `--max-workers=4` and `-Xmx4096m` (both can be overridden). An optional GCS Gradle-home tarball cache (`_GRADLE_CACHE_URI`, keyed by the wrapper + `libs.versions.toml` hash) is added; it is off by default. | The Gradle run was limited to one CPU. | Likely 10-15 min |
| e | `stg-clouddeploy-task.sh`: analytics-events, kernel-worker restore, MCP and the MQTT bridge each run as a parallel lane after the API traffic switch. Admin-web and the job image updates run in the foreground at the same time. Every lane is `wait`ed, its exit code is checked and its log printed. If any lane fails, the deploy fails. | These services do not depend on each other. | about 2 min |

The overall target was that backend+web goes from about 31 to about 17-19 min, and backend+web+Android together goes from about 65 to about 20 min (the Android build hides inside the backend window). **These are estimates. No Cloud Build has been run.**

### Safety properties kept

- **Exact-SHA provenance.** Every image is still rebuilt from the checked-out commit, with the same `GIT_SHA` / `NEXT_PUBLIC_APP_VERSION` build args, and pushed at `:<12-char sha>`. The `:buildcache` tag is only a source of reusable layers and is never deployed.
- **Android build failure cannot cancel an in-flight rollout.** The build phase always exits 0 and writes a `failed <sha>` marker instead. The publish step runs only after the rollout. It refuses to publish unless the marker says `ok <same sha>`, and it fails the build with the normal Slack "Backend/web rollout succeeded. Android mobile distribution failed." message.
- **Force-update floor is never published before the new backend is live.** Firebase upload, the GCS mirror, the Play Internal upload, the Remote Config floor and the recheck push all stay in the publish step, which runs after the rollout. The APK is the same file on every channel, and the SHA comparison against the mirror is unchanged.
- **Secrets stay out of the release source.** `gcloud deploy releases create --source=.` could now run while the Android build is writing to `/workspace`. `.gcloudignore` now excludes `android-sdk/`, `.gradle-home/`, `.docker/` and `.local/` (signing material). `.docker/` holds an access token and was not ignored before this change either.
- **Strict rollout order is unchanged.** Kernel-worker drain, then migration, then the API revision is ready, then traffic moves. A new test asserts that the lanes start only after the API is ready and that the settle checks run only after every lane has been joined.

### Judge review fixes (PR comment 5824290893), one commit each

1. **HIGH, e79633b6e.** The whole `android-mobile-build` step body now runs in a `set -e` subshell. Any failure (apt, SDK, signing restore, Gradle) writes `failed <sha> rc=N` and the step exits 0. Tested locally: a PATH without apt gives step rc=0 and the marker is written.
2. **MED, bca0cfe4c.** `stg-image-build.sh` falls back to the plain docker build when `buildx create` or `inspect --bootstrap` fails, for example on a BuildKit image pull rate limit. A genuine `buildx build` failure still fails the step. Tested with a fake docker: create fails → plain build, build fails → rc propagates.
3. **MED, 3dfaadf95.** `.gcloudignore` also excludes Android `build/`, `.gradle/` and `.kotlin/`.
4. **LOW, 9fa5b0799.** The task's EXIT trap joins any still-running rollout lanes and prints their logs before writing FAILED. There is a test for this.
5. **LOW (follow-up).** The publish step installs the JDK with apt again and reuses the `/workspace` SDK, which costs about 1-2 min. A prebaked Android builder image would remove this and the build step's install too. Recorded under open decisions.

## Judge rounds

- **Round 1 (PR comment 5824290893):** five findings (1 HIGH Android step can fail the build, 2 MED buildx fallback + `.gcloudignore`, 2 LOW trap joins lanes + JDK reinstall). Items 1-4 fixed, one commit each (listed above); item 5 deferred to open decisions.
- **Round 2 (PR comment 5824385913):** land-main lock stale-reclaim race — two runs seeing the same dead holder could both acquire, and PID reuse could make a stale lock look live. **Fixed:** lock logic moved to `tools/ci/land-lock.sh`; the holder records pid + process start time (`ps -o lstart=`, mismatch = stale); reclaim atomically renames the stale dir to `<lock>.stale.<pid>.<rand>` only while holding a `<lock>.reclaim` mutex and after re-reading the holder, then retries `mkdir`. `tools/ci/land-main.test.sh` adds PID-reuse, live-holder-kept and a 3-way concurrent reclaim (10 rounds, exactly one winner) case; passes 3/3 runs.

**What this PR does and does not buy for land-main:** a single uncontended landing already takes 5-9 min. The land-main speed-up in this PR comes **only from removing contention** (the landing queue stops two landings fighting over CPU/Gradle/OCI, which is what produced 30-50 min runs). The remaining gain — warm Gradle/Go/npm caches for the landing worktree — is deferred and not in this PR.

## Changes (local CI) — pending in this PR

- f. **DONE.** Landing queue in `tools/ci/land-main.sh`, placed after the clean-tree and git-operation checks:
  - It creates a `mkdir` lock at `$(git rev-parse --git-common-dir)/goatos-land-main.lock`, so every worktree of the clone shares it. `GOATOS_LAND_MAIN_LOCK_DIR` overrides the location.
  - The lock records the holder's pid, worktree, SHA and start time, and is released on EXIT.
  - If the lock is held by a live pid, it prints the holder and exits 1. It never waits and never kills.
  - If the holder pid has exited, the lock is stale and is reclaimed.
  - `tools/ci/land-main.test.sh` covers both cases: a refused run does not start CI and leaves the holder's lock in place; a stale lock is reclaimed and released.
  - Limit: separate clones do not share the lock. If that is needed, set `GOATOS_LAND_MAIN_LOCK_DIR` to one shared machine path.
- g. **No change needed.** `apps/goatos-android/gradle.properties` already sets `org.gradle.caching=true`, so the receipt-path Gradle runs already use the local build cache. `--no-daemon`, in-process Kotlin and `--max-workers=1` stay. Full paparazzi keeps `--rerun-tasks`, which bypasses the cache on purpose. (Superseded 2026-09-28: the Paparazzi test task now refuses UP-TO-DATE/cache itself; `--rerun-tasks` is gone.) Raising the worker count is still an open decision that needs a recorded A/B test.
- h. **Already done on main by the "CI speed Phase 1" work. No change here.** In `tools/ci/parallel-dispatch.sh`, `backend` is in group `none` on the default path, so it already runs in parallel with `query-plans` (group `postgres`). `validate-sqlc-query-plans.sh` already creates a scratch database per run (`goatos_sqlc_plans_$$`) and drops it on exit. The Docker fallback in `prepare_query_plan_database` was **not removed**. It is only reached when there is no admin DSN and no OCI tunnel, and it already fails closed with the OCI instructions when Docker is missing. Removing it would change behaviour on other machines for no speed gain on the laptop path.
- i. **DONE, and it found a real gap.** New adversarial fixtures in `check-android-screenshot-scope.test.sh` cover theme colors, type, dimens, core-ui strings (including a localized copy), app strings and the app theme. Each must be classified as UI and must force full Paparazzi. `MeshaColors.kt`, `MeshaType.kt`, `MeshaDimens.kt` and `MeshaIcons.kt` have no `@Composable`, so the detector treated design-token changes as **not UI** and skipped screenshot proof. Fix: `android-ui-diff.sh` now treats `core/core-designsystem/src/main/**/*.kt` as UI unconditionally. This makes the gate **stricter**. Remaining blind spot: non-Composable text formatters in core-ui, for example `PartitionLabel.kt`.
- j. **Deferred (follow-up).** A persistent landing worktree (`git clean -ffdx` plus `reset --hard <candidate>`) would keep Gradle/Next/Go caches warm between landings. But `clean -ffdx` also deletes the ignored build caches, which defeats the purpose. Keeping them means an allow-list of cache dirs, and that needs its own design and review. The warm-cache win (Go about 30x, Android 7-10x) is real, so this is the next candidate.

## Guard preservation

| Guard / check | Status |
|---|---|
| Exact-SHA image build + tag | Kept: rebuilt every deploy, same args |
| Cloud Deploy release/rollout verification, image-settle checks, smokes | Kept, run after all lanes are joined |
| kernel-worker drain → migration → API order | Kept, and asserted by a test |
| Android: same bytes to Firebase + GCS, SHA check, Play Internal, Remote Config floor, recheck push | Kept, and still after the backend rollout |
| Grafana/observability deploy + dashboard smoke | Untouched (owned by another session) |
| Everything in `ci-local` / landing receipt | Untouched. The landing queue only adds a refusal before any CI runs. |

## Rejected (per judge)

- A fast mode that writes a landing receipt.
- Reusing earlier image tags for components that did not change. That breaks exact-SHA provenance.
- Using the Gradle daemon or configuration cache on the receipt path.
- Dropping `--rerun-tasks` from full paparazzi. (Superseded 2026-09-28: replaced by task-level `--rerun`, which keeps the staleness protection; see `docs/progress/ci-15min-landing-20260928.md`.)
- Touching `normal_observability_deploy` / `smoke_grafana_dashboards`. They cost about 4.3 min or more of the rollout. That is a follow-up for the session moving them to mesha-ops.

## Open decisions

1. Cost of E2_HIGHCPU_32 against E2_HIGHCPU_8. It costs more per minute, but the build takes far fewer minutes. It also needs Cloud Build private-pool/quota headroom in asia-south1 (the default pool supports E2_HIGHCPU_32).
2. The publish step reinstalls the JDK (about 1-2 min). Creating a GCS bucket/prefix for `_GRADLE_CACHE_URI`, and a prebaked Android builder image (JDK + SDK). Both are infrastructure this PR does not create.
3. A/B test for raising `--max-workers` on the local receipt path.

## Verification

Done:
- `bash -n` on the changed scripts.
- `yaml.safe_load` on `cloudbuild.stg.yaml` (step DAG checked).
- `node --test tools/deploy/stg-*.test.mjs`: all pass (41 tests, including 1 new).
- Fake-docker matrix for `stg-image-build.sh`: ok / builder-fail → plain / build-fail → rc.
- The `android-mobile-build` body run with a failing apt: step rc=0 and a `failed` marker is written.
- `check-android-screenshot-scope.test.sh`: failed before the fix (3 token fixtures) and passes after it. `check-android-ui-diff.test.sh` passes.
- `bash tools/ci/land-main.test.sh`: passed, including the new lock and stale-lock cases.

Pending:
- A real Cloud Build STG run. The first run warms the cache, so the second run gives the real number.
- Confirming buildx is present in `gcr.io/cloud-builders/docker`. If it is not, the plain docker fallback runs.
- Confirming that `appDistributionUploadProdRelease` in the publish step reuses the built outputs (the same `/workspace` and Gradle home) and does not rebuild.

## Cost (Cloud Build, goatos-stg)
Today every STG deploy build runs on Cloud Build's default machine: e2-standard-2 (2 vCPU, 8 GB), because `cloudbuild.stg.yaml` on main sets no `machineType`. The machine is billed per build-minute only while a build runs; only builds using `cloudbuild.stg.yaml` change machine. Local CI / land-main are unaffected; the Cloud Deploy rollout/migration jobs run in their own environment.

Measured history 2026-08-25 to 2026-09-24:
| Type | Builds | Build-minutes | Average |
|---|---|---|---|
| Deploys with Android (`_DEPLOY_MOBILE=true`) | 73 | 2,929 | ~40 min |
| Backend+web only (incl. failed/skipped) | 70 | 1,101 | ~16 min |
| Other small builds (not affected) | 27 | 72 | ~3 min |

Monthly estimate (list prices from memory, verify in billing; times are estimates until a measured run):
| Machine | vCPU / RAM | Android deploy | Backend+web | Month cost |
|---|---|---|---|---|
| default e2-standard-2 (today) | 2 / 8 GB | ~40 min | ~16 min | ~₹750 (free tier covers ~2,500 min) |
| E2_HIGHCPU_8 | 8 / 8 GB | ~22-26 min | ~11-13 min | ~₹3,000-4,000 |
| E2_HIGHCPU_32 (current PR) | 32 / 32 GB | ~18-22 min | ~10 min | ~₹9,000-12,500 |

Most of the speed-up comes from parallel steps and the layer cache, not core count; ~13 min of a fast deploy is the rollout. BUT E2_HIGHCPU_8 has only 8 GB RAM, and this PR runs Gradle (4 GB heap + Kotlin daemon) alongside four Docker builds (Go + Next.js). That risks OOM on 8 GB, so 8-core would need Gradle heap/workers reduced (slower Android) or the image builds to finish before Gradle starts. Decision pending the first measured run: stay on E2_HIGHCPU_32 until a real run shows peak memory; drop to 8-core only if it fits.

## Step log (2026-09-25, single-PR consolidation)

Machine RAM snapshot that motivated steps 2-4: 32 GB total, 20.2 of 21.5 GB swap in use, 4 idle Gradle daemons holding about 6.4 GB (one of them on Android Studio's jbr-17.0.11), and fseventsd at 13 GB from dozens of worktrees.

### Step 1: PR #405 folded in
- Merged `chore/grafana-out-of-stg-deploy` (0b91ef20a, bf614efe1, a422af1e5). #405 is closed and its branch deleted.
- Grafana/Alloy is no longer applied or smoke-tested by the STG deploy. Mesha-ops owns it: `docs/decisions/grafana-owned-by-mesha-ops.md`.
- The only conflict was `docs/runbooks/stg-deploy.md`. Both sections were kept (parallel build shape and Grafana out of deploy).
- **Before the first deploy from this PR, rebuild the deploy runner image** with `tools/deploy/stg-clouddeploy-runner-build.sh`, then refresh `deploy/clouddeploy/stg/runner-receipt.json`. The runner Dockerfile changed, so the old image does not match.
- Checks: `bash -n` on tools/deploy/*.sh, YAML parse, `node --test tools/deploy/*.test.mjs`, `make stg-deploy-scripts-test`, `make guardrail-registration-guard`. All green.

### Step 2: Gradle always runs on JDK 21
- **Root cause (measured).** The land-main that started a Java 17 daemon had no `JAVA_HOME` at all. The only JVM registered with macOS `/usr/libexec/java_home` is `~/Library/Java/JavaVirtualMachines/jbr-17.0.11` (an old IntelliJ download). Homebrew `openjdk@21` is not registered. So anything that finds Java through the system (the `/usr/bin/java` stub, `java_home`, Gradle's own detection) got 17. `run-local-ci.sh` used `${JAVA_HOME:-openjdk@21}` and never checked the version.
- **Fix.** New shared resolver `tools/ci/java21.sh`:
  - An inherited `JAVA_HOME` is kept only if it is major 21.
  - Otherwise it tries Homebrew openjdk@21 first, then Linux JDK 21 paths, and `java_home` last. Every candidate is version-checked, so a registered jbr-17 is skipped.
  - If no JDK 21 exists, it fails before Gradle starts.
- **Where it is used:**
  - `run-local-ci.sh` (android job, so land-main too).
  - `tools/dev/android-env.sh`, which covers android-dev-run, android-e2e-run, android-doctor and emulator-ensure.
  - `tools/deploy/stg-mobile-distribution.sh`.
  - `tools/local/e2e-devices.sh` and `tools/local/multi-role-emulators.sh`.
- **Daemon JVM pin.** `apps/goatos-android/gradle/gradle-daemon-jvm.properties` sets `toolchainVersion=21`. It lists no download URLs, so Gradle never downloads a JDK. It uses the detected JDK 21, or fails with a clear error instead of picking jbr-17. Not yet proven by a real Gradle run in this session (Gradle builds were out of scope). If Android Studio sync cannot find a JDK 21, register the Homebrew JDK with the system (`sudo ln -sfn /opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk /Library/Java/JavaVirtualMachines/openjdk-21.jdk`) or set Studio's Gradle JDK to it.
- **Tests.** `tools/ci/java21.test.sh`, wired into run-local-ci's `tools/ci` self-tests and `make java21-self-test`. It uses fake JDKs and covers: `JAVA_HOME` unset, `JAVA_HOME` pointing at 17, a registered jbr-17 candidate, only 17 present (fails), and the enforcement at every entrypoint. `check-screenshot-remediation.test.sh` passes with its fake JDK now reporting 21.

### Step 3: RAM — shared Gradle home, idle daemons, one Gradle build at a time
- `org.gradle.daemon.idletimeout=600000` is set in the repo `gradle.properties` and in the user-level managed block. Idle daemons now exit after 10 minutes instead of 3 hours. The Gradle heap stays capped at `-Xmx4g` per daemon.
- `tools/ci/gradle-home.sh`: a temp `GRADLE_USER_HOME` (`/tmp`, `/private/tmp`, `/var/folders`) is reset to `~/.gradle`, with a warning. `GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1` keeps it. Why: a scratchpad Gradle home meant a cold cache, its own daemon, and a different lock key.
- **Machine queue, two layers:**
  1. `~/.gradle/init.d/goatos-machine-lock.init.gradle` (source in `tools/ci/gradle-init/`, installed by `tools/ci/gradle-machine-setup.sh` from `make ai-setup` and every repo Gradle entrypoint).
     - One lock per build (an OS `FileChannel` lock on `~/.gradle/goatos-build.lock`), never held by an idle daemon.
     - Released automatically if the process dies. Prints the holder while waiting.
     - Scoped to `/apps/goatos-android`, skipped for nested builds. On errors it continues without the lock.
     - This layer catches plain `./gradlew` from any Claude or Codex session and Android Studio.
  2. Repo scripts (`run-local-ci.sh`, `tools/dev/android-dev-run.sh`, `android-e2e-run.sh`, `tools/local/*.sh` via `tools/ci/gradle-run.sh`) also take the existing ci-local mkdir lock. Its lock dir now defaults to `/tmp`, not a per-session `$TMPDIR`.
  - The order is always the mkdir lock, then the file lock, so the two cannot deadlock.
- The user-level managed block in `~/.gradle/gradle.properties` also pins `org.gradle.java.home` to a verified JDK 21. It is written after any older line, so it wins over it.
- Known limit: on a configuration-cache hit the file lock is taken at the first task completion, so the first task can run before the lock is held.
- **Not proven with a real Gradle build in this session** (Gradle builds were out of scope). What was checked: the init script compiles against the local Gradle 9.6.1 API (Groovy compiler). The OS lock semantics were tested with a Java probe: exclusive across processes, and freed by `kill -9`. Nothing has been installed into the real `~/.gradle` by this work yet. The first ci-local, dev script or `make ai-setup` run installs it.
- AGENTS.md now has the rule: all goatos Gradle goes through the machine queue, no private Gradle home, JDK 21 only, never kill another session's build.
- Tests: `tools/ci/gradle-home.test.sh`, `tools/ci/gradle-machine-setup.test.sh` (`make gradle-home-self-test`, and the run-local-ci `tools/ci` self-tests).

### Step 4: land-main-batch dropped
Dropped at the maintainer's request, so nothing custom was built. Landings queue on the free self-hosted runner instead: `.github/workflows/land.yml`, run with `gh workflow run land -R vgoats/goatos -f pr=<n>`. The single runner runs one job at a time, so GitHub queues landings FIFO, and each job runs the normal `make land-main`. To land several PRs together, combine them into one PR and land it once.

## Judge round 4 (PR comments 5824815253, 5825036310)

| # | Finding | Fix | Commit |
|---|---|---|---|
| 1 | HIGH: grafana-durability-guard removed while `infra/grafana/{dashboards,provisioning}` still feed `infra/envs/stg/observability.tf` | New `grafana-provisioning-guard` (`tools/ci/check-grafana-provisioning.mjs`): dashboards parse with uid+title, Terraform keeps the provisioning files and dashboards fileset wired, STG deploy scripts never read `infra/grafana`. Adversarial self-test; registered in manifest, `make guardrails`, `run-local-ci.sh` common. | ba20e302f |
| 2 | HIGH: land.yml closed PRs instead of the PR Review + Land Main Rule | After land-main: origin/main verified to contain HEAD; each PR head ref force-with-lease moved to the landed SHA (same repo only) so GitHub marks it Merged; closed with a reason only if that push is refused; state reported per PR. | 74b054f3b |
| 3 | MED: land.yml trust | `permissions: contents: read, pull-requests: write`; fork PRs refused (`isCrossRepository=false`, owner `vgoats`); preflight + ai-setup run from a `main` checkout before any PR code is fetched. land-main rebases, which keeps each commit's AUTHOR; `git config user.*` is only the committer (documented in the workflow). | 74b054f3b |
| 4 | MED: init lock waits forever | Timeout (default 45 min; `GOATOS_GRADLE_MACHINE_LOCK_TIMEOUT_MIN` or `-Pgoatos.machineLockTimeoutMin`) fails the build naming the holder, never kills it; waiting logged every 60s. Known limit: on a configuration-cache hit the lock is taken at the first task-completion event, where a timeout is logged but may not fail the build. | c4b997b70 |
| 5 | MED: user-level `org.gradle.java.home` forces JDK 21 on all projects | Kept on purpose (maintainer: Java 21 only, machine-wide); documented in the script header and as a comment inside the managed block. | 0c6ab2619 |
| 6 | MED: GRADLE_USER_HOME reset breaks sandboxes | Reset only when `$HOME/.gradle` is writable (or creatable); otherwise keep the private home with a warning. Test added. | 0c6ab2619 |
| 7 | LOW: gradle-home test inherits `GOATOS_ALLOW_PRIVATE_GRADLE_HOME` | `env -u` in the test helpers. | 0c6ab2619 |
| 8 | Multi-PR landing | `land.yml` input `prs` (space/comma list; `pr` still works): PRs merged in order onto a branch from origin/main (conflict fails naming the PR), one `make land-main`, each PR resolved per item 2. AGENTS.md: `gh workflow run land -f prs='x y z'`. | 74b054f3b |
| 9 | Nits | m1-local-ci jobs use `[self-hosted, macOS, ARM64, goatos-local-ci]`. Rollout: the self-hosted runner image must be REBUILT because Alloy was removed from it. | 74b054f3b |
| 10 | Run the two Postgres query-plan steps concurrently | FOLLOW-UP, not done: `validate-sqlc-plans`'s Docker fallback uses a fixed container/db name, so two concurrent plan runs can collide. Needs per-run scratch names first. | — |

## land-main 20-min cap (PR #418, 2026-09-25)

Critical path = max over parallel jobs + fetch/rebase/stamp/push overhead.

| Step (critical path) | Before (PR #404 landing) | After (measured on the M1 Pro, this branch) |
|---|---|---|
| android `:app compile+unit+lint` | 1814 s (`--max-workers=1`, whole :app lint) | 882 s cold / 387 s warm (scoped tasks, 6 workers, shared build cache) |
| android config-cache guard (+ self-test) | 346 s + 51 s | skipped unless Android build logic changed (220 s when it runs) |
| android benchmark compile | 45 s | skipped unless build logic / benchmark changed |
| gradle-worktree-lock mutation self-test | 1431 s (on any lock diff) | MODE=all + nightly only |
| backend govulncheck | 114 s | go.mod/go.sum diff only (+ nightly) |
| dispatch | width 3, android launched last | width 5, android launched first |
| **ci-local wall** | ~43 min landing | **918 s cold (15.3 min, overlapped another session's ci-local) / 457 s warm (7.6 min)** |

Method: throwaway local commit touching a feature module Kotlin file, a backend
Go file and an admin-web TS file, `make ci-local` twice (cold worktree, then warm),
commit dropped afterwards. A full-scope Android measurement (build-logic +
design-system diff: all module unit tests + :app lint) was started and stopped
at the maintainer's request before it finished; the build-wide scope first
exposed that JVM modules (core-common, core-model) have `test`, not
`testDebugUnitTest` -- fixed in 7772b7696. Worst-case (full Android set) wall
time is therefore NOT yet measured.

Moved to MODE=all + `.github/workflows/nightly-full-ci.yml` (03:00 IST,
self-hosted, alerts via an issue on failure): every Android module's unit tests
and lint, full Paparazzi, config-cache guard, benchmark compile, govulncheck,
the lock mutation self-test. Budget: land-main prints per-job/total wall time and
warns (never fails) over 20 min into `~/.goatos/land-main-budget.log`.


## 2026-09-26 — fast fail + fast retry (input-keyed step cache)

Problem: `make land-main` failed at ~minute 19, the session fixed one thing and paid
the full ~20 min again, per failure. Passes were cached by commit SHA (and not at all
on auto/all), so any fix commit re-ran everything.

Change: step PASS keyed by input digest (`tools/ci/step-input-digest.mjs`), receipt
carries a verified step ledger, dispatch fails fast (stops only its own jobs, prints
log tail + single-step re-run), and `make land-check` pre-warms the cache without
pushing. Runbook: `docs/runbooks/local-ci.md` -> "Fast fail + fast retry".

Expected (not yet measured on a real landing):

| scenario | before | after |
|---|---|---|
| a step fails | RED after the longest job (~19 min) | RED within seconds of the failing step (fail-fast) |
| re-land after a one-file backend fix | full ~20 min | backend narrow steps + whole-tree guards re-run; android/admin-web build+test reused (android is the long pole) |
| re-land after a docs-only fix | full ~20 min | only whole-tree guards re-run (minutes) |
| `make land-main` right after green `make land-check`, main unchanged | ~20 min | every step reused (cache lookups + receipt) |
| main moved between check and land | ~20 min | whole-tree guards re-run (base is in their key); narrow build/test steps reused unless main touched their inputs |
