# Local Full-Stack Rehearsal

Use this runbook to rehearse Phase 1 Goat Passport locally, without GCP or
Cloud SQL.

The local path is:

```text
local XLSX export
  -> local Docker Postgres
  -> source discovery
  -> RFID import staging
  -> RFID apply
  -> final sanitized review report
  -> identity counter rebuild
  -> backend APIs
  -> admin-web build/client smoke
```

## Safety Rules

- Do not commit private workbooks, rows, screenshots, RFID values, old tags,
  names, media URLs, local absolute paths, or PII.
- Run local DB-writing commands only with `GOATOS_ENV=local` or
  `GOATOS_ENV=dev`.
- Local DB-writing commands reject production/staging-looking database URLs,
  remote DB hosts, and Cloud SQL Unix sockets.
- Frontend code must read Goat OS backend APIs only. It must not read Google
  Sheets, Apps Script, CSV exports, BigQuery, or XLSX files directly.
- Google Sheets live export/import is deferred. If a fresh source is needed,
  export the sheet to a local `.xlsx` file and run the local XLSX flow.

## Source Shapes

Shape 2 is importable by the Phase 1 RFID importer:

```text
Farm, Old ID, Old ID Suffix, RFID, Age, Gender, Breed, Tag, Shed, Partition
```

Shape 1 is recognized but not importable until a mapping extension is written:

```text
Farm, Origin Farm, Old Tag ID, RFID, Breed, Gender, Shed, Shed Tag, Age
```

Operational counting/feed/health/death/shifting/dashboard sheets are not Phase
1 identity source inputs and must be rejected before staging.

## Environment

Start from a migrated local Docker Postgres database and set:

```bash
export GOATOS_ENV=local
export DATABASE_URL='postgres://postgres:goatos@127.0.0.1:<port>/goatos?sslmode=disable'
```

Use the Docker storage runbook before large tests:

```text
docs/runbooks/local-docker-storage.md
```

## Discover Source

For a local XLSX export, choose the source tab explicitly:

```bash
cd backend
go run ./cmd/rfid-import \
  --discover \
  --source-type local_xlsx \
  --input <local-export.xlsx> \
  --sheet Combined
```

Expected importable output includes:

```text
classification=importable_shape_2 importable=true
```

Google Sheet live export is intentionally skipped in this phase:

```bash
go run ./cmd/rfid-import --discover --source-type google_sheet
```

Expected output includes:

```text
classification=google_sheet_export_skipped google_sheet_export_built=false
```

## Dry Run

Dry run records only the run row and aggregate counts:

```bash
go run ./cmd/rfid-import \
  --input <local-export.xlsx> \
  --sheet Combined \
  --tenant-id <tenant_uuid> \
  --dry-run
```

It must not insert `legacy_import_rows` and must not mutate canonical goats,
identifiers, events, outbox, counters, conflicts, candidates, or corrections.

## Stage

Stage the local XLSX:

```bash
go run ./cmd/rfid-import \
  --input <local-export.xlsx> \
  --sheet Combined \
  --tenant-id <tenant_uuid>
```

The CLI prints `import_run_id=<uuid>`.

## Apply

Apply clean staged rows:

```bash
go run ./cmd/rfid-apply \
  --tenant-id <tenant_uuid> \
  --import-run-id <import_run_id> \
  --actor-id <actor_uuid>
```

## Final Review Report

Write the final sanitized local review report after `rfid-apply`:

```bash
go run ./cmd/rfid-import \
  --anomaly-report \
  --tenant-id <tenant_uuid> \
  --import-run-id <import_run_id> \
  --sheet Combined \
  --output-dir .codex-goatos-render/import-reports
```

The report uses actual emitted reason codes from `legacy_import_rows.error_reason`
and `normalized_payload.processing_reasons`, including apply-stage review reasons
such as `unknown_status_mapping` and `species_or_breed_requires_review`. It
writes row details, reason-code summary, and grouped review-summary CSVs.
Grouped summaries use only safe source labels (`Tag`, `Breed`, `Gender`,
`Farm`, `Shed`, and `Partition`) and never write raw RFID, old-tag, full row
JSON, or full `raw_payload`.

`species_or_breed_requires_review` groups are a human gate for deciding whether a
label should become a breed alias or an exclusion; the report does not
auto-alias. `blank_old_tag_suffix` groups show safe Farm/Shed/Partition context;
RFID-only goat creation for those rows is a future policy decision, not part of
this rehearsal slice.

The report masks RFID and old-tag values by default and writes only to ignored
local output paths. Do not use `--include-sensitive` unless the output stays
local and uncommitted; grouped summaries remain aggregate/safe-label only.

## Rebuild Counters

Rebuild Phase 1 identity counters:

```bash
go run ./cmd/rebuild-identity-counters \
  --tenant-id <tenant_uuid> \
  --source-import-run-id <import_run_id>
```

## Backend And Admin-Web Smoke

The integrated local smoke script exercises the full local path with synthetic
fixtures:

```bash
backend/tests/integration/smoke-auth-local.sh
```

It starts Docker Postgres, applies migrations, discovers the synthetic Shape 2
fixture, verifies Google Sheet skip behavior, dry-runs, stages, applies clean
rows, writes the final masked review report, rebuilds counters, starts the API,
checks authenticated API access, and typechecks/builds admin-web.

## Repeatable Fresh Export Loop

This is a repeatable import loop, not a parallel state machine:

```text
same source_row_key + same source_row_version_hash -> idempotent replay/no duplicate canonical writes
same source_row_key + different source_row_version_hash -> needs_review/source_row_changed
new source_row_key -> new staged source row
```

Re-export a fresh XLSX when the source changes, rerun discovery, then stage and
apply through the same pipeline.
