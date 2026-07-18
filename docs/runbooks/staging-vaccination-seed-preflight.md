# Staging Vaccination Seed Preflight

Run this before any `goatos-stg` vaccination seed. Do not seed staging when a
blocker below is red.

For the compact-safe resume handoff, source manifest, last observed staging
state, and exact stop conditions, read:

```text
docs/runbooks/staging-vaccination-seed-resume-2026-07-11.md
```

For binding source-date semantics, read:

```text
docs/runbooks/vaccination-seed-source-date-contract.md
```

For any destructive staging rebuild, follow the canonical sequence and
postflight gates in:

```text
docs/runbooks/staging-vaccination-clean-slate.md
```

## Source Of Record

- Use `Vaccination_DB_-V2` / `Demo DB` as the vaccination seed source. Demo DB
  must stay an exact staging workbook derived from V2, not an independently
  edited copy.
- Vaccination date cells are base schedule anchors, not due dates. Dates on or
  before the backend business date seed trusted anchor history, and the kernel
  may materialize only strictly future open work from that anchor. Future source
  dates need reviewed semantics or are blocked from production seed; they must
  not be marked late merely because they appear in the sheet.
- Current verified source counts: V2 has 1,311 animal rows, 1,310 unique
  nonblank primary animal IDs, and 1 blank-primary-ID-but-secondary-ID-present
  row. Demo DB has the same 1,311 vaccination rows, the same 1,310 nonblank
  primary IDs, and zero normalized vaccination-cell diffs from V2.
- Local must be a rehearsal of the staging seed. Before seeding `goatos-stg`,
  local must contain the exact same 1,311 source animal keys and no extra Goat OS
  animal keys.
- The known blank-primary-ID row is not identity-less: farm `CPT`, secondary ID
  source pieces `1388` and `BLR`, age `Kid`, gender `Male`, breed `Beetal`,
  tag value `K2`, and shed `Yashoda`. It may be seeded using the secondary
  animal ID as a real animal identifier; do not label it "missing ID".
  Only rows where both primary and secondary animal IDs are blank are identifier
  blockers.
- Primary and secondary source animal IDs are both valid animal identifiers. Use
  the primary ID as the join key where present. When primary ID is blank but a
  secondary ID exists, use the secondary ID as the available animal identifier.
  Never invent a composite key from farm + shed + age + gender + breed.
- Reviewed HRMS shed ownership source:
  `/Users/ravi/mesha/source-material/vgoats-seed/shed-manager-mapping.jul11-vaccination.csv`.
  This CSV is generated from the source `goats.json` shed list and reviewed
  roster rows, not from local database state.
  Validation on creation: 116 shed rows, 72 CBE sheds, 44 CPT sheds, 116 source
  goat sheds covered, 1,311 source goats covered, 0 missing source sheds, 0 extra
  seed sheds, 0 blank manager/backup values, and 0 rows failing the
  reviewed-source contract.
  Staging assignments are:
  - CBE sheds -> manager `Eshwar` (`HRMS-MANUAL-9030`), backup `Saheb mete`
    (`HRMS-JUN26-020`).
  - CPT sheds -> manager `Darshan Talwar` (`HRMS-JUN26-013`), backup
    `Amit Kumar` (`HRMS-JUN26-016`).

## Strict Blockers

- Founder/builder visibility: `ravi@mesha.sg`, `manohark@mesha.sg`,
  `manju@mesha.sg`, `abhishek@mesha.sg`, and `aryaman@mesha.sg` must be
  `ceo_internal`, tenant-scoped, and must have every built visible module
  available, including `admin.people`.
- Identity columns: Goat OS `display_id` is the short readable internal ID.
  `animal_identifier_1` and `animal_identifier_2` are real-world animal ID
  slots. UI labels must be `Display ID`, `Tag 1`, and `Tag 2`. Missing values
  render `-`. Never render a separate "missing ID" badge/chip.
- Vaccination grouping: the main vaccination UI is shed-wise. Do not show
  `cohort`, `partition`, or vaccine-wise rows on the main page. Park owns sheds;
  sheds contain animals; vaccination planning and execution is shed-wise.
- Protocol shape: real vaccination seed must publish one canonical
  `vaccination.matrix` version (`V1 Real Vaccination`) containing the
  vaccination rules. Do not create separate obligation families such as
  `vaccination.ppr`, `vaccination.fmd`, or "Real herd import" protocols for the
  same real seed.
- Config/SOP visibility: the staging demo must not show old authoring/import
  debris in the normal UI. The default Protocol Rules page should expose one
  active vaccination matrix entry for the real seed, and the SOP Library should
  expose one intended vaccination SOP. Retired/history rows may exist only if the
  UI hides them from the default business view or clearly separates them as
  archive/history.
- Source identifier completeness: reject or fix goat rows only when both primary
  and secondary animal IDs are blank. The known blank-primary-ID row
  `CPT / 1388 / BLR / K2 / Yashoda` still has a secondary identity.
- Farm/shed placement: every imported goat must resolve to a real park and shed.
  Blank shed means the animal is rejected from the seed.
- Shed manager/backup ownership: reuse the existing workforce Position model;
  do not create a separate vaccination-owned shed ownership table. Manager is
  the shed-scoped Position holder. Backup is the center/park backup-manager slot
  unless a real shed-specific backup exists. `Manager: unassigned`,
  `owner missing`, or missing backup is a staging seed blocker, not a normal UI
  business state. Use the reviewed shed ownership CSV above as an override. If
  a new source refresh adds sheds before the shed CSV is regenerated, the seed
  must materialize those shed seats from the reviewed park
  `preventive_care_manager` position (for example Darshan for CPT, Eshwar for
  CBE). If a shed code or manager code present in the CSV does not resolve in
  staging, stop; if a park has no preventive-care manager or backup-manager
  position, stop. Never let the vaccination UI surface an unassigned manager as
  normal business state.
- HRMS/vaccination ownership source: the real roster seed must include the
  vaccination-relevant people from the roster discussion: Health Managers and
  Health AMs, with known examples such as Darshan for CPT and Eshwar for CBE
  when confirmed by the source sheet/WhatsApp owner. These must be represented as
  workforce members/positions with the right center/shed responsibility and
  backup chain, because vaccination execution and the mobile app read this
  ownership data. The current reviewed staging overlay is
  `shed-manager-mapping.jul11-vaccination.csv`; if it changes, regenerate and
  revalidate coverage before seeding.
- Sex and dates: sex must be explicit `Male`/`Female`; dates must be real
  `YYYY-MM-DD`. Do not let importer defaults hide source mistakes.
- Past vaccination source dates: every trusted animal-level source vaccination
  date is a base schedule start point. It must seed visible vaccination anchor
  history, not a new due/late card. For local/dev/stg/prod real-data seeds, if
  the matching `vaccination.matrix` rule exists, the seed may persist the anchor
  as accepted history/completion evidence with `administered_at = source date`
  so recurrence can schedule from it, but open work materialized from that
  anchor must be strictly future-only. If the rule does not exist yet, the old
  date must still show in Passport/Vaccination/Action Center as history plus a
  config/review gap; do not fabricate open obligations. When the rule is later
  published, the next generation/reconciliation run must schedule from that
  preserved past date without turning it into overdue/late work.
- Workflow links: Passport/history links may point only to real workflow rows.
  Source obligation IDs are lineage, not clickable workflow records.
- No E2E/story/dev data in shared staging: reject seed inputs, protocol codes,
  idempotency keys, or labels containing `e2e`, `story`, `stub`, `dummy`,
  `local`, `test`, `goatos-pgtest`, or `seed-calendar-vaccination-dev` unless a
  migration note explicitly proves they are production-safe.
- Action Center counts must be explained before staging. The sidebar badge is
  grouped open process-integrity work, not raw goat count. Export the counts by
  obligation status, dose rule, distinct goats, and grouped work state before
  importing into `goatos-stg`.
- Source `Pending` cells have one writer only: the vaccination kernel. Zero
  seed-owned open `vacc-real-obl:%` placeholders and zero duplicate active
  goat/rule pairs are hard staging gates.
- Accepted history wins over missing scheduling anchors. A completed source
  dose must not become a deferred missing-DOB/missing-entry-date obligation.
- Missing scheduling anchors are checked by trigger, not globally. `birth_age`
  rules require DOB, `post_arrival` rules require entry date, and
  `after_previous_completion` rules require accepted completion evidence. A
  rule must not create normal active work when its own anchor is missing.
- Sidebar counts must be backend-computed, never seeded constants. After seed,
  `Action Center`, `Preventive Care (PC)`, and `Vaccination` badges must be
  reconciled to the same grouped open-work query used by the page list. A number
  such as `661` is acceptable only when the query proves 661 open grouped work
  items; otherwise it is a seed/projection bug.
- Shed-row counts are animal-level: `Animals` is alive animals in the shed,
  `Due` is distinct animals with at least one pending/due/overdue/in-progress
  vaccination item, and `Done = Animals - Due`. Per-vaccine obligation counts
  belong only inside the shed detail page.
- Work-session capacity config is a staging blocker. Daily capacity must be
  authored in the same vaccination matrix/rule config flow, with fields for
  `max vaccinations per day`, `capacity scope`, `max buffer days`, and
  `overflow policy`. Do not hide this cap in Calendar, Action Center, env vars,
  or a separate ops-only settings page.
- `max buffer days` is fixed at 7 for staging unless Ravi explicitly changes the
  business rule. It means the planner may split work up to 7 days after the first
  due date before flagging `Needs review`; it does not replace vaccine medical
  windows or cross-vaccine spacing rules.
- Capacity counts vaccination administrations, not animals. One goat receiving
  FMD + HS counts as 2 vaccination cells. Medical due windows, cross-vaccine
  spacing, and max buffer days override the cap when needed; if the cap cannot
  fit all due work inside the safe window, the planner must mark a capacity
  exception.
- Future scheduling from seeded history must run through the same constraints as
  normal runtime generation: active matrix scope, species/breed/sex/stage,
  lifecycle/death/sold state, current park/shed, sick/ICU/quarantine defer
  states, pregnancy/lactation holds, procurement warm-up, min-gap and
  cross-vaccine spacing, latest safe date, inventory/FEFO availability,
  manager/backup ownership, daily capacity, max buffer days, and session split
  policy. A past trusted completion is only the anchor; it does not override any
  current constraint.
- Draft rule publish must show an impact preview before activation: total cells,
  cap per day, number of sessions, per-day counts, capacity state, and latest
  safe date.
- Built (2026-07-11, read side): the deterministic session-split planner plus a
  tenant-default cap config (`vaccination_capacity_config`: 100 vaccinations/day,
  tenant scope, 7 buffer days, split-within-safe-window-then-mark-needs-review;
  migrations through 000158). The shed-wise read model now returns backend-computed
  `Sessions`, per-day `Planned sessions`, `Capacity` (within_cap/over_cap/
  capacity_breach), and a merged `Status` headline.
- Built (2026-07-11, shed-wise + capacity UI): `/vaccination` is now the shed-wise
  board (`features/vaccination-sheds/shed-board.tsx`, 10 columns Park…Sessions…
  Status, server-side park/status/capacity/search filters + offset pagination,
  Sessions info tooltip) and the shed detail
  (`features/vaccination-sheds/shed-detail.tsx`: Planned sessions table above the
  vaccine breakdown, Capacity info tooltip, animal roster) at
  `/vaccination/execution/sheds/{shed_id}`. All labels are backend-driven via the
  `vaccination` + `shed-execution` page contracts (`shed_status_chips`,
  `capacity_chips`, tooltip copy) in `backend/internal/adminui/app/service.go`; no
  internal enum token (`within_cap`/`over_cap`/`capacity_breach`) or the word
  `state` is rendered. Local E2E verified against the real seed (109 sheds,
  capacity filter → breach shed → 7-day planned split with first days Within cap
  then Needs review, both tooltips, back-nav preserves filter/page state). The old
  cohort/vaccine-wise status matrix + per-cohort detail + drive shed-event board
  are removed from the main page. STILL PENDING: the capacity-config authoring UI
  inside the matrix/rule flow, the draft-publish impact preview, Calendar/Action
  Center work-session rows, and the animal-roster Breed/Sex/Age columns (contract
  gap — the roster endpoint returns Display ID/Tag 1/Tag 2/Vaccination status
  only).

## UI And E2E Gate

Do not seed staging for demo until the local admin-web vaccination slice passes
this smoke/e2e gate with the exact staging seed source:

- Sidebar `Preventive Care (PC) > Vaccination` opens the vaccination page.
- Main vaccination page shows one row per shed, not one row per vaccine.
- Main table columns are exactly: `Park`, `Shed`, `Animals`, `Due`, `Done`,
  `Sessions`, `Next due`, `Manager`, `Backup`, `Status` (10 columns; `Last done`
  is not a main-table column).
- No `cohort`, `partition`, `action`, `owner missing`, `missing ID`,
  `source obligation`, workflow row ID, or internal row key appears in the main
  vaccination UI.
- Filters work server-side: park, shed, status, and search.
- Pagination works server-side: default 25 rows, page-size options 25/50/100,
  next/previous, and displayed range such as `1-25 of N sheds`.
- Sorting is stable: overdue first, due second, scheduled third, done last, then
  park and shed.
- Row click opens shed detail.
- Shed detail shows vaccine breakdown for that shed.
- Shed detail animal list shows `Display ID`, `Tag 1`, `Tag 2`, `Breed`, `Sex`,
  `Age`, and `Vaccination status`.
- Missing `Tag 1` or `Tag 2` values render `-`; no missing-ID chip is rendered.
- Back navigation from shed detail returns to the same filtered/paginated shed
  list state.
- Sessions column renders the planned visit count (usually 1; 2/3/... when the
  daily cap forces a split). The info icon beside Sessions opens a tooltip
  explaining Goat OS splits a shed's work across days when the daily limit is
  reached, and that one goat getting two vaccines counts as two vaccinations.
- Capacity filter (All / Within cap / Split / Needs review) filters the shed
  list server-side.
- Status is the merged CEO headline (priority: Needs review > Split > Overdue >
  Due > Scheduled > On track). The word `state` never appears in the UI.
- Shed detail shows a Planned sessions table ABOVE the vaccine breakdown, with
  columns Session date / Vaccinations / Daily limit / Capacity, and an info icon
  beside Capacity whose tooltip explains the daily limit, FMD + HS on one goat =
  2 vaccinations, and Needs review when work cannot fit the safe window.
- No internal capacity enum name (`within_cap`, `over_cap`, `capacity_breach`)
  and no `state` token is ever rendered; only the labels Within cap / Split /
  Needs review appear.
- Date range controls stay hidden until backend-owned range semantics exist.
- Config UI exposes the daily capacity fields inside the vaccination matrix/rule
  config flow.
- Config UI has an info popover explaining that the cap counts vaccination cells,
  not animals, and that FMD + HS on one goat counts as 2 vaccinations.
- Draft publish impact preview shows the planned session split before publish.
- Local E2E proves:
  - FMD + HS combo in one shed work session;
  - mixed goat-vaccine matrix cells;
  - per-cell replay/idempotency;
  - 300 cells with cap 100/day -> 100/100/100;
  - 250 cells with cap 100/day -> 100/100/50;
  - capacity breach when safe window cannot fit the cap;
  - cross-shed/team capacity scope;
  - Calendar and Action Center rows for dated work sessions;
  - config info popover and impact preview evidence.

## Pub/Sub And Projection Gate

Do not assume DB seed rows automatically update every read model. Before or
immediately after staging seed, verify the event/projection chain:

- Config impact preview uses the `vaccination_eligibility_rollups` read model,
  not a live `goats` scan. After seed/import and before preview or demo
  verification, run the backend recompute entry point
  `backend/cmd/vaccination-eligibility-rollup-recompute` for the target tenant.
  The UI and `POST /protocols/vaccination/impact-preview` may only read this
  rollup. They must not write it and must not scan animal rows on the request
  path. If the migration/table or CLI entry point is missing, staging seed is
  blocked until it lands.
- Pub/Sub topics exist for staging domain/outbox events.
- Subscriptions exist for every staging consumer and have DLQ routing.
- Cloud Run services/jobs that publish or consume events are deployed and
  healthy.
- Cloud Scheduler jobs for sweepers/projectors are enabled.
- IAM allows Scheduler/Cloud Run service accounts to publish, subscribe, and
  ack messages.
- Trigger or wait for the required staging jobs:
  - obligation sweeper
  - counts projection recompute
  - vaccination eligibility rollup recompute
  - outbox publisher if it is a separate service/job
- A seed is incomplete if any live vaccination API fails to return canonical
  data. After the source seed, these endpoints must return `200` from the
  target tenant without any projector warmup:
  - `GET /vaccination/sheds`
  - `GET /vaccination/execution`
  - `GET /vaccination/operations`
  - `GET /vaccination/schedule`
- Verify DLQ/dead-letter counts are zero or explicitly explained.
- Verify Action Center, Calendar, Protocol Adherence, Workflows, and the
  shed-wise vaccination page match the seeded DB counts.

## Local Preflight Snapshot - 2026-07-11

Latest local rehearsal state before staging seed:

- Source identity parity is green: source Tag 1 set has 1,311 keys, local
  `animal_identifier_1` has the same 1,311 keys, with zero missing and zero
  extra. The one blank-primary-ID source row maps to Tag 1 `BLR-1388`.
- Source secondary identity parity is green: source Tag 2 set has 1,122 keys,
  local `animal_identifier_2` has the same 1,122 keys, with zero missing and
  zero extra.
- Local goat counts match the intended seed: 1,311 total goats, 1,192 alive,
  78 sold, and 41 dead. No local `display_id` contains a long source primary ID.
- Local vaccination math matches the workbook: 5,860 due obligations, 3,836
  accepted completion rows, and 9,696 total seeded vaccination obligations. The
  accepted completion rows come from source last-administered dates; they are
  done/history anchors, and the due rows are next-cycle work derived after the
  matrix and constraints are applied.
- Local protocol shape is green: exactly one non-retired vaccination protocol is
  published, `vaccination.matrix / V1 Real Vaccination`.
- Capacity config must be staging-correct before seed: tenant cap is configurable
  and defaults to 100 vaccinations/day, tenant scope, and
  `max_buffer_days = 7`. Any local or staging row with buffer 3 is stale and must
  be corrected before demo.

Current red items in the local rehearsal. These do not block GCP/staging
infrastructure setup, and they do not automatically mean a clean staging seed
would inherit the same data. They do block treating local as a staging template,
and the staging seed must pass equivalent checks before demo:

- Local DB does not yet contain `vaccination_eligibility_rollups`; the recompute
  CLI path exists in the working tree, but the local table is not applied or
  populated. Config preview must not hit staging until this read model is green.
- Capacity config is red anywhere `max_buffer_days` is not 7. Earlier local
  rehearsals had 3; staging must not.
- Local SOP data is polluted: 12 non-retired vaccination SOP versions exist,
  including 11 old `vaccination.authoring_*` rows. Staging should have one
  intended vaccination SOP version, not authoring debris.
- Local Protocol Rules UI can show many rows when it includes retired/archive
  rows. That is not acceptable for the default staging demo view. Confirm the
  default UI filters to the one active real vaccination matrix, or explicitly
  hides/archive-separates retired rows before staging.
- Historical local outbox was not clean: 3 failed rows (`config.changed` x2 and
  one obsolete pre-fix regenerated-obligation envelope, all
  `invalid_event_envelope`) plus 126 pending rows. Current staging preflight must
  either drain live paths or prove they are intentionally excluded; terminal
  obligations are no longer rewritten into regenerated scheduled work.
- Calendar projection count is inconsistent: local has 5,854
  `vaccination_dose_due` rows plus 9 `vaccination_drive` rows, while seeded due
  obligations are 5,860. This must be reconciled before staging demo.
- Workforce ownership seed source is now explicit:
  `/Users/ravi/mesha/source-material/vgoats-seed/shed-manager-mapping.jul11-vaccination.csv`.
  If local/staging still shows center-scoped-only positions or zero shed-scoped
  manager positions after running the seed, that is an execution failure, not a
  missing source-data decision.
- Founder/builder grants are not locally proven: the five expected
  `@mesha.sg` pending email grants are absent in the local auth grant table.
- Goat `health_status` is blank for all 1,311 local rows. If the source health
  columns are meant to drive the UI, importer mapping must be fixed before
  staging.
- Identifier query compatibility: this local database uses
  `goat_identifiers.status = 'active'`; it does not have a boolean
  `goat_identifiers.active` column. Preflight SQL and seed checks must use the
  actual deployed schema for the target DB, not an assumed newer/older column
  name.

To make the red items green:

1. Apply staging-bound migrations locally through the eligibility-rollup
   migration, then run `backend/cmd/vaccination-eligibility-rollup-recompute` for
   the tenant. Verify `vaccination_eligibility_rollups` exists and has rows.
2. Seed/update capacity config through the real config path with
   `max_buffer_days = 7`, not 3.
3. Retire, purge, or hide the old `vaccination.authoring_*` SOP rows from the
   default business UI. Verify the default SOP Library shows one intended
   vaccination SOP.
4. Verify the default Protocol Rules UI shows one active real vaccination matrix
   entry. Retired/archive rows must not confuse the business view.
5. Drain or explain outbox rows. Before staging demo, failed rows must be zero
   and pending rows must either be zero or an intentional currently-running async
   queue with documented consumers.
6. Run the obligation sweeper and verify Calendar, Action Center,
   process-integrity, `/vaccination`, `/vaccination/execution`,
   `/vaccination/operations`, and `/vaccination/schedule` all return canonical
   backend rows. Do not wait for or recreate retired vaccination projections.
7. Seed HRMS vaccination ownership before vaccination demo from the reviewed
   roster mapping, attendance/leave, and timetable source files; create/resolve
   Preventive Care Manager and Backup Manager workforce members, then run
   `seed-shed-positions -mapping /Users/ravi/mesha/source-material/vgoats-seed/shed-manager-mapping.jul11-vaccination.csv -strict`
   to create shed-scoped `workforce_positions` for every vaccination shed. The
   mapping file is an override; any active shed omitted by a refreshed source is
   filled from that park's reviewed `preventive_care_manager`. Verify no
   vaccination shed renders `Manager: unassigned`, `owner missing`, or blank
   backup.
8. Seed founder/builder access grants for `ravi@mesha.sg`,
   `manohark@mesha.sg`, `manju@mesha.sg`, `abhishek@mesha.sg`, and
   `aryaman@mesha.sg` as tenant-scoped `ceo_internal` users with every built
   visible module.
9. Decide whether source health columns drive Goat OS `health_status`. If yes,
   fix importer mapping and verify nonblank health rows. If no, hide/label the UI
   so blank health is not presented as missing seed data.

Conclusion: local source animal/vaccination data is clean, but the local
kernel/read-model/SOP/outbox/workforce rehearsal is not clean enough to copy or
trust as the staging template. A clean `goatos-stg` seed may still proceed after
the staging seed scripts, migrations, and projections independently pass the
same gates.

## Clean Seed Checks

After local seed and before staging:

```sql
-- No long source primary IDs in short display IDs.
SELECT count(*) FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND display_id LIKE 'G-901%';

-- All alive seeded goats have at least one real-world animal identifier.
WITH goat_ids AS (
  SELECT g.goat_id,
         g.lifecycle_status,
         max(gi.identifier_value) FILTER (
           WHERE gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
         ) AS animal_identifier_1,
         max(gi.identifier_value) FILTER (
           WHERE gi.identifier_type = 'animal_identifier_2' AND gi.status = 'active'
         ) AS animal_identifier_2
  FROM goats g
  LEFT JOIN goat_identifiers gi
    ON gi.tenant_id = g.tenant_id
   AND gi.goat_id = g.goat_id
  WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'
  GROUP BY g.goat_id, g.lifecycle_status
)
SELECT count(*) FILTER (WHERE lifecycle_status = 'alive') AS alive,
       count(*) FILTER (
         WHERE lifecycle_status = 'alive' AND NULLIF(btrim(animal_identifier_1), '') IS NOT NULL
       ) AS alive_with_identifier_1,
       count(*) FILTER (
         WHERE lifecycle_status = 'alive' AND NULLIF(btrim(animal_identifier_2), '') IS NOT NULL
       ) AS alive_with_identifier_2,
       count(*) FILTER (
         WHERE lifecycle_status = 'alive'
           AND (
             NULLIF(btrim(animal_identifier_1), '') IS NOT NULL OR
             NULLIF(btrim(animal_identifier_2), '') IS NOT NULL
           )
       ) AS alive_with_any_identifier
FROM goat_ids;

-- This must return zero rows before staging.
WITH goat_ids AS (
  SELECT g.display_id,
         g.lifecycle_status,
         max(gi.identifier_value) FILTER (
           WHERE gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
         ) AS animal_identifier_1,
         max(gi.identifier_value) FILTER (
           WHERE gi.identifier_type = 'animal_identifier_2' AND gi.status = 'active'
         ) AS animal_identifier_2
  FROM goats g
  LEFT JOIN goat_identifiers gi
    ON gi.tenant_id = g.tenant_id
   AND gi.goat_id = g.goat_id
  WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'
  GROUP BY g.goat_id, g.display_id, g.lifecycle_status
)
SELECT display_id, animal_identifier_1, animal_identifier_2
FROM goat_ids
WHERE lifecycle_status = 'alive'
  AND NULLIF(btrim(animal_identifier_1), '') IS NULL
  AND NULLIF(btrim(animal_identifier_2), '') IS NULL;

-- Local rehearsal count. This must match the V2/Demo source animal-key count.
SELECT count(*) AS local_goats,
       count(DISTINCT gi.identifier_value) FILTER (
         WHERE gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
       ) AS local_distinct_primary_animal_ids
FROM goats g
LEFT JOIN goat_identifiers gi
  ON gi.tenant_id = g.tenant_id
 AND gi.goat_id = g.goat_id
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001';

-- No blank vaccination grouping labels.
SELECT count(*) FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND lifecycle_status = 'alive'
  AND NULLIF(btrim(management_stage), '') IS NULL;

-- One real vaccination config family.
SELECT pd.code, pv.version_label, pv.status, count(pr.rule_id) AS rules
FROM protocol_versions pv
JOIN protocol_definitions pd USING (tenant_id, protocol_id)
LEFT JOIN protocol_rules pr USING (tenant_id, protocol_version_id)
WHERE pv.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pd.category = 'vaccination'
  AND pd.status = 'active'
  AND pv.status <> 'retired'
GROUP BY pd.code, pv.version_label, pv.status
ORDER BY pd.code;

-- No test/E2E vaccination protocol families in the shared environment.
SELECT pd.code, pd.name, pd.category, pd.status
FROM protocol_definitions pd
WHERE pd.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pd.category = 'vaccination'
  AND (
    pd.code ILIKE '%e2e%' OR pd.code ILIKE '%test%' OR
    pd.name ILIKE '%story%' OR pd.name ILIKE '%stub%' OR
    pd.name ILIKE '%dummy%' OR pd.name ILIKE '%local%'
  );

-- No test/E2E seeded obligations in the shared environment.
SELECT count(*)
FROM obligation_instances oi
WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND (
    oi.idempotency_key ILIKE '%e2e%' OR
    oi.idempotency_key ILIKE '%test%' OR
    oi.idempotency_key ILIKE '%story%' OR
    oi.idempotency_key ILIKE '%stub%' OR
    oi.idempotency_key ILIKE '%dummy%' OR
    oi.idempotency_key ILIKE '%local%'
  );

-- Explain the Action Center/PC sidebar badge before staging.
SELECT pd.code,
       pv.version_label,
       pr.dose_code,
       oi.status,
       count(oi.obligation_id) AS obligations,
       count(DISTINCT oi.target_id) FILTER (WHERE oi.target_type = 'goat') AS goats
FROM obligation_instances oi
JOIN protocol_versions pv USING (tenant_id, protocol_version_id)
JOIN protocol_definitions pd USING (tenant_id, protocol_id)
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pd.category = 'vaccination'
GROUP BY pd.code, pv.version_label, pr.dose_code, oi.status
ORDER BY pd.code, pr.dose_code, oi.status;

-- Campaign/grouping check. If this returns zero, Action Center rows are grouped
-- directly by process-integrity dimensions rather than an explicit batch table.
SELECT count(*) FROM obligation_batches
WHERE tenant_id = '00000000-0000-4000-8000-000000000001';
```
