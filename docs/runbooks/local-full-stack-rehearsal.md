# Local Full-Stack Rehearsal

Status: replaced for the current Admin Config + PHC Vaccination + Parks
vaccination execution slice. Calendar vaccination due-work is an approved build
target and joins this runbook when the route/nav/smoke implementation lands.

The old local rehearsal used deleted RFID/BQ/import-review tooling and must not
be followed.

Current local rehearsal:

```bash
cd /Users/ravi/mesha/goatos
make dev-local
```

Then verify:

```bash
cd /Users/ravi/mesha/goatos/backend
go test ./...

cd /Users/ravi/mesha/goatos/apps/admin-web
npm run check:mock-fidelity
npm run lint
npm run typecheck
npm run build
npm run smoke:visual:live
```

Implemented admin-web routes:

```text
/login
/
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
/config
/sops
/goats/{goat_id}
```

Approved build target, not yet part of live smoke:

```text
/calendar
```

When `/calendar` is implemented, add the page, primary nav, mock-fidelity scan
coverage, and `smoke:visual:live` route coverage in the same frontend PR.

Do not use this runbook to revive `rfid-import`, `rfid-apply`,
`bq-reconcile`, Import Review, Data Quality, Legacy Sync, old generic Counts
dashboards/reconciliation, mortality, or old dashboard parity workflows.
