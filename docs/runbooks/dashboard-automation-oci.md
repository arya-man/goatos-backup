# Dashboard Automation OCI Runbook

This runbook is the implementation companion for
`docs/engineering/dashboard-nightly-automation-plan.md`.

## Current status

The repository now contains the guarded automation entrypoint and validation checks. Enabling the OCI
timer still requires host-specific secrets and service wiring on the OCI VM.

Do not enable a timer until three manual dry or report-only runs produce clean receipts.

## Commands

Dry-run the deterministic setup without touching production or OCI resources:

```sh
node tools/dashboard-automation/run-dashboard-automation.mjs --mode production-smoke --dry-run
```

Run the production-safe smoke only after the authenticated read-only environment and explicit OCI
Always Free readback are present:

```sh
GOATOS_DASHBOARD_OCI_FREE_CLASSIFICATION="Always Free" \
GOATOS_ADMIN_WEB_BASE_URL=https://dashboard.mesha.sg \
GOATOS_API_BASE_URL=... \
GOATOS_TENANT_ID=... \
GOATOS_BEARER_TOKEN=... \
node tools/dashboard-automation/run-dashboard-automation.mjs --mode production-smoke
```

The runner writes receipts under `.codex-goatos-render/dashboard-automation/<run>/receipt.json`.
Receipts redact bearer tokens, JWT-looking strings, and Anthropic key material.

## Anthropic or agent review

Agent review is opt-in:

```sh
GOATOS_DASHBOARD_AGENT_REVIEW=1 ANTHROPIC_API_KEY=... \
node tools/dashboard-automation/run-dashboard-automation.mjs --mode production-smoke --with-agent
```

The repo runner records the policy and budget caps only. The actual OCI host adapter must enforce the
same USD 1 per-run and USD 25 monthly caps before making model calls. Missing `ANTHROPIC_API_KEY`
produces `agent_requested_without_key`; it must not turn a deterministic failure green.

## Free-tier guard

The preflight refuses to start a non-dry run when filesystem headroom is below 20 GB, when OCI
classification is unknown, or when OCI classification signals a non-free resource. The runner must not
create volumes, backups, public load balancers, paid resizes, or paid services.

## Route and future-surface guard

`make dashboard-automation-guard` compares `apps/admin-web/app/(admin)/**/page.tsx` against the
deterministic smoke inventory. Adding a new admin route without smoke coverage fails the guard.

This is the future-proof part: the guard watches for new pages, not today's route count.
