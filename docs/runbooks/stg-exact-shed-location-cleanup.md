# STG Exact Shed Location Cleanup

This runbook is the staging data contract for PR #35.

## Business Rule

The live animal/work location is the exact physical shed.

Examples:

- `Castro 1`, `Castro 2`, `Castro 3` are three sheds.
- `Gandhi 1`, `Gandhi 2`, `Gandhi 3` are three sheds.
- `Godel 1 - Part 1` is a shed.
- `Godel 1` is only a legacy/group name when its animals live in `Godel 1 - Part N`.
- If a shed has no split, the plain shed name remains the exact shed, for example `Ho Chi Minh`.

Do not treat a live row as two fields that must be joined. Old `partition_label`
columns may remain only for compatibility/history and must not be appended to an
exact shed name for display.

## What Changes In STG

After backup and deploy, every live animal with partition evidence is moved to
the exact shed row:

```text
before:
goats.shed_id = Gandhi
goat_shed_partitions.partition_label = 1
source_shed_name = Gandhi 1

after:
goats.shed_id = Gandhi 1
goats.current_location_id = Gandhi 1
goats.shed_group_id = Gandhi
```

For `Godel/Sumathi/Mandela` style rows, the exact shed name is a single stored
name. It is not built at runtime by joining two fields:

```text
before:
goats.shed_id = Godel 1
partition_label = Part 1

after:
goats.shed_id = Godel 1 - Part 1
goats.current_location_id = Godel 1 - Part 1
goats.shed_group_id = Godel 1
```

For unsplit sheds:

```text
before and after:
goats.shed_id = Ho Chi Minh
goats.current_location_id = Ho Chi Minh
goats.shed_group_id = NULL
```

## Rows That Must Be Rewritten

The cleanup is not only `goats`.

These live or submitted operational rows must point to the same exact shed id as
the animal/work location:

- vaccination drive assignments, eligibility rollups, obligations, batches, SOP tasks, SOP submissions
- vaccination proof artifacts and verification items
- weighing campaign sheds and work items
- feed transport, feed packing, feed distribution, feed direction issue rows
- feed experiment config
- shifting events
- procurement PC handoffs and `goat.created` outbox/audit scope
- health cases

For these rows, `partition_label` should be blank/null once the `shed_id` is the
exact shed. It must not be required to understand the live location.

The concrete bug this prevents:

```text
before migration:
shed_id = Castro
partition_label = 2
display = Castro 2

bad half-migrated state:
shed_id = Castro 2
partition_label = 2
bad display = Castro 2 2

correct final state:
shed_id = Castro 2
partition_label = NULL / ''
display = Castro 2
```

The same rule applies to weighing buckets: if `weighing_campaign_sheds.location_id`
or any UI-facing location id is moved to `Castro 2`, the old `partition_label=2`
must be cleared or ignored. Do not leave exact shed plus stale partition metadata
as a live display contract.

## STG Execution Order

1. Take a Cloud SQL backup/snapshot and record the backup id.
2. Deploy the PR migrations.
3. Run the batched goat residence backfill (`000154`) to completion.
4. Run the STG cleanup verification queries below.
5. Only then publish Android/Firebase or ask users to retest.

## Required Proof Queries

These checks must return zero rows unless explicitly listed as historical-only.

### Live goats still on a grouped shed while partition evidence exists

```sql
SELECT g.goat_id, g.shed_id, gsp.source_shed_name, gsp.partition_label
FROM goats g
JOIN goat_shed_partitions gsp ON gsp.goat_id = g.goat_id
WHERE g.lifecycle_status = 'active'
  AND COALESCE(gsp.partition_label, '') NOT IN ('', 'whole')
  AND g.shed_id <> gsp.source_shed_name;
```

### UI-facing task rows still carrying stale partition metadata

```sql
SELECT 'vaccination_drive_assignments' AS table_name, shed_id, partition_label, count(*)
FROM vaccination_drive_assignments
WHERE COALESCE(partition_label, '') NOT IN ('', 'whole')
GROUP BY shed_id, partition_label
UNION ALL
SELECT 'weighing_campaign_sheds', shed_id, partition_label, count(*)
FROM weighing_campaign_sheds
WHERE COALESCE(partition_label, '') NOT IN ('', 'whole')
GROUP BY shed_id, partition_label
UNION ALL
SELECT 'feed_transport_tasks', shed_id, partition_label, count(*)
FROM feed_transport_tasks
WHERE COALESCE(partition_label, '') NOT IN ('', 'whole')
GROUP BY shed_id, partition_label
UNION ALL
SELECT 'feed_direction_issue_rows', shed_id, partition_label, count(*)
FROM feed_direction_issue_rows
WHERE COALESCE(partition_label, '') NOT IN ('', 'whole')
GROUP BY shed_id, partition_label;
```

### Exact shed names accidentally rendered with stale suffixes

Application responses must prefer the stored exact shed name in
`operational_location_display`. If a fallback receives both exact shed name and
stale partition label, the UI must show only the exact shed:

```text
Castro 2 + 2 -> Castro 2
Gandhi 1 + 1 -> Gandhi 1
Godel 2 - Part 1 + Part 1 -> Godel 2 - Part 1
```

Never accept:

```text
Castro 2 2
Gandhi 1 1
Godel 2 - Part 1 - Part 1
```

## Aryaman Shed List Normalization

Use Aryaman's list as the allowed shape for CBE/CPT.

- `Castro 1-3` means `Castro 1`, `Castro 2`, `Castro 3`.
- `Gandhi 1-3` means `Gandhi 1`, `Gandhi 2`, `Gandhi 3`.
- `Mandela 1 Part 1-7` means `Mandela 1 - Part 1` through `Mandela 1 - Part 7`.
- `Sumathi/Godel 1/2 Part 1-8` means each of `Sumathi 1`, `Sumathi 2`, `Godel 1`, `Godel 2` has `Part 1` through `Part 8`.
- `Ho Chi Minh` in CBE is one unsplit shed. Do not create/use `Ho Chi Minh 1` or `Ho Chi Minh 2` for STG unless farm ops changes this rule.
- CPT has no active Ho Chi Minh animals per the current Aryaman confirmation.

Anything outside this list must be reviewed before leaving it active.

## Rollback Expectation

Rollback must restore from the recorded Cloud SQL backup. Do not rely on manually
reconstructing old compatibility fields after users create new proofs or shifts.
