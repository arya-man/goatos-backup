# Goat OS STG Deploy Runbook (Canonical Contract)

This is the short, authoritative contract. Full Cloud Deploy mechanics (release
values, custom target, rollout ordering) live in
[`cloud-deploy-staging.md`](./cloud-deploy-staging.md). Org/project boundary
lives in [`google-cloud-environments.md`](./google-cloud-environments.md).

## Non-Deploy Paths (NEVER valid for STG deploy)

- GitHub Actions (billing-blocked FOREVER — validate only via `make ci-local`)
- main→stg PR as a deployment trigger
- GitHub MCP / any GitHub identity as the deploy mechanism
- Slice or Heva GitHub identity for Goat OS
- force-pushing a `stg` branch and waiting for CI
- deploying from a dirty worktree
- deploying from Heva / Slice / system-gsuite / apps-script / goatos-sheets
  projects

## Valid Deploy Path

STG deploy is **manual Google Cloud Deploy**. Cloud Deploy is the deployment
authority for `goatos-stg`. GitHub Actions / Cloud Build / a local operator may
only build images and create a release; Cloud Run staging services and jobs are
mutated by the Cloud Deploy rollout task only.

Scripts:

```bash
tools/deploy/stg-clouddeploy-release.sh   # build/create the release
tools/deploy/stg-clouddeploy-task.sh      # custom-target rollout task
```

Before running any deploy command, verify:

```bash
gcloud auth list
gcloud config get-value account
gcloud config get-value project
git rev-parse HEAD
git rev-parse origin/main
git status --short --branch
```

Expected:

- account: `ravi@mesha.sg`
- org: `vgoats.com`
- project / environment: `goatos-stg` (STG)
- source SHA: latest approved `origin/main`
- working tree: clean

## BUG-041 Policy

BUG-041 is NOT a deployment blocker unless explicitly stated. If BUG-041 remains:

- deploy may proceed
- validation must report the impact
- do not claim vaccination E2E is fully clean
- do not patch BUG-041 inside the deploy task
