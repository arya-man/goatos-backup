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
- Run `make ci-local` on the exact candidate SHA first. It always runs common
  guards and every component selected by the checked-in path rules. A green
  result is the authoritative repository release gate and must be reported with
  that SHA.
- Only report CI as failed when a local gate fails, or when hosted Actions
  actually starts jobs and one of those jobs fails.

- **Root cause**: vgoats org Actions spending limit / billing / platform state (GitHub REST `orgs/vgoats/actions/permissions` billing endpoint returns `410 moved`)
- **Symptom**: All workflow runs fail before any job starts with synthetic `BuildFailed`, `(Unknown event)`, `conclusion=startup_failure`, and **0 jobs**
- **Not a repo defect**: All workflow YAML files parse valid locally, all workflows show `state=active`, and Actions are enabled with `allowed_actions=all` at both repo and org level
- **Outside scope of this session**: Restoring org Actions billing is a GitHub org maintainer action, not a code/config fix

## Local Release Evidence — Authoritative Gate

Per Goat OS AGENTS.md governance, hosted Actions unavailability never blocks
continuing fixable work. Run the same affected-component gates locally with
`make ci-local`; CI/shared-tooling and unmapped runtime paths force the full
suite. Treat a green result on the exact pushed SHA as authoritative evidence.

A **green `make ci-local`** on the pushed commit SHA is the authoritative release evidence when remote GitHub Actions cannot execute.

## Running Local CI Gates

### Affected Jobs (Default)

```bash
make ci-local
```

This compares the candidate to `origin/main`, runs common repository guards,
then adds backend, admin-web, and/or Android according to
`tools/ci/component-paths.json`.

To force every job:

```bash
make ci-local MODE=all
```

This still skips every Postgres/Docker database gate. Run those only when
explicitly requested:

```bash
GOATOS_RUN_POSTGRES_TESTS=1 make ci-local
```

### Individual Jobs

Run a single job with `JOB=<job-name>`:

```bash
# Common repository/agent/contract/file guards
make ci-local JOB=common

# Backend/kernel/scale static guards + Go package/unit tests (no Postgres by default)
make ci-local JOB=backend

# Backward-compatible combined static guard surface
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

| Remote Job | Local Job | Commands |
|------------|-----------|----------|
| `common` | `JOB=common` | Repository, agent, contract, large-file, and diff guards |
| `backend` | `JOB=backend` | Kernel/scale/E2E static guards and Go package/unit tests; Postgres gates require explicit opt-in |
| `admin-web` | `JOB=admin-web` | lint, unit tests, typecheck, request/fidelity guards, build |
| `android` | `JOB=android` | mobile static guards, app compile, unit tests |

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

## Exact-SHA Local-CI Push Gate (Main)

Only a complete green `make ci-local` on the exact commit SHA authorizes a push
to `main`. A machine-local pre-push hook installed by `make ai-setup` (or
`make stg-promotion-guard-install`) enforces this gate via a SHA-bound receipt.

### Flow

```
Developer/Codex/Claude commits in a clean candidate worktree
    ↓
    make land-main
    ↓
    fetch origin/main -> rebase -> make ci-local -> fetch origin/main again
    ↓
    git mesha-push HEAD:main (issued inside the landing script)
    ↓
    Pre-push hook runs check-local-ci-evidence.mjs --pre-push
    ↓
    Hook checks: current remote-main SHA is an ancestor of candidate AND
                 receipt.sha === commit SHA AND result === green AND
                 mode === all
                 OR
                 mode === scoped AND base === current remote-main SHA AND
                 rules hash is current AND recorded jobs exactly match the
                 classifier's recomputed required jobs
    ↓
    If YES: push is permitted
    If NO:  push is rejected (hook exits 1)
```

Codex and Claude hook configs block direct agent-issued main pushes before Git
is invoked. This forces agents through `make land-main`; the pre-push checks
remain necessary for human terminals and defense in depth.

### Recording the Receipt

The receipt is written automatically by the default `make ci-local`:

```bash
# Run common plus every affected component
make ci-local

# If all gates pass, the script runs:
# tools/ci/check-local-ci-evidence.mjs --record <current-sha> --mode <all|scoped> ...
# which writes: <git-dir>/goatos-ci-local-receipt.json

# The receipt is machine-local and never committed
# It binds the exact SHA to either full or recomputable scoped coverage
```

The receipt contains:

```json
{
  "sha": "<commit-sha>",
  "result": "green",
  "mode": "scoped",
  "base": "<remote-main-sha>",
  "jobs": ["backend", "common"],
  "rulesHash": "<sha256-of-component-paths.json>",
  "timestamp": "<iso-8601>"
}
```

### Partial Runs Do NOT Authorize a Push

If you run `make ci-local JOB=<job-name>` (a partial job):

```bash
make ci-local JOB=guardrails
make ci-local JOB=backend
make ci-local JOB=common
make ci-local JOB=admin-web
make ci-local JOB=android
```

The explicit partial run **intentionally writes NO receipt**. It passes or fails
the selected job for development iteration, but it does not authorize a `main`
push. Use default `make ci-local` for complete affected-component evidence, or
`make ci-local MODE=all` to force everything.

### How to Install the Hook

The pre-push hook is installed by:

```bash
make ai-setup
# or
make stg-promotion-guard-install
```

Both commands invoke `tools/agent-hooks/install-stg-push-guard.sh`, which chains:

1. The existing staging-promotion block (prevents direct pushes to remote `stg`)
2. The new exact-SHA main-CI-evidence gate (this new feature)

The hook is scoped to the `vgoats/goatos` origin. It applies to ALL updates to
`refs/heads/main` including `HEAD:main`, `main:main`, local branch `main`, and
branch deletes. Non-main pushes and branch deletes to other refs are not gated.

### Bypassing the Hook

Do NOT bypass the hook with `git push --no-verify`. The hook is a governance layer:

- It enforces that every `main` push has passed the complete classifier-selected
  local-CI suite on the exact commit and remote-main base.
- Skipping it with `--no-verify` is a circumvention, not a valid escape hatch.
- If the receipt or main base is stale, run `make land-main`; it rebases and
  reruns CI before retrying the guarded push.

If you absolutely must override in an emergency (rare, maintainer-only):

1. Have a CLEAR, documented reason (e.g., production incident, data loss)
2. Coordinate with other maintainers to ensure the bypassed commit is later
   certified or rolled back
3. Document the override in the incident record

### Receipt State Management

- **Machine-local only**: `goatos-ci-local-receipt.json` lives in the worktree git
  directory (`.git/`) and is NEVER committed
- **Per-machine, per-clone**: Each developer or CI agent build has its own receipt
- **Stale receipt**: If you rebase or reset `HEAD` after a previous `make ci-local`,
  the old receipt's SHA no longer matches; you must re-run `make ci-local` on the
  new SHA to generate a fresh receipt before pushing
- **Changed remote base**: A scoped receipt is rejected if `main` advanced after
  the run; fetch/rebase and re-run so the classifier covers the actual push diff
- **Full receipts still require fresh main**: Even `MODE=all` evidence does not
  authorize a non-rebased candidate; remote main must be an ancestor of the SHA
  being pushed
- **No global receipt store**: The receipt is ephemeral and machine-local; it does
  not sync between developers or persist after a clone/checkout
