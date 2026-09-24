# CI + STG deploy speed-up

Status: DRAFT, in progress. Updated on every push.

## Problem
- `make land-main` takes 30-60 min in practice; target typical <=15 min.
- STG deploy backend+web+Android takes ~65 min (backend+web ~31, Android separate ~34); target <=15-20 min.
- Constraint: no guard, test, or safety check may be lost or weakened.

## Measured baseline
### Local CI (`.git/goatos-ci-local-timings.jsonl`, 167 runs)
- Quiet full pass: p50 5.2 min, p90 8.8 min.
- Contended (several agents on one laptop): 30-50 min. 09-24 20:06: android 40.5, backend 14.7, admin-web 12.2, wall 50.6.
- Same step runs 5-15x slower under contention; warm cache makes go test ~30x and Android compile 7-10x faster.
- 16% of ci-local runs overlapped another; bursts on 23-24 Sep.
- Root causes: concurrent agents, deliberately conservative Gradle flags (`--no-daemon`, `--max-workers=1`, in-process Kotlin, `--rerun-tasks` on full Paparazzi), cold caches in fresh worktrees.

### STG deploy (Cloud Build, goatos-stg)
| Step | Backend+web (dd268f77 / b7a209c2) | Android (0cf96274) |
|---|---|---|
| Images: backend / migration / admin-web / agent (serial, no cache, default machine) | 5.4-5.8 / 0.5 / 4.8-5.0 / 2.0 | - |
| Release step (incl. 9.5 min Cloud Deploy rollout) | 15.4-15.7 | - |
| Android distribution (Gradle 28m56s, 563/676 tasks executed, 1 worker) | - | 33.6 |
| Total | 30.4 / 31.3 | 34.6 |

Rollout 9.5 min: task setup ~1.7, kernel-worker pre-migration 0.8, migration 13 s, API+cutover 0.5, 5 services serial ~2.5, Grafana/observability ~4.3.

## Changes (this PR)
- Deploy: parallel image builds (`waitFor`), bigger machine, BuildKit registry layer cache, Android build concurrent with images, post-API services rolled out in parallel (worker -> migration -> API order unchanged). Every image still rebuilt at the exact commit.
- Local CI (in progress): landing lock, Gradle build cache on the receipt path (daemon/config cache stay off), screenshot-scope adversarial fixtures, query plans parallel on per-run OCI throwaway DB.

## Rejected (would weaken guards)
- Fast mode writing the landing receipt (wrong base, skips benchmark compile).
- Reusing prior image tags (breaks exact-SHA provenance; GIT_SHA baked into images).
- Gradle daemon / configuration cache on the landing path (Firebase Perf instrumentation corruption history, f4a63345).
- Dropping `--rerun-tasks` from full Paparazzi.

## Out of scope here
- Grafana/observability steps (~4.3 min rollout + 2 release smokes): being moved to mesha-ops by another session; mesha-ops should deploy on its own commits and smoke on a schedule, with no goatos trigger.

## Estimates (untested)
| | Now | Best | Typical | Worst |
|---|---|---|---|---|
| land-main | 30-60 | 3-4 | 5-8 | 12-15 |
| Deploy backend+web | ~31 | 11 | 12-15 (after Grafana move) | 18 |
| Deploy backend+web+Android | ~65 | 12 | 15-20 | 25 |

## Verification
- Done: `bash -n` on changed scripts, YAML parse of `cloudbuild.stg.yaml`.
- Pending: guard self-tests, a measured Cloud Build run, a measured land-main after the concurrent landing finishes.
