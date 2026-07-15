# Local Release Evidence — Hosted GitHub Actions Outage

## Context

GitHub Actions for the Goat OS repository cannot execute any workflow runs due to an **organization-level billing/platform block** in the vgoats GitHub organization. The remote CI gate is externally blocked and cannot be restored by code changes — this requires a maintainer action in the GitHub org billing UI.

This is an operations/runbook item, **not a software bug** and not a counted row in the consolidated bug ledger. Agents must keep fixing product defects and use the checked-in local CI runner for proof while hosted Actions is unavailable.

## Mandatory Agent Reporting Rule

Until an organization maintainer restores hosted Actions and a workflow actually
starts jobs, agents must use the following language and behavior:

- A synthetic `BuildFailed` / `(Unknown event)` / zero-job `startup_failure` is
  **hosted Actions unavailable**, not "CI failed" and not a code-test failure.
- Do not treat that synthetic run as a release blocker, reopen a fixed defect, or
  keep polling/re-running GitHub Actions for code evidence.
- Run `make ci-local` on the exact candidate SHA first. A green result is the
  authoritative repository release gate and must be reported with that SHA.
- Only report CI as failed when a local gate fails, or when hosted Actions
  actually starts jobs and one of those jobs fails.

- **Root cause**: vgoats org Actions spending limit / billing / platform state (GitHub REST `orgs/vgoats/actions/permissions` billing endpoint returns `410 moved`)
- **Symptom**: All workflow runs fail before any job starts with synthetic `BuildFailed`, `(Unknown event)`, `conclusion=startup_failure`, and **0 jobs**
- **Not a repo defect**: All workflow YAML files parse valid locally, all workflows show `state=active`, and Actions are enabled with `allowed_actions=all` at both repo and org level
- **Outside scope of this session**: Restoring org Actions billing is a GitHub org maintainer action, not a code/config fix

## Local Release Evidence — Authoritative Gate

Per Goat OS AGENTS.md governance: "CI availability is never a closure blocker (Claude AND Codex). A GitHub Actions billing/spending/platform failure — the synthetic `BuildFailed` / `(Unknown event)` / zero-job `startup_failure` runs — must NOT be recorded as an external blocker or used to defer a fix. When remote GitHub Actions cannot execute, run the SAME required CI gates LOCALLY via `make ci-local` (which mirrors `.github/workflows/ci.yml` job-for-job: agent guardrails, scale-guard + self-test, clinical-defer-guard, mobile-guard, `go test ./...`, sqlc/migration validation, admin-web lint/typecheck/mock-fidelity, and the Android compile/unit gate with mandatory JDK/SDK; no USB device is required) and treat a green `make ci-local` on the exact pushed SHA as the authoritative gate."

A **green `make ci-local`** on the pushed commit SHA is the authoritative release evidence when remote GitHub Actions cannot execute.

## Running Local CI Gates

### All Jobs (Default)

```bash
make ci-local
```

This runs guardrails, admin-web, and android jobs in sequence.

### Individual Jobs

Run a single job with `JOB=<job-name>`:

```bash
# Guardrails: agent guards, scale-guard, clinical-defer-guard, mobile-guard,
#             large-file guard, go test ./..., sqlc validation, migration validation
make ci-local JOB=guardrails

# Admin-web: lint, typecheck, mock-fidelity, request-plan validation, production build
make ci-local JOB=admin-web

# Android: :app compile + unit tests (JDK/SDK mandatory, no USB device required)
make ci-local JOB=android
```

### Android Toolchain Requirement

The android job requires:

- **JDK 21** — defaults to `/opt/homebrew/opt/openjdk@21`; override with `JAVA_HOME` env var
- **Android SDK** — defaults to `$HOME/Library/Android/sdk`; override with `ANDROID_HOME` env var

If JDK or SDK is missing:

```bash
# Verify installed tooling
make android-doctor

# Install/update tooling (if needed)
# JDK 21 is pinned in Gradle; SDK version handled by local.properties
echo "sdk.dir=$HOME/Library/Android/sdk" > apps/goatos-android/local.properties
```

No physical device or emulator is required for the compile + unit gate.

## Mirror to Remote Workflows

The local CI script (`tools/ci/run-local-ci.sh`) runs the **exact same checks** as the remote GitHub Actions workflows (`.github/workflows/ci.yml`):

| Remote Job      | Local Job       | Commands                                                 |
|-----------------|-----------------|----------------------------------------------------------|
| `guardrails`    | `JOB=guardrails`| Agent guards, scale-guard, clinical-defer, mobile-guard, go test, sqlc, migrations, large-file check |
| `admin-web`     | `JOB=admin-web` | lint, typecheck, mock-fidelity, request-plan, build |
| *(not yet)*     | `JOB=android`   | :app compile + unit tests (available locally; not yet in remote workflow) |

Both local and remote invoke the same runner: `bash tools/ci/run-local-ci.sh` with environment setup (Go, Node, tooling) matching the GitHub Actions images.

## Restoring GitHub Actions (Maintainer Action)

To restore the remote CI gate, the vgoats GitHub org maintainer must:

1. Open **GitHub Settings** → **Billing** for the vgoats organization
2. Navigate to **Actions** tab
3. Review spending limits, billing status, and platform state
4. Restore Actions billing/spending to an active state (the REST endpoint returning `410 moved` indicates the account is in a moved/suspended state)
5. Verify the fix by:
   - Opening the Goat OS repository Settings
   - Navigating to **Actions** → **General**
   - Confirming **Allow all actions and reusable workflows** is enabled
   - Pushing a test commit to trigger a workflow run and confirm it starts (no longer `BuildFailed` with 0 jobs)

## Branch Protection & Guardrails

**Recommended**: Update branch protection rules to treat missing or skipped checks as blocking:

- GitHub Settings → **Branches** → **Branch protection rules** → Edit rule for `main`
- Enable **Require status checks to pass before merging**
- Enable **Require branches to be up to date before merging**
- Add an external monitor/alert for zero-job startup failures (`conclusion=startup_failure`) to catch future org-level blockers

## Current Status (This Session)

- **Remote gate**: BLOCKED externally (org Actions billing; maintainer action required)
- **Local gate**: GREEN at commit `f78d62b9` (`43/43` local CI steps)
- **Local proof**: full `make ci-local` passed on the exact calendar-canonical commit before it was fast-forwarded to `main`
- **Android**: JDK 21 + SDK available; compile/unit gate runnable
- **Recommendation**: Use `make ci-local` on the exact pushed commit SHA as the authoritative current release evidence until org Actions billing is restored

## Exit Code Semantics

```bash
make ci-local        # exit 0 = all gates pass, exit 1 = any gate fails
make ci-local JOB=X  # exit 0 = JOB X passes, exit 1 = JOB X fails or toolchain missing
```

Use in CI/CD as:

```bash
SHA=$(git rev-parse HEAD)
make ci-local
if [ $? -eq 0 ]; then
  echo "CERTIFIED @ ${SHA}" >> release-gates.txt
else
  echo "FAILED @ ${SHA}" >> release-gates.txt
  exit 1
fi
```
