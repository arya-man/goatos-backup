# Legacy God Sheet Import Plan

Last reviewed: 2026-07-10

## Problem Statement

Mesha currently has live animal, vaccination, procurement, shifting, mortality,
sales, feed, health, weight, and dashboard data spread across Google Sheets,
BigQuery cleaned tables, legacy dashboard APIs, and Slack audit alerts. The
legacy CEO dashboard is useful, but it is not strict enough to seed Goat OS
directly because Goat OS has stronger identity, evidence, idempotency, and
vaccination-process requirements.

The immediate goal is to create a Google Sheet workbook that becomes the
temporary "god sheet" for migration and cleanup:

- Sheet URL:
  https://docs.google.com/spreadsheets/d/1QhW22Awg7WKGhYf7tCaj_pXE-9kwzPyfwhSwenDW2RM/edit?gid=0#gid=0
- The sheet accumulates canonical animal data from all relevant legacy sources.
- Rows are marked RED, AMBER, or GREEN so ground/source/data teams can clean the
  data before Goat OS import.
- GREEN rows are import-ready for Goat OS.
- RED rows block import until the evidence issue is fixed.
- AMBER rows are usable only under an explicit temporary rule, such as Animal ID
  2 being optional until the double RFID rollout is complete.

This workbook is a migration and reconciliation staging artifact. It must not
become the long-term Goat OS runtime database. Long-term writes go through Goat
OS backend APIs, not direct Sheet-to-Postgres imports.

## Verified Context

Read-only context used for this plan:

- Google account: `ravi@mesha.sg`
- Google Cloud organization: `vgoats.com` / `organizations/563962826703`
- Legacy BigQuery project used by the dashboard audit: `goatos-sheets`
- Goat OS target repo: `https://github.com/vgoats/goatos.git`
- Goat OS dev project for future importer jobs: `goatos-dev`
- Legacy dashboard API host:
  `https://dashboard--goatos-sheets.us-central1.hosted.app`

The active `gcloud` token could list BigQuery datasets/tables and read table
schemas/data. It could not mint a fresh Drive-scoped token for full "all shared
Drive spreadsheets" enumeration without re-auth. Therefore this plan is based
on:

- dashboard audit source links,
- BigQuery dataset/table inventories,
- BigQuery external table source URIs,
- Goat OS identity and vaccination contracts,
- current scheduled Slack audit output.

Future implementation must add an explicit Drive/Sheets inventory step using a
service account or OAuth credential with Drive and Sheets read scopes.
It must also capture Apps Script and other loader lineage for native BigQuery
tables, because many `*_clean` and `*_dev` tables do not expose their upstream
spreadsheet through BigQuery external-table metadata.

For animal-level import, Drive/Sheets read access is a hard prerequisite, not a
nice-to-have. The primary RFID source of truth is the `Combined` tab in
https://docs.google.com/spreadsheets/d/1FulMrlb8_AGwL5nFwoORACnbDstSFMg-GCPaKIZnnF8/edit?gid=0#gid=0.
`RFID` is the candidate `animal_identifier_1`; `Old ID` is the candidate
`animal_identifier_2`. Older source-specific mappings such as
`goatsDB.goatsDB_rfid_mapping` and `CPT_RFID_Beetal` remain fallback/provenance
inputs, but they do not replace the primary source-of-truth sheet. Do not treat
native stage/location fields such as `dst_tag` as animal RFID/tag identifiers.

## Metric Ownership

This Markdown file does not own live dashboard, BigQuery, or Google Sheets
numbers. It defines source systems, query formulas, validation rules, cron
cadence, and the target tabs where each run writes computed values.

Do not hand-maintain daily counts, weights, values, pass/fail totals, or
business-date snapshots in this runbook. The implementation must calculate them
on every sync run and write them into the god sheet:

- `Run_Log`: run id, run type, trigger, start/end timestamps, status, source
  watermarks, and job/version metadata.
- `Raw_Source_Snapshots`: source id, grain, business date, row/content hash,
  read timestamp, and raw value payload or pointer.
- `Counts_Snapshots`: source totals, canonical totals, deltas, and selected
  source-of-truth decisions.
- `Issue_Queue` / `Audit_Findings`: RED/AMBER/GREEN blockers with owner,
  evidence refs, and resolution status.
- Domain tabs such as `Animal_Master`, `Vaccination_History`,
  `Current_Location_Status`, `Birth_Events`, and
  `Procurement_Load_Reconciliation`: the current reconciled rows.

Historical values found during planning are only evidence that a validation
rule is needed. They are not canonical targets and should not be refreshed by
editing this document.

Important nuance: `/api/counts` uses `farm.daily_summary_dev` for top KPI
numbers and same-row adult/kid breakdowns. Raw
`ceo_dashboard.counting_db_with_holding_dev` rows are source-count evidence and
cannot be treated as canonical without reconciliation. The god sheet must store
both the canonical chosen value and source evidence for each run.

Dashboard API and BigQuery reads can also differ by timestamp, rounding, or
pipeline freshness. The workbook must store the observed value, source
(`api`, `bq`, or `sheet`), query/run timestamp, and the selected canonical value
instead of overwriting one source with another.

The sync must also detect same-source drift. Some native BigQuery tables, such
as `farm.daily_summary_dev`, can recompute the same `business_date` row in place
without a row-level `updated_at`, ingestion timestamp, or version column. Every
run must snapshot each stable `(source_id, business_date, grain, source_row_id)`
record with a content hash and read timestamp. If the hash changes for the same
key across runs, create a `business_date_row_hash_drift` issue before using the
new value as import or audit truth.

Audit metrics to calculate on every run:

- Births total from `goatsDB.mother_kid_facts` with `is_birth = 1`.
- Sales animal count and sales value from `salesDB.salesDB_clean`.
- Mortality total from `ceo_dashboard.mortality_total_dev`.
- Procurement purchase total from `procurement_farm.procurement_dB_clean` with
  `Record_Type = 'Purchase'`.
- GOATS DB purchase identity counts from `goatsDB.goats_db_clean` at every
  candidate grain: `farm_goat_id`, global `goat_id`, `inp_goat_id`, and the
  chosen audit/coalesced definition.
- Procurement headline deltas for each identity grain, written with the grain
  name and query timestamp.
- Fattening vs shifting source parity by farm/stage, comparing
  `growth_farmwise_weighing` with `cbe/cpt_kids_current_stage_days`.

## What Powers `/counts/overall`

The live dashboard `/counts/overall` page is backed by `/api/counts`.

Primary tables:

| Purpose | Source |
| --- | --- |
| Top count, farm value, weight, adult/kid totals | `goatos-sheets.farm.daily_summary_dev` |
| Current count detail rows by farm/shed/stage/breed/age | `goatos-sheets.ceo_dashboard.counting_db_with_holding_dev` |
| Fallback age/gender/fattening KPI rows | `goatos-sheets.ceo_dashboard.counting_kpis_daily` |

Default date behavior:

- The dashboard computes yesterday in `Asia/Kolkata`.
- The god sheet must use explicit business-date watermarks and not blindly use
  the max date from every source, because different source families can refresh
  at different times.

## Dashboard Audit Coverage

The scheduled audit endpoint inventory is owned by the audit script. The god
sheet sync must read that inventory or keep a generated source catalog in step
with it. Current endpoint families include:

- `/api/counts`
- `/api/counts?farm=cbe`
- `/api/counts?farm=cpt`
- `/api/counts?farm=holdings`
- `/api/fattening`
- `/api/shiftings?farm=cbe`
- `/api/shiftings/cpt`
- `/api/mortality`
- `/api/mortality?period=month-wise`
- `/api/births`
- `/api/feed`
- `/api/sales`
- `/api/purchase-cost`
- `/api/infra`
- `/api/goats-health`
- `/api/vaccination`
- `/api/parent-stock`
- `/api/milking-mothers`

The god sheet sync should import the audit output as structured rows in
`Issue_Queue` and `Counts_Snapshots`, not only as Slack text.

## Source Catalog

The workbook must have a `Source_Catalog` tab. It should include every source
below, plus owner, grain, extractor, freshness SLA, and last successful sync.

### Dashboard And Audit Sources

| Source | Grain | Notes |
| --- | --- | --- |
| `scripts/ceo-dashboard-audit.mjs` | Audit run | Posts scheduled Slack reports to `#dashboard-audit-alerts`. Audit runner is read-only. |
| `https://dashboard--goatos-sheets.us-central1.hosted.app` | Dashboard API | Live legacy dashboard API host. |
| `goatos-sheets` | BigQuery project | Legacy project under `vgoats.com`; read-only for migration planning. |
| Apps Script/native-table loaders | Sheet-to-BigQuery lineage | Required for native `*_clean` and `*_dev` tables whose upstream sheets are invisible to BigQuery external metadata. |

### Key Google Sheets And BigQuery Tables

| Family | Sheet or table |
| --- | --- |
| Counting DB | https://docs.google.com/spreadsheets/d/1vWtbgZI2Yz__noTocwPWzCtEDcS-UXW7mJ9ZqSQ5w_4/edit?gid=0#gid=0 |
| Counting cleaned rows | `ceo_dashboard.counting_db_with_holding_dev` |
| Counting KPI rollups | `ceo_dashboard.counting_kpis_daily` |
| Count dashboard summary | `farm.daily_summary_dev` |
| GOATS DB / DB tab | https://docs.google.com/spreadsheets/d/1R648AutCSXS247DZb7dc07dDgyue6_R4mZDd93oW3M8/edit?gid=0#gid=0 |
| GOATS DB cleaned events | `goatsDB.goats_db_clean` |
| GOATS DB active shedwise details | `goatsDB.active-goats-list-shedwise-details` |
| RFID source of truth | https://docs.google.com/spreadsheets/d/1FulMrlb8_AGwL5nFwoORACnbDstSFMg-GCPaKIZnnF8/edit?gid=0#gid=0, `Combined` tab |
| GOATS DB RFID mapping | `goatsDB.goatsDB_rfid_mapping` |
| GOATS DB farm-id remap | `goatsDB.farm_goat_id_mapping` |
| Mother/kid facts | `goatsDB.mother_kid_facts` |
| Kids tag IDs | `goatsDB.kids_tag_ids` |
| Shifting Reports | https://docs.google.com/spreadsheets/d/1QXhAbV0wAT739S84LfUhHMWPaGw-oUlbZLZHPxEr5G4/edit?resourcekey=&gid=589667268#gid=589667268 |
| CBE current kid stage source | `Shiftings.cbe_kids_current_stage_days` |
| CPT current kid stage source | `Shiftings.cpt_kids_current_stage_days` |
| Shiftings fact/report rows | `Shiftings.shiftings_fact`, `Shiftings.shiftings_reports_clean` |
| Fattening stage source | `ceo_dashboard.growth_farmwise_weighing` |
| Sales source | `salesDB.salesDB_clean` |
| Sales sheet | https://docs.google.com/spreadsheets/d/1ACQJIQIZRkoQx76HtO_vORsGCVekHsI6TFFH-vohT8I/edit?gid=0#gid=0 |
| Sales vs feed comparison | `ceo_dashboard.monthly_feed_vs_sales` |
| Procurement DB | https://docs.google.com/spreadsheets/d/1_856nbDd5jHORrq1ISpaELGn6dewzG2AedlsxE1_bXE/edit?gid=0#gid=0 |
| Procurement tag IDs | https://docs.google.com/spreadsheets/d/1_856nbDd5jHORrq1ISpaELGn6dewzG2AedlsxE1_bXE/edit?gid=524206733#gid=524206733 |
| Procurement holding | https://docs.google.com/spreadsheets/d/1_856nbDd5jHORrq1ISpaELGn6dewzG2AedlsxE1_bXE/edit?gid=1331687687#gid=1331687687 |
| Procurement clean rows | `procurement_farm.procurement_dB_clean` |
| Procurement loadwise status | `procurement_farm.load_wise_procurement_with_status` |
| Procurement loadwise summary | `procurement_farm.loadwise_summary` |
| Procurement holding clean | `procurement_farm.procurement_holding_farm_clean` |
| Shed capacity/source | https://docs.google.com/spreadsheets/d/1Qo5k40CIkS4Lu0074DYSRsiFrmW8LAA8Oi0vMK0X1oI/edit?gid=0#gid=0 |
| Shed tag table | `Shiftings.shed_tag_table`, `counting.shed_tag_capacity_external_table` |
| Health DB | https://docs.google.com/spreadsheets/d/1uvDO_vipNsLcB4S0O7Bj-L8VCSMCd0eX5U9cJS8F-QE/edit?gid=474314888#gid=474314888 |
| Health diagnosis tab | https://docs.google.com/spreadsheets/d/1uvDO_vipNsLcB4S0O7Bj-L8VCSMCd0eX5U9cJS8F-QE/edit?gid=515434741#gid=515434741 |
| Health cleaned rows | `healthDB.health_db_clean_dev`, `healthDB.diagnosis_clean_table` |
| Vaccination source sheet | https://docs.google.com/spreadsheets/d/1L1fZG37ZL9ZmPyOHZxoR6rSYqMrPrWbYbR4ppbRy-G4/edit?gid=0#gid=0 |
| Vaccination source external rows | `ceo_dashboard.vaccination_external_table` |
| Vaccination dashboard rollup | `ceo_dashboard.vaccination_dashboard` |
| Feed DB | https://docs.google.com/spreadsheets/d/1HXaHFTEquc0iVxfC_ZeEm9pB3kAxE58-0oiGtiQpSp8/edit?gid=0#gid=0 |
| Feed cleaned rows | `feedDB.feedDB_clean`, `feedDB.feedDirections_clean`, `feedDB.feed_daily_spend` |
| Feed Directions | https://docs.google.com/spreadsheets/d/1OEr8j_9fYYWmQm0VkP6UgZ1YBcWIs-Km08XGs44W6Hg/edit?gid=723978225#gid=723978225 |
| Experiment sheds config | https://docs.google.com/spreadsheets/d/1Hs6PVH0U9nE52YBGxGVWNloiWgS7mZ45A5GpEvqHHUw/edit?gid=968192338#gid=968192338 |
| Weights DB | https://docs.google.com/spreadsheets/d/1TlSf-Pg1dyIB6ZKbd_FQ5ILiJX3AkkZp9Pg4dVmak2g/edit?gid=1889151758#gid=1889151758 |
| Weight cleaned rows | `weights.weights_db_clean`, `weights.weights_db_standardized` |
| Holding farm details | https://docs.google.com/spreadsheets/d/1dygPv9l0CLGIlauWcuH38SCNhtzh2tHo8Gu3CI-XP_w/edit?gid=212783005#gid=212783005 |
| Mortality total | `ceo_dashboard.mortality_total_dev` |
| Mortality monthly | `ceo_dashboard.monthly_mortality_rate` |
| Breeding DB sheet | https://docs.google.com/spreadsheets/d/1h04WpLExJBdZ-J-H2YLGXyHnlWSdjtldiaB6n3x3rYg/edit?gid=0#gid=0 |
| Breeding DB external table | `breedingDB.breedingDB_external_table` |
| Parent stock view | `ceo_dashboard.parent_stock_table` |
| Breeding total summary | `ceo_dashboard.total_summary_breeding` |
| Delivery/Birth DB sheet | https://docs.google.com/spreadsheets/d/1bYNW8c6BMb6wgBXEIHkWO57nYRTkYc4mnPkDE14S2Yw/edit?gid=32106927#gid=32106927 |
| Delivery/Birth DB raw external rows | `deliveryDB.birthDB_unclean` |
| Delivery/Birth DB cleaned rows | `deliveryDB.delivery_db_clean_dev` |
| Farmer network sheet | https://docs.google.com/spreadsheets/d/16bEJqIVZ4gFGSPFCPvTud8Z0dZEa8Wur5tUM0JNwJaM/edit?gid=0#gid=0 |
| Farmer network table | `farmersDB.farmer_crops_db` |
| Milk consumption sheet | https://docs.google.com/spreadsheets/d/1qJRQK2DDy2C4y359CHVBh1OhWBk0K7FJMMVvXCUqr0A/edit?gid=1263357772#gid=1263357772 |
| Milk consumption table | `ceo_dashboard.milk_consumption_db` |
| Milk feeding summary sheet | https://docs.google.com/spreadsheets/d/1qJRQK2DDy2C4y359CHVBh1OhWBk0K7FJMMVvXCUqr0A/edit?gid=1556648101#gid=1556648101 |
| Milk feeding summary table | `ceo_dashboard.milk_feeding_summary` |
| Alternate milk/lactation source sheet | https://docs.google.com/spreadsheets/d/1J3WWJbuFp3PPpj-UzB4zmzYA7g7G0-KvMonx9-cE9FE/edit?gid=0#gid=0 |
| Alternate milk/lactation source table | `ceo_dashboard.milk_consumption_external_table` |
| Milking goats/lactating mother view | `ceo_dashboard.milking_goats_list` |
| Dashboard users/RBAC sheet | https://docs.google.com/spreadsheets/d/13TtoRv0pKYtatsuc3-YDZHcTdWALekBd-e-WMblrniY/edit |
| Dashboard users/RBAC table | `ceo_dashboard.dashboard_users` |
| History automation dataset | `historyAutomation` |
| History automation complete history view | `historyAutomation.goat_history_complete` |

Credential exclusion: never snapshot `ceo_dashboard.dashboard_user_password_overrides`
or password-hash fields from `ceo_dashboard.dashboard_users_seed` into the god
sheet. RBAC import is limited to role, owner, and escalation mapping fields.

### Known Workbook Tabs To Verify With Drive Scope

The future Drive inventory pass must enumerate and confirm all tabs in the
shared source workbooks. Known tabs and tab families to include:

- Counting DB:
  - `DB`
  - `Active-Goats`
  - `Validation`
  - `Born-Datewise`
  - `Death-Datewise`
  - `Sales-Datewise`
  - `CBE-Shedwise`
  - `CPT-Shedwise`
  - `CBE-Tagswise`
  - `CPT-Tagswise`
  - `CBE-Kids-Adults`
  - `CPT-Kids-Adults`
  - `CBE-Breedwise`
  - `CPT-Breedwise`
  - `Total-Count`
  - `FutureDB`
  - `Projected-DB`
- GOATS DB:
  - `DB`
  - active goat shedwise/detail tabs
  - RFID mapping tabs
  - shed tag/source lookup tabs
- Procurement DB:
  - `DB`
  - `Quratine Center Goat DB`
  - `Procurment SOP Selection DB`
  - `Procurement App Responses`
  - `Procurement Transit Responses`
  - `Validation`
  - tag ID tabs
  - holding farm tabs
- Shifting Reports:
  - `Shifting Reports`
  - current-stage derived tabs
- Health DB:
  - `DB`
  - `Problem`
  - `Diagnosis Form`
  - `Follow Up`
  - `Treatments-Schedule`
- Vaccination source:
  - shed/count-level vaccination rows
  - vaccine, shed tag, age, animal type, dosage, count, and status columns
- Feed DB and Feed Directions:
  - feed consumption rows
  - feed direction rows
  - experiment shed config rows
- Weights DB:
  - raw weight rows
  - standardized weight rows
- Sales DB:
  - sales event rows
  - buyer/vendor detail columns
- Holding farm:
  - holding details rows
  - procurement holding rows
- Breeding DB:
  - breeding event rows
  - mating medicine rows
  - sponge medicine rows
  - kid lifecycle enrichment rows
- Delivery/Birth DB:
  - delivery rows
  - birth-event source rows
- Milk and lactation:
  - milk consumption rows
  - milk feeding summary rows
  - milking goat/lactation rows
- Dashboard users/RBAC:
  - dashboard users
  - owner, role, and escalation mapping rows

If a tab is missing, renamed, protected, or stale, the sync must create a RED
`source_schema_or_access` issue before any import preview runs.

### BigQuery Datasets To Track

At minimum:

- `ceo_dashboard`
- `farm`
- `goatsDB`
- `counting`
- `Shiftings`
- `procurement_farm`
- `salesDB`
- `healthDB`
- `feedDB`
- `feed_directions`
- `weights`
- `holding_farm`
- `breedingDB`
- `deliveryDB`
- `farmersDB`
- `historyAutomation`
- `crop_season`

## Goat OS Import Contract

Goat OS requires stricter canonical animal identity than the legacy dashboard.

Current creation/import requirements:

- `animal_identifier_1` is the primary RFID/tag identifier and is required.
- `animal_identifier_2` is the secondary RFID/tag identifier and is optional
  until double RFID tagging is live.
- If both identifiers are present, they must be different.
- Identifier values are globally single-use for life.
- Legacy `goat_id`, `farm_goat_id`, and `inp_goat_id` columns are source
  identifiers/crosswalk inputs only.
- Legacy old-tag and new-tag columns are candidate RFID/tag inputs for
  `animal_identifier_1` / `animal_identifier_2`. They may populate Goat OS
  identifiers only when the source value is an actual RFID/tag value and passes
  global uniqueness checks.
- Legacy tag reuse is dirty data for Goat OS. A fallen/retired/reused RFID/tag
  must be recorded in identifier history as broken/retired/disputed for the
  original animal and must not be assigned to a different animal.
- `species` is required and must be `goat` or `sheep`.
- `sex` is required and must be `female` or `male`.
- `dob` is required for current create/import contracts.
- `dob_estimated` must be tracked when DOB is derived or weak.
- `origin_type` is required: `birth`, `procured`, or `imported`.
- `entry_date` is required.
- `park_id` or `park_code` is required.
- `shed_id` or `shed_code` is required.
- `evidence_refs` must be present.

The god sheet should use the exact canonical field names above. Raw legacy
column names must be stored as provenance only.

## Import Gate Metrics To Compute

The sync job must compute import-gate feasibility on every run. This runbook
defines the grain, formulas, issue routing, and target tabs; it does not own the
current metric values.

Definition used for the active-spine check:

- animal key:
  `COALESCE(NULLIF(TRIM(goat_id), ''), NULLIF(TRIM(farm_goat_id), ''), NULLIF(TRIM(inp_goat_id), ''))`
- latest event: one row per animal key ordered by `date DESC`, then event
  priority `Death=5`, `Sale=4`, `Abortion=4`, `Shifting=3`, `Purchase=2`,
  `Birth=1`, everything else `0`
- active candidate: latest event excluding `Sale`, `Death`, and `Abortion`
- DOB evidence: a `Birth` event with non-null `date` for that animal key.
  `birth_time` is time-of-day only; never use it as date-of-birth evidence.
- tie-check: compare terminal-event priority against latest-date active/inactive
  ambiguity and write both results to `Counts_Snapshots`

Pinned active-spine check:

```sql
WITH base AS (
  SELECT
    *,
    COALESCE(NULLIF(TRIM(CAST(goat_id AS STRING)), ''),
             NULLIF(TRIM(CAST(farm_goat_id AS STRING)), ''),
             NULLIF(TRIM(CAST(inp_goat_id AS STRING)), '')) AS animal_key
  FROM `goatos-sheets.goatsDB.goats_db_clean`
),
ranked AS (
  SELECT
    *,
    ROW_NUMBER() OVER (
      PARTITION BY animal_key
      ORDER BY date DESC,
        CASE event
          WHEN 'Death' THEN 5
          WHEN 'Sale' THEN 4
          WHEN 'Abortion' THEN 4
          WHEN 'Shifting' THEN 3
          WHEN 'Purchase' THEN 2
          WHEN 'Birth' THEN 1
          ELSE 0
        END DESC,
        event DESC,
        COALESCE(shifting_id, '') DESC,
        COALESCE(goat_id, '') DESC,
        COALESCE(farm_goat_id, '') DESC,
        COALESCE(inp_goat_id, '') DESC
    ) AS rn
  FROM base
  WHERE animal_key IS NOT NULL
)
SELECT COUNT(*) AS active_candidates
FROM ranked
WHERE rn = 1
  AND COALESCE(event, '') NOT IN ('Death', 'Sale', 'Abortion');
```

Coverage metrics to write each run:

| Metric id | Formula / source rule | Target tab |
| --- | --- | --- |
| `active_spine_candidates` | Pinned active-spine SQL above | `Counts_Snapshots` |
| `dashboard_active_total` | Canonical `/api/counts` / `farm.daily_summary_dev` active total for the run business date | `Counts_Snapshots` |
| `event_spine_dashboard_delta` | `active_spine_candidates - dashboard_active_total` at the selected canonical grain | `Counts_Snapshots`, `Issue_Queue` when non-zero |
| `active_with_birth_date` | Active-spine rows with Birth-event `date` evidence | `Counts_Snapshots` |
| `active_missing_birth_date` | Active-spine rows without Birth-event `date` evidence | `Issue_Queue` as RED unless approved estimated DOB evidence exists |
| `active_missing_species` | Active-spine rows without trusted `species` from source `Animal_Type` or approved breed/species crosswalk | `Issue_Queue` as RED |
| `purchase_no_birth_has_age` | Purchase-origin/no-Birth active rows with legacy `age` evidence | `Issue_Queue` as AMBER candidate for estimated-DOB review |
| `purchase_no_birth_no_age` | Purchase-origin/no-Birth active rows without Birth `date` or `age` evidence | `Issue_Queue` as RED |
| `active_missing_gender` | Active-spine rows without trusted sex evidence | `Issue_Queue` as RED unless Drive/source backfill supplies evidence |
| `active_birth_date_and_gender_present` | Active-spine rows with native Birth `date` and trusted sex evidence | `Counts_Snapshots`; still not GREEN unless RFID and location gates pass |
| `current_location_source_coverage` | Active-spine rows resolved from latest-event `farm` plus latest Shifting `dst_shed`/`dst_tag`, with fallback to other animal-grain shed evidence | `Counts_Snapshots`, `Issue_Queue` for unresolved rows |
| `breed_species_taxonomy_reconcile` | Compare normalized breed/species vocabulary across event spine, dashboard count rows, procurement `Animal_Type`, and crosswalk entries | `Counts_Snapshots`, `Issue_Queue` for one-sided or unmapped taxonomy values |
| `dropped_no_identifier_rows` | Raw source rows with all candidate identifiers blank before active-spine grouping | `Issue_Queue` as one RED issue per source row |

Implications:

- Do not promote any row to strict GREEN until a trusted RFID/tag source for
  `animal_identifier_1` is readable and globally unique. Valid values should
  come first from the RFID source-of-truth `Combined` tab, with older
  source-specific mappings retained as fallback/provenance. The event spine has
  legacy source IDs, DOB, and gender signals, but source IDs and location tags
  such as `dst_tag` are not substitutes for the RFID-backed Goat OS identifiers.
- The vaccination demo must treat DOB, species, sex, RFID, and unresolved
  current-location rows as gating cleanup lanes, not incidental polish.
- Estimated-DOB recovery is narrow, not broad. Only rows with approved age/date
  evidence can become AMBER estimated-DOB candidates; rows with neither Birth
  `date` nor usable age evidence remain RED until source or ground verification
  supplies DOB evidence.
- Do not seed `Animal_Master` by blindly taking the latest-event spine as active
  truth; reconcile it to dashboard/current-status sources first.
- Resolve current location from animal-grain sources first. Latest-event `farm`
  plus latest Shifting `dst_shed`/`dst_tag` is the primary location evidence
  when present; other event-spine shed evidence may be fallback evidence. The
  RED gap is the unresolved remainder and any missing shed-to-park mapping, not
  every active animal.
- Treat dashboard active totals as aggregate targets, not animal-grain proof.
  `farm.daily_summary_dev` and `ceo_dashboard.counting_db_with_holding_dev`
  do not carry animal identifiers, so they can expose count/taxonomy gaps but
  cannot close row-level `Animal_Master` reconciliation without an
  animal-identifier source.
- Treat the event-spine vs dashboard active gap as bidirectional. The event
  spine is not simply a superset; the two sources can disagree on breed/species
  vocabulary and can contain populations missing from the other.

Estimated DOB policy required before GREEN promotion:

1. Use trusted Birth-event `date` evidence first. `goatsDB.mother_kid_facts`
   can be used as corroborating birth provenance only when the sync proves it
   joins at the same animal grain; do not assume it rescues purchase/no-Birth
   animals. In `goatsDB.goats_db_clean`, the native DOB-evidence test is
   `event = 'Birth'` with non-null `date`. `birth_time` is only clock time and
   must be stored as optional provenance, not treated as a DOB column.
2. For purchase-origin or shifted animals with no birth evidence, derive DOB only
   through an approved `dob_estimated=true` policy using age class, event date,
   source row, and owner approval. The legacy `age` column is a weak AMBER proxy,
   not a GREEN DOB by itself. Record `dob_estimation_method` and evidence.
3. Rows with derived DOB remain AMBER until the policy is approved and Goat OS
   preview accepts the estimate. Rows with no derivable DOB remain RED.

Sex backfill policy required before GREEN promotion:

1. Use non-empty native event-spine `gender` first.
2. Use `goatsDB.goatsDB_rfid_mapping.Gender` after Drive/Sheets read access is
   available.
3. If both are missing or conflicting, require ground/source verification. Rows
   with unresolved sex remain RED.

## Workbook Tabs

### 1. `README_Problem_Statement`

Human-readable purpose inside the workbook:

- why the workbook exists,
- what RED/AMBER/GREEN means,
- who owns cleanup,
- what "ready for Goat OS import" means,
- current temporary exception for `animal_identifier_2`,
- warning that the workbook is migration staging, not runtime truth.

### 2. `Source_Catalog`

Columns:

`source_id`, `source_family`, `source_system`, `dataset`, `table_name`,
`sheet_url`, `tab_name`, `grain`, `primary_keys`, `owner`, `extractor`,
`freshness_sla`, `watermark_field`, `last_successful_watermark`,
`last_schema_hash`, `last_row_count`, `notes`.

### 3. `Sync_Runs`

Columns:

`run_id`, `run_type`, `scheduled_for_ist`, `started_at`, `completed_at`,
`status`, `business_date`, `source_id`, `rows_read`, `rows_written`,
`failed_rows`, `source_hash`, `schema_hash`, `bq_job_id`, `cloud_run_job_id`,
`error_summary`.

### 4. `Raw_Source_Snapshots`

This tab can either contain raw records for small sources or links to protected
per-source snapshot tabs/files.

Required columns:

`run_id`, `source_id`, `business_date`, `source_grain`, `source_row_id`,
`source_row_key`, `source_row_hash`, `raw_payload_json`, `read_at`,
`snapshot_created_at`.

`source_row_key` must be stable for comparison across runs, for example
`source_id|business_date|farm|stage|grain`. When a later run reads the same key
with a different `source_row_hash`, the validation engine must raise
`business_date_row_hash_drift`.

### 5. `Mapping_Crosswalks`

This is the identity reconciliation layer.

Columns:

`crosswalk_id`, `source_id`, `source_record_id`, `raw_identifier_value`,
`normalized_identifier_value`, `identifier_type_candidate`,
`animal_identifier_1`, `animal_identifier_2`, `goat_os_animal_id`,
`match_status`, `match_confidence`, `duplicate_group_id`, `merge_blocker`,
`chosen_canonical_reason`, `last_reviewed_by`, `last_reviewed_at`.

### 6. `Animal_Master`

One row per candidate canonical animal.

Columns:

`goat_os_animal_id`, `animal_identifier_1`, `animal_identifier_2`,
`legacy_ids`, `species`, `sex`, `breed`, `dob`, `dob_estimated`, `age_class`,
`origin_type`, `entry_date`, `current_status`, `current_farm`, `current_park`,
`current_shed`, `current_stage`, `management_stage`, `health_status`,
`reproductive_status`, `mother_identifier`, `sire_or_lot`, `current_weight_kg`,
`photo_url`, `dob_estimation_method`, `sex_source`, `source_record_id`,
`source_links`, `evidence_refs`, `verification_status`, `issue_reason`,
`ground_owner`, `last_verified_at`, `ready_for_goat_os_import`.

### 7. `Animal_Identifier_History`

Track broken, replaced, retired, disputed, and duplicate tags.

Columns:

`animal_identifier`, `identifier_type`, `animal_identifier_1`,
`animal_identifier_2`, `goat_os_animal_id`, `status`, `valid_from`, `valid_to`,
`source_system`, `source_record_id`, `reason`, `approved_by`, `review_status`.

### 8. `Identity_Grain_Audit`

This tab prevents aggregate reconciliation from hiding identifier-grain
problems. It records every major source's available identifier columns and the
canonical animal-grain decision before a mismatch is treated as data truth.

Initial rows must include live-computed `goatsDB.goats_db_clean` purchase
identity counts at each candidate grain:

- distinct `farm_goat_id`
- distinct global `goat_id`
- distinct `inp_goat_id`
- audit coalesced/farm-scoped definition

Executable resolver sources:

- `goatsDB.goats_db_clean` carries `inp_goat_id`, global `goat_id`, and
  `farm_goat_id` in the same event row.
- `goatsDB.farm_goat_id_mapping` maps `old_farm_goat_id` to
  `new_farm_goat_id`; use it to collapse retagged or renumbered farm IDs before
  treating farm-scoped counts as distinct animals.

Columns:

`source_id`, `event_type`, `id_column`, `distinct_count`,
`coalesced_definition`, `canonical_candidate`, `delta_vs_procurement`,
`example_duplicate_group_ids`, `decision_owner`, `decision_status`,
`decision_note`.

Goat OS import must choose the canonical identifier strategy before treating
procurement purchase deltas as row-level import blockers.

### 9. `Location_Profile`

Canonical location mapping for farms, parks, sheds, shed tags, capacity, and
aliases.

Columns:

`location_key`, `farm`, `park_code`, `shed_code`, `shed_name`, `shed_tag`,
`stage_profile`, `capacity`, `species_allowed`, `sex_grouping`,
`is_holding`, `is_quarantine`, `is_icu`, `usable_for_vaccination`,
`source_link`, `verification_status`.

### 10. `Species_Taxonomy_Crosswalk`

Controlled species derivation for Goat OS import. `goatsDB.goats_db_clean` does
not expose a dedicated `species` column, so the sync must never infer species
from free text without this approved crosswalk.

Columns:

`source_system`, `source_field`, `raw_value`, `normalized_value`,
`canonical_species`, `confidence`, `mapping_rule`, `approved_by`,
`approved_at`, `source_link`, `verification_status`.

Seed behavior:

- Prefer explicit source species/type fields such as procurement
  `Animal_Type` when present.
- Use breed-to-species mapping only through this crosswalk.
- Treat blank, unmapped, or ambiguous breed/species values as RED for import.
- Keep sheep/goat breed vocabulary separate from dashboard display labels so
  taxonomy mismatches do not silently become count mismatches.

### 11. `Current_Location_Status`

Animal-level current location and current status resolution.

Columns:

`animal_identifier_1`, `animal_identifier_2`, `current_farm`, `current_park`,
`current_shed`, `current_stage`, `source_priority_used`, `latest_event_farm`,
`latest_shifting_dst_shed`, `latest_shifting_dst_tag`, `fallback_shed_evidence`,
`counting_db_status`, `goats_db_status`, `shiftings_status`, `procurement_status`,
`resolved_status`, `resolution_reason`, `verification_status`.

Source priority:

1. Latest active event's animal-grain `farm`.
2. Latest Shifting event's animal-grain `dst_shed` and `dst_tag`.
3. Other event-spine animal-grain shed evidence.
4. `Location_Profile` shed-to-park/stage mapping.

Aggregate dashboard count rows may validate totals but cannot resolve
animal-level location by themselves.

### 12. `Birth_Events`

Columns:

`event_id`, `kid_identifier`, `mother_identifier`, `birth_date`, `farm`,
`shed`, `breed`, `kid_status`, `source_system`, `source_record_id`,
`source_link`, `evidence_refs`, `verification_status`.

### 13. `Breeding_Delivery_History`

Tracks breeding, mating, delivery, and related medicine/source evidence that may
affect birth provenance, dam/lactation status, and future Goat OS reproductive
history.

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `event_type`,
`event_date`, `farm`, `shed`, `mate_or_sire_identifier`, `delivery_outcome`,
`medicine_or_protocol`, `source_system`, `source_record_id`, `source_link`,
`verification_status`.

### 14. `Death_Events`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `death_date`,
`cause`, `farm`, `source_system`, `source_record_id`, `source_link`,
`verified_by`, `verification_status`.

### 15. `Sale_Events`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `sale_date`, `farm`,
`buyer_or_vendor`, `sale_amount`, `sale_weight_kg`, `sales_id`,
`source_record_id`, `source_link`, `verification_status`.

### 16. `Procurement_Source_Entry`

One row per procurement source/load candidate.

Columns:

`load_id`, `source_party`, `source_location`, `animal_identifier_1`,
`animal_identifier_2`, `species`, `sex`, `breed`, `purchase_date`,
`loaded_at`, `arrived_at`, `accepted_intake_at`, `source_entry_state`,
`current_state`, `ownership_state`, `health_state`, `warmup_started_at`,
`warmup_ended_at`, `holding_location`, `proof_refs`, `verification_status`.

### 17. `Procurement_Load_Reconciliation`

Used to close the current procurement mismatch. Do not assume the procurement
purchase total vs GOATS DB purchase total is a clean animal-level delta until
`Identity_Grain_Audit` and `Mapping_Crosswalks` decide the canonical grain.

Columns:

`load_id`, `procurement_db_count`,
`goats_db_purchase_distinct_farm_goat_id`,
`goats_db_purchase_distinct_goat_id`,
`goats_db_purchase_distinct_inp_goat_id`,
`goats_db_purchase_distinct_coalesced`, `loadwise_status_rows`,
`loadwise_summary_accounted_count`, `delta_by_canonical_id`,
`unmatched_procurement_ids`, `unmatched_goats_db_ids`, `owner`, `next_action`,
`verification_status`.

### 18. `Movement_Stage_History`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `from_farm`,
`to_farm`, `from_shed`, `to_shed`, `from_stage`, `to_stage`, `shift_date`,
`stage_entry_date`, `days_in_stage`, `source_system`, `source_record_id`,
`source_link`, `verification_status`.

### 19. `Fattening_Shifting_Reconciliation`

Columns:

`farm`, `stage`, `fattening_source_count`, `shiftings_current_stage_count`,
`delta`, `candidate_animal_ids`, `candidate_load_ids`, `source_decision`,
`owner`, `next_action`, `verification_status`.

### 20. `Weight_History`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `weight_date`,
`weight_kg`, `farm`, `shed`, `stage`, `source_system`, `source_record_id`,
`source_link`, `verification_status`.

### 21. `Health_Treatment_History`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `problem`,
`diagnosis`, `treatment`, `medicine`, `dose`, `follow_up_date`, `vet_or_staff`,
`source_system`, `source_record_id`, `source_link`, `verification_status`.

### 22. `Milk_Lactation_History`

Tracks milk consumption, feeding summaries, milking-mother rows, and lactation
signals needed for Goat OS reproductive and nutrition history.

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `event_type`,
`event_date`, `farm`, `shed`, `lactation_status`, `milk_quantity_l`,
`feeding_quantity_l`, `kid_identifier`, `source_system`, `source_record_id`,
`source_link`, `verification_status`.

### 23. `Vaccination_History`

One row per actual vaccination evidence item. Do not use one column per vaccine.
The legacy vaccination source
`ceo_dashboard.vaccination_external_table` is shed/count grained (`Date`,
`Farm`, `Vaccine`, `Shed_Tag`, `Shed`, `Age`, `Animal_Type`, `Dosage_Type`,
`Total_Count`, `Status`) and has no animal identifier. It cannot seed
animal-level `Vaccination_History`; treat it only as `untrusted_legacy_note`
context and never let it suppress `Vaccination_Due_View` due work.

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`,
`goat_os_animal_id`, `protocol_version_id`, `rule_id`, `vaccine_code`,
`vaccine_name`, `dose_code`, `dose_number`, `administered_at`, `dose_ml`,
`batch_or_vial_lot`, `administered_by`, `farm_at_time`, `park_at_time`,
`shed_at_time`, `proof_link`, `source_context`, `trust_status`,
`review_status`, `suppresses_due`, `source_record_id`, `verification_status`.

Trust classes:

- `trusted_our_park`
- `trusted_procurement_holding_park`
- `untrusted_vendor_claim`
- `untrusted_legacy_note`
- `conflicting`
- `duplicate`

Only trusted, reviewed evidence may suppress Goat OS due work.

### 24. `Vaccination_Due_View`

This is the demo-critical view.

Columns:

`animal_identifier_1`, `animal_identifier_2`, `goat_os_animal_id`, `species`,
`breed`, `sex`, `dob`, `age_days`, `farm`, `park`, `shed`, `stage`,
`vaccine_code`, `vaccine_name`, `dose_code`, `last_trusted_dose_date`,
`next_due_date`, `latest_safe_date`, `due_status`, `due_reason`,
`blocking_issue_id`, `verification_status`.

Due-date behavior:

- Rows with trusted DOB or approved estimated DOB can compute `age_days`,
  `next_due_date`, and `latest_safe_date`.
- Rows without DOB evidence must stay visible in the view with
  `blocking_issue_id` and `due_status` explaining the missing DOB blocker.
- Legacy shed-count vaccination history may add context, but it must not
  suppress due work without trusted animal-level evidence.
- The demo should show both computable due rows and blocked rows honestly; a
  sparse due schedule is a data-cleanup signal, not a view failure.

### 25. `Counts_Snapshots`

Columns:

`business_date`, `source_id`, `farm`, `park`, `shed`, `stage`, `breed`,
`age_class`, `source_count`, `canonical_count`, `delta`, `source_link`,
`verification_status`.

Required reconciliation rows to compute each run:

- `dashboard_active_total`: canonical dashboard active total for the run
  business date.
- `event_spine_active_total`: latest-event active candidates from
  `goatsDB.goats_db_clean` at the selected active-spine grain.
- `event_spine_dashboard_delta`: difference between the event spine and
  canonical dashboard active total.
- The active-count gap must stay open until `Current_Location_Status` decides
  the active source-of-truth priority and row-level differences.
- Dashboard rows are aggregate-only at source-count grain, so use them as
  canonical aggregate targets and gap detectors. Do not use them as the
  row-level join source for `Animal_Master`; row-level closure needs an
  animal-identifier source such as Drive RFID mapping or another current-status
  table with animal IDs.
- The gap is bidirectional and includes breed/species vocabulary mismatch, not
  just extra event-spine animals. The sync must write breed/species-level delta
  rows for each side instead of hardcoding example counts here.

### 26. `Feed_Sales_Reconciliation`

Columns:

`month`, `farm`, `sales_animals`, `sales_value`, `feed_spend`, `source_sales`,
`source_feed`, `delta`, `verification_status`.

### 27. `Validation_Rules`

Columns:

`rule_id`, `rule_name`, `severity`, `blocking`, `owner`, `source_family`,
`description`, `check_type`, `sql_or_formula_ref`, `expected_result`,
`failure_message`, `last_run_id`, `last_status`.

Seed rules:

| Rule id | Severity | Blocking | Owner | Expected result / issue routing |
| --- | --- | --- | --- | --- |
| `business_date_row_hash_drift` | P1 | Yes, when the changed row affects import or canonical audit values | data/dev | For each stable `(source_id, business_date, grain, source_row_id)`, current hash must match the prior accepted hash unless the run records an intentional source refresh. Hash changes create `business_date_row_hash_drift` issues. |
| `active_spine_dashboard_reconcile` | P1 | Yes, for import; no, for source visibility | data/dev | Event-spine active total and dashboard aggregate total must either reconcile at the chosen grain or produce a blocking aggregate reconciliation issue with source rows attached. |
| `dashboard_rows_are_aggregate_only` | P1 | Yes, for row-level import | data/dev | Aggregate dashboard rows must never be used as the row-level join source for `Animal_Master`. Any attempt to close animal rows from aggregate-only data fails validation. |
| `raw_source_row_has_identifier` | P1 | Yes | data/dev + ground/source team | Every raw animal-event source row must have at least one usable candidate identifier before grouping. Rows missing all candidate identifiers create one RED `Issue_Queue` row per source row. |
| `animal_identifier_1_present` | P1 | Yes | data/dev | Import candidate must have trusted primary RFID/tag `animal_identifier_1` from Drive-readable RFID mapping or validated old-tag/new-tag columns; legacy source IDs and stage/location tags such as `dst_tag` do not satisfy this. |
| `animal_identifier_uniqueness` | P1 | Yes | data/dev | Normalized RFID/tag identifiers must be globally single-use across current and historical identifier sources. Reused fallen/retired tags create RED issues until resolved. |
| `species_evidence_present` | P1 | Yes | data/dev + ground/source team | Import candidate must have trusted species from explicit source type or approved `Species_Taxonomy_Crosswalk`; blank/unmapped/ambiguous values create RED issues. |
| `breed_species_taxonomy_reconcile` | P2 | Yes, when taxonomy affects import or canonical counts | data/dev + ground/source team | Breed/species vocabulary across event spine, dashboard counts, procurement, and crosswalk must reconcile or produce taxonomy issues with source refs. |
| `dob_evidence_or_approved_estimate` | P1 | Yes | data/dev + ground/source team | Active import candidates must have trusted Birth-event `date` evidence or an approved estimated-DOB policy with source evidence. |
| `birth_time_not_dob` | P1 | Yes | data/dev | `birth_time` must never satisfy DOB evidence; it is stored only as time-of-day provenance. |
| `purchase_no_birth_no_age_red` | P2 | Yes, for import | ground/source team | Purchase-origin candidates with no Birth `date` and no usable age evidence remain RED until source/ground DOB evidence is supplied. |
| `sex_evidence_present` | P1 | Yes | data/dev + ground/source team | Active import candidates must have trusted sex evidence from native events, Drive RFID mapping, or another approved source. |
| `current_location_status_resolved` | P1 | Yes | data/dev + ground/source team | Active import candidates must have current farm/park/shed/stage resolved from latest event/Shifting animal-grain evidence plus `Location_Profile` mapping. Aggregate dashboard rows alone cannot pass this rule. |
| `vaccination_legacy_count_not_suppressing_due` | P1 | Yes | data/dev | Legacy shed-count vaccination rows may be stored as notes but must not suppress animal-level Goat OS due work unless trusted animal-level evidence exists. |
| `procurement_identity_grain_resolved` | P1 | Yes, for procurement import | data/dev + ground/source team | Procurement and GOATS DB purchase deltas must be evaluated at each candidate identity grain; unresolved grain mismatch creates a blocking issue. |
| `fattening_shiftings_stage_parity` | P2 | No, unless attributable to import row | data/dev + ground/source team | Fattening and current-stage sources must be compared by farm/stage. Differences produce reconciliation issues with both source refs. |
| `source_stale_past_sla` | P1 | Yes, when source affects import or canonical audit values | data/dev | Source watermark must be within configured SLA for its source family. Stale source creates a RED issue for dependent rows. |

### 28. `Issue_Queue`

The real cleanup control center. One row per issue.

Columns:

`issue_id`, `issue_type`, `severity`, `blocking`, `animal_identifier_1`,
`animal_identifier_2`, `load_id`, `source_id`, `source_record_id`,
`evidence`, `owner`, `next_action`, `sla_date`, `status`, `resolved_by`,
`resolved_at`, `resolution_note`.

### 29. `Import_Batches`

Columns:

`batch_id`, `created_at`, `created_by`, `source_run_id`, `row_count`,
`green_rows`, `amber_rows`, `red_rows`, `preview_status`,
`goat_os_preview_request_id`, `goat_os_import_request_id`, `idempotency_key`,
`import_status`, `imported_rows`, `failed_rows`, `failure_summary`.

## RED, AMBER, GREEN Rules

### RED

RED means do not import into Goat OS.

RED blockers:

- Missing `animal_identifier_1`.
- Duplicate normalized identifier across any current or historical source.
- `animal_identifier_1` equals `animal_identifier_2`.
- Missing `species`.
- Missing `sex`.
- Missing `dob`, unless there is an approved estimated DOB policy and evidence.
- Missing `origin_type`.
- Missing `entry_date`.
- Missing current park/shed for an active animal.
- Missing source row id/hash or evidence refs.
- Animal is active in one source and dead/sold/lost/transferred in another.
- Procurement animal has unresolved source entry, ownership, health, or accepted
  intake state.
- Vendor or third-party vaccination claim is being used to suppress due work.
- Vaccination row lacks date, vaccine, dose code, proof/review status, or has a
  conflicting duplicate.
- Counting DB stage/age conflict is attributable to this animal and affects
  eligibility or counts.
- Fattening vs shifting source mismatch is attributable to this animal/load.
- Source snapshot is stale past SLA.

### AMBER

AMBER means usable only under a temporary or non-blocking rule.

AMBER examples:

- `animal_identifier_2` is missing during the current double-tag rollout.
- DOB is estimated but has acceptable source evidence.
- Source mismatch exists at aggregate level but is not yet attributable to this
  animal.
- Weak source proof exists, but it does not suppress vaccination due work.
- Ground verification is pending but the row is not used for import yet.

When double RFID tagging is live, missing `animal_identifier_2` must move from
AMBER to RED in both the sheet and Goat OS DB/app validation.

### GREEN

GREEN means:

- Goat OS required fields are complete.
- Evidence refs exist.
- Identifier uniqueness checks pass.
- Current status/location is reconciled.
- Vaccination trust rules are applied correctly.
- Import preview passes.
- No open blocking issue exists.

## Current Open Findings To Track

These should be preloaded into `Issue_Queue` or `Audit_Findings`.

The table below seeds issue categories only. Current counts, deltas, and source
row links must be filled by the sync job in `Issue_Queue`, not maintained here.

| Finding | Severity | Owner | Rule / source family |
| --- | --- | --- | --- |
| Counting DB kid-stage rows need age-field source verification | P2 | ground/source team | `ceo_dashboard.counting_db_with_holding_dev`; validate kid/fattening-stage rows where `age` does not match stage semantics |
| Customized age-only risk signature needs UI regression coverage | P2 | data/dev | Dashboard customized-count filters; compare age-only and stage-aware splits before treating customized counts as closed |
| Procurement DB vs farm procured animal count mismatch | P1 | data/dev + ground/source team | `procurement_identity_grain_resolved`; procurement source must use `Record_Type='Purchase'` and compare against GOATS DB purchase counts at each candidate identity grain |
| Procurement loadwise status rollup mismatch | P2 | data/dev + ground/source team | `procurement_farm.load_wise_procurement_with_status` vs `procurement_farm.loadwise_summary` |
| Fattening vs shiftings stage-source reconciliation | P2 | data/dev + ground/source team | `fattening_shiftings_stage_parity`; compare `growth_farmwise_weighing` with current-stage source tables by farm/stage |
| Animal import gate feasibility: species, DOB, sex, RFID, and location coverage | P1 | data/dev + ground/source team | `animal_identifier_1_present`, `species_evidence_present`, `dob_evidence_or_approved_estimate`, `sex_evidence_present`, `current_location_status_resolved` |
| Breed/species taxonomy reconciliation | P2 | data/dev + ground/source team | `breed_species_taxonomy_reconcile`; compare event-spine breed values, dashboard count breed values, procurement animal types, and approved species crosswalk mappings |
| Drive/Sheets credential blocker for RFID and sex source | P1 | data/dev | `goatsDB.goatsDB_rfid_mapping`; source requires Drive/Sheets credentials before RFID/Gender extraction can pass |
| Event-spine active vs dashboard active gap | P1 | data/dev | `active_spine_dashboard_reconcile`; dashboard sources are aggregate-only and cannot close row-level `Animal_Master` reconciliation |
| Dropped no-identifier event rows | P2 | data/dev + ground/source team | `raw_source_row_has_identifier`; emit one RED issue per raw source row missing all candidate identifiers |
| Purchase-origin DOB estimation gap | P2 | data/dev + ground/source team | `purchase_no_birth_no_age_red`; purchase-origin rows without Birth `date` or usable age evidence require source/ground DOB verification |

## Sync And Cron Architecture

Use scheduled workers, not manual formulas, as the source of truth.

Current executable bootstrap:

```bash
make legacy-god-sheet-sync-dry-run
make legacy-god-sheet-sync-apply
```

The current implementation creates the managed workbook tabs, writes the
control/source catalog rows, seeds validation rules and open issue categories,
and appends `Sync_Runs`. It is idempotent: non-empty human/data tabs are always
preserved. `--replace-managed-tabs` refreshes only seed/config tabs
(`README_Problem_Statement`, `Source_Catalog`, `Validation_Rules`, and
`Species_Taxonomy_Crosswalk`) and never clears `Animal_Master`, `Issue_Queue`,
`Mapping_Crosswalks`, event/history tabs, raw snapshots, or import batches.
Each run records a hash of the actual managed tab/column schema in
`Sync_Runs.schema_hash`, not a static version string. Apply mode also verifies
the Google ADC principal against Mesha/VGoats allowlists before writing and
uses bounded retry/backoff for transient Sheets API `429`/`5xx` responses. Live
source extraction and row-level normalization are the next implementation slice;
Goat OS DB import remains blocked until the RFID/DOB/sex/species/location gates
pass.

Recommended control plane:

```text
Cloud Scheduler
-> Cloud Run Job: legacy-god-sheet-sync
-> read-only Sheets/Drive/BigQuery extractors
-> immutable raw snapshots
-> normalized staging tabs
-> validation engine
-> Issue_Queue and RAG status refresh
-> optional Slack summary for new RED rows
-> Goat OS import preview only for GREEN rows
```

Recommended schedules in `Asia/Kolkata`:

Operational assumption: farm/source teams may submit Slack forms or update
source sheets during night operations, and current night-shift updates can land
until about `01:00 IST`. The first full sync should therefore run after that
window, with later pre-audit refreshes catching late corrections and same-date
row drift.

During cleanup and demo preparation, run frequent validation. The `05:30` job is
the complete daily fill. Hourly/daytime jobs are lighter refreshes that detect
changed rows, source freshness, drift, and issue status without treating the
source day as newly closed.

| Time | Job | Purpose |
| --- | --- | --- |
| 05:30 | `legacy-god-sheet-sync-full` | Full daily fill. Pull previous business day's complete source snapshots after night updates and sheet automations settle. |
| 06:00-23:30, every 60 minutes | `legacy-god-sheet-validate-hourly` | Light validation refresh. Re-read source watermarks/hashes, refresh changed source slices, recompute RED/AMBER/GREEN, and catch late source corrections or same-date row drift. |
| 11:30 | `legacy-god-sheet-pre-audit-check` | Forced validation before the noon dashboard audit; fail loudly if source access, drift, or P1 import blockers changed. |
| 17:30 | `legacy-god-sheet-pre-evening-check` | Forced validation before the 18:00 dashboard audit; catch same-day fixes and prevent stale Slack reporting. |
| Event-triggered, debounced 5-10 minutes | `legacy-god-sheet-source-change-refresh` | Optional trigger from Slack form/App Script/Sheet write events. Refresh only affected source families, then recompute dependent validation and issues. |
| Manual | `legacy-god-sheet-sync-on-demand` | Re-run after ground/source fixes or before import preview. |

Frequent validation jobs may update the god sheet's snapshots, normalized rows,
status columns, and `Issue_Queue`. They must not edit legacy source sheets, and
they must not commit data into Goat OS. Goat OS import remains a separate
approved preview-and-commit flow for selected GREEN batches.

Each run must:

1. Verify account/project/org context before reading or writing sources.
2. Read the `Source_Catalog`.
3. Pull BigQuery rows by explicit business date and source watermark.
4. Pull Google Sheets tabs by spreadsheet id, gid, and header hash.
5. Pull Apps Script/native-table loader lineage so native BigQuery tables have
   sheet/job provenance.
6. Write raw snapshots with `run_id`, `source_row_id`, and `source_row_hash`.
7. Normalize records into staging/domain tabs.
8. Recompute `Identity_Grain_Audit`.
9. Recompute `Mapping_Crosswalks`.
10. Recompute all `Validation_Rules`.
11. Update `Issue_Queue`.
12. Update `Animal_Master` and domain tabs.
13. Update `Sync_Runs` with source hash, actual schema hash, and Cloud
    Run/BigQuery job ids; log and verify writer context before the write.
14. Alert if sync failed, source schema changed, source is stale, or new RED
    blockers appear.

## Import Policy

Do not import directly from Sheets to Postgres.

Goat OS import flow:

1. Sheet marks candidate rows GREEN.
2. Importer calls Goat OS bulk preview API.
3. Preview validates required fields, identifier uniqueness, current lifecycle,
   park/shed validity, evidence refs, and vaccination trust.
4. Preview result is written to `Import_Batches`.
5. Only approved preview batches can commit through Goat OS backend APIs.
6. Import requests use idempotency keys.
7. Goat OS response IDs are written back to `goat_os_animal_id` and
   `Import_Batches`.

## Two-Day Vaccination Demo Scope

For demo readiness, prioritize:

1. `Animal_Master` for active animals.
2. `Mapping_Crosswalks` for identifiers.
3. `Identity_Grain_Audit` for procurement/GOATS DB identity-grain decisions.
4. `Species_Taxonomy_Crosswalk` for Goat OS `species`.
5. `Location_Profile` and `Current_Location_Status` for farm/park/shed/stage.
6. `Vaccination_History`.
7. `Vaccination_Due_View`.
8. `Issue_Queue`.

Demo boundary:

- Until Drive/Sheets access can read the RFID source-of-truth `Combined` tab
  plus any fallback old-tag/new-tag source columns for `animal_identifier_1`,
  the demo is a god-sheet readiness demo: `Vaccination_Due_View`,
  RED/AMBER/GREEN status, and cleanup queue.
- Goat OS DB import is not part of that demo unless `animal_identifier_1`,
  species, sex, DOB/approved estimated DOB, and current location all pass the
  import gate.
- Do not create a demo-only provisional `animal_identifier_1` from legacy
  `goat_id`/`farm_goat_id`/`inp_goat_id`. The current temporary identifier
  exception applies only to missing `animal_identifier_2`.

Do not block the vaccination demo on closing every procurement/fattening
aggregate issue. Instead:

- keep unresolved procurement/stage issues RED/AMBER in the queue,
- do not import affected animals until their row-level evidence is clean,
- keep vaccination due work honest by treating untrusted history as review or
  catch-up, not completed vaccination.

## Acceptance Criteria Before Goat OS DB Seed

Minimum import gate:

- All imported rows have `animal_identifier_1`.
- No duplicate normalized identifiers across current and historical identifiers.
- No active imported rows without `species`, `sex`, `dob` or approved estimated
  DOB, `origin_type`, `entry_date`, park, shed, and evidence refs.
- No dead/sold animals included in active import.
- No vaccination due suppressions from untrusted vendor/legacy notes.
- No RED rows in selected import batch.
- All AMBER rows are explicitly allowed by a temporary migration rule.
- Goat OS preview API passes for the batch.

## Implementation Phases

### Phase 0: Source Inventory

- Add every known sheet/table to `Source_Catalog`.
- Add Drive/Sheets read credential with explicit scopes.
- Block animal-level import preview until the sync can query the Drive/Sheets
  RFID/tag sources, including the RFID source-of-truth `Combined` tab and
  fallback mappings such as `goatsDB.goatsDB_rfid_mapping`, and record
  RFID/Gender/tag provenance.
- Capture Apps Script and native BigQuery loader lineage for `*_clean` and
  `*_dev` tables.
- Compute schema/header hashes.
- Confirm source owners and freshness SLA.

### Phase 1: Snapshot And Normalize

- Build scheduled extractors.
- Store immutable raw snapshots.
- Normalize identifiers, locations, lifecycle events, and vaccination evidence.
- Do not overwrite human review columns.

### Phase 2: Validation And Cleanup Queue

- Implement validation rules.
- Generate `Issue_Queue`.
- Add RED/AMBER/GREEN conditional formatting from `verification_status`.
- Add Slack summary for new blockers.

### Phase 3: Vaccination Demo Readiness

- Populate `Vaccination_History` and `Vaccination_Due_View`.
- Mark trusted vs untrusted vaccination evidence.
- Preview Goat OS vaccination import rows.

### Phase 4: Controlled Goat OS Import

- Import only GREEN batches through Goat OS backend APIs.
- Write import results back to `Import_Batches`.
- Lock imported source rows against silent manual changes.

### Phase 5: Retire The God Sheet As Runtime Truth

- After Goat OS becomes canonical, the sheet remains only a review/export
  surface.
- Daily source-of-truth writes move to Goat OS APIs and event pipelines.
