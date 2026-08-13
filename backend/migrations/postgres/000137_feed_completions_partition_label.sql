-- +goose Up
-- A feed completion belonged to a SHED, so one pen's video closed out every pen in it.
--
-- Reported on STG 2026-08-08: submitting the Castro - 1 morning video flipped Castro - 2 morning
-- to "in review" as well, and the same for evening; identically for packing. Both completion
-- tables key on (tenant_id, park_id, shed_id, session_no, target_date, workflow) with no pen, and
-- the read side keys the same way (app.completedKey), so every partition of a shed resolves to the
-- SAME completion row.
--
-- This is not cosmetic. The completion carries the proof reference, so one clip becomes the
-- evidence for every pen of that shed, and a verifier approving it approves them all on evidence
-- from one. Castro has 3 pens and Godel 1 has 7, so a single video could close out seven pens of
-- work that nobody filmed.
--
-- Same shape as 000135 on feed_direction_issue_rows: partition_key normalizes NULL/''/whitespace to
-- 'whole' because Postgres treats NULLs as DISTINCT in a unique index -- keying on the raw nullable
-- label would let the same undivided shed be completed twice, which is the opposite of what the
-- natural key is for.
ALTER TABLE feed_distribution_completions
  ADD COLUMN IF NOT EXISTS partition_label text;
ALTER TABLE feed_packing_completions
  ADD COLUMN IF NOT EXISTS partition_label text;

ALTER TABLE feed_distribution_completions
  ADD COLUMN IF NOT EXISTS partition_key text
  GENERATED ALWAYS AS (
    CASE WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
         ELSE lower(btrim(partition_label)) END
  ) STORED;
ALTER TABLE feed_packing_completions
  ADD COLUMN IF NOT EXISTS partition_key text
  GENERATED ALWAYS AS (
    CASE WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
         ELSE lower(btrim(partition_label)) END
  ) STORED;

-- Widen the natural key. Every existing row gets partition_key='whole' (the column is new and
-- NULL), so the new index is a superset of the old one and cannot fail on existing data.
--
-- Existing rows are NOT back-filled to a pen on purpose: a completion recorded before this
-- migration genuinely does not know which pen it covered, and inventing one would fabricate proof
-- provenance. They stay shed-level ('whole') and read as the pre-partition history they are.
DROP INDEX IF EXISTS feed_distribution_completions_natural_uq;
CREATE UNIQUE INDEX IF NOT EXISTS feed_distribution_completions_natural_uq
  ON feed_distribution_completions (tenant_id, park_id, shed_id, partition_key, session_no, target_date, workflow);

DROP INDEX IF EXISTS feed_packing_completions_natural_uq;
CREATE UNIQUE INDEX IF NOT EXISTS feed_packing_completions_natural_uq
  ON feed_packing_completions (tenant_id, park_id, shed_id, partition_key, session_no, target_date, workflow);

-- +goose Down
DROP INDEX IF EXISTS feed_distribution_completions_natural_uq;
DROP INDEX IF EXISTS feed_packing_completions_natural_uq;

-- The pre-000137 index has no room for per-pen rows -- they are exactly what it cannot hold. Keep
-- the earliest completion per legacy key so the survivor is the first proof recorded, not an
-- arbitrary one, then restore the narrow index.
DELETE FROM feed_distribution_completions r
WHERE EXISTS (
  SELECT 1 FROM feed_distribution_completions keep
  WHERE keep.tenant_id = r.tenant_id AND keep.park_id = r.park_id AND keep.shed_id = r.shed_id
    AND keep.session_no = r.session_no AND keep.target_date = r.target_date AND keep.workflow = r.workflow
    AND (keep.created_at, keep.completion_id) < (r.created_at, r.completion_id)
);
DELETE FROM feed_packing_completions r
WHERE EXISTS (
  SELECT 1 FROM feed_packing_completions keep
  WHERE keep.tenant_id = r.tenant_id AND keep.park_id = r.park_id AND keep.shed_id = r.shed_id
    AND keep.session_no = r.session_no AND keep.target_date = r.target_date AND keep.workflow = r.workflow
    AND (keep.created_at, keep.completion_id) < (r.created_at, r.completion_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS feed_distribution_completions_natural_uq
  ON feed_distribution_completions (tenant_id, park_id, shed_id, session_no, target_date, workflow);
CREATE UNIQUE INDEX IF NOT EXISTS feed_packing_completions_natural_uq
  ON feed_packing_completions (tenant_id, park_id, shed_id, session_no, target_date, workflow);

ALTER TABLE feed_distribution_completions DROP COLUMN IF EXISTS partition_key;
ALTER TABLE feed_packing_completions DROP COLUMN IF EXISTS partition_key;
ALTER TABLE feed_distribution_completions DROP COLUMN IF EXISTS partition_label;
ALTER TABLE feed_packing_completions DROP COLUMN IF EXISTS partition_label;
