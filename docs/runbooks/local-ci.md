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
```

Hosted workflows, if later enabled, invoke these same targets. They are a mirror,
not the present authority. Do not maintain a second hand-copied command list in
workflow YAML.

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
SQL plans, migration replay, and live latency are skipped by default. They run
only with `GOATOS_RUN_POSTGRES_TESTS=1` locally or the hosted workflow's manual
`run_postgres_tests` input. `MODE=all` does not imply Postgres. Admin-web owns its request-read guard, dependency install,
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
