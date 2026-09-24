# Dashboard Automation OCI Runbook (moved)

Dashboard automation (OCI nightly production smoke + post-main certification, Slack
`#goatos-automation-alerts`) is owned and deployed by the separate repo
[`vgoats/mesha-ops`](https://github.com/vgoats/mesha-ops):

- code: `mesha-ops/dashboard-automation/tooling/` (overlaid at `tools/dashboard-automation/` inside a
  Goat OS `origin/main` worktree at runtime by `run-oci.sh`; Goat OS no longer tracks that directory)
- runbook: `mesha-ops/dashboard-automation/tooling/RUNBOOK.md`
- guard (route/journey coverage): `mesha-ops/dashboard-automation/tooling/guard/check-dashboard-automation-guard.mjs`
- gate: `./check.sh` in mesha-ops (not Goat OS `make land-main`)

A Goat OS route/schema change can break those checks; fix them in mesha-ops.
