-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: feed transport grain repair only; no seed contract change
--
-- Feed Transport is ONE daily task per active PHYSICAL shed. 000155 closed half of that: it stopped
-- the materializer fanning out over `shed_partitions`, so `partition_label` is '' on every new row.
-- The other half stayed open, because a pen reaches the materializer through the LOCATION ROW too.
--
-- `locations` carries the farm's pens twice: as the canonical parent shed plus its `shed_partitions`
-- entry ("Castro" + partition "1"), and as an OLD row -- still `status='active'`, still
-- `location_type='shed'` -- literally named "Castro 1" or "Godel 1 - Part 3". Migration 000112
-- assumed those alias rows were `inactive` (its Seed 2 only reads inactive ones); in the live data
-- they are ACTIVE. So the materializer's membership test, `location_type='shed' AND
-- status='active'`, counted 126 sheds where the farm has 21, and Mandela 1 alone raised ELEVEN
-- transport tasks for one physical load -- eleven videos of the same trip.
--
-- The code fix excludes them going forward (oploc.PartitionAliasExclusionSQL, shared with the
-- weighing bucket catalog rather than hand-rolled a third time). This retires the rows already
-- written. 000143, 000146 and 000155 are applied and STG records migration checksums, so none of
-- them is amended; this is the forward repair.
--
-- The alias LOCATION rows are deliberately NOT touched. Retiring them is a much wider change --
-- weighing buckets still point at some of them -- and it is not needed to fix the transport grain.
--
-- Same care as 000155: only UNSTARTED work retires. A task carrying an attempt was filmed by a real
-- operator and may already hold a verdict, so it keeps its status and its evidence even though the
-- location it names is an alias. Verified on the live clone before writing this: of the 704 alias
-- transport tasks present, ZERO carry an attempt and ZERO sit on a location holding any goat.
UPDATE public.feed_transport_tasks t
SET status = 'retired',
    updated_at = now(),
    row_version = row_version + 1
WHERE t.status = 'due'
  AND t.current_attempt_id IS NULL
  AND t.completed_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM public.locations shed
    JOIN public.locations parent_shed
      ON parent_shed.tenant_id = shed.tenant_id
     AND parent_shed.parent_location_id = shed.parent_location_id
     AND parent_shed.location_id <> shed.location_id
     AND parent_shed.location_type = 'shed'
     AND parent_shed.status = 'active'
     AND parent_shed.retired_at IS NULL
    JOIN public.shed_partitions parent_partition
      ON parent_partition.tenant_id = parent_shed.tenant_id
     AND parent_partition.shed_id = parent_shed.location_id
     AND parent_partition.status = 'active'
    WHERE shed.tenant_id = t.tenant_id
      AND shed.location_id = t.shed_id
      AND shed.location_type = 'shed'
      AND starts_with(BTRIM(shed.name), BTRIM(parent_shed.name))
      AND NULLIF(
        regexp_replace(
          BTRIM(replace(BTRIM(shed.name), BTRIM(parent_shed.name), '')),
          '^\s*-\s*part\s*|\s+',
          '',
          'gi'
        ),
        ''
      ) = parent_partition.normalized_label
  );

-- +goose Down
-- +goose NO TRANSACTION
-- Forward data repair only. A retired unstarted task carries no evidence, so there is nothing to
-- restore; re-materializing pen tasks would mean reinstating the defect.
