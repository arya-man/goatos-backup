# Dashboard automation Go test cwd follow-up — 2026-09-22

## Scope

Fix the dashboard automation vaccination lifecycle layer so backend Go tests run from the `backend` Go module instead of the repository root.

## Why

The production-smoke dry-run proved the runner now reaches the browser-smoke layer after business-data parity failure, but it also exposed that `go test ./internal/...` was launched from the repo root. This repo has the Go module under `backend`, so that layer failed with `go.mod file not found`.

## Done

- `tools/dashboard-automation/run.mjs` now runs vaccination lifecycle Go tests with `cwd = <repo>/backend`.
- Runner self-test now asserts the backend cwd wiring is present.

## Proof

- `node tools/dashboard-automation/run.mjs --self-test`
- `make dashboard-automation-self-test`
- `GOATOS_OCI_ALWAYS_FREE_CONFIRMED=1 GOATOS_DASHBOARD_API_LATENCY=0 GOATOS_DASHBOARD_SLACK_ALERTS=0 GOATOS_DASHBOARD_SELF_HEALING=0 node tools/dashboard-automation/run.mjs --mode production-smoke --out-dir /tmp/goatos-dashboard-run-dry-proof-2`

The dry-run showed both vaccination lifecycle Go packages passed after the cwd fix. Browser smoke reached the production module-journey layer and stopped only because the dry-run intentionally did not provide production auth env vars.
