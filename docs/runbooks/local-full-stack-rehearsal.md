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

After the guarded blank old-tag suffix policy is approved for a local run, use
the explicit opt-in flag:

```bash
go run ./cmd/rfid-apply \
  --tenant-id <tenant_uuid> \
  --import-run-id <import_run_id> \
  --actor-id <actor_uuid> \
  --allow-rfid-only-blank-suffix
```

Default apply remains pending-only. The opt-in path creates RFID-only goats only
when `blank_old_tag_suffix` is the row's complete staged reason set and all
apply-time gates pass; it creates no `old_tag` identifier and does not derive a
scope from Farm/Shed/Partition.

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
writes the existing row details, reason-code summary, and grouped review-summary
CSVs at the report root, plus a reviewer-focused CSV pack under a per-run
`reviewer-<import_run_id>/` subdirectory:

- `reviewer-<import_run_id>/review-summary.csv`
- `reviewer-<import_run_id>/needs-review-rows.csv`
- `reviewer-<import_run_id>/non-goat-exclusion-candidates.csv`
- `reviewer-<import_run_id>/blank-old-tag-suffix.csv`
- `reviewer-<import_run_id>/blank-gender.csv`
- `reviewer-<import_run_id>/duplicate-old-tag-same-scope.csv`
- `reviewer-<import_run_id>/README.txt`

Grouped summaries use only safe source labels (`Tag`, `Breed`, `Gender`,
`Farm`, `Shed`, and `Partition`) and never write raw RFID, old-tag, full row
JSON, or full `raw_payload`.

The current `species_or_breed_requires_review` bucket is Anantapur Sheep only, so
those rows are exported to `non-goat-exclusion-candidates.csv` and should be
confirmed as non-goat exclusions, not created as goats. The tool does not emit a
duplicate breed/species issue file for those sheep rows. If a future import has a
breed/species review label that is not a confirmed non-goat label, the tool emits
`breed/species-needs-classification.csv` for that classification work.
`blank_old_tag_suffix` rows show safe Farm/Shed/Partition context. RFID-only goat
creation for those rows is available only through the explicit
`rfid-apply --allow-rfid-only-blank-suffix` flag after a dry-run confirms the
redistribution. `blank_gender` and `duplicate_old_tag_same_scope` rows remain
source correction or explicit reviewed-policy work.

The reviewer CSVs are export-only. `reviewer_action` and `reviewer_notes` are
scratch columns for the data team; Goat OS does not ingest edited review CSVs
yet. Corrections must re-enter through the source workbook, or a future approved
correction overlay, and then the normal Shape-2 import/apply flow must be rerun.

The report masks RFID and hashes old-tag/source-row references by default and
writes only to ignored local output paths. Internal cleanup usually needs
`--include-sensitive`, but use it only when the output stays local and
uncommitted; committed docs/examples must use masked or aggregate data only.

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
