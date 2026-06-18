# Local Full-Stack Rehearsal

Use this runbook to rehearse Phase 1 Goat Passport locally, without Goat OS
GCP, Cloud SQL, Sheets API writes, or production/staging resources.

Current source-of-truth rule:

```text
Legacy BigQuery is the source for current-data reconciliation and backfill.
Private local XLSX workbook exports are not source of truth anymore.
```

The local XLSX importer remains in the repo as a parser/regression harness for
the original Shape-2 RFID import path. Use it only for synthetic fixtures or
temporary BQ-derived rehearsal exports. Do not use a private local workbook copy
as the authoritative source for current Phase 1 corrections.

The local path is:

```text
legacy BigQuery read-only export/reconciliation
  -> local Docker Postgres
  -> bounded local import/reconciliation tooling
  -> identity counter rebuild
  -> backend APIs
  -> admin-web build/client smoke
```

For current location correction, use the legacy dashboard BQ taxonomy. CBE and
CPT remain park-scope locations because old-tag identity uses `park:CBE` and
`park:CPT`; BQ shed labels are seeded as child `shed` locations under those
parks. Apply shed correction only for deterministic BQ-matched goats whose BQ
current shed is present in that dashboard taxonomy. Do not derive or invent
Holding Farm shed rows from aggregate dashboard counts.

## Safety Rules

- Do not commit private workbooks, rows, screenshots, RFID values, old tags,
  names, media URLs, local absolute paths, or PII.
- Run local DB-writing commands only with `GOATOS_ENV=local` or
  `GOATOS_ENV=dev`.
- Local DB-writing commands reject production/staging-looking database URLs,
  remote DB hosts, and Cloud SQL Unix sockets.
- Frontend code must read Goat OS backend APIs only. It must not read Google
  Sheets, Apps Script, CSV exports, BigQuery, or XLSX files directly.
- Backend/local operator reconciliation may read legacy BigQuery in read-only
  mode for current herd truth. Generated local exports and reports must stay
  under ignored `.codex-goatos-render/` paths.
- Do not use private local XLSX workbook exports as current source truth. If the
  legacy XLSX parser is exercised, use synthetic fixtures or a temporary
  BQ-derived rehearsal export.

## Source Shapes

Shape 2 is importable by the Phase 1 RFID importer:

```text
Farm, Old ID, Old ID Suffix, RFID, Age, Gender, Breed, Tag, Shed, Partition
```

Shape 1 is recognized but not importable until a mapping extension is written:

```text
Farm, Origin Farm, Old Tag ID, RFID, Breed, Gender, Shed, Shed Tag, Age
```

Operational counting/feed/health/death/shifting/dashboard sheets are not direct
frontend inputs. For reconciliation, use their legacy BigQuery outputs rather
than local workbook copies.

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

## Live Legacy Replay Gate

Use this before mutating `goatos-dev` whenever the question is "will the import
recipe port the full legacy herd deterministically?" It starts a new throwaway
Docker Postgres database, exports the current live upstream sources through
Google APIs, applies migrations, runs the complete replay recipe, rebuilds
counters, and fails unless the final active-goat count matches the live legacy
dashboard count for the latest available date:

```bash
make replay-live
```

The replay reads live upstreams only:

```text
RFID source of truth Sheet ID: 1FulMrlb8_AGwL5nFwoORACnbDstSFMg-GCPaKIZnnF8
Census DB Sheet ID:          1tye3uknlVMoPIiYk2pdkwy9PLQwo5m8wdFIsc7yI5Ho
Goats DB Sheet ID:           1R648AutCSXS247DZb7dc07dDgyue6_R4mZDd93oW3M8
BigQuery project/location:   goatos-sheets / US
```

The replay writes timestamped exports and reports under ignored
`.codex-goatos-render/replay-live/` for audit, but those files are outputs, not
inputs. It does not use the normal local DB and does not mutate Google Cloud. By
default the container is deleted when the run exits. To inspect a failed replay
DB:

```bash
GOATOS_REPLAY_KEEP_DB=1 make replay-live
```

The RFID workbook is live-exported from Drive. Because that sheet is
append-edited by operators, the replay imports the canonical farm blocks and
exports reopened append-block rows to a review CSV instead of silently creating
extra passports that the legacy dashboard does not count.

### What The Live Replay Gate Proves

`make replay-live` has three independent gates:

1. Active-count parity: the throwaway replay DB must match the live legacy
   dashboard active-goat count for the latest reported legacy date.
2. Goat-level parity: when the local oracle container
   `goatos-local-current-persist` is running, the throwaway replay DB is compared
   against that oracle and must have zero missing goats, zero extra goats, and
   zero changed matched goats.
3. Same-live idempotency: after the full replay succeeds, the harness reruns the
   same live RFID/BQ/old-tag sync recipe against the already-loaded replay DB.
   That rerun must stage zero new RFID rows, plan/apply zero BQ patches,
   regenerate zero old-tag candidates, create/repair zero goats, keep counts
   unchanged, and keep goat-level oracle parity clean.

The second gate is deliberate. A replay can produce the same active count while
still containing the wrong goats or wrong lifecycle facts. The oracle comparison
exports both databases to normalized TSV snapshots and matches goats by active
RFID and scoped old-tag identity tokens. For matched goats, the comparator checks
these fields:

```text
lifecycle_status
park
breed
sex
age_band
identity_state
```

The generated report is written to:

```text
.codex-goatos-render/replay-live/<timestamp>/oracle-compare/
```

with:

```text
missing_in_replay.csv
extra_in_replay.csv
changed_common_keys.csv
summary.json
```

The replay summary includes both booleans:

```json
{
  "active_count_parity": true,
  "goat_level_parity": true,
  "same_live_idempotency_parity": true,
  "parity": true
}
```

`parity` is true only when the active count matches, the same-live rerun is
idempotent, and, if the oracle check ran, the goat-level comparison is also
clean. This was tightened after the first live replay work: the script used to
fail only on the active count, which allowed goat-level drift such as missing,
extra, or changed goats to be reported but not block the run. Current behavior
fails closed on goat-level drift or replay rerun drift.

Do not reload `goatos-dev` from legacy inputs unless this live replay gate
passes first.

## Legacy XLSX Parser Harness

This section is retained only for parser regression or a temporary BQ-derived
rehearsal export. It is not the current source-of-truth path. Choose the source
tab explicitly:

```bash
cd backend
go run ./cmd/rfid-import \
  --discover \
  --source-type local_xlsx \
  --input <synthetic-or-bq-derived-export.xlsx> \
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
  --input <synthetic-or-bq-derived-export.xlsx> \
  --sheet Combined \
  --tenant-id <tenant_uuid> \
  --dry-run
```

It must not insert `legacy_import_rows` and must not mutate canonical goats,
identifiers, events, outbox, counters, conflicts, candidates, or corrections.

## Stage

Stage the synthetic or BQ-derived XLSX harness input:

```bash
go run ./cmd/rfid-import \
  --input <synthetic-or-bq-derived-export.xlsx> \
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

Write the final sanitized local review report after the apply steps:

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
- `reviewer-<import_run_id>/blank-old-tag-suffix.csv`
- `reviewer-<import_run_id>/blank-gender.csv`
- `reviewer-<import_run_id>/duplicate-old-tag-same-scope.csv`
- `reviewer-<import_run_id>/README.txt`

Grouped summaries use only safe source labels (`Tag`, `Breed`, `Gender`,
`Farm`, `Shed`, and `Partition`) and never write raw RFID, old-tag, full row
JSON, or full `raw_payload`.

Before migration 000015, the `species_or_breed_requires_review` source label was
`Anantapur Sheep`. That was corrected: `Anantapur Sheep` is a Mesha source
`Breed` value and should create passports when all other apply gates pass.
Current reviewer packs should only use `breed/species-needs-classification.csv`
for truly blank/missing Breed or a label the business explicitly marks as
non-passport/non-goat.
`blank_old_tag_suffix` rows show safe Farm/Shed/Partition context. RFID-only goat
creation for those rows is available only through the explicit
`rfid-apply --allow-rfid-only-blank-suffix` flag after a dry-run confirms the
redistribution. `blank_gender` rows can now be filled from deterministic BQ RFID
evidence by `bq-reconcile --import-run-id` and re-applied through normal
`rfid-apply`; `duplicate_old_tag_same_scope` rows remain source correction or
explicit reviewed-policy work.

The reviewer CSVs are export-only. `reviewer_action` and `reviewer_notes` are
scratch columns for the data team; Goat OS does not ingest edited review CSVs
yet. Corrections must re-enter through a future approved correction overlay or
BQ-backed reconciliation path, and then normal apply/rebuild steps must be
rerun.

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

## Replay BQ Lifecycle And Location Reconciliation

Use this after the RFID parser-harness import/apply path when validating the
current BQ-backed state. The required input is a read-only export of the legacy
BQ event rows needed for lifecycle reconciliation. It can be JSON array or JSONL
and must include the fields used by the command: `goat_id`, `farm_goat_id`,
`farm`, `event`, and `date`. Current-location replay should also pass the
prepared latest-location export with `--locations-json`; those rows must include
`goat_id`, `farm`, `date`, and either `current_shed` or `dst_shed`. The `date`
field must be canonical `YYYY-MM-DD`; `bq-reconcile` rejects blank or non-ISO
dates before planning so lifecycle ordering cannot depend on ambiguous strings.
Do not commit either export.

Dry-run first:

```bash
export GOATOS_ENV=local
export DATABASE_URL=<local Docker Postgres URL>

(cd backend && go run ./cmd/bq-reconcile \
  --tenant-id <tenant_uuid> \
  --import-run-id <import_run_id> \
  --events-json <ignored-bq-event-export.json> \
  --locations-json <ignored-current-location-export.json>)
```

Apply only after the dry-run matches the expected correction shape:

```bash
(cd backend && go run ./cmd/bq-reconcile \
  --tenant-id <tenant_uuid> \
  --import-run-id <import_run_id> \
  --events-json <ignored-bq-event-export.json> \
  --locations-json <ignored-current-location-export.json> \
  --execute)
```

Then rebuild counters before trusting API/dashboard reads:

```bash
(cd backend && go run ./cmd/rebuild-identity-counters \
  --tenant-id <tenant_uuid> \
  --source-import-run-id <import_run_id>)
```

`bq-reconcile` is idempotent and audited. It updates deterministic
RFID/scoped-old-tag matches for lifecycle and current location. It fills blank
local attributes from deterministic BQ evidence and normalizes same-meaning breed
labels, but nonblank BQ/passport sex or breed disagreements become Data Quality
review conflicts rather than silent overwrites.
When `--import-run-id` is supplied, it can also fill sole-reason `blank_gender`
staging rows from deterministic BQ RFID evidence and requeue those rows for the
normal `rfid-apply` path. It does not create goats directly, add identifiers, or
emit outbox events.
BQ Shifting evidence without terminal Sale/Death is proof-of-life and should
keep the goat `alive`. Death remains terminal. Sale is reversed only by a later
Purchase; later non-purchase activity such as Shifting, Birth, or Abortion after
Sale or Death opens a Data Quality `status_mismatch` conflict instead of
silently reviving the goat. Unmatched local goats stay
untouched until identifier reconciliation/backfill work supplies stronger
evidence.

## Real Shape 2 Closeout Expectations

For the historical Shape 2 parser harness and migrations through 000015, a
fresh local run before BQ reconciliation must reconcile exactly:

```text
normal rfid-apply:
  created_goat = 780
  needs_review = 443
  error = 0

rfid-apply --allow-rfid-only-blank-suffix:
  created_goat = 1215
  needs_review = 8
  error = 0

remaining actionable review reason occurrences:
  blank_gender = 4
  duplicate_old_tag_same_scope = 4

source Breed policy proof:
  Anantapur Sheep source rows = 508
  Anantapur Sheep created passports = 504

rebuild-identity-counters:
  tenant_lifecycle alive = 1215
```

After the BQ reconciliation pass, run `rfid-apply --allow-rfid-only-blank-suffix`
again if blank-gender rows were requeued, then rebuild counters. Current proof
settles at 1219 passports while leaving only duplicate-old-tag rows in Import
Review:

```text
import rows:
  created_goat = 1219
  needs_review = 4
  error = 0
  remaining review reason = duplicate_old_tag_same_scope 4

created passports:
  total = 1219
  no current BQ-derived location = 272
  shed-level current location = 891
  park-only current location = 56

tenant_lifecycle after counter rebuild:
  alive = 1113
  sold = 71
  dead = 35
  inactive = 0

Data Quality after BQ reconciliation:
  status_mismatch conflicts = 54
  deterministic BQ patches planned = 0
```

If these numbers differ on a fresh local database, stop and investigate before
using the result as a Phase 1 proof. Usual causes are wrong BQ export/table,
changed legacy data, stale database, missing migration, skipping the explicit
RFID-only blank-suffix apply flag for parser-harness runs, or accidentally using
a pre-000015 database. Do not classify BQ Shifting-only goats as inactive:
Shifting without terminal Sale/Death is proof-of-life. Do not expect the
historical 711/512 split; nonblank source Breed rows should move into created
passports instead of species/breed
review. Unknown nonblank source labels are cataloged in review status rather
than silently promoted to trusted active breeds.

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

For day-to-day local UI development, use the one-command local stack. It starts
or reuses the local API, waits for readiness, seeds the local `ceo_internal`
grant idempotently, mints a fresh server-side bearer token, and starts admin-web
on `127.0.0.1:3300`:

```bash
cd <goatos-repo>
make dev-local
```

Use this path instead of reusing an old `GOATOS_BEARER_TOKEN` from a shell. If
the browser shows `401 invalid_bearer_token`, restart through `make dev-local`
so the backend and admin-web share the same local auth issuer/audience/secret.

For day-to-day local use where the browser should keep working after terminal
tabs close, install the macOS user LaunchAgent:

```bash
cd <goatos-repo>
make dev-local-service-install
```

This still uses the local Docker Postgres database for data. The LaunchAgent
only supervises the host API/admin-web processes (`127.0.0.1:8080` and
`127.0.0.1:3300`) and restarts the stack if either process exits or fails health
checks.

```bash
make dev-local-service-status
make dev-local-service-restart
make dev-local-service-stop
make dev-local-service-logs
```

For the real local post-000015 proof, pass the proven import run id so
`/import-review` opens the live run directly:

```bash
GOATOS_IMPORT_RUN_ID=<import_run_id> make dev-local
```

In another terminal, capture the live browser proof:

```bash
cd apps/admin-web
npm run smoke:visual:live
```

The smoke captures `/`, `/counts`, `/herd`, `/import-review`,
`/data-quality`, and one real `/goats/<goat_id>` at desktop and narrow
viewports under ignored `.codex-goatos-render/admin-web-screenshots/` paths. It
must not be wired into CI because it requires local private proof data.

## Repeatable Fresh Export Loop

This is a repeatable import loop, not a parallel state machine:

```text
same source_row_key + same source_row_version_hash -> idempotent replay/no duplicate canonical writes
same source_row_key + different source_row_version_hash -> needs_review/source_row_changed
new source_row_key -> new staged source row
```

Re-export a fresh XLSX when the source changes, rerun discovery, then stage and
apply through the same pipeline.
