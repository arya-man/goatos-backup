# Legacy God Sheet Import Plan

Last reviewed: 2026-07-09

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

## Current Dashboard Snapshot

The scheduled dashboard audit at `2026-07-09 18:02:49 IST` reported:

- Run id: `dashboard-audit-2026-07-09T12-31-48-057Z`
- Dashboard run date: `2026-07-09`
- Counts business date: `2026-07-08`
- Checks run: 22
- Passing checks: 17
- Failing checks: 3
- Warning checks: 2
- Critical findings: 0
- Open findings: 5

Verified dashboard count context for `2026-07-08`:

- Total active goats: `2147`
- Adults: `1317`
- Kids: `830`
- CBE total: `958`
- CPT total: `734`
- Holdings total: `455`
- Total weight: `57187.2 kg`
- Farm value: `33741807`

Important nuance: `/api/counts` uses `farm.daily_summary_dev` for top KPI
numbers and same-row adult/kid breakdowns. The raw
`ceo_dashboard.counting_db_with_holding_dev` rows for `2026-07-08` summed to
`2148` in read-only verification, so raw source rows cannot be treated as
canonical without reconciliation. The god sheet must store both the canonical
chosen value and source evidence.

Dashboard API and BigQuery reads can also differ by timestamp, rounding, or
pipeline freshness. The workbook must store the observed value, source
(`api`, `bq`, or `sheet`), query/run timestamp, and the selected canonical value
instead of overwriting one source with another.

Other verified audit values:

- Births total from `goatsDB.mother_kid_facts` where `is_birth = 1`: `1006`
- Sales animals from `salesDB.salesDB_clean`: `544`
- Sales value from `salesDB.salesDB_clean`: `7072579`
- Mortality total from `ceo_dashboard.mortality_total_dev`: `310`
- Procurement purchase total from `procurement_farm.procurement_dB_clean`:
  `1670`
- GOATS DB purchase identity counts from `goatsDB.goats_db_clean`:
  - distinct `farm_goat_id`: `1822`
  - distinct global `goat_id`: `1738`
  - distinct `inp_goat_id`: `1617`
  - audit coalesced/farm-scoped definition: `1822`
- Procurement headline delta depends on chosen identity grain:
  - `1822 - 1670 = 152` at farm-scoped/coalesced grain
  - `1738 - 1670 = 68` at global `goat_id` grain
- Fattening vs shifting source mismatch:
  - CBE K2: fattening source `52`, current-stage source `57`
  - CBE K3: fattening source `10`, current-stage source `0`

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
- On 2026-07-09, the business date used by the scheduled audit was
  2026-07-08.
- A partial 2026-07-09 `daily_summary_dev` row existed for Holdings only, so
  the god sheet must use explicit business-date watermarks and not blindly use
  the max date from every source.

## Dashboard Audit Coverage

The scheduled audit currently checks 18 dashboard API endpoints:

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
| GOATS DB RFID mapping | `goatsDB.goatsDB_rfid_mapping` |
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
| Dashboard users/RBAC sheet | https://docs.google.com/spreadsheets/d/13TtoRv0pKYtatsuc3-YDZHcTdWALekBd-e-WMblrniY/edit |
| Dashboard users/RBAC table | `ceo_dashboard.dashboard_users` |
| History automation dataset | `historyAutomation` |
| History automation complete history view | `historyAutomation.goat_history_complete` |

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

- `animal_identifier_1` is required.
- `animal_identifier_2` is optional until double RFID tagging is live.
- If both identifiers are present, they must be different.
- Identifier values are globally single-use for life.
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

`run_id`, `source_id`, `business_date`, `source_row_id`, `source_row_hash`,
`raw_payload_json`, `snapshot_created_at`.

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
`photo_url`, `source_record_id`, `source_links`, `evidence_refs`,
`verification_status`, `issue_reason`, `ground_owner`, `last_verified_at`,
`ready_for_goat_os_import`.

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

Initial rows must include `goatsDB.goats_db_clean` purchase identity counts:

- distinct `farm_goat_id`: `1822`
- distinct global `goat_id`: `1738`
- distinct `inp_goat_id`: `1617`
- audit coalesced/farm-scoped definition: `1822`

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

### 10. `Current_Location_Status`

Animal-level current location and current status resolution.

Columns:

`animal_identifier_1`, `animal_identifier_2`, `current_farm`, `current_park`,
`current_shed`, `current_stage`, `source_priority_used`, `counting_db_status`,
`goats_db_status`, `shiftings_status`, `procurement_status`,
`resolved_status`, `resolution_reason`, `verification_status`.

### 11. `Birth_Events`

Columns:

`event_id`, `kid_identifier`, `mother_identifier`, `birth_date`, `farm`,
`shed`, `breed`, `kid_status`, `source_system`, `source_record_id`,
`source_link`, `evidence_refs`, `verification_status`.

### 12. `Breeding_Delivery_History`

Tracks breeding, mating, delivery, and related medicine/source evidence that may
affect birth provenance, dam/lactation status, and future Goat OS reproductive
history.

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `event_type`,
`event_date`, `farm`, `shed`, `mate_or_sire_identifier`, `delivery_outcome`,
`medicine_or_protocol`, `source_system`, `source_record_id`, `source_link`,
`verification_status`.

### 13. `Death_Events`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `death_date`,
`cause`, `farm`, `source_system`, `source_record_id`, `source_link`,
`verified_by`, `verification_status`.

### 14. `Sale_Events`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `sale_date`, `farm`,
`buyer_or_vendor`, `sale_amount`, `sale_weight_kg`, `sales_id`,
`source_record_id`, `source_link`, `verification_status`.

### 15. `Procurement_Source_Entry`

One row per procurement source/load candidate.

Columns:

`load_id`, `source_party`, `source_location`, `animal_identifier_1`,
`animal_identifier_2`, `species`, `sex`, `breed`, `purchase_date`,
`loaded_at`, `arrived_at`, `accepted_intake_at`, `source_entry_state`,
`current_state`, `ownership_state`, `health_state`, `warmup_started_at`,
`warmup_ended_at`, `holding_location`, `proof_refs`, `verification_status`.

### 16. `Procurement_Load_Reconciliation`

Used to close the current procurement mismatch. Do not assume the headline
`1670` vs `1822` is a clean animal-level delta until `Identity_Grain_Audit` and
`Mapping_Crosswalks` decide the canonical grain.

Columns:

`load_id`, `procurement_db_count`,
`goats_db_purchase_distinct_farm_goat_id`,
`goats_db_purchase_distinct_goat_id`,
`goats_db_purchase_distinct_inp_goat_id`,
`goats_db_purchase_distinct_coalesced`, `loadwise_status_rows`,
`loadwise_summary_accounted_count`, `delta_by_canonical_id`,
`unmatched_procurement_ids`, `unmatched_goats_db_ids`, `owner`, `next_action`,
`verification_status`.

### 17. `Movement_Stage_History`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `from_farm`,
`to_farm`, `from_shed`, `to_shed`, `from_stage`, `to_stage`, `shift_date`,
`stage_entry_date`, `days_in_stage`, `source_system`, `source_record_id`,
`source_link`, `verification_status`.

### 18. `Fattening_Shifting_Reconciliation`

Columns:

`farm`, `stage`, `fattening_source_count`, `shiftings_current_stage_count`,
`delta`, `candidate_animal_ids`, `candidate_load_ids`, `source_decision`,
`owner`, `next_action`, `verification_status`.

### 19. `Weight_History`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `weight_date`,
`weight_kg`, `farm`, `shed`, `stage`, `source_system`, `source_record_id`,
`source_link`, `verification_status`.

### 20. `Health_Treatment_History`

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `problem`,
`diagnosis`, `treatment`, `medicine`, `dose`, `follow_up_date`, `vet_or_staff`,
`source_system`, `source_record_id`, `source_link`, `verification_status`.

### 21. `Milk_Lactation_History`

Tracks milk consumption, feeding summaries, milking-mother rows, and lactation
signals needed for Goat OS reproductive and nutrition history.

Columns:

`event_id`, `animal_identifier_1`, `animal_identifier_2`, `event_type`,
`event_date`, `farm`, `shed`, `lactation_status`, `milk_quantity_l`,
`feeding_quantity_l`, `kid_identifier`, `source_system`, `source_record_id`,
`source_link`, `verification_status`.

### 22. `Vaccination_History`

One row per actual vaccination evidence item. Do not use one column per vaccine.

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

### 23. `Vaccination_Due_View`

This is the demo-critical view.

Columns:

`animal_identifier_1`, `animal_identifier_2`, `goat_os_animal_id`, `species`,
`breed`, `sex`, `dob`, `age_days`, `farm`, `park`, `shed`, `stage`,
`vaccine_code`, `vaccine_name`, `dose_code`, `last_trusted_dose_date`,
`next_due_date`, `latest_safe_date`, `due_status`, `due_reason`,
`blocking_issue_id`, `verification_status`.

### 24. `Counts_Snapshots`

Columns:

`business_date`, `source_id`, `farm`, `park`, `shed`, `stage`, `breed`,
`age_class`, `source_count`, `canonical_count`, `delta`, `source_link`,
`verification_status`.

### 25. `Feed_Sales_Reconciliation`

Columns:

`month`, `farm`, `sales_animals`, `sales_value`, `feed_spend`, `source_sales`,
`source_feed`, `delta`, `verification_status`.

### 26. `Validation_Rules`

Columns:

`rule_id`, `rule_name`, `severity`, `blocking`, `owner`, `source_family`,
`description`, `check_type`, `sql_or_formula_ref`, `expected_result`,
`failure_message`, `last_run_id`, `last_status`.

### 27. `Issue_Queue`

The real cleanup control center. One row per issue.

Columns:

`issue_id`, `issue_type`, `severity`, `blocking`, `animal_identifier_1`,
`animal_identifier_2`, `load_id`, `source_id`, `source_record_id`,
`evidence`, `owner`, `next_action`, `sla_date`, `status`, `resolved_by`,
`resolved_at`, `resolution_note`.

### 28. `Import_Batches`

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

| Finding | Severity | Owner | Evidence |
| --- | --- | --- | --- |
| Counting DB kid-stage rows need age-field source verification | P2 | ground/source team | Counting DB / `ceo_dashboard.counting_db_with_holding_dev`; audit reported 34 kid/fattening-stage goats not marked `age=Kid` |
| Customized age-only risk signature needs UI regression coverage | P2 | data/dev | Strict Core + Adults + Female + Non-Pregnant source `0`; age-only risk signature `1622`; stage-aware would be `1656`; Core adult Non-Pregnant all gender `794` |
| Procurement DB vs farm procured animal count mismatch | P1 | data/dev + ground/source team | `procurement_farm.procurement_dB_clean` purchase total `1670`; `goatsDB.goats_db_clean` purchase distinct `farm_goat_id`/audit-coalesced count `1822` gives headline delta `152`, while distinct global `goat_id` count `1738` gives delta `68`; resolve identity grain in `Identity_Grain_Audit` before treating rows as import blockers |
| Procurement loadwise status rollup mismatch | P2 | data/dev + ground/source team | `procurement_farm.load_wise_procurement_with_status` vs `procurement_farm.loadwise_summary`; Goat/Sheep/Unknown status rows do not reconcile |
| Fattening vs shiftings stage-source reconciliation | P2 | data/dev + ground/source team | CBE K2 `52` vs `57`; CBE K3 `10` vs `0` |

## Sync And Cron Architecture

Use scheduled workers, not manual formulas, as the source of truth.

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

| Time | Job | Purpose |
| --- | --- | --- |
| 05:30 | `legacy-god-sheet-sync-full` | Pull previous business day's complete source snapshots after sheet automations settle. |
| 11:30 | `legacy-god-sheet-pre-audit-check` | Refresh validation before the noon dashboard audit. |
| 17:30 | `legacy-god-sheet-pre-evening-check` | Refresh validation before the 18:00 dashboard audit. |
| Manual | `legacy-god-sheet-sync-on-demand` | Re-run after ground/source fixes or before import preview. |

Each run must:

1. Verify account/project/org context before reading sources.
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
13. Update `Sync_Runs`.
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

For the demo by 2026-07-11, prioritize:

1. `Animal_Master` for active animals.
2. `Mapping_Crosswalks` for identifiers.
3. `Identity_Grain_Audit` for procurement/GOATS DB identity-grain decisions.
4. `Location_Profile` for farm/park/shed/stage.
5. `Vaccination_History`.
6. `Vaccination_Due_View`.
7. `Issue_Queue`.

Do not block the vaccination demo on closing every procurement/fattening
aggregate issue. Instead:

- keep unresolved procurement/stage issues RED/AMBER in the queue,
- do not import affected animals until their row-level evidence is clean,
- keep vaccination due work honest by treating untrusted history as review or
  catch-up, not completed vaccination.

## Acceptance Criteria Before Goat OS DB Seed

Minimum import gate:

- 100 percent of imported rows have `animal_identifier_1`.
- 0 duplicate normalized identifiers across current and historical identifiers.
- 0 active imported rows without `species`, `sex`, `dob` or approved estimated
  DOB, `origin_type`, `entry_date`, park, shed, and evidence refs.
- 0 dead/sold animals included in active import.
- 0 vaccination due suppressions from untrusted vendor/legacy notes.
- 0 RED rows in selected import batch.
- All AMBER rows are explicitly allowed by a temporary migration rule.
- Goat OS preview API passes for the batch.

## Implementation Phases

### Phase 0: Source Inventory

- Add every known sheet/table to `Source_Catalog`.
- Add Drive/Sheets read credential with explicit scopes.
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
