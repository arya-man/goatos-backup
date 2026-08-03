-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: operational Weighing verification items are written by the Weighing capture/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- FORWARD-REPAIR (M11(a), P1) for 000077, which does not repair the COMMON
-- case it was written for.
--
-- 000077 IS NOT EDITED HERE (checksum drift; see 000081's header).
--
-- ROOT CAUSE -- an off-by-one universe in the ranking.
--
-- 000077 must identify the LOSER rows of a duplicate-tag group so it can
-- withdraw their verifier queue entries. It does that by re-deriving 000073's
-- own rank:
--     row_number() OVER (PARTITION BY tenant_id, campaign_shed_id,
--                                     lower(btrim(scanned_identifier))
--                        ORDER BY accepted_at DESC, observation_id DESC)
-- ...but it computes that rank over a PRE-FILTERED universe:
--     WHERE submitted_at IS NOT NULL AND submitted_at = accepted_at
-- and then takes rn > 1.
--
-- That filter is the 000073 LOSER fingerprint itself. So the ranking is
-- computed over losers ONLY -- the group's actual WINNER (the surviving open
-- row, submitted_at IS NULL) is excluded from the partition before
-- row_number() ever runs. In the canonical duplicate produced by the race
-- 000073 exists to stop -- ONE winner and ONE loser, which is exactly what two
-- concurrent captures of the same tag create -- the single loser is therefore
-- ranked rn = 1 inside its filtered partition and `rn > 1` matches NOTHING.
-- The two-row case, i.e. the overwhelmingly common case, is silently skipped.
-- Only a group with THREE or more rows (two or more losers) gets any repair at
-- all, and even then only its second-and-later losers -- the newest loser is
-- always missed for the same reason.
--
-- CONSEQUENCE: precisely the symptom 000077 was written to remove survives.
-- The loser's verification_items row stays 'pending', so the verifier queue
-- still shows a superseded round as a genuine, undecided submission. A verifier
-- is asked to judge video/weight evidence that was never the animal's accepted
-- round, and until someone decides it the bucket's close gate
-- (pending_verification_count) keeps counting it. Verification stays stuck on a
-- row nobody can legitimately decide.
--
-- FIX: rank over the WHOLE group, then apply the loser fingerprint as a
-- FILTER on the ranked result instead of as a precondition of the ranking.
-- The winner then correctly occupies rn = 1 and the single loser correctly
-- lands at rn > 1, so the two-row case repairs. The fingerprint condition is
-- kept exactly as 000077 defined it (submitted_at IS NOT NULL AND
-- submitted_at = accepted_at) so the SET of rows this migration is willing to
-- touch is unchanged -- this is strictly a correction of WHICH of those rows
-- are recognised as losers, never a widening to rows 000077 would have
-- refused. A genuine submit stamps submitted_at = now() strictly after its own
-- accepted_at, and the audit/submit-derived backfills in 000070/000074 stamp
-- an audit or submit-event timestamp, so equality with accepted_at remains the
-- 000073/000082 "superseded" marker and not a real submission.
--
-- RETIRE PATH: unchanged from 000077 -- verification_items.status =
-- 'withdrawn', the module's own "producer supersedes, not decides" state added
-- in 000067 and used by WithdrawItemsBySource / reviseVerificationRound. No
-- verdict ('verified'/'rework') is fabricated for a round no verifier actually
-- reviewed, and weighing_observations.submitted_at is not touched (000073 and
-- 000082 already made the immutable-history call there).
--
-- KNOWN RESIDUAL, unchanged and restated so it is not mistaken for fixed:
-- weighing_observations.verification_status stays 'pending' on the loser
-- because its CHECK constraint admits only 'pending'/'verified'/'rework' and
-- close.go's pending_verification_count reads that column with no
-- "superseded/excluded" bucket. Widening that is an application-code change
-- (close.go + a widened CHECK) outside this worktree's ownership. The
-- verifier-facing QUEUE -- driven by verification_items.status, which is what
-- M11(a) actually reports -- is fixed by this migration for the two-row case.
--
-- BATCHED / LOCK-SAFE / RESUMABLE / IDEMPOTENT: per the contract documented
-- once in 000081. Withdrawals happen in committed slices bounded by
-- item_id keyset order; lock_timeout and statement_timeout are re-armed inside
-- each slice; the predicate is self-draining (a withdrawn item is no longer
-- 'pending', so it cannot be selected twice), so a killed run keeps its
-- completed slices and resumes, and a re-run -- or a database that never raced
-- -- withdraws zero rows and exits on the first slice. Re-running after
-- 000077 already withdrew a 3+-row group's older losers is likewise a no-op
-- for those rows and only picks up the ones 000077's ranking missed.
--
-- NO TRANSACTION is required because the batching procedure COMMITs between
-- slices.
--
-- projection-review: producer = weighing_observations (tenant_id,
-- campaign_shed_id, scanned_identifier, accepted_at, submitted_at), used only
-- to derive the loser observation_id set; the partition key is byte-identical
-- to 000073's and to weighing_observations_one_open_tag_uidx's grain.
-- Consumer = verification_items (tenant_id, source_module='weighing',
-- source_ref_type='weighing_observation', source_ref_id=observation_id,
-- status). Row multiplicity: 1:1 -- each capture raises its own
-- idempotency-keyed verification item, so a loser observation maps to at most
-- one still-pending item and no fan-out is possible.

CREATE OR REPLACE PROCEDURE public.weighing_repair_duplicate_loser_verification_items(
  batch_size int DEFAULT 500,
  max_slices int DEFAULT 100000
)
LANGUAGE plpgsql
AS $$
DECLARE
  slice_no int := 0;
  withdrawn bigint := 0;
  remaining bigint;
  slice_withdrawn bigint;
BEGIN
  INSERT INTO public.weighing_repair_batch_progress (repair_key)
  VALUES ('000084_weighing_duplicate_loser_verification_two_row_repair')
  ON CONFLICT (repair_key) DO NOTHING;

  -- TARGET SET, COMPUTED ONCE. The duplicate groups are a closed historical
  -- set (000073's index stops new ones), so the ranking scan is paid once here
  -- rather than once per slice. Losing this table to a crash is harmless: it is
  -- rebuilt from live data on the next run, and an already-withdrawn item is no
  -- longer 'pending' so it cannot be selected twice.
  CREATE TEMP TABLE weighing_loser_verification_targets AS
  WITH ranked AS (
    -- THE FIX: no submitted_at pre-filter here. The whole duplicate group --
    -- winner included -- is ranked, so the winner takes rn = 1 and a lone loser
    -- is correctly rn = 2 instead of being crowned rn = 1 by its own absence of
    -- competition (000077's defect).
    SELECT wo.observation_id,
           wo.tenant_id,
           wo.submitted_at,
           wo.accepted_at,
           row_number() OVER (
             PARTITION BY wo.tenant_id, wo.campaign_shed_id, lower(btrim(wo.scanned_identifier))
             ORDER BY wo.accepted_at DESC, wo.observation_id DESC
           ) AS rn
    FROM public.weighing_observations wo
    WHERE wo.campaign_shed_id IS NOT NULL
      AND btrim(wo.scanned_identifier) <> ''
  ),
  loser_ids AS (
    SELECT observation_id, tenant_id
    FROM ranked
    WHERE rn > 1
      -- 000077's loser fingerprint, applied AFTER ranking rather than before.
      AND submitted_at IS NOT NULL
      AND submitted_at = accepted_at
  )
  SELECT vi.item_id
  FROM public.verification_items vi
  JOIN loser_ids l
    ON l.tenant_id = vi.tenant_id
   AND l.observation_id = vi.source_ref_id
  WHERE vi.source_module = 'weighing'
    AND vi.source_ref_type = 'weighing_observation'
    AND vi.status = 'pending';

  LOOP
    slice_no := slice_no + 1;
    IF slice_no > max_slices THEN
      RAISE EXCEPTION 'weighing duplicate-loser verification repair exceeded % slices; predicate is not draining', max_slices;
    END IF;

    -- Transaction-scoped, so they MUST be re-armed after each COMMIT below.
    PERFORM set_config('lock_timeout', '2s', true);
    PERFORM set_config('statement_timeout', '30s', true);

    -- Pop a bounded slice of targets and withdraw them. Popping is what bounds
    -- the loop: the target table strictly shrinks every iteration. The status =
    -- 'pending' guard is kept on the UPDATE itself so an item a legitimate
    -- verdict or ReopenScope withdrawal touched between the scan and now is
    -- left exactly as that path left it.
    WITH popped AS (
      DELETE FROM weighing_loser_verification_targets t
      WHERE t.ctid IN (
        SELECT ctid FROM weighing_loser_verification_targets
        ORDER BY item_id
        LIMIT batch_size
      )
      RETURNING t.item_id
    )
    UPDATE public.verification_items vi
    SET status = 'withdrawn',
        row_version = vi.row_version + 1,
        updated_at = now()
    FROM popped p
    WHERE vi.item_id = p.item_id
      AND vi.status = 'pending';

    GET DIAGNOSTICS slice_withdrawn = ROW_COUNT;
    withdrawn := withdrawn + slice_withdrawn;

    SELECT count(*) INTO remaining FROM weighing_loser_verification_targets;

    UPDATE public.weighing_repair_batch_progress
    SET batches_run = batches_run + 1,
        rows_repaired = withdrawn,
        last_batch_at = now(),
        completed_at = CASE WHEN remaining = 0 THEN now() ELSE NULL END
    WHERE repair_key = '000084_weighing_duplicate_loser_verification_two_row_repair';

    COMMIT;

    EXIT WHEN remaining = 0;
  END LOOP;

  DROP TABLE IF EXISTS weighing_loser_verification_targets;
END;
$$;

CALL public.weighing_repair_duplicate_loser_verification_items();

-- One-shot migration tool, not an API: dropping it prevents an out-of-band
-- re-run withdrawing items a legitimate later flow re-raised.
DROP PROCEDURE IF EXISTS public.weighing_repair_duplicate_loser_verification_items(int, int);

-- +goose Down
-- +goose NO TRANSACTION
-- DOWN is a no-op, same rationale as 000077's: restoring these items to
-- 'pending' would push superseded duplicate rounds back into the verifier
-- queue as if they were genuine undecided submissions, and a DOWN cannot tell
-- an item this migration withdrew from one a legitimate ReopenScope/rework
-- path withdrew afterwards.
SELECT 1;
