# Vaccination Drive Create / Rollover Runbook

Use this when creating a new vaccination drive manually, or rolling unfinished work from one date to another.

## Golden Rule

A vaccination drive is not only `vaccination_drive_assignments`.

The drive is valid only when these stay in sync:

- `obligation_batches`: one batch for the planned drive/date/scope.
- `obligation_instances`: the actual animal obligations, attached to that batch with the correct `due_at`, `window_start`, and `window_end`.
- `vaccination_drive_assignments`: operator-facing drive cards, grouped by park/shed/partition/operator/vaccine.
- `vaccination_completions`: proof/completion rows, used to exclude animals already done or waiting verification.

If you only insert `vaccination_drive_assignments`, the planner UI may show a drive while the app/command board still reads the old obligations. If you only update `obligation_instances`, the obligations exist but the operator drive card may be missing.

## Which Tables To Look At

When a user asks "what is scheduled", "what is due", "why is this card showing", or "move this drive", do not trust one table.

| Question | Primary table | Also check | Why |
| --- | --- | --- | --- |
| What animals actually owe a vaccine on a date? | `obligation_instances` | `protocol_rules`, `locations`, `goat_shed_partitions`, `vaccination_completions` | This is the source of actual due work. Mobile cards and overdue/open counts can exist here even when no drive assignment row exists. |
| What drive cards were explicitly planned? | `vaccination_drive_assignments` | `obligation_batches`, `protocol_rules`, `workforce_members`, `vaccination_drive_date_overrides` | This is the planner/operator assignment layer. It can be incomplete if obligations were generated but not assigned into a drive. |
| Is a drive planned/completed/in progress? | `obligation_batches` | `obligation_instances`, `vaccination_completions` | Batch status names the drive state, but animal truth still comes from obligations/completions. |
| Which vaccine is this rule? | `protocol_rules` | `protocol_versions` if needed | Use `dose_code` and `eligibility_json->'vaccine'`; labels alone are not stable enough. |
| Which park/shed/partition is this animal/card? | `locations` | `goat_shed_partitions` | `locations.parent_location_id` gives park; `goat_shed_partitions.partition_label` gives the physical partition for per-card counts. |
| Was it done? | `vaccination_completions` | `sop_submission_items` / proof tables only when video details are needed | Non-rejected completion means submitted/done; `status='accepted'` means verifier accepted. |
| Is it waiting verification? | `vaccination_completions.status` | `verified_at`, `verified_by` | Completion rows with status not in `accepted/verified/rejected` are submitted but pending verification. |
| Was a date moved? | `vaccination_drive_date_overrides` | `vaccination_drive_assignments`, `obligation_instances` | Assignment date overrides can change effective planned date without changing the original row date. |
| Why does web command board disagree with mobile? | compare `vaccination_drive_assignments` vs `obligation_instances` | `vaccination_completions`, `obligation_batches` | Web planner/catalog can read explicit assignments while mobile/obligation views read actual due obligations. |

## Minimum Query Pattern Before Answering A Schedule Question

For a park/date/vaccine question, run both views:

1. **Assignment view**: `vaccination_drive_assignments` joined to `protocol_rules` and `locations`.
2. **Obligation view**: `obligation_instances` joined to `protocol_rules`, `locations`, `goat_shed_partitions`, and `vaccination_completions`.

If the answers differ, report the difference instead of choosing one silently.

Assignment view skeleton:

```sql
SELECT
  park.name AS park,
  vda.planned_date,
  vda.physical_shed,
  vda.partition_label,
  pr.dose_code,
  COALESCE(
    NULLIF(pr.eligibility_json -> 'vaccine' ->> 'display_name', ''),
    NULLIF(pr.eligibility_json -> 'vaccine' ->> 'name', ''),
    NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''),
    pr.dose_code
  ) AS vaccine,
  SUM(vda.animal_count) AS animals
FROM vaccination_drive_assignments vda
JOIN locations park
  ON park.tenant_id = vda.tenant_id
 AND park.location_id = vda.park_id
LEFT JOIN LATERAL unnest(vda.vaccine_rule_ids) assigned_rule(rule_id) ON true
LEFT JOIN protocol_rules pr
  ON pr.tenant_id = vda.tenant_id
 AND pr.rule_id = assigned_rule.rule_id
WHERE vda.tenant_id = '<tenant_id>'::uuid
  AND vda.planned_date BETWEEN '<from_date>'::date AND '<to_date>'::date
GROUP BY park.name, vda.planned_date, vda.physical_shed, vda.partition_label, pr.dose_code, vaccine
ORDER BY vda.planned_date, park.name, vda.physical_shed, vda.partition_label, vaccine;
```

Obligation view skeleton:

```sql
WITH completion_state AS (
  SELECT
    obligation_id,
    COUNT(*) FILTER (WHERE status <> 'rejected') AS submitted,
    COUNT(*) FILTER (WHERE status IN ('accepted', 'verified')) AS accepted,
    COUNT(*) FILTER (WHERE status NOT IN ('accepted', 'verified', 'rejected')) AS waiting_verification
  FROM vaccination_completions
  WHERE tenant_id = '<tenant_id>'::uuid
  GROUP BY obligation_id
)
SELECT
  park.name AS park,
  loc.name AS shed,
  COALESCE(gsp.partition_label, '') AS partition,
  (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date AS due_date,
  ob.batch_id,
  ob.planned_date AS batch_planned_date,
  ob.status AS batch_status,
  pr.dose_code,
  COALESCE(
    NULLIF(pr.eligibility_json -> 'vaccine' ->> 'display_name', ''),
    NULLIF(pr.eligibility_json -> 'vaccine' ->> 'name', ''),
    NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''),
    pr.dose_code
  ) AS vaccine,
  COUNT(DISTINCT oi.target_id) AS animals,
  COUNT(DISTINCT oi.target_id) FILTER (WHERE COALESCE(cs.submitted, 0) > 0) AS submitted,
  COUNT(DISTINCT oi.target_id) FILTER (WHERE COALESCE(cs.waiting_verification, 0) > 0) AS waiting_verification,
  COUNT(DISTINCT oi.target_id) FILTER (WHERE COALESCE(cs.accepted, 0) > 0) AS accepted,
  COUNT(DISTINCT oi.target_id) FILTER (WHERE COALESCE(cs.submitted, 0) = 0) AS not_done
FROM obligation_instances oi
LEFT JOIN obligation_batches ob
  ON ob.tenant_id = oi.tenant_id
 AND ob.batch_id = oi.batch_id
JOIN locations loc
  ON loc.tenant_id = oi.tenant_id
 AND loc.location_id = oi.scope_id
JOIN locations park
  ON park.tenant_id = loc.tenant_id
 AND park.location_id = loc.parent_location_id
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = oi.tenant_id
 AND gsp.goat_id = oi.target_id
 AND gsp.shed_id = oi.scope_id
LEFT JOIN completion_state cs
  ON cs.obligation_id = oi.obligation_id
WHERE oi.tenant_id = '<tenant_id>'::uuid
  AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN '<from_date>'::date AND '<to_date>'::date
GROUP BY park.name, loc.name, partition, due_date, ob.batch_id, ob.planned_date, ob.status, pr.dose_code, vaccine
ORDER BY due_date, park.name, loc.name, partition, vaccine;
```

## Interpretation Rules

- If `vaccination_drive_assignments` has animals but `obligation_instances` does not, the planner has rows that are not backed by actual obligations.
- If `obligation_instances` has animals but `vaccination_drive_assignments` does not, the app may show due/open work but the drive picker/operator assignment layer is missing the drive.
- If `vaccination_completions.status = 'accepted'`, it is done and verifier accepted.
- If a completion row exists but is not accepted/rejected, it is waiting verification.
- If no non-rejected completion exists, it is not done and should roll forward when the work window moves.
- For IST business dates, always compare `(timestamp AT TIME ZONE 'Asia/Kolkata')::date`, not raw UTC date.

## Required Pre-Checks

1. Resolve the park id from `locations`.
2. Resolve the vaccine rule id from `protocol_rules`.
3. Identify the exact animal set.
4. Exclude animals with non-rejected completion rows.
5. Count distinct animals and obligations before writing.
6. Confirm all target obligations have one protocol version and one vaccine rule unless intentionally mixed.

Example count check:

```sql
SELECT
  COUNT(DISTINCT target_id) AS animals,
  COUNT(DISTINCT obligation_id) AS obligations,
  COUNT(DISTINCT protocol_version_id) AS protocol_versions,
  COUNT(DISTINCT rule_id) AS rules
FROM target_obligations;
```

Do not proceed if the counts do not match the requested drive.

## Create / Roll A Drive

Inside one transaction:

1. Create one `obligation_batches` row with:
   - `scope_type = 'park'`
   - `scope_id = park_id`
   - `planned_date = target date`
   - `window_start` at target date 00:00 IST
   - `window_end` at the close of the allowed dose window
   - `status = 'planned'`
   - `estimated_targets = distinct animal count`
   - `context` recording source/reason.

2. Update target `obligation_instances`:
   - set `batch_id` to the new batch
   - set `due_at` to target date 00:00 IST
   - set `window_start` / `window_end`
   - keep or set `status = 'scheduled'`
   - bump `row_version`.

3. Insert `vaccination_drive_assignments` rows:
   - one row per park/shed/partition/operator grouping
   - `planned_date = target date`
   - `batch_id = new batch`
   - `vaccine_rule_ids = ARRAY[rule_id]`
   - `animal_count = COUNT(DISTINCT target_id)` for that partition
   - `total_doses = animal_count`
   - clone operator/shed/partition labels from the prior drive only when rolling the same animal set.

## Post-Write Validation

Run all of these before calling the move done:

```sql
-- Batch total.
SELECT planned_date, status, estimated_targets
FROM obligation_batches
WHERE batch_id = '<new_batch_id>';

-- Assignment partition total.
SELECT physical_shed, partition_label, SUM(animal_count)
FROM vaccination_drive_assignments
WHERE batch_id = '<new_batch_id>'
GROUP BY physical_shed, partition_label
ORDER BY physical_shed, partition_label;

-- Obligation attachment total.
SELECT COUNT(DISTINCT target_id)
FROM obligation_instances
WHERE batch_id = '<new_batch_id>';

-- No not-done source obligations left on the old date.
SELECT COUNT(DISTINCT oi.target_id)
FROM obligation_instances oi
WHERE (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date = '<old_date>'::date
  AND oi.rule_id = '<rule_id>'::uuid
  AND NOT EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    WHERE vc.tenant_id = oi.tenant_id
      AND vc.obligation_id = oi.obligation_id
      AND vc.status <> 'rejected'
  );
```

## Failure Pattern To Avoid

The Aug 2026 Blue Tongue rollover bug happened because two truths diverged:

- `vaccination_drive_assignments` showed only the explicit 84-animal Aug 11 drive.
- `obligation_instances` also had a separate 145-animal Aug 11 Blue Tongue due set from the Aug 10 Sheep Pox completed animals.

For rollovers, always query obligations first, then create/adjust the drive assignment rows from that obligation set.
