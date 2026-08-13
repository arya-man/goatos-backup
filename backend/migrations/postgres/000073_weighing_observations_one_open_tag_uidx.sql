-- +goose Up
-- +goose NO TRANSACTION
-- recordUnknownAnimalObservationTx was check-then-act: an "updated" CTE tries to
-- UPDATE an existing open row (submitted_at IS NULL OR verification_status=
-- 'rework') for this tag, then an "inserted" CTE inserts WHERE NOT EXISTS
-- (SELECT 1 FROM updated). Under READ COMMITTED, two concurrent captures of the
-- SAME scanned tag in the SAME bucket, with DIFFERENT idempotency keys, each see
-- no open row and both insert. The only unique index on the table
-- (weighing_observations_idempotency_uidx) only stops a retry of the SAME key --
-- it does nothing for two different keys racing the same tag. Both commit,
-- leaving two rows with submitted_at IS NULL for one animal: two videos equally
-- "current" proof, nothing disambiguates them.
--
-- The sibling lump-sum path already has exactly this shape of guarantee --
-- weighing_shed_observations_one_open_scope_uidx, partial on withdrawn_at IS
-- NULL, added in 000067 -- and repository.go already knows how to turn its
-- 23505 into a typed domain conflict (mapShedUniqueViolation) instead of a raw
-- 500. This migration adds the per-animal-tag equivalent and repository.go
-- gets the matching translation for it.
--
-- GRAIN: (tenant_id, campaign_shed_id, lower(btrim(scanned_identifier))).
-- campaign_shed_id, not just campaign_id, because free-flow weighing allows the
-- SAME raw tag to legitimately appear in DIFFERENT buckets/sheds at once (a
-- goat is not fenced to one shed by this table) -- exactly the grain the write
-- path itself already matches on in the "updated" CTE.
--
-- PREDICATE: WHERE submitted_at IS NULL.
--
--   1. Re-capture after submit is unaffected: submit stamps submitted_at
--      (migration 000061), so a later fresh capture of the same tag in the
--      same bucket inserts a NEW row with submitted_at NULL that does not
--      collide with the old, now-excluded, submitted row.
--   2. Rework does NOT need a separate carve-out. markObservationRework
--      (verification_verdict.go) flips verification_status to 'rework' but
--      deliberately leaves submitted_at AS-IS -- it stays NOT NULL, exactly as
--      it was when the operator submitted it (see the comment at
--      rework_rescan_and_captured_total_integration_test.go:86). A rework row
--      is therefore ALREADY excluded from this partial index by submitted_at
--      alone; recapture of that tag continues to hit the "updated" CTE's
--      explicit "OR verification_status='rework'" branch and UPDATEs the SAME
--      row in place, which never touches this index. No dupe-vs-rework
--      ambiguity: a rework row and a fresh open row for the same tag do not
--      (and structurally cannot, by this table's own write path) coexist,
--      since the very first fresh capture after rework recaptures the rework
--      row itself rather than inserting a second one.
--
-- PRE-CLEANUP: this partial index enforces at most one submitted_at IS NULL row
-- per (tenant_id, campaign_shed_id, tag). The bug being fixed here means real
-- data CAN already hold duplicates of exactly that shape, so CREATE UNIQUE
-- INDEX CONCURRENTLY would abort with a uniqueness violation on the very rows
-- this migration exists to stop producing. Before building the index, collapse
-- each duplicate group down to one open survivor: keep the newest by
-- accepted_at (tie-broken by observation_id) -- the most recently captured
-- video is the most defensible "current" proof, matching what the buggy
-- concurrent path itself intended as the final state absent the race. Losers
-- are NOT deleted (immutable-history discipline, same as 000067's withdrawn_at
-- pattern): they are stamped submitted_at = accepted_at, the same value 000061
-- backfilled for rows made historical by a bucket completion, which is exactly
-- what "this row stopped being the open round" already means on this table.
-- This is additive/corrective data movement only -- no row is destroyed, and
-- the surviving row for every group is unchanged.
UPDATE public.weighing_observations losers
SET submitted_at = losers.accepted_at
FROM (
  SELECT observation_id,
    row_number() OVER (
      PARTITION BY tenant_id, campaign_shed_id, lower(btrim(scanned_identifier))
      ORDER BY accepted_at DESC, observation_id DESC
    ) AS rn
  FROM public.weighing_observations
  WHERE submitted_at IS NULL
    AND campaign_shed_id IS NOT NULL
    AND btrim(scanned_identifier) <> ''
) ranked
WHERE ranked.observation_id = losers.observation_id
  AND ranked.rn > 1;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS weighing_observations_one_open_tag_uidx
  ON public.weighing_observations (tenant_id, campaign_shed_id, (lower(btrim(scanned_identifier))))
  WHERE submitted_at IS NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_observations_one_open_tag_uidx;
