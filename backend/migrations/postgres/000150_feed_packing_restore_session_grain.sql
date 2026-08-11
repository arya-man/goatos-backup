-- +goose Up
-- Feed PACKING goes back to ONE VIDEO PER PEN PER FEEDING SESSION.
--
-- Maintainer decision 2026-08-11, REVERTING migration 000149 and the packing half of the 2026-08-10
-- pen-day grain. A pen's morning and evening shares are two separate bags: each is packed on its
-- own, filmed on its own, and verified on its own. Merging them into one card backed by one clip
-- meant a single video stood as proof for two bags, and a verifier judging it against a day total
-- could not tell a crew that packed the morning share twice from one that packed both correctly.
--
-- WHAT THIS CAN AND CANNOT UNDO. 000149 did three destructive things. This migration reverses the
-- reversible one and is honest about the rest:
--
--   1. It ADDED feed_packing_completions_pen_day_uq         -> DROPPED here.
--   2. It set every surviving row's session_no to 0          -> PROMOTED to 1 here.
--   3. It DELETED the losing row of each collapsed pen-day   -> GONE. Cannot be restored, and this
--      migration does not pretend to. Those pens' second bags simply reappear on the worklist as
--      unpacked work, which is the honest state: no video for that session exists any more.
--
-- WHY 000149 IS NOT AMENDED. It has already been applied to STG (release 2026-08-11), and STG records
-- migration checksums -- editing an applied file makes every later migration fail before it runs.
-- Forward-only, always.
--
-- THE ROLLOUT WINDOW IS NOT AN ISSUE HERE, and it is worth saying why, because 000149 needed a whole
-- expand/contract dance for it. The index the reverted binary's ON CONFLICT resolves against --
-- feed_packing_completions_natural_uq (tenant_id, park_id, shed_id, partition_key, session_no,
-- target_date, workflow) -- was deliberately KEPT by 000149 and its contract half was never shipped.
-- So the key this release needs is already in place before this file runs. Backend and app ship in
-- one deploy (maintainer decision), so there is no window in which an old instance submits packing
-- against a key that is gone.

-- ---------------------------------------------------------------------------
-- 1. Drop the pen-day key
-- ---------------------------------------------------------------------------
-- Must come BEFORE the promotion below. Both of a pen's sessions are allowed to coexist again, and
-- while this index stands they cannot: it uniques on the pen-day without the session.
DROP INDEX IF EXISTS feed_packing_completions_pen_day_uq;

-- ---------------------------------------------------------------------------
-- 2. Promote the pen-day sentinel back to a real session
-- ---------------------------------------------------------------------------
-- Maintainer decision: a surviving pen-day row becomes SESSION 1. Its video and its verification
-- verdict are kept and stand as the MORNING packing; the pen's second session carries no completion
-- row and therefore reappears on the worklist as work still owed. Nothing an operator filmed and
-- nothing a verifier approved is thrown away.
--
-- Guarded by NOT EXISTS rather than promoting blindly. After 000149 a pen-day holds exactly one row
-- and it carries 0, so the guard is expected to exclude nothing -- but a database where 000149 was
-- interrupted mid-way, or a hand-repaired one, could hold both a 0 row and a real session-1 row, and
-- an unguarded UPDATE would abort the whole migration on a unique violation with no explanation.
UPDATE feed_packing_completions c
SET session_no = 1,
    updated_at = now()
WHERE c.session_no = 0
  AND NOT EXISTS (
    SELECT 1
    FROM feed_packing_completions s
    WHERE s.tenant_id = c.tenant_id
      AND s.park_id = c.park_id
      AND s.shed_id = c.shed_id
      AND s.partition_key = c.partition_key
      AND s.target_date = c.target_date
      AND s.workflow = c.workflow
      AND s.session_no = 1
  );

-- FAIL LOUDLY on anything left behind. A remaining session_no = 0 row means the guard above found a
-- colliding session-1 row, which should be impossible. The two honest options are to delete the 0
-- row (destroying an operator's video and a verifier's verdict) or to invent a session number for it
-- (a lie about which bag was filmed). Neither is acceptable silently, so the migration stops and a
-- human decides. A broken database is not a warning.
-- +goose StatementBegin
DO $$
DECLARE
  stranded bigint;
BEGIN
  SELECT count(*) INTO stranded FROM feed_packing_completions WHERE session_no = 0;
  IF stranded > 0 THEN
    RAISE EXCEPTION
      'feed_packing_completions still holds % pen-day row(s) with session_no = 0 that collide with an existing session 1 row; resolve them by hand before re-running (see migration 000150)',
      stranded;
  END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 3. Restore the session floor
-- ---------------------------------------------------------------------------
-- Back to >= 1, the constraint 000033 shipped. 000149 relaxed it to >= 0 only to admit its sentinel;
-- with the sentinel gone, 0 is meaningless again and the database should refuse it. This is the
-- stronger control: the service also rejects session < 1 with ErrInvalidSession, but a check
-- constraint holds for importers, repair scripts and any future writer that never reads that code.
ALTER TABLE feed_packing_completions
  DROP CONSTRAINT IF EXISTS feed_packing_completions_session_no_check;
ALTER TABLE feed_packing_completions
  ADD CONSTRAINT feed_packing_completions_session_no_check CHECK (session_no >= 1);

-- ---------------------------------------------------------------------------
-- 4. Put the session back on the verifier's subject label
-- ---------------------------------------------------------------------------
-- 000149 step 5 stripped "Session N · " from every in-flight packing item, because one clip then
-- covered the whole day. A pen produces two packing videos again, so a verifier holding two cards for
-- Castro - 2 needs to know which bag each one proves; without the prefix they are indistinguishable
-- in the queue.
--
-- Joined to the completion so the number is the row's REAL session_no, never a guess. Items whose
-- completion 000149 deleted do not join and are left alone -- they are 'withdrawn' history pointing
-- at a row that no longer exists, and stamping "Session 1" on one would claim it proved a bag it did
-- not.
--
-- Bounded lock: verification_items is a hot table, so this waits a few seconds for its lock and fails
-- fast rather than queueing behind a long reader and stalling every writer behind it.
SET lock_timeout = '5s';

-- 4a. Labels that still carry a location get the prefix back.
--     Guarded against double-prefixing so a re-run is a no-op.
UPDATE verification_items v
SET subject_label = 'Session ' || c.session_no::text || ' · ' || v.subject_label,
    updated_at = now()
FROM feed_packing_completions c
WHERE v.source_module = 'feed'
  AND v.source_ref_type = 'feed_packing_completion'
  AND v.source_ref_id = c.completion_id
  AND v.subject_label IS NOT NULL
  AND btrim(v.subject_label) <> ''
  AND v.subject_label !~ '^Session [0-9]+ · ';

-- 4b. Labels 000149 set to NULL had no pen to fall back on (an undivided shed whose location could
--     not be composed). The session alone is what the producer composes for that case, so restore it
--     rather than leaving the card unlabelled.
UPDATE verification_items v
SET subject_label = 'Session ' || c.session_no::text,
    updated_at = now()
FROM feed_packing_completions c
WHERE v.source_module = 'feed'
  AND v.source_ref_type = 'feed_packing_completion'
  AND v.source_ref_id = c.completion_id
  AND (v.subject_label IS NULL OR btrim(v.subject_label) = '');

-- +goose Down
-- Deliberately NOT reversible, for the same reason 000149 was not: the rows 000149 deleted are gone
-- either way, and re-collapsing a pen's sessions into one row would have to choose which video to
-- discard. Roll forward.
SELECT 1;
