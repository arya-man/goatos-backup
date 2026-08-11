# Partition Operational Location STG Handoff

Date: 2026-08-10
Branch: `feature/partition-operational-location-mapping`

## Rollback Anchor

Before deploying the partition operational-location migration to STG, a manual
Cloud SQL backup was created for `goatos-stg-core-db`.

```text
Project:     goatos-stg
Instance:    goatos-stg-core-db
Backup ID:   1786348300210
Status:      SUCCESSFUL
Type:        ON_DEMAND
Started:     2026-08-10T07:51:40.210Z
Ended:       2026-08-10T07:53:01.317Z
Description: pre-partition-oploc-42ae40f874ce-20260810T075135Z
```

If STG migration produces incorrect live goat placement, restore from this
backup or clone it for forensic comparison before attempting manual repair.

## Migration Scope

This PR adds the durable partition-to-location bridge and moves Goat OS to the
maintainer-approved physical-location model:

```text
shed_partitions.operational_location_id
```

Each partition that is physically used as a shed becomes the exact operational
shed row for animals and work. In plain English:

```text
If animals live in Godel 1 - Part 1:
  goat.shed_id must point to Godel 1 - Part 1.

If animals live in Castro 2:
  goat.shed_id must point to Castro 2.

If a shed has no split:
  goat.shed_id stays that shed.
```

Important: not every numbered shed is a partition. Some are just standalone
sheds. `Castro 1`, `Castro 2`, `Castro 3`, `Gandhi 1`, `Gandhi 2`, `Gandhi 3`,
`Yashoda 1`, and `Old Yashoda 1` are physical sheds in the approved list, not
software partitions under `Castro`, `Gandhi`, `Yashoda`, or `Old Yashoda`.

So the final rule is:

```text
Standalone shed:
  Castro 2 -> Castro 2
  Gandhi 1 -> Gandhi 1
  Yashoda 4 -> Yashoda 4

Partitioned shed:
  Godel 1 + Part 3 -> Godel 1 - Part 3
  Mandela 2 + Part 6 -> Mandela 2 - Part 6
  Sumathi 1 + Part 8 -> Sumathi 1 - Part 8

Collapsed HCM exception:
  Ho Chi Minh 1 / Ho Chi Minh 2 -> Ho Chi Minh
```

Aryaman confirmed this HCM exception on 2026-08-12:

```text
Use HCM only in CBE.
Do not keep HCM in CPT.
Do not keep separate HCM 1 / HCM 2 rows.
```

The old plain parent rows such as `Castro`, `Gandhi`, `Godel 1`, `Mandela 1`,
`Sumathi 1`, and `Yashoda` are not the goat's physical residence when expanded
sheds/parts exist. They may remain only as legacy/grouping metadata while the
cutover is in progress, and application code must not use them as real animal
locations.

After the STG data cleanup, live partitioned goats must satisfy:

```text
goats.current_location_id = exact physical shed/partition location
goats.shed_id             = exact physical shed/partition location
goats.shed_group_id       = old grouping/parent shed only when needed for legacy grouping
goats.park_id             = parent park rollup
```

Undivided/lumpsum sheds remain unchanged:

```text
goats.current_location_id = shed location
goats.shed_id             = same shed location
```

Terminal history, accepted proofs, submitted observations, and immutable event
payloads are not rewritten by this migration.

## STG Cleanup Contract

Before applying the cleanup in STG, take a fresh Cloud SQL backup. The 2026-08-10
backup below is the original rollback anchor for PR #35, but the actual STG data
cleanup must create a new same-day backup before mutating rows.

The cleanup changes data like this:

1. Create or reuse exact shed rows from Aryaman's approved list.
2. Move live goats from collapsed parent/group rows to the exact shed rows.
3. Move open work rows that are still keyed by old parent/group shed plus
   `partition_label` to the exact shed row.
4. Retire or hide extra active location/catalog rows outside Aryaman's list.
5. Keep no animal/work item pointing at a plain parent/group row when expanded
   sheds/parts exist.

Do not deploy the cleanup if only goats are backfilled. Vaccination, weighing,
feed direction, experiment feed, SOP, proof, and verification rows also carry
the old parent+partition grain today. They must be remapped in the same release
or canceled/reseeded when the row has no partition evidence.

Examples:

```text
Before:
  goat.shed_id = Gandhi
  goat_shed_partitions.partition_label = 1
  goat_shed_partitions.source_shed_name = Gandhi 1

After:
  goat.shed_id = Gandhi 1
  goat.current_location_id = Gandhi 1
  goat.shed_group_id = Gandhi, only if legacy grouping is still needed
```

```text
Before:
  vaccination_drive_assignments.shed_id = Gandhi
  partition_label = Part 1
  animal_count = 42
  total_doses = 84

After:
  vaccination_drive_assignments.shed_id = Gandhi 1
  partition_label = whole/null-equivalent
  animal_count = 42
  total_doses = 84
```

```text
Before:
  locations contains Castro, Castro 1, Castro 2, Castro 3
  goats point to Castro
  goat_shed_partitions.source_shed_name says Castro 1/2/3

After:
  goats point to Castro 1/2/3
  Castro is not exposed as a real shed choice
```

## Aryaman-Approved STG Shed List

Aryaman's 2026-08-12 message expands to the following valid physical sheds.
These are the rows to keep visible as shed choices. Counts below came from the
live STG audit performed on 2026-08-12 before cleanup.

```text
CBE / Coimbatore: 68 rows, 935 animals
Castro 1..3
Gandhi 1..3
Mandela 1 - Part 1..7
Mandela 2 - Part 1..8
Sumathi 1 - Part 1..8
Sumathi 2 - Part 1..8
Godel 1 - Part 1..8
Godel 2 - Part 1..8
Yashoda 1..10
Q1..Q3 / Quarantine 1..3
Ho Chi Minh

CPT / Channapatna: 42 rows, 735 animals
Castro 1..2
Gandhi 1..3
Mandela 1 - Part 1..10
Mandela 2 - Part 1..10
Godel 1 - Part 1..4
Godel 2 - Part 1..4
Yashoda 1..4 / New Yashoda 1..4
Old Yashoda 1..5
```

Current STG animal coverage matched Aryaman's list: there were no active animals
in a shed outside this list. The only CBE approved rows with zero active animals
at audit time were:

```text
Yashoda 5
Q1
Q2
Q3
```

Ho Chi Minh is kept as a single CBE shed with 17 active animals. The STG rows
`Ho Chi Minh 1` and `Ho Chi Minh 2` are collapsed into `Ho Chi Minh`; `Ho Chi
Minh 2` has zero animals.

CPT had no active HCM animals at audit time, and Aryaman confirmed HCM should
not exist in CPT final shed choices.

## Rows Outside Aryaman's List To Retire Or Hide

These rows existed in STG during the 2026-08-12 audit but are not valid final
animal shed choices.

Plain parent/group rows to remove from picker semantics:

```text
CBE: Castro, Gandhi, Godel 1, Godel 2, Mandela 1, Mandela 2,
     Sumathi 1, Sumathi 2, Yashoda

CPT: Castro, Gandhi, Godel 1, Godel 2, Mandela 1, Mandela 2,
     Old Yashoda, Yashoda
```

Out-of-range or stale zero rows:

```text
CBE:
Gandhi 1 - Part 1
Gandhi 1 - Part 2
Godel 1 - Part 9
Godel 1 - Part 10
Godel 2 - Part 9
Godel 2 - Part 10
Mandela 1 - Part 8
Mandela 1 - Part 9
Mandela 1 - Part 10
Sumathi 1 - Part 9
Sumathi 1 - Part 10
Sumathi 2 - Part 9
Sumathi 2 - Part 10

CPT:
Ho Chi Minh 1
Ho Chi Minh 2
Godel 1 - Part 5..10
Godel 2 - Part 5..10
Sumathi 1 - Part 1..10
Sumathi 2 - Part 1..10
```

## STG Proof Queries

After cleanup, the following statements must be true:

```sql
-- No live goat should remain on a plain group row when the approved exact shed exists.
SELECT p.name AS park, l.name AS shed, count(*) AS goats
FROM goats g
JOIN locations p ON p.tenant_id = g.tenant_id AND p.location_id = g.park_id
JOIN locations l ON l.tenant_id = g.tenant_id AND l.location_id = g.shed_id
WHERE g.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu')
  AND g.merged_into_goat_id IS NULL
  AND g.exited_at IS NULL
  AND l.name IN (
    'Castro','Gandhi','Godel 1','Godel 2','Mandela 1','Mandela 2',
    'Sumathi 1','Sumathi 2','Yashoda','Old Yashoda'
  )
GROUP BY p.name, l.name
ORDER BY p.name, l.name;
-- EXPECTED: zero rows
```

```sql
-- All live goats should point to the same exact location through shed_id and current_location_id.
SELECT count(*) AS mismatched_live_goats
FROM goats
WHERE lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu')
  AND merged_into_goat_id IS NULL
  AND exited_at IS NULL
  AND current_location_id IS DISTINCT FROM shed_id;
-- EXPECTED: 0
```

```sql
-- Open vaccination assignment rows should no longer be keyed to plain group sheds
-- plus a partition label.
SELECT p.name AS park, l.name AS shed, a.partition_label, count(*) AS assignments
FROM vaccination_drive_assignments a
JOIN locations p ON p.tenant_id = a.tenant_id AND p.location_id = a.park_id
JOIN locations l ON l.tenant_id = a.tenant_id AND l.location_id = a.shed_id
WHERE l.name IN (
    'Castro','Gandhi','Godel 1','Godel 2','Mandela 1','Mandela 2',
    'Sumathi 1','Sumathi 2','Yashoda','Old Yashoda'
  )
  AND a.partition_label <> 'whole'
GROUP BY p.name, l.name, a.partition_label
ORDER BY p.name, l.name, a.partition_label;
-- EXPECTED: zero rows
```

The full STG audit workbook used for Aryaman review is stored locally at:

```text
/Users/ravi/mesha/outputs/stg-shed-inventory/aryaman-shed-list-with-stg-animal-counts.xlsx
```

## Cross-Module Remap Audit

Read-only STG audits on 2026-08-12 found the following rows that must be handled
before the exact-shed cleanup is deployed.

Deterministic remaps:

```text
goats.shed_id/current_location_id:
  1,670 live goats currently on parent/group rows.
  All resolve to Aryaman final exact sheds.

goat_shed_partitions:
  1,670 rows currently reference parent/group shed ids.
  These become historical/source evidence only after exact goat shed_id is set.

vaccination_drive_assignments:
  464 rows require remap.
  Set shed_id=exact shed, physical_shed=exact shed name, partition_label='whole'.

obligation_instances:
  7,097 active obligations require exact shed scope.
  Terminal historical obligations also need preservation/remap; do not infer
  terminal history only from current goat location.

vaccination_eligibility_rollups:
  19 stale group/partition rollups covering 324 animals.
  Delete/recompute after exact-shed remap.

feed_direction_issue_rows:
  9,198 rows require exact shed_id and shed_label remap.

feed_experiment_config:
  176 active rows resolve to exact sheds.

feed_transport_tasks:
  260 parent+partition rows resolve to exact sheds.

feed_distribution_completions:
  360 parent+partition rows resolve to exact sheds.

feed_packing_completions:
  129 parent+partition rows resolve to exact sheds.

verification_items:
  436 rows have old group+partition shape outside vaccination-specific review.

weighing_campaign_sheds:
  76 bucket normalizations are needed.
  9 change parent/group location_id to exact shed_id.
  60 already use exact shed but still have nonblank partition_label.
  7 already use exact shed but still have empty-string partition_label.
```

Rows requiring human/operator decision or source-artifact reconstruction:

```text
feed parent-only operational rows with no partition evidence:
  Channapatna / Castro feed_distribution_completions: 2 rows, 2026-08-08
  Channapatna / Godel 2 feed_distribution_completions: 1 row, 2026-08-08
  Channapatna / Castro feed_packing_completions: 1 row, 2026-08-09
  Coimbatore / Yashoda feed_transport_tasks: 1 row, 2026-08-09

HCM exception:
  Coimbatore / Ho Chi Minh feed_transport_tasks: 1 row, 2026-08-08.
  This maps to final Ho Chi Minh because HCM is collapsed to one shed.

vaccination terminal history:
  55 terminal obligation rows changed shed since assignment and cannot be
  reconstructed from current goat location alone. Use the committed Aug-4 RFID
  source artifact or preserve source-fact history; do not guess.

vaccination SOP/proof split:
  3 multi-shed submissions/reviews and 5 linked proofs cannot legally receive
  one shed_id. Split by exact shed or preserve as aggregate history with an
  explicit legacy marker.

weighing RFID exceptions:
  16 individual weighing observations across 11 tags do not match an active
  goat identifier in current STG. Their shed can be recovered from the weighing
  bucket, but goat-history linkage needs source evidence.

canceled zero bucket rows:
  CPT Godel 1 Parts 9-10, CBE Godel 1 Parts 9-10, and CBE Mandela 1 Parts 8-10
  are canceled with zero observations/proofs/work items. Do not invent final
  exact sheds for them.
```

Code paths must also stop writing parent+partition identity after cleanup:

```text
vaccination execution readers/writers
SOP assignment matching
obligation partition move path
feed config ListPens/write paths
feed projected counts
shifting destination picker
weighing proof/verification/replay snapshot paths
generic location listing/pickers
```

## Partition Retirement Follow-up

This PR keeps mapped active pens protected from ordinary location retirement so
an admin cannot accidentally hide a partition that `shed_partitions` still marks
active. When a product flow for retiring a partition is added, it must retire the
catalog row and deactivate the mapped pen in one database transaction:

```text
1. lock shed_partitions row by tenant_id + shed_id + normalized_label
2. verify no live goats/tasks still require that partition
3. set mapped locations.status = inactive
4. set shed_partitions.status = retired
5. invalidate location/partition pickers
```

Do not implement partition retirement by calling the generic location retire API
on the mapped pen first; that path intentionally blocks mapped pens.
