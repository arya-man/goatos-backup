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

## Stale local API/admin-web processes

Use `make dev-local-service-restart` as the recovery button whenever a local
browser looks inconsistent with the current checkout. The restart path now:

1. stops the launch agent,
2. stops orphan `run-local-stack-supervised.sh` supervisors,
3. frees `:8080` and `:3300`,
4. starts API/admin-web from the current repo, and
5. prints the repo path, commit, and port-owner cwd in
   `make dev-local-service-status`.

Do not diagnose vaccination/calendar/protocol UI behavior against an old
manual `go run` or `next dev` process. If `status` says a listener is `not
current repo`, run `make dev-local-service-restart` before testing.

The supervised stack also refuses to silently reuse an already-healthy API or
admin-web process. This is intentional: a healthy old process can serve stale
code against a migrated/current local DB and create false UI bugs. The guard is
enforced by `make local-stack-service-guard`, registered in the guardrail
manifest, and required by standard `make ci-local` for both Claude and Codex.

## Shared stack contract

The browser-visible local environment is one indivisible appliance:

```text
admin-web  http://127.0.0.1:3300  clean exact origin/main checkout
API        http://127.0.0.1:8080  the same exact origin/main checkout
database   127.0.0.1:5433/goatos  container: goatos-local-current
```

Install or recover it from the dedicated clean serving checkout:

```bash
make dev-local-service-install
make dev-local-service-status
```

The persistent supervisor fetches `origin/main` with the authenticated GitHub
CLI credential, permits only a clean fast-forward, re-executes the updated
supervisor before any migration/closeout, and passes that proof to FE and BE. It
checks `origin/main` while running. If main advances or cannot be verified, it
stops both services; launchd restarts the supervisor, fast-forwards, prepares the
canonical DB, and starts the pair together. The LaunchAgent PATH includes
Homebrew `libpq` so `psql`-backed closeout cannot create a boot loop.

The shared supervisor deliberately discards ambient `DATABASE_URL`,
`GOATOS_E2E_DATABASE_URL`, custom API base URLs, and custom shared ports. This
prevents an agent shell or another test task from silently wiring the frontend
to a different backend/database. A failed frontend start reaps the API process
tree and owned listeners before retrying, so a half-old/half-new pair cannot
survive.

## Isolated E2E boundary

An isolated E2E stack is a separate appliance. It must use non-shared FE/BE
ports and an explicit throwaway database. For example, API `:18080` and a
throwaway Postgres port are not part of the shared service and are not stale
shared-process candidates merely because they are Goat OS processes.

Never stop, fast-forward, migrate, seed, reuse, or delete an isolated E2E stack
while repairing `:3300`/`:8080`. The service wrapper targets its exact
supervisor path plus the two shared ports only. Before any manual cleanup,
inspect `lsof` CWDs, process commands, container names, and database URLs. E2E
scripts must never claim `:3300`/`:8080` or mutate `127.0.0.1:5433/goatos`.

Verification after recovery:

```bash
git rev-parse HEAD origin/main                 # identical in serving checkout
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:3300/calendar >/dev/null
make local-stack-service-guard
```
