# Local Full-Stack Rehearsal

Status: replaced for the current Admin Config + PHC Vaccination + Parks
vaccination execution slice.

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

Active admin-web routes:

```text
/login
/
/vaccination
/vaccination/adherence
/config
/goats/{goat_id}
```

Do not use this runbook to revive `rfid-import`, `rfid-apply`,
`bq-reconcile`, Import Review, Data Quality, Legacy Sync, counts, mortality, or
old dashboard parity workflows.
