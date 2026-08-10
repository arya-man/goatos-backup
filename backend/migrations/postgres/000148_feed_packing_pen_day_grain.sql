-- +goose Up
-- Feed PACKING is proved ONCE PER PEN PER DAY, not once per pen per feeding session.
--
-- Maintainer decision 2026-08-10, SUPERSEDING the packing half of the 2026-07-26 verification gate.
-- A packer packs a pen's whole day in one go, so the crew was being shown the same pen twice --
-- Morning and Evening -- and asked to film the same work twice. The two sessions are now a
-- BREAKDOWN inside one worklist card ("Morning 12.5 kg, Evening 12.5 kg") backed by ONE video and
-- ONE verification item.
--
-- Feed DISTRIBUTION is NOT touched. It is still gated per shed-session and keeps session_no in its
-- natural key; only feed_packing_completions changes here. The two tables have moved in lockstep
-- since 000032/000033 and this is the first place they diverge, deliberately.
--
-- The PEN stays in the key. 000137 put it there because one Castro - 1 clip was closing out
-- Castro - 2 and Castro - 3; collapsing the session must not be read as licence to collapse the
-- partition too. After this migration the key is
-- (tenant_id, park_id, shed_id, partition_key, target_date, workflow) -- session out, pen in.

-- ---------------------------------------------------------------------------
-- 1. session_no becomes the pen-day sentinel 0
-- ---------------------------------------------------------------------------
-- The column is RETAINED rather than dropped so pre-merge rows stay readable as history with the
-- session they were actually shot for. Everything written from now on carries 0. The old CHECK
-- (session_no >= 1) has to go for that, and is replaced with >= 0 rather than removed outright so a
-- negative session number is still rejected.
ALTER TABLE feed_packing_completions
  DROP CONSTRAINT IF EXISTS feed_packing_completions_session_no_check;
ALTER TABLE feed_packing_completions
  ADD CONSTRAINT feed_packing_completions_session_no_check CHECK (session_no >= 0);

-- ---------------------------------------------------------------------------
-- 2. Retire the verification items of the rows about to be superseded
-- ---------------------------------------------------------------------------
-- Done BEFORE the completions are collapsed, while the superseded completion_ids still exist.
--
-- 'withdrawn' rather than DELETE: the clip and its audit trail stay readable, and the item leaves
-- the verifier's PENDING queue instead of sitting there forever pointing at a completion row that
-- no longer exists. An orphaned pending item is the failure mode this step exists to prevent --
-- FeedPackingVerificationHandler would find no row to apply, so a verifier could approve it every
-- day and nothing would ever happen.
--
-- Only 'pending' items are touched. An already-approved or already-rejected item is a verdict that
-- was really cast and is left exactly as it is.
WITH ranked AS (
  SELECT
    completion_id,
    row_number() OVER (
      PARTITION BY tenant_id, park_id, shed_id, partition_key, target_date, workflow
      ORDER BY
        -- Never discard the most advanced state: a verified completion outranks one still waiting,
        -- which outranks one bounced back for rework.
        CASE status WHEN 'completed' THEN 0 WHEN 'pending_verification' THEN 1 ELSE 2 END,
        -- Then the earliest real submission, so the survivor is the video that has been waiting on
        -- the verifier longest. completion_id last purely so the order is total and the migration
        -- is deterministic on a re-run.
        created_at,
        completion_id
    ) AS rn
  FROM feed_packing_completions
)
UPDATE verification_items v
SET status = 'withdrawn',
    updated_at = now(),
    row_version = v.row_version + 1
FROM ranked
WHERE ranked.rn > 1
  AND v.source_module = 'feed'
  AND v.source_ref_type = 'feed_packing_completion'
  AND v.source_ref_id = ranked.completion_id
  AND v.status = 'pending';

-- ---------------------------------------------------------------------------
-- 3. Collapse the completions to one row per pen-day
-- ---------------------------------------------------------------------------
-- Only rows that would violate the new unique index are removed, and only the losers of the same
-- deterministic ranking used above, so the survivor is the one whose item was just left alone.
--
-- On a park that has never had two sessions submitted for one pen this deletes nothing.
WITH ranked AS (
  SELECT
    completion_id,
    row_number() OVER (
      PARTITION BY tenant_id, park_id, shed_id, partition_key, target_date, workflow
      ORDER BY
        CASE status WHEN 'completed' THEN 0 WHEN 'pending_verification' THEN 1 ELSE 2 END,
        created_at,
        completion_id
    ) AS rn
  FROM feed_packing_completions
)
DELETE FROM feed_packing_completions c
USING ranked
WHERE c.completion_id = ranked.completion_id
  AND ranked.rn > 1;

-- Every surviving row is now THE pen-day row, so it carries the sentinel.
UPDATE feed_packing_completions
SET session_no = 0,
    updated_at = now()
WHERE session_no <> 0;

-- ---------------------------------------------------------------------------
-- 4. Swap the natural key
-- ---------------------------------------------------------------------------
-- Dropped first: the two indexes cannot coexist while every row carries session_no 0, and creating
-- the new one before dropping the old would leave the old index uniquing on a column that is now
-- constant -- which is the same constraint, spelled worse.
DROP INDEX IF EXISTS feed_packing_completions_natural_uq;
CREATE UNIQUE INDEX IF NOT EXISTS feed_packing_completions_natural_uq
  ON feed_packing_completions (tenant_id, park_id, shed_id, partition_key, target_date, workflow);

-- ---------------------------------------------------------------------------
-- 5. Repair the surviving items' subject label
-- ---------------------------------------------------------------------------
-- An in-flight item still reads "Session 1 · Castro - 2", which now names one half of work the
-- verifier is being asked to judge whole and reads as though a second clip were still owed. Strip
-- the prefix and leave the pen, which is what the producer composes from here on.
--
-- Anchored with a strict prefix match so a label that never carried the prefix is untouched, and
-- the pen is taken from the text AFTER the separator rather than being recomposed, so this cannot
-- invent a partition for a shed that has none.
UPDATE verification_items
SET subject_label = btrim(substring(subject_label FROM position(' · ' IN subject_label) + 3)),
    updated_at = now()
WHERE source_module = 'feed'
  AND source_ref_type = 'feed_packing_completion'
  AND subject_label ~ '^Session [0-9]+ · .+'
  AND btrim(substring(subject_label FROM position(' · ' IN subject_label) + 3)) <> '';

-- A packing item whose label was ONLY "Session N" (an undivided shed whose location could not be
-- composed) has no pen to fall back to. NULL is the honest value -- the schema allows it, and the
-- verifier's screen then shows the item's own shed/park fields instead of a session number that no
-- longer describes anything.
UPDATE verification_items
SET subject_label = NULL,
    updated_at = now()
WHERE source_module = 'feed'
  AND source_ref_type = 'feed_packing_completion'
  AND subject_label ~ '^Session [0-9]+$';

-- +goose Down
-- Deliberately NOT reversible. The collapse in step 3 removed rows and step 2 withdrew items; a
-- down migration cannot resurrect either, and re-splitting a pen-day into per-session rows would
-- have to invent which session a video was shot for. Roll forward.
SELECT 1;
