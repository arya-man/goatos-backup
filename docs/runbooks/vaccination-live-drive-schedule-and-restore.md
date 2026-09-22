# Vaccination Live Drive Scheduling And Restore Runbook

This runbook is for emergency STG/live-ops vaccination drive repair when the
kernel worker or obligation sweep removes an active vaccination drive while
operators are already scanning animals and uploading proof.

Use this only for a known live drive. Do not bulk-run these steps across parks
or dates.

## Current Known Incident

On `2026-09-22`, CBE Godel 2 had a live ET+TT + PPR drive for 58 procured
animals. Operators had already started submitting proof. The drive then
collapsed to zero rows in the live tracker.

Evidence showed the data was removed by backend automation, not a field user:

- Cloud Run service: `goatos-kernel-worker-stg`
- Stages involved: `vaccination-generation`, then `obligation-sweep`
- Generation failed around `2026-09-22 12:15 IST`
- Affected status-event reason: `vaccine_history_outranks_primary_seed`
- Drive rows disappeared because `vaccination_drive_assignments` is a rebuilt
  read model derived from open `obligation_instances`

The unsafe behavior is that sweep rebuilt active drive assignments from a
partially canceled/deferred obligation set while proof was already arriving.

## Rule For Combo Drives

For a combo drive such as ET+TT + PPR:

- one animal has two administrations
- one submitted proof video for the animal closes both administrations
- assignment `animal_count` is animal count
- assignment `total_doses` is animal count times vaccine count
- assignment members are per obligation/admin row, not per animal

Example: 58 animals with ET+TT + PPR means:

- 58 animals
- 116 administrations
- 116 assignment member rows

## Scheduling CBE Godel 2 ET+TT + PPR

Known IDs for the `2026-09-22` CBE Godel 2 incident:

| Entity | ID |
| --- | --- |
| Tenant | `00000000-0000-4000-8000-000000000001` |
| CBE park | `00000000-0000-4000-8000-000000003001` |
| CBE Godel 2 shed | `fed85958-40f5-5369-b69b-c5a288bcf6da` |
| Protocol version | `c6a481f9-7e1f-44d3-89aa-5adf40a5f136` |
| ET+TT dose 1 rule | `865ebdde-3b37-40d7-a7ad-61733191ea06` |
| PPR rule | `8c377a78-93d2-47a6-bc1e-63afb3ee1910` |
| Pramod | `f88d5409-9c93-41c9-883f-d716173e0724` |
| Naveen | `4fd8e5e2-b1d3-4d6b-9cbd-47c184f6fa9b` |

Animal set:

- `G-005348` through `G-005405`
- `dob = 2026-06-10`
- `origin_type = procured`
- CBE Godel 2

Operator split:

| Partition | Operator | Animals | Doses |
| --- | --- | ---: | ---: |
| Part 1 | Pramod | 10 | 20 |
| Part 2 | Pramod | 10 | 20 |
| Part 3 | Pramod | 10 | 20 |
| Part 4 | Naveen | 10 | 20 |
| Part 5 | Naveen | 10 | 20 |
| Part 6 | Naveen | 8 | 16 |

Do not schedule these 58 animals as `et_tt_adult_w1`.

Do not use `et_tt_kid_7w` for this same-day dose 1 drive. That is the ET+TT
booster due 21 days after dose 1.

## Preflight Before Any Repair

Confirm the target animal set:

```sql
SELECT gsp.partition_label, count(*) AS animals, min(g.display_id), max(g.display_id)
FROM goats g
JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id
 AND gsp.goat_id = g.goat_id
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND g.display_id BETWEEN 'G-005348' AND 'G-005405'
  AND g.park_id = '00000000-0000-4000-8000-000000003001'
  AND g.shed_id = 'fed85958-40f5-5369-b69b-c5a288bcf6da'
  AND gsp.shed_id = 'fed85958-40f5-5369-b69b-c5a288bcf6da'
  AND g.merged_into_goat_id IS NULL
  AND g.lifecycle_status = 'alive'
GROUP BY gsp.partition_label
ORDER BY gsp.partition_label;
```

Expected:

- Part 1-5 have 10 animals each
- Part 6 has 8 animals
- total 58 animals

Confirm completed proof videos before restoring completion state:

```sql
SELECT count(DISTINCT pa.subject_id) AS animals, count(*) AS videos,
       min(coalesce(pa.uploaded_at, pa.created_at)) AS first_video,
       max(coalesce(pa.uploaded_at, pa.created_at)) AS last_video
FROM proof_artifacts pa
JOIN goats g
  ON g.tenant_id = pa.tenant_id
 AND g.goat_id = pa.subject_id
WHERE pa.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pa.subject_type = 'goat'
  AND pa.proof_type = 'video'
  AND pa.upload_state = 'completed'
  AND coalesce(pa.uploaded_at, pa.created_at) >= timestamptz '2026-09-22 00:00 Asia/Kolkata'
  AND coalesce(pa.uploaded_at, pa.created_at) <  timestamptz '2026-09-23 00:00 Asia/Kolkata'
  AND g.display_id BETWEEN 'G-005348' AND 'G-005405';
```

## Restore Shape

The restore must do all of these in one transaction:

1. Build the 58 target goats from `goats` + `goat_shed_partitions`.
2. Build the two target rules: ET+TT dose 1 and PPR.
3. Read first completed proof video per goat from `proof_artifacts`.
4. Update the 116 matching `obligation_instances`:
   - proof exists: `status = completed`
   - proof missing: `status = scheduled`
   - `batch_id` set to the drive batch
   - `due_at/window_start/window_end` set to the live drive window
5. Insert accepted `vaccination_completions` for completed obligations.
6. Insert status events for the restore.
7. Recreate six `vaccination_drive_assignments`, one per partition/operator.
8. Recreate 116 `vaccination_drive_assignment_members`.
9. Update the `obligation_batches` quantity fields.

Do not delete proof artifacts or GCS objects. Proof media is independent of
drive assignment rows.

## Restore Validation Query

After restore, validate assignment rows:

```sql
WITH assignments AS (
  SELECT vda.*, wm.display_name AS operator
  FROM vaccination_drive_assignments vda
  LEFT JOIN workforce_members wm
    ON wm.tenant_id = vda.tenant_id
   AND wm.workforce_member_id = vda.operator_id
  WHERE vda.tenant_id = '00000000-0000-4000-8000-000000000001'
    AND vda.planned_date = date '2026-09-22'
    AND vda.park_id = '00000000-0000-4000-8000-000000003001'
    AND vda.shed_id = 'fed85958-40f5-5369-b69b-c5a288bcf6da'
    AND vda.partition_label IN ('Part 1','Part 2','Part 3','Part 4','Part 5','Part 6')
)
SELECT partition_label, operator, animal_count, total_doses,
       cardinality(vaccine_rule_ids) AS vaccines,
       (
         SELECT count(*)
         FROM vaccination_drive_assignment_members m
         WHERE m.tenant_id = a.tenant_id
           AND m.assignment_id = a.assignment_id
       ) AS members
FROM assignments a
ORDER BY partition_label;
```

Expected:

| Partition | Operator | Animals | Doses | Members |
| --- | --- | ---: | ---: | ---: |
| Part 1 | Pramod | 10 | 20 | 20 |
| Part 2 | Pramod | 10 | 20 | 20 |
| Part 3 | Pramod | 10 | 20 | 20 |
| Part 4 | Naveen | 10 | 20 | 20 |
| Part 5 | Naveen | 10 | 20 | 20 |
| Part 6 | Naveen | 8 | 16 | 16 |

Validate completed versus pending:

```sql
WITH target AS (
  SELECT g.goat_id
  FROM goats g
  WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'
    AND g.display_id BETWEEN 'G-005348' AND 'G-005405'
)
SELECT count(DISTINCT target_id) FILTER (WHERE status = 'completed') AS completed_animals,
       count(*) FILTER (WHERE status = 'completed') AS completed_admins,
       count(DISTINCT target_id) FILTER (WHERE status = 'scheduled') AS pending_animals,
       count(*) FILTER (WHERE status = 'scheduled') AS pending_admins
FROM obligation_instances oi
JOIN target t ON t.goat_id = oi.target_id
WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND oi.rule_id IN (
    '865ebdde-3b37-40d7-a7ad-61733191ea06',
    '8c377a78-93d2-47a6-bc1e-63afb3ee1910'
  )
  AND oi.due_at >= timestamptz '2026-09-22 00:00 Asia/Kolkata'
  AND oi.due_at <  timestamptz '2026-09-23 00:00 Asia/Kolkata';
```

For the `2026-09-22` incident immediately after restore, this was:

- completed animals: 34
- completed administrations: 68
- pending animals: 24
- pending administrations: 48

Those numbers should naturally change only when more operators submit proof.

## Monitoring For Re-Deletion

During an active recovery, poll every 30 seconds:

- assignment rows must stay at 6
- `sum(animal_count)` must stay at 58
- `sum(total_doses)` must stay at 116
- member rows must stay at 116
- completed can increase
- scheduled can decrease
- canceled/deferred/superseded for the two active rules must not increase

If rows disappear again, immediately check Cloud Run logs for:

- `goatos-kernel-worker-stg`
- `vaccination-generation`
- `obligation-sweep`
- `protocol_version_replaced`
- `vaccine_history_outranks_primary_seed`

## Backend Guard Required

The correct code behavior is:

- generation must not cancel old vaccination work until replacement generation
  has succeeded for the same goat/rule/course
- sweep must not replace an active drive with an empty or partial derived set
- sweep must not delete assignment rows for a today/in-progress drive when
  proof, scans, or completions exist
- system cancellations must write audit evidence
- live tracker must exclude terminal rows from carry totals but preserve active
  completed/proofed work
