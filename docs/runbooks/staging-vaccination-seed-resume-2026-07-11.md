# Staging Vaccination Seed Resume Handoff - 2026-07-11

This is the compact-safe handoff for resuming the `goatos-stg` vaccination seed.
Read this before continuing staging writes. The goal is a clean, source-based
staging seed, not a copy of local DB state.

## Current Decision

- Do not copy local database rows into staging.
- Do not use local Action Center, SOP, outbox, projection, or HRMS rows as
  staging truth.
- Resume staging from source files and staging-safe seed commands only.
- Re-verify all cloud and database state before any write. Treat the state below
  as last observed, not permanent truth.

## Last Observed Staging State

Last observed target:

- GCP account: `ravi@mesha.sg`
- Project: `goatos-stg`
- Cloud SQL instance: `goatos-stg:asia-south1:goatos-stg-core-db`
- Region: `asia-south1`
- Tenant: `00000000-0000-4000-8000-000000000001`

Last observed DB state after reset and migrations:

- Schema was reset from scratch with `DROP SCHEMA public CASCADE` and recreated.
- Migrations applied through `000158_vaccination_capacity_default_buffer_7`.
- `goats = 0`
- `goat_identifiers = 0`
- `protocol_versions = 0`
- `protocol_rules = 0`
- `vaccination_eligibility_rollups = 0`
- `vaccination_capacity_config = 1` with `max_per_day = 100`,
  `max_buffer_days = 7`, tenant scope.
- Migration/demo baseline rows existed:
  - `sop_versions = 3`
  - `outbox_messages = 2`, failed

Before seed, re-check these. If the baseline SOP/outbox rows still exist, clean
only those known migration/demo rows. Do not delete source-seeded rows after the
real seed starts.

## Source Data Manifest

Canonical local source bundle:

```text
/Users/ravi/mesha/source-material/vgoats-seed/
```

Vaccination/goat source:

- `/Users/ravi/mesha/source-material/vgoats-seed/goats.json`
- `/Users/ravi/mesha/source-material/vgoats-seed/vaccination.json`
- `/Users/ravi/Downloads/Demo DB.xlsx`
- `/Users/ravi/Downloads/Vaccination_DB_-V2.xlsx`
- `/Users/ravi/Downloads/RFID source of truth.xlsx`

Verified vaccination identity rules:

- V2/Demo has 1,311 animal rows.
- V2/Demo has 1,310 unique nonblank primary IDs.
- V2 and source truth have equal RFID counts but 6 old-ID records differed by
  RFID before source truth was corrected. The group confirmed to use V2 RFIDs.
- One row has blank primary RFID but has secondary identity: `CPT / 1388 / BLR /
  K2 / Yashoda`. Seed it as a real identifier fallback, not as missing ID.
- UI labels are `Display ID`, `Tag 1`, and `Tag 2`.
- Missing tag values render `-`. No `missing ID` chip anywhere.

HRMS/roster source:

- `/Users/ravi/mesha/source-material/vgoats-seed/roster-name-mapping.jun26-review.csv`
  - 35 lines including header.
  - Maps timetable names to reviewed people/designations.
- `/Users/ravi/mesha/source-material/vgoats-seed/attendance-jun-26.json`
- `/Users/ravi/mesha/source-material/vgoats-seed/timetable-goats-team-v1.json`
  - Contains Health/Kidding AM and manager roles by center.
  - Includes CBE/CPT health/kidding people such as Bittu, Munna Kumar, Funtos,
    Sittu Kumar, Sagar, Sharath, Darshan, and backup manager slots.
- `/Users/ravi/mesha/source-material/vgoats-seed/timetable-farm-goat-managers-v1.json`
  - Has schedule-like goat-manager text, not a complete reviewed shed-manager map.
- `/Users/ravi/mesha/source-material/vgoats-seed/timetable-head-backup-manager.json`
  - Invalid source file at last check. It contains a Google 404 HTML response,
    not JSON.

Reviewed HRMS shed ownership seed:

- `/Users/ravi/mesha/source-material/vgoats-seed/shed-manager-mapping.jul11-vaccination.csv`
  is the reviewed staging seed artifact for vaccination shed ownership.
- It is generated from the source `goats.json` shed list plus reviewed roster
  rows in `roster-name-mapping.jun26-review.csv`. It is not copied from the
  local database.
- Validation on creation:
  - `116` shed rows.
  - `72` CBE sheds and `44` CPT sheds.
  - `116` source goat sheds covered.
  - `1,311` source goats covered by those sheds.
  - `0` missing source sheds.
  - `0` extra seed sheds.
  - `0` blank manager or backup values.
  - `0` rows failing the reviewed-source contract.
- Reviewed assignments:
  - CBE sheds -> manager `Eshwar` (`HRMS-MANUAL-9030`), backup `Saheb mete`
    (`HRMS-JUN26-020`).
  - CPT sheds -> manager `Darshan Talwar` (`HRMS-JUN26-013`), backup
    `Amit Kumar` (`HRMS-JUN26-016`).
- Required columns include `shed_code`, `shed_name`, `park_code`,
  `manager_code`, `manager_name`, `assignment_source`, `source_ref`,
  `confidence`, `needs_review`, `backup_manager_code`, and
  `backup_manager_name`.
- For staging, every row must keep `assignment_source = reviewed`,
  `needs_review = false`, and a non-provisional `source_ref`.
- The CSV is an override, not the only way a shed may get a vaccination owner.
  When a refreshed goat source introduces extra active sheds before this CSV is
  regenerated, `seed-shed-positions` must materialize those shed manager seats
  from the reviewed park `preventive_care_manager` position (`Eshwar` for CBE,
  `Darshan Talwar` for CPT) and the park Backup Manager. If a code present in
  the CSV does not resolve, or if a park has no reviewed preventive-care manager
  or backup-manager position, stop. Never accept `Manager: unassigned` as a
  normal vaccination board state.

## Seed Command Status

Known seed commands and staging readiness:

- `backend/cmd/seed-vaccination-real`
  - Source: `goats.json`, `vaccination.json`.
  - Seeds goats, identifiers, vaccine matrix/protocol, obligations/history.
  - Source vaccination date cells are last-administered/done dates. When a
    matching active rule exists, seed them as accepted history with
    `administered_at = source date`, then derive future work from that date
    after current eligibility, defer, pregnancy/lactation, warm-up, gap,
    inventory, ownership, and capacity constraints. When a matching rule is
    missing, preserve the old date as visible history/config-gap evidence; do
    not invent a rule or completion.
  - Currently guarded for local/dev/test; needs a narrow staging Cloud SQL guard
    before `GOATOS_ENV=stg`.
- `backend/cmd/seed-roster-real`
  - Source: reviewed roster mapping, attendance, timetable source files.
  - Seeds workforce members/positions from source data.
  - Currently guarded for local/dev/test; needs a narrow staging Cloud SQL guard
    before `GOATOS_ENV=stg`.
- `backend/cmd/seed-shed-positions`
  - Source: explicit reviewed shed-to-manager mapping CSV.
  - `-generate-provisional` writes a review CSV only; it does not write DB rows.
  - For staging use `-mapping <reviewed.csv> -strict`.
  - Currently guarded for local/dev/test; needs a narrow staging Cloud SQL guard
    before `GOATOS_ENV=stg`.
- `backend/cmd/seed-position-duties`
  - Derives duty coverage after workforce positions exist.
  - Currently guarded for local/dev/test; needs a narrow staging Cloud SQL guard
    before `GOATOS_ENV=stg`.
- `backend/cmd/seed-dev-email-grants`
  - Already has staging target validation.
  - Use with `GOATOS_ENV=stg` for the five founder/builder accounts.
- `backend/cmd/vaccination-eligibility-rollup-recompute`
  - Must run after goat/vaccination seed.
  - Populates `vaccination_eligibility_rollups` for cheap config preview.
- `backend/cmd/process-integrity-projection-recompute`
  - Must run after goat/vaccination seed and after sweeper changes.
  - Populates Action Center, Control Tower, Protocol Adherence, Workflow, and
    grouped sidebar-count read models.
- `backend/cmd/vaccination-shed-projection-recompute`
  - Must run after goat/vaccination seed and after sweeper changes.
  - Populates the shed-wise `/vaccination` read model.
- `backend/cmd/vaccination-execution-projection-recompute`
  - Must run after goat/vaccination seed and after sweeper changes.
  - Populates `/vaccination/execution`.
- `backend/cmd/vaccination-operations-projection-recompute`
  - Must run after goat/vaccination seed and after sweeper changes.
  - Populates `/vaccination/operations`.

## Problem Statements Captured

These issues happened during local/staging rehearsal and must not be reproduced
in staging:

- Local was confused with staging. Staging must be source-based from scratch.
- Local had old SOP authoring rows visible in business UI. Staging should show
  one intended vaccination SOP in the default UI.
- Local showed multiple vaccination protocol/config rows. Staging should show one
  visible active `vaccination.matrix / V1 Real Vaccination`.
- Local had outbox failed/pending rows and invalid envelopes. Staging should not
  start with old failed outbox rows.
- Local Action Center and PC badges showed unexplained counts such as `661`.
  Staging counts must come from backend grouped open-work queries after seed.
- Local showed `Manager: unassigned` and owner-missing states. Staging must not.
- Local HRMS visibility was incomplete for the five founder/builder users. The
  five founder accounts must see every built visible module.
- Local initially labeled RFID/old IDs incorrectly and rendered `missing ID`.
  Staging UI must use `Display ID`, `Tag 1`, `Tag 2`.
- Display ID is internal and short. It must not be the RFID. RFID/legacy IDs are
  real-world tags.
- The main vaccination page must be shed-wise, not vaccine-wise/cohort-wise.
- The word `partition` is not a business concept for staging UI.
- Date range controls should stay hidden until backend-owned range semantics
  exist.
- Capacity belongs to the versioned vaccination rule publish flow. `max buffer
  days = 7` for staging unless Ravi changes the business rule.
- Config preview must be cheap. It should use rollup/read-model counts, not scan
  animal rows from the UI request.
- Audit Log and DLQ Center should not be demo decoration. If visible, they must
  be backed by real audit/outbox data and show clean initial state.

## Founder/Builder Access Rule

The following accounts are platform/founder/builder accounts:

- `ravi@mesha.sg`
- `manohark@mesha.sg`
- `manju@mesha.sg`
- `abhishek@mesha.sg`
- `aryaman@mesha.sg`

Strict rule:

- They are tenant-scoped `ceo_internal`.
- They must see every built visible module at all times, including new features
  and changed existing features.
- Department-scoped hiding is only for later operational users, not these five.

## Safe Resume Sequence

1. Verify target before writes:
   - account `ravi@mesha.sg`
   - project `goatos-stg`
   - Cloud SQL instance in `asia-south1`
   - tenant `00000000-0000-4000-8000-000000000001`
2. Start Cloud SQL Auth Proxy for `goatos-stg`.
3. Read-only staging probe:
   - migration version
   - required tables exist
   - goats/protocols/obligations are still empty
   - list any SOP/outbox baseline rows
4. Clean only known migration/demo baseline rows:
   - old/default SOP rows
   - failed outbox rows created by migrations/demo bootstrap
   - never delete real seed rows after source seed starts
5. Patch seed command target guards narrowly for `GOATOS_ENV=stg` and staging
   Cloud SQL only. Do not broaden to arbitrary Postgres hosts.
6. Seed founder/builder grants.
7. Seed HRMS roster source:
   - members
   - center/role positions
   - backup slots
8. Apply reviewed shed ownership:
   `seed-shed-positions -mapping /Users/ravi/mesha/source-material/vgoats-seed/shed-manager-mapping.jul11-vaccination.csv -strict`.
   Stop if any shed code, manager code, backup code, or reviewed-source
   validation fails.
9. Seed position duties after center/role and shed-scoped positions exist.
10. Seed vaccination/goats from `goats.json` and `vaccination.json`.
11. Run eligibility rollup recompute.
12. Run projection rebuilds from the seeded source state:
    - process-integrity projection
    - vaccination shed projection
    - vaccination execution projection
    - vaccination operations projection
    - calendar vaccination projection
    - counts projection, if the Counts surface is visible in that environment
13. Run or trigger outbox processing required for Action Center, Calendar,
    Workflow, and sidebar counts.
14. Verify final green gates:
    - 1,311 source animal rows represented.
    - 1,310 nonblank primary IDs plus known secondary fallback row.
    - no missing-ID chip condition.
    - one visible active vaccination matrix.
    - one intended vaccination SOP in default UI.
    - no failed outbox rows.
    - no unexplained pending outbox rows.
    - no `Manager: unassigned`, no owner missing, no blank backup.
    - founder accounts see all visible modules.
    - Action Center/PC/Vaccination counts reconcile.
    - past source vaccination dates are visible as history/done anchors or
      explicit config/review gaps; none are silently dropped because a rule was
      missing at seed time.
    - `vaccination_eligibility_rollups` has rows.
    - `vaccination_shed_projection_state`,
      `vaccination_execution_projection_state`,
      `vaccination_operations_projection_state`, and
      `process_integrity_projection_state` have a serving version built after
      the final seed write.
    - `GET /vaccination/sheds`, `GET /vaccination/execution`, and
      `GET /vaccination/operations` return `200`, not
      `projection_unavailable`.
    - capacity config/rule uses buffer 7.
15. Stop the Cloud SQL proxy when finished.

## Stop Conditions

Stop and report instead of seeding if any of these is true:

- The active GCP project is not `goatos-stg`.
- The Cloud SQL target is not the `goatos-stg` instance in `asia-south1`.
- Source files are missing or changed without revalidation.
- Staging already contains non-empty goat/protocol/obligation data not created by
  the current seed attempt.
- The reviewed shed-to-manager mapping CSV is missing, has coverage drift, or
  has unresolved shed/manager/backup codes.
- A seed command would require copying local DB rows.
- A seed command would create owner-missing or manager-unassigned states.
- A migration or seed would introduce `e2e`, `story`, `test`, `dummy`, `stub`,
  or `local` rows into shared staging business data.
