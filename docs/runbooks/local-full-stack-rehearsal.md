# Local Full-Stack Rehearsal

Status: replaced for the current Admin Config + Preventive Care (PC) Vaccination + Parks
vaccination execution slice, including Calendar due-work and Operations DLQ
repair visibility.

The old local rehearsal used deleted RFID/BQ/import-review tooling and must not
be followed.

Current local rehearsal:

```bash
cd /path/to/goatos
make dev-local
```

Then verify:

```bash
cd /path/to/goatos/backend
go test ./...

cd /path/to/goatos/apps/admin-web
npm run check:mock-fidelity
npm run lint
npm run typecheck
npm run build
npm run smoke:visual:live
```

The default `smoke:visual:live` permits the calendar drive-target roster to be
absent — it logs `identity_calendar_roster=skipped_no_targets` and passes. To
hard-assert the Display ID / Tag 1 / Tag 2 identity columns on a vaccination
drive drawer, run the focused strict gate:

```bash
npm run smoke:calendar-identity:live
```

This exits non-zero **by design** in the current seed: no obligations are
generated for drive `protocol_version`s, so every drive roster is empty. It goes
green once that seed/obligation-generation gap is closed. You can also scope any
run with `GOATOS_SMOKE_ONLY_ROUTES=<name,...>` (validated against the known route
list up front).

Implemented admin-web routes:

```text
/login
/
/calendar
/vaccination
/vaccination/execution/sheds/[shedId]
/action-center
/protocol-adherence
/workflows
/workflows/{row_id}
/procurement/source-entry
/procurement/source-entry/loads/{load_id}
/counts/herd
/operations/audit
/operations/dlq
/config
/sops
/goats/{goat_id}
```

Do not use this runbook to revive `rfid-import`, `rfid-apply`,
`bq-reconcile`, Import Review, Data Quality, Legacy Sync, old generic Counts
dashboards/reconciliation, mortality, or old dashboard parity workflows.

## Stale API process vs. a migrated local DB

`tools/dev/run-local-stack.sh` (`make dev-local`) and
`tools/dev/run-local-stack-supervised.sh` (`make dev-local-service-*`) both
decide whether to reuse an already-running API on `:8080` by curling
`/readyz`: if it is already healthy, they log "Using existing Goat OS API"
and skip starting a new one. Before
`docs/decisions/stale-binary-migration-drift-guard.md`, `/readyz` only
checked that Postgres was reachable, so an `api` process left running from an
older worktree/build would still report healthy even after a different
session applied new migrations to the same local DB (e.g.
`goatos-local-current` on `:5433`, see
`docs/runbooks/google-cloud-environments.md`) — exactly the incident that
guard exists to catch.

`/readyz` now also fails when the running binary's embedded migration
ceiling and the DB's applied migration level disagree, so a stale reused
process is correctly reported unhealthy instead of silently kept alive. If a
launcher run reports the API as unhealthy/refusing to start and you have
multiple worktrees pointed at the same local DB, check
`curl -s http://127.0.0.1:8080/version` first (or the log line from a fresh
`bootstrap.NewAPI` failure) before assuming a code regression — kill the
stale process and let the launcher start a freshly built one instead of
chasing a phantom bug. Neither launcher script was modified by that guard;
this section is deliberately just a runbook note (a different session had
uncommitted edits to `run-local-stack-supervised.sh` in a different worktree
at the time).
