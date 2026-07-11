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
