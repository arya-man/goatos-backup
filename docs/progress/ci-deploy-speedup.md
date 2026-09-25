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
- Android runs with `--no-daemon --no-configuration-cache --max-workers=1` and in-process Kotlin. The full paparazzi run uses `--rerun-tasks`.

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
- g. **No change needed.** `apps/goatos-android/gradle.properties` already sets `org.gradle.caching=true`, so the receipt-path Gradle runs already use the local build cache. `--no-daemon`, in-process Kotlin and `--max-workers=1` stay. Full paparazzi keeps `--rerun-tasks`, which bypasses the cache on purpose. Raising the worker count is still an open decision that needs a recorded A/B test.
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
- Dropping `--rerun-tasks` from full paparazzi.
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
- Measured 2026-08-25 to 2026-09-24: 170 builds, 4,102 build-minutes, all on the default machine. At ~$0.006/min minus the default-machine free tier: roughly $10/month.
- With `E2_HIGHCPU_32`: builds finish ~2-3x faster, ~1,500-2,000 build-minutes/month at ~$0.064/min (no free tier): roughly $100-130/month (~₹8.5-11k).
- Delta ~₹8-10k/month against an August GCP bill of ~₹34k. Per-minute prices are list prices from memory; verify in the billing report. Decision: keep `E2_HIGHCPU_32` (speed is the goal).
