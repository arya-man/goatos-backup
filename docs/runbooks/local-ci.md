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
policy, scale-guard plus self-tests, clinical and sweeper deployment safety,
mobile and large-file guards, all Go tests, sqlc generation drift, SQL plan and
migration validation, and `git diff --check`. Admin-web includes dependency
install, lint, typecheck, whole-tree request-plan/fidelity guards, and the
production build with bearer-token leak detection. Android includes the staging
release compile and unit suite under JDK 21.

This fallback certifies repository code only. Restoring GitHub billing and
required-check enforcement remains an operational task, but it never blocks
continuing other fixable ledger work.
