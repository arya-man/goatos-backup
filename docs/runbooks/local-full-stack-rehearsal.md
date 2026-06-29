# Local Full-Stack Rehearsal

Status: replaced for the current Admin Config + PHC Vaccination + Parks
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
