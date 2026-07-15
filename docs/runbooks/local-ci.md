# Local CI Mirror

GitHub Actions billing or platform startup failure is not a reason to stop Goat
OS defect work. The repository has one CI command surface used by both GitHub
workflows and local agents:

```bash
make ci-local                 # guardrails + admin-web + Android
make ci-local JOB=guardrails
make ci-local JOB=admin-web
make ci-local JOB=android
```

`.github/workflows/ci.yml` and `.github/workflows/android-quality.yml` invoke
these same targets. Do not maintain a second hand-copied list of commands in
workflow YAML. A gate added to `tools/ci/run-local-ci.sh` therefore protects
both local and hosted runs.

## Billing/platform fallback

When GitHub creates only a zero-job `startup_failure`/`BuildFailed` run:

1. Verify the failure happened before any job; do not relabel a real test
   failure as billing trouble.
2. Check out the exact candidate SHA with a clean tree.
3. Run `make ci-local`. Missing JDK/SDK, a skipped job, generated-code drift,
   build failure, or any red sub-step is failure.
4. Record the full SHA and the final `ci-local: GREEN @ <sha>` line in the proof
   packet.
5. Push that exact SHA through the Mesha credential and re-run locally if a
   rebase changes it.

The guardrails job includes agent boundary/contract/E2E checks, API latency
policy, scale-guard plus self-tests, natural hot-query planner proofs (including
the outbox UUID-array release path), clinical and sweeper deployment safety
(including the adversarial same-block actor-wiring fixture),
mobile and large-file guards, all Go tests, sqlc generation drift, SQL plan and
migration validation, and `git diff --check`. Admin-web includes dependency
install, lint, typecheck, whole-tree request-plan/fidelity guards, and the
production build with bearer-token leak detection. Android includes the staging
release compile and unit suite under JDK 21.

This fallback certifies repository code only. Restoring GitHub billing and
required-check enforcement remains an operational task, but it never blocks
continuing other fixable ledger work.

## Guardrail registration and exact-SHA push evidence

Every machine guardrail in `make guardrails` and `make ci-local` is registered in
a single source of truth: `tools/ci/guardrail-manifest.json`. Each entry declares:

- The Make target that runs the guard (e.g., `scale-guard`, `clinical-defer-guard`)
- The real-check command executed by that target
- A self-test command that validates the guard itself works, or an explicit
  `selfTestExemptReason` explaining why the guard is exempt from self-testing
- The owning documentation (file path or runbook reference)
- A `requiredInCI` flag: `true` if the guard is part of the full local-CI guardrails
  job, `false` if it's optional or local-only

The `guardrail-registration-guard` (Make target, part of `make guardrails`) is a
meta-guard that FAILS if:

- A new `check-*.mjs` guard exists under `tools/agent-hooks/` or `tools/ci/` but
  is absent from the manifest (silent hole: unregistered guards skip themselves)
- A manifest guard declares neither a self-test command nor an `selfTestExemptReason`
  (incomplete registration: unvalidated guards might silently break)
- A `requiredInCI=true` guard's Make target is missing from the `guardrails:` target
  in the Makefile (unwired guard: appears to run but doesn't)
- A `requiredInCI=true` guard's CI step is missing from `tools/ci/run-local-ci.sh`
  (half-wired: local passes but remote CI skips it)

The `guardrail-registration-guard` itself has a `--self-test` mode. Run it locally
with `make guardrails JOB=guardrail-registration-guard --self-test` to validate the
guard logic.

When you add a new guardrail, register it BEFORE the commit:

1. Add a `check-<name>.mjs` script under `tools/agent-hooks/` or `tools/ci/`
2. Register it in `tools/ci/guardrail-manifest.json` with a Make target, real
   command, self-test or exemption reason, owning docs, and `requiredInCI` flag
3. Wire the Make target into `Makefile:guardrails` (if `requiredInCI=true`)
4. Add the CI step to `tools/ci/run-local-ci.sh` (if `requiredInCI=true`)
5. Run `make guardrails` locally to verify the registration passes

The `guardrail-registration-guard` runs first in `make guardrails`, so registration
failures are caught immediately. Silent holes (unregistered/self-test-less/unwired
guards) are the class of defects this meta-guard prevents.

A full green `make ci-local` (not `JOB=<name>`) writes an exact-SHA receipt into the
worktree git directory (`goatos-ci-local-receipt.json`): a JSON object binding that
run's commit SHA, result (`green`), and mode (`all`). This receipt is machine-local,
never committed, and serves as proof that the exact commit passed the full local-CI
suite. The pre-push hook (installed by `make ai-setup`) uses this receipt to gate a
main push: only if a receipt exists, its SHA matches `HEAD`, result is green, and
mode is `all` does `git push origin HEAD:main` succeed. Partial runs (`JOB=...`)
intentionally record NOTHING, so they never authorize a push.
