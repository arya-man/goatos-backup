-- +goose Up
-- A FROZEN feed sheet could not represent a partitioned shed, so freezing one was impossible.
--
-- Since experiment config became partition-aware, generation emits one cell PER PEN: Castro 1,
-- Castro 2 and Castro 3 share shed_id, session_no, shed_tag_key, breed_key and feed_item_key and
-- differ ONLY by pen. The natural-key unique index did not include the pen, so the three cells
-- collided and the whole INSERT rolled back with SQLSTATE 23505.
--
-- The collision stayed dormant while every read served a live preview (which never touches this
-- table). The 2026-08-08 dispatch gate made the first read past the clock FREEZE the sheet, so the
-- dormant defect became a 500 on every feed-direction read on STG. The gate exposed this; it did
-- not cause it -- a sheet frozen by the scheduler would have hit exactly the same wall.
--
-- partition_key mirrors 000122 on feed_experiment_config: NULL / '' / whitespace all normalize to
-- 'whole', so an undivided shed has ONE stable key instead of a NULL. That matters here because
-- Postgres treats NULLs as distinct in a unique index -- keying on the raw nullable label would
-- silently permit duplicate rows for every unpartitioned shed, which is the opposite of the
-- constraint's purpose.
ALTER TABLE feed_direction_issue_rows
  ADD COLUMN IF NOT EXISTS partition_label text;

ALTER TABLE feed_direction_issue_rows
  ADD COLUMN IF NOT EXISTS partition_key text
  GENERATED ALWAYS AS (
    CASE
      WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
      ELSE lower(btrim(partition_label))
    END
  ) STORED;

-- Swap the natural key. Existing rows all carry partition_key='whole' (the column is new and NULL),
-- so the new index is a superset of the old one and cannot fail on legacy data.
DROP INDEX IF EXISTS feed_direction_issue_rows_natural_key_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS feed_direction_issue_rows_natural_key_uidx
  ON feed_direction_issue_rows (
    tenant_id, feed_direction_issue_id, shed_id, partition_key,
    session_no, shed_tag_key, breed_key, feed_item_key
  );

-- +goose Down
DROP INDEX IF EXISTS feed_direction_issue_rows_natural_key_uidx;

-- The pre-000135 index cannot be recreated while partitioned rows exist: they are exactly the rows
-- it has no room for. Deduplicate down to one cell per legacy key first, keeping the lowest
-- row_seq/item_seq so the survivor is the sheet's first-generated cell rather than an arbitrary one.
DELETE FROM feed_direction_issue_rows r
WHERE EXISTS (
  SELECT 1 FROM feed_direction_issue_rows keep
  WHERE keep.tenant_id = r.tenant_id
    AND keep.feed_direction_issue_id = r.feed_direction_issue_id
    AND keep.shed_id = r.shed_id
    AND keep.session_no = r.session_no
    AND keep.shed_tag_key = r.shed_tag_key
    AND keep.breed_key = r.breed_key
    AND keep.feed_item_key = r.feed_item_key
    AND (keep.row_seq, keep.item_seq, keep.feed_direction_issue_row_id)
      < (r.row_seq, r.item_seq, r.feed_direction_issue_row_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS feed_direction_issue_rows_natural_key_uidx
  ON feed_direction_issue_rows (
    tenant_id, feed_direction_issue_id, shed_id, session_no, shed_tag_key, breed_key, feed_item_key
  );

ALTER TABLE feed_direction_issue_rows DROP COLUMN IF EXISTS partition_key;
ALTER TABLE feed_direction_issue_rows DROP COLUMN IF EXISTS partition_label;
