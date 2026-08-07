-- +goose Up
-- Feed experiment config becomes partition-aware.
--
-- WHY. The 34 authored experiment sheds are named for PARTITIONS ("Castro 1", "Godel 1 - Part 3",
-- "Mandela 1 - Part 7"), but this table keys on shed_id alone with a natural key of
-- (tenant_id, park_id, shed_id, feed_item_key). Under the canonical storage rule an animal lives in
-- physical shed "Castro" carrying partition "1", so all three Castro partitions collapse onto ONE
-- shed_id -- and three different absolute_kg values for the same feed item cannot coexist under that
-- unique key. The table simply could not represent the authored data, and seeding it failed closed.
--
-- This is not a cosmetic column. A shed can be PARTLY experimental: CBE's Godel 2 holds eight
-- partitions of which only Parts 3, 4 and 5 are authored experiments -- Parts 1, 2, 6, 7 and 8
-- (141 animals) are ordinary per-head-grid sheds. Without the partition the generator can only ask
-- "is this whole shed an experiment?", and either answer is wrong for Godel 2.
--
-- NULL vs '' vs 'whole'. Consistent with the operational-location rule, all three mean
-- "not partitioned". The generated partition_key normalizes them to the single matching token
-- 'whole' so a non-partitioned shed has exactly one row per feed item and cannot be duplicated by a
-- label whitespace/case variant. 'whole' is a MATCHING key and must never be rendered as copy --
-- partition_label keeps the raw authored label for display ("Part 3", "1").
--
-- The old unique key is REPLACED, not supplemented: leaving it in place would still forbid the
-- second partition of a shed. Existing rows (none on any environment at the time of writing, and
-- whole-shed experiments elsewhere) normalize to partition_key 'whole' and keep their identity, so
-- this widening is non-destructive.

ALTER TABLE feed_experiment_config
  ADD COLUMN IF NOT EXISTS partition_label text;

ALTER TABLE feed_experiment_config
  ADD COLUMN IF NOT EXISTS partition_key text
  GENERATED ALWAYS AS (
    CASE
      WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
      ELSE feed_config_norm(partition_label)
    END
  ) STORED;

DROP INDEX IF EXISTS feed_experiment_config_natural_key_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS feed_experiment_config_natural_key_uidx
  ON feed_experiment_config (tenant_id, park_id, shed_id, partition_key, feed_item_key);

-- The hot read is "every active experiment cell for this shed", which the generator then buckets by
-- partition in memory -- one lookup per shed, not one per partition. partition_key rides in the
-- INCLUDE list so that bucketing needs no heap fetch.
DROP INDEX IF EXISTS feed_experiment_config_shed_lookup_idx;

CREATE INDEX IF NOT EXISTS feed_experiment_config_shed_lookup_idx
  ON feed_experiment_config (tenant_id, park_id, shed_id)
  INCLUDE (partition_key, feed_item_key, absolute_kg)
  WHERE status = 'active';

-- +goose Down
-- Restores the shed-grained natural key. Any row carrying a real partition would collide under it,
-- so those are removed first: they are authored config re-seedable from
-- backend/cmd/seed-feed-ration, and silently keeping one arbitrary partition per shed would leave a
-- shed fed from another partition's absolute kg.
DELETE FROM feed_experiment_config WHERE partition_key <> 'whole';

DROP INDEX IF EXISTS feed_experiment_config_natural_key_uidx;
DROP INDEX IF EXISTS feed_experiment_config_shed_lookup_idx;

ALTER TABLE feed_experiment_config
  DROP COLUMN IF EXISTS partition_key,
  DROP COLUMN IF EXISTS partition_label;

CREATE UNIQUE INDEX IF NOT EXISTS feed_experiment_config_natural_key_uidx
  ON feed_experiment_config (tenant_id, park_id, shed_id, feed_item_key);

CREATE INDEX IF NOT EXISTS feed_experiment_config_shed_lookup_idx
  ON feed_experiment_config (tenant_id, park_id, shed_id)
  INCLUDE (feed_item_key, absolute_kg)
  WHERE status = 'active';
