# Local CI Mirror

The current Goat OS acceptance authority is local CI. GitHub Actions is not
required or assumed to be available; billing/platform startup state must never
delay a review, fix, or main landing. Every developer and agent uses this command
surface:

```bash
make ci-local                 # common + affected components vs origin/main
make ci-local MODE=all        # force common + backend + admin-web + Android
make ci-local JOB=common
make ci-local JOB=backend
make ci-local JOB=guardrails  # compatibility: common + backend + mobile static guards
make ci-local JOB=admin-web
make ci-local JOB=android
GOATOS_RUN_POSTGRES_TESTS=1 make ci-local  # deliberate DB/Docker integration run
GOATOS_SQLC_PLAN_ADMIN_DSN="$DATABASE_URL" make validate-sqlc-plans  # SQL plans via OCI tunnel
```

Hosted workflows, if later enabled, invoke these same targets. They are a mirror,
not the present authority. Do not maintain a second hand-copied command list in
workflow YAML.

## Landing scope vs the nightly full suite (20-min land-main cap)

`make land-main` must finish in 20 minutes wall time. A landing (`make ci-local`,
auto scope) therefore runs only what the diff can break, and the rest runs in
`MODE=all` and in the nightly `nightly-full-ci` workflow on the self-hosted
runner (03:00 IST). Nothing was deleted; it moved.

| Gate | Landing (auto) | `MODE=all` / nightly |
|---|---|---|
| Android compile | `:app:compileStgReleaseKotlin` (all modules :app uses), 6 workers, shared build cache | same |
| Android unit | `:app` + changed library modules + their dependents (`tools/ci/android-gradle-scope.mjs`) | every module |
| Android lint | `:app:lintStgRelease` when `app/**`, `core-designsystem`, any `res/` or build logic changed; else changed library modules' `lintRelease` | every module + `:app` |
| Paparazzi | diff-mapped on an Android UI diff (`ci-local-screenshots`) | full `--rerun-tasks` (nightly: `GOATOS_RUN_ANDROID_SCREENSHOTS=1`) |
| (library-only change) | the library's own `lintRelease`, not `:app` lint: no module has a lint config/baseline and `:app` lint never set `checkDependencies`, so this is the same rules on the changed code | |
| config-cache guard, benchmark compile | Android build-logic diff only | always |
| backend govulncheck | `backend/go.mod`/`go.sum` diff only | always |
| gradle-worktree-lock mutation self-test (~24 min) | never (the real lock guard still runs on a lock diff) | always |

`make land-main` prints per-job and total wall time; over 20 minutes it prints a
loud WARNING with the top 5 steps and appends to `~/.goatos/land-main-budget.log`.
It never fails a landing on budget. Knobs: `GOATOS_ANDROID_MAX_WORKERS` (default
6; lower it, e.g. to 3, if Gradle exits 137 = OOM SIGKILL), `GOATOS_CI_LOCAL_JOBS` (default 5, max 5), `GOATOS_LAND_BUDGET_SECONDS`.

Per machine (opt-in, set in the maintainer's `~/.zshenv`):
`GOATOS_LAND_VIA_QUEUE=1` hands `make land-main` to `land.yml` on the self-hosted
runner (FIFO, one landing CI on the Mac at a time; `GOATOS_LAND_LOCAL=1` is the
runner/emergency override) and `GOATOS_WORKSPACE_ROOT=<dir>` refuses landings from
a checkout outside `<dir>`. Unset, `make land-main` behaves as before.

## Fast fail + fast retry

A failed landing must cost minutes, and a re-land after a fix must re-run only what
the fix could affect. Three pieces, none of which weakens a guard:

**Input-keyed step cache.** Every `step` PASS is cached under a key built from
`tools/ci/step-input-digest.mjs` (git tree ids of the step's input set at the commit),
the job, the step name, the exact command, and CI-relevant env
(`GOATOS_RUN_POSTGRES_TESTS`, `GOATOS_QUERY_PLAN_STEPS`, DSN presence, fast mode,
screenshots). Input sets:

| set | inputs | steps |
|---|---|---|
| `backend` | `backend/ contracts/ tools/ci/ tools/scale-guard/ Makefile` | go mod verify, go vet, govulncheck, sqlc vet, targeted race, `go test ./...`, scale-guard self-test |
| `adminweb` | `apps/admin-web/ contracts/ mock/ packages/` root package/lock files, `tools/ci/ Makefile` | lint, typecheck, unit tests, mock-fidelity, request-plan, production build |
| `android` | `apps/goatos-android/ contracts/ tools/ci/ Makefile` | `:app` compile+unit+lint, screenshots, benchmark compile |
| `whole` | the entire tree **plus the CI base** (many guards are diff-scoped) | every other step (all make guards, query plans) |
| `none` | never cached | `admin-web deps`, live CEO-AI eval |

Only PASS is cached. The cache is off on a dirty tree, in trace mode, and with
`GOATOS_CI_STEP_CACHE=0`. It lives in the git **common** dir
(`goatos-ci-step-cache/`), shared by every worktree.

**Receipt.** A receipt-writing run records a `steps` ledger: every PASS step with
`status` `run` or `reused`, its input set and digest. `check-local-ci-evidence.mjs`
recomputes each digest for the receipt SHA at record time AND at pre-push/verify; a
reused (or run) pass whose digest differs from the pushed tree blocks the push. A
receipt without `steps` (pre-cache, or `--reuse-after-rebase`) carries no reuse claim
and is judged as before.

**Fail fast.** In a dispatched run (auto/all, i.e. every landing), the first failing
step writes a sentinel; the dispatcher stops ONLY the jobs it launched (leaves-first
tree walk of its own pids; never `pkill` by name), prints the failing job's last 40
log lines and the exact re-run line:

```text
GOATOS_CI_ONLY_STEP='<step>' tools/ci/run-local-ci.sh <job>
```

That runs just that step (other steps report SKIP), writes no receipt (it is refused
on auto/all), and on a clean committed tree caches the PASS for the next landing.
`GOATOS_CI_FAIL_FAST=0` restores run-everything semantics (e.g. to see every failure
of a MODE=all sweep).

**`make land-check`.** Runs the exact ci-local land-main would (temp worktree, rebased
on fresh origin/main, same `ci-local` / `ci-local-screenshots` pick) without pushing,
and leaves your worktree untouched. Loop: `make land-check` -> fix -> commit ->
single-step re-run -> `make land-check` -> `make land-main` (reuses all unchanged
passes).

Self-tests: `make ci-fast-retry-guard` (`step-input-digest.mjs --self-test` +
`tools/ci/check-ci-fast-retry.test.sh`) and `node tools/ci/check-local-ci-evidence.mjs --self-test`.

## Landing on main

Codex and Claude must use this command when ordinary work or this documentation
foundation requires a push to `main`:

```bash
make land-main
```

It requires a clean worktree and performs this sequence automatically:

```text
fetch origin/main
-> rebase candidate onto that SHA
-> install/refresh local push guards
-> make ci-local on the rebased candidate
-> fetch origin/main again
-> if main moved, rebase and rerun CI
-> git mesha-push HEAD:main
-> fetch and verify origin/main contains the certified SHA
```

Direct agent-issued `git push` or `git mesha-push` commands targeting `main`
are rejected by both Claude and Codex hooks. The Git pre-push hook is the second
line of defense: it rejects stale-main candidates even when their receipt came
from a full CI run. `make ci-local` remains non-mutating for development and
hosted workflows; automatic rebase belongs only to `make land-main` because a
session can start inside a dirty/shared worktree that must not be rewritten.

Run the deterministic fixture test with `make land-main-self-test`.

### Whole-ledger and task-kernel program exception

`make land-main` remains the ordinary direct-main landing gate and is also the
gate for the documentation foundation that authorizes the program. It is not
the final landing path for the approved whole-ledger/task-kernel implementation
program. That program keeps one externally visible integration PR against
`main`; internal agents contribute reviewed commits without external milestone
PRs. F0 must add `make land-integration-pr PR=<number>`, a repo-owned gate that
verifies Mesha/VGoats authority, one open same-repo program PR with base `main`,
expected base/head, local HEAD equal to remote PR head, fresh main as an
ancestor, exact-head local-CI/proof/review receipts, and required certification
lanes. After a final refetch it uses the existing guarded Mesha fast-forward
push so the tested PR head itself becomes `main`; any race fails. It then
requires fresh `origin/main` to equal that head and the PR to report merged.
Until the helper and adversarial tests land, implementation batches remain
closure-pending and the program PR cannot land.

### Landing queue

Only one `make land-main` runs at a time per clone. The lock is a directory at `$(git rev-parse --git-common-dir)/goatos-land-main.lock`, shared by all worktrees. `GOATOS_LAND_MAIN_LOCK_DIR` overrides the location.

- **Lock held by a live process:** land-main prints the holder's pid, worktree, SHA and start time, then exits 1. It never waits and never kills anything. Rerun after that landing finishes.
- **Lock left by a process that has exited:** the lock is stale and is reclaimed automatically.

Many parallel sessions should not run `make land-main` themselves. Land a PR with
`gh workflow run land -R vgoats/goatos -f pr=<n>`: the single self-hosted runner on the
laptop runs one landing at a time, so GitHub queues them FIFO, and each job runs the
normal `make land-main`. To club PRs, pass `-f prs='x y z'`: they are merged in order
onto a branch cut from fresh main (a conflict fails naming the PR), `make land-main` runs
once, and each PR's head branch is moved to the landed SHA so GitHub marks it merged.
Fork PRs are refused. The runner image must be rebuilt after Alloy was removed from it.

### Machine Gradle queue

Every goatos Android Gradle build on the laptop runs one at a time:

- `~/.gradle/init.d/goatos-machine-lock.init.gradle` (installed by
  `tools/ci/gradle-machine-setup.sh` via `make ai-setup` and every repo Gradle
  entrypoint) takes an OS file lock on `~/.gradle/goatos-build.lock` per build. It
  covers plain `./gradlew`, Android Studio and every agent session. The OS drops it
  if the process dies, and it prints the holder while waiting.
- Repo scripts also take the ci-local machine lock (`tools/ci/gradle-run.sh`,
  `tools/ci/gradle-worktree-lock.sh`) and reset a temp `GRADLE_USER_HOME` to
  `~/.gradle`.
- Gradle must run on JDK 21 (`tools/ci/java21.sh`). A managed block in
  `~/.gradle/gradle.properties` pins `org.gradle.java.home` to JDK 21 and makes idle
  daemons exit after 10 minutes.
- Opt outs: `GOATOS_GRADLE_MACHINE_LOCK=0` (one build), `GOATOS_GRADLE_MACHINE_SETUP=0`
  (skip install), `GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1`.
- Tests: `make java21-self-test gradle-home-self-test`.

## Local-only enforcement when hosted Actions is unavailable

When GitHub creates only a zero-job `startup_failure`/`BuildFailed` run:

1. Verify the failure happened before any job; do not relabel a real test
   failure as billing trouble.
2. Check out the exact candidate SHA with a clean tree.
3. Run `make ci-local`. It selects the complete affected-component set from
   `tools/ci/component-paths.json`; CI/shared-tooling and unmapped runtime paths
   force all jobs. Missing required tooling, generated-code drift, build failure,
   or any red selected sub-step is failure.
4. Record the full SHA and the final `ci-local: GREEN @ <sha>` line in the proof
   packet.
5. For ordinary work and this documentation foundation, run `make land-main`;
   it performs fresh-main rebase, exact-SHA CI, race recheck, and the
   Mesha-credential push in the required order. For the sole approved
   whole-ledger/task-kernel program PR, run
   `make land-integration-pr PR=<number>` only after F0 implements and proves
   that gate.

The common job always runs repository, agent, contract, operational read-model,
domain-event architecture, large-file, and diff hygiene. In particular,
movement/Vaccination producer-to-consumer closure is checked by
`domain-event-architecture-guard` on every normal `make ci-local` run; it is not
confined to the legacy compatibility job. Pluggable vertical/read-model
discoverability is checked by `operational-read-model-contract-guard`, which
keeps `docs/architecture/operational-read-model-contract.md` wired into AGENTS,
SKILLS, build skills, review lenses, frontend/mobile references, and this
runbook.
It also runs `local-stack-service-guard`, which mechanically checks the exact
origin/main shared FE/BE contract, canonical DB pin, LaunchAgent tool PATH,
atomic child cleanup, live main-drift watchdog, and the isolated E2E boundary.
Backend owns kernel/E2E/scale static guards and Go package/unit tests. Postgres
containers, DB-backed Go tests, the Docker E2E chain, sqlc schema regeneration,
migration replay, and live latency are skipped by default. They run only with
`GOATOS_RUN_POSTGRES_TESTS=1` locally or the hosted workflow's manual
`run_postgres_tests` input. Required SQL query-plan validation is separate:
open the OCI tunnel and run `make validate-sqlc-plans` with
`GOATOS_SQLC_PLAN_ADMIN_DSN` set to the tunnel DSN. `MODE=all` does not imply
Postgres. Admin-web owns its request-read guard, dependency install,
lint, tests, typecheck, fidelity gates, and production build. Android owns its
mobile/offline/telemetry/memory/Room guards plus staging release compile and unit
suite under JDK 21.

Local CI certifies repository code. Restoring GitHub billing or required-check
enforcement is a separate optional operational task and is never part of PR
acceptance while Actions is unavailable.

## Guardrail registration and exact-SHA push evidence

Guardrails are part of root-cause closure, not an optional clean-up after the
behavior lands. For every bug, audit batch, kernel milestone, migration, or new
feature, apply
`context/execution/defect-prevention-execution-contract.md`. If the recurrence
is mechanically detectable, the fix batch must add or strengthen the structural
guard, its adversarial self-test, manifest entry, Make target, and ordinary
affected local-CI step together. If a DB/transaction/type/schema or runtime
reconciler is the stronger control, record why a static guard is unsuitable.
Route deterministic regressions through the ordinary affected job; record and
run applicable PostgreSQL, migration, device, browser, deploy, or live-state
certification separately. A skip is absence of proof, and a green
compatibility-only `JOB=guardrails` run does not prove an ordinary PR is
protected.

Every machine guardrail in `make guardrails` and `make ci-local` is registered in
a single source of truth: `tools/ci/guardrail-manifest.json`. Each entry declares:

- The Make target that runs the guard (e.g., `scale-guard`, `clinical-defer-guard`)
- The real-check command executed by that target
- A self-test command that validates the guard itself works, or an explicit
  `selfTestExemptReason` explaining why the guard is exempt from self-testing
- The owning documentation (file path or runbook reference)
- A `requiredInCI` flag: `true` if the guard is assigned to a local-CI component
  job, `false` if it's optional or local-only

The current `guardrail-registration-guard` (Make target, part of
`make guardrails`) provides a partial textual registration check. It fails for
these declared shapes:

- A new `check-*.mjs` guard exists under `tools/agent-hooks/` or `tools/ci/` but
  is absent from the manifest (silent hole: unregistered guards skip themselves)
- A manifest guard declares neither a self-test command nor an `selfTestExemptReason`
  (incomplete registration: unvalidated guards might silently break)
- A `requiredInCI=true` guard's declared target/step text is absent from the
  expected Make/CI files.

It does **not yet** prove that the target invokes the real check, that the
ordinary affected job executes it, that IDs/scripts/docs are unique and exist,
or that a comment, `echo`, dead branch, or wrong target cannot spoof wiring.
Those semantic checks and adversarial fixtures are mandatory F0 work in the
current remediation ledger. Until F0 closes them, review the actual recipes and
job routing directly.

The `guardrail-registration-guard` target runs its adversarial self-test and real
check together:

```bash
make guardrail-registration-guard
```

When you add a new guardrail, register it BEFORE the commit:

1. Add a `check-<name>.mjs` script under `tools/agent-hooks/` or `tools/ci/`
2. Register it in `tools/ci/guardrail-manifest.json` with a Make target, real
   command, self-test or exemption reason, owning docs, and `requiredInCI` flag
3. Wire the Make target into `Makefile:guardrails` (if `requiredInCI=true`)
4. Add the CI step to a standard common/component function in
   `tools/ci/run-local-ci.sh` (if `requiredInCI=true`); compatibility-only wiring
   is rejected
5. Run `make guardrails` locally to verify the registration passes

Admin-web sectionable aggregate reads are covered by
`make admin-web-sectioned-aggregate-reads-guard`. When a page calls a backend
aggregate endpoint that supports `sections`, local CI expects the page to pass
only the rendered sections. The only permitted full-payload exception is an
adjacent `sectioned-aggregate-reads:allow reason=<why>` comment reviewed with
latency evidence.

The `guardrail-registration-guard` runs first in `make guardrails`, so the
textual registration failures it recognizes are caught immediately. Do not
claim it prevents every silent/unwired guard hole until the F0 semantic
hardening and spoof tests land.

A green default `make ci-local` writes an exact-SHA receipt into the worktree git
directory (`goatos-ci-local-receipt.json`). A full-classified or `MODE=all` run
records mode `all`. A narrower run records mode `scoped`, the exact remote-main
base, component-rule hash, and selected jobs. The pre-push hook recomputes the
diff, verifies current remote main is an ancestor of the pushed candidate, and
rejects stale/incomplete receipts. Explicit `JOB=...` runs record nothing and
never authorize a push.

## Performance-budget guards (STG latency program, 2026-09-24)

Budget: API p95 50-100ms target, 200-300ms acceptable, 500ms hard max. Patterns
the guards enforce are catalogued once in
`.agents/skills/scale-anti-patterns/SKILL.md` ("STG latency catalog", P1-P25);
design in `docs/perf/2026-09-24-stg-latency/audit/guardrails-design.md`.

Current guards:

```bash
make scale-guard                      # static N+1 / OFFSET / god-CTE / non-SARGable shapes
make validate-sqlc-plans              # named EXPLAIN index assertions (needs Docker or GOATOS_SQLC_PLAN_ADMIN_DSN)
node tools/perf/api-latency-policy.test.mjs   # latency policy ceilings
make admin-web-request-reads-guard admin-web-prefetch-guard admin-web-sectioned-aggregate-reads-guard
make worker-stage-budgets-guard deployed-job-flags-guard
```

Planned in Wave G (`docs/perf/2026-09-24-stg-latency/QUEUE.md`; not yet wired,
do not claim them as evidence until they are registered in
`tools/ci/guardrail-manifest.json`): `query-plan-budget-guard` (every query
EXPLAINed at stg-sized seed: seq scans > 5k rows, row blowup, temp spill,
unbounded history, missing FK index), `api-latency-budget` + route inventory
(every GET route budgeted; every list route declares page params and a max page
size), `oltp-hygiene-guard` (retention required, no analytics writes in OLTP,
pool `Acquire` allowlist), `job-safety-guard` (watermark, non-fatal external
deps, backoff, conn cap), `stg-drift` (deployed SHA vs `origin/main`), and a PR
perf-evidence check.

DB-backed guards run against Docker or the OCI clone's throwaway DB and must
fail closed in `make land-main` when no DB is reachable (never skip).

Allowlist rule: an exemption is an allowlist JSON entry
`{ "id", "reason", "owner", "expires": "YYYY-MM-DD", "evidence" }` or an inline
`perf-budget:allow <rule> reason="..." owner=@x expires=YYYY-MM-DD` next to the
line. The reason is specific (>= 20 chars), the evidence links EXPLAIN or latency
output, expired entries fail, and baselines only ratchet down. Never disable a
guard or raise a budget to get green.

## Reused pgtest template on OCI

When the Postgres gates run against a supplied server (`GOATOS_PGTEST_ADMIN_DSN` /
`GOATOS_SQLC_PLAN_ADMIN_DSN`, normally the OCI Postgres through the `127.0.0.1:15432` tunnel), they
no longer replay all migrations per run. `backend/internal/platform/pgtemplate` keys a migrated
template by a hash of `backend/migrations/postgres/*.sql` (names + contents + a harness version):

| Database | Meaning | Lifetime |
|---|---|---|
| `goatos_pgtest_template_<16 hex>` | completed template, connection-locked, commented with hash + build time | kept; the current and the most recent previous are always kept, others dropped after 3 days |
| `goatos_pgtest_build_<16 hex>_<unix>` | template being built; renamed to the template name only when complete | dropped by the next builder of that hash, or after 6 hours |
| `goatos_pgtest_clone_<unix>_*` | per-test (pgtest) or per-run (`validate-sqlc-plans`) clone | dropped by its owner; leftovers of killed runs dropped after 6 hours |

Concurrent runs needing the same missing template serialize on a Postgres advisory lock: one builds,
the others wait and reuse it. The first run after a migration change pays the full build (tens of
minutes over the tunnel); later runs only clone. `go run ./internal/platform/pgtemplate/cmd/pgtest-template "$DSN"`
(from `backend/`) builds/reaps on demand and prints the template name.

**Disk rule:** on OCI, the stg clone DB `goatos` and the current `goatos_pgtest_template_*` must
NEVER be dropped to free disk; stale `goatos_pgtest_clone_*`, `goatos_pgtest_build_*` and older
templates are safe to delete. Cleanup code only ever matches those three exact name patterns
(`TestCleanupNeverMatchesForeignDatabases`); legacy `goatos_tmpl_*` / `goatos_test_*` /
`goatos_sqlc_plans_*` leftovers from the old per-run scheme are not reaped automatically.
