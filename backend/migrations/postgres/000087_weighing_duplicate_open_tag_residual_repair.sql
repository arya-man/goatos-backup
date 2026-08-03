-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: operational Weighing observations are written by the Weighing mobile/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- FORWARD-REPAIR (M5, P1) for the DUPLICATE residual 000074 deliberately left
-- behind.
--
-- 000074 IS NOT EDITED HERE (checksum drift; see 000086's header and 000074's
-- own preamble). Neither is 000070 or 000073.
--
-- ROOT CAUSE -- what 000074 fixed and what it could not.
--
-- 000070 mis-stamped submitted_at from CAPTURE-time audit evidence
-- ('weighing.observation_accepted'), freezing never-submitted draft captures.
-- 000074's step 1 undoes that by clearing submitted_at back to NULL for rows
-- still bearing that exact fingerprint (submitted_at = the latest
-- 'weighing.observation_accepted' audit timestamp for that observation --
-- a real submit stamps now() strictly AFTER its own capture, so the exact
-- equality is 000070's signature, not a coincidence).
--
-- But 000074 guards that clear with:
--     AND NOT EXISTS (... another row for the same
--         (tenant_id, campaign_shed_id, lower(btrim(scanned_identifier)))
--         with submitted_at IS NULL)
-- because clearing into a key that already has an open row would violate
-- 000073's partial unique index weighing_observations_one_open_tag_uidx. Its
-- own comment names the leftover honestly: "this row is left as-is (still
-- incorrectly frozen) ... a narrow, identifiable residual that a follow-up can
-- target explicitly". THIS is that follow-up.
--
-- The residual is not cosmetic. The duplicate-tag race 000073 exists to stop
-- produced groups of rows for ONE animal in ONE bucket. 000073 then collapsed
-- each group to a single open survivor -- the newest by
-- (accepted_at DESC, observation_id DESC), "the most recently captured video is
-- the most defensible current proof". When 000070 had ALSO frozen that very
-- survivor with capture-time evidence, 000074 could not reopen it (an older
-- sibling was the one left open), so the group is left with the WRONG round
-- open: the animal's current, verifier-facing, closeable proof is a superseded
-- older capture, and the newest capture is frozen behind a timestamp nobody
-- ever submitted. Both rows are "corrupted" relative to 000073's own stated
-- tie-break, and no later code path re-evaluates it -- the live write path
-- only ever recaptures the open row, so the group stays wrong forever.
--
-- FIX -- restore 000073's tie-break inside the duplicate group, in an order
-- that never collides with the very index that blocked 000074:
--
--   PHASE 1 (freeze) runs FIRST: every non-winner row that is currently open
--   is stamped submitted_at = accepted_at -- exactly 000073's own loser
--   convention (immutable history: the row is retired, never deleted, and the
--   value means "this row stopped being the open round", which is what 000061
--   already backfilled for rows made historical by bucket completion).
--   PHASE 1 also NORMALISES any non-winner row still carrying 000070's
--   fabricated capture timestamp to accepted_at, so a loser's submitted_at
--   stops being a value that was never a submit and starts being the
--   recognised "superseded" marker -- which is also what makes the (HELD) loser
--   verification-item withdrawal's
--   verification withdrawal able to see it.
--
--   PHASE 2 (reopen) runs SECOND, only after phase 1 has vacated the key:
--   the group's tie-break winner, if it is still frozen with 000070's
--   fingerprint, is cleared back to submitted_at = NULL. Because phase 1
--   already committed no other open row into that key, this cannot raise
--   23505 on weighing_observations_one_open_tag_uidx -- the exact hazard that
--   forced 000074 to skip these groups.
--
-- SCOPE GUARD: only groups that contain at least one 000070-fingerprint row
-- are touched at all, and within them phase 2 only fires when the winner
-- itself is fingerprinted AND some other row is open (the precise
-- 000074-skipped shape). A group whose winner is legitimately frozen by a real
-- submit is never reopened -- this migration never manufactures an open round
-- out of genuinely submitted work. The single-row (non-duplicate) case is
-- 000074's own domain and is deliberately not re-litigated here.
--
-- WHAT IS NOT DONE: verification_status / verification_items are untouched
-- here -- retiring the losers' verifier queue entries was to be the job of the
-- loser-verification withdrawal that was drafted as 000084 and WITHDRAWN before
-- merge (000084 is a burned version number; see 000086's header -- when this
-- work does ship it takes a NEW number, never 000084). That
-- migration is deliberately HELD OUT of this change: withdrawing the loser's
-- verification item while weighing_observations.verification_status stays 'pending'
-- leaves close.go's hard pending>0 gate permanently unsatisfiable, so the bucket
-- could never be closed AT ALL. That is now strictly true: the close gate is
-- UNCONDITIONAL and there is no force path of any kind, so a bucket whose loser
-- was retired without also clearing its observation would be stuck forever. It
-- ships with the widened CHECK + close.go change, not before. Until then the
-- loser's queue entry stays pending and a verifier can still clear it, which is
-- exactly what keeps the bucket closable. The rest of
-- doing it in one migration would conflate two independent repairs. No row is
-- deleted; every change is a submitted_at movement inside one duplicate group.
--
-- BATCHED / LOCK-SAFE / RESUMABLE / IDEMPOTENT: per the contract documented
-- once in 000086. Groups are repaired in committed slices; lock_timeout and
-- statement_timeout are re-armed inside every slice (they are
-- transaction-scoped and the procedure COMMITs); the selection predicate is
-- self-draining -- a repaired group stops matching (phase 2 clears the
-- winner's fingerprint, phase 1 clears the "open non-winner" and "fabricated
-- value" conditions) -- so a killed run resumes exactly where it stopped and a
-- re-run, or a fresh database that never raced, matches zero groups and exits
-- on the first slice. A hard slice ceiling makes the loop non-infinite by
-- construction.
--
-- TWO GAPS IN THAT CONTRACT, CLOSED HERE BEFORE THIS EVER RAN ANYWHERE.
--
-- (1) The timeouts were armed only INSIDE the loop, so the one statement that
-- reads the whole observations table -- the candidate scan -- was the single
-- statement in this migration running with the session's inherited (in
-- practice unlimited) budget. On a large table that is an unbounded scan
-- holding a deploy open with no ceiling and no way to fail fast, which is
-- exactly the shape a migration must never have. The timeouts are now armed
-- before the scan as well, with a budget stated for the scan specifically:
-- large enough that an honest scan finishes, bounded enough that a pathological
-- one fails instead of hanging.
--
-- (2) The candidate queue was a TEMP table, so an operator killing the run --
-- or the new scan timeout firing -- threw the completed scan away and the next
-- attempt paid it again from zero. On a table big enough for (1) to matter,
-- that is a repair that can never finish under a maintenance window: every
-- attempt spends its budget re-deriving work the previous attempt had already
-- derived. The queue is now an ordinary table committed as soon as it is
-- populated, and the loop pops from it transactionally, so a killed run resumes
-- against the groups that are genuinely still queued and never rescans. It is
-- dropped when the queue drains, so a finished (or never-damaged) database is
-- left with no residue and a re-run scans once and exits.
--
-- The queue is a work list, not a decision: it is derived from live rows and
-- every group it names is re-planned against live rows in the slice that
-- repairs it, so losing it, truncating it, or resuming from it can only change
-- how much work is REDONE, never what the repair concludes. That is what keeps
-- (2) compatible with 000086's rule that progress bookkeeping is observability
-- only.
--
-- LOCKED, NOT JUST SNAPSHOTTED: the slice plan is built from live rows and then
-- used to write those same rows, so between the two an operator rescanning that
-- animal could move accepted_at/submitted_at underneath it -- and phase 1
-- writes submitted_at = the accepted_at it PLANNED on, so the repair would
-- stamp a fresh capture with a stale value and retire evidence that had just
-- become the current round. The slice therefore takes a row lock on every
-- member of the popped groups BEFORE planning, in observation_id order (a
-- stable order, so two slices can never lock each other in opposite directions)
-- and holds it to the slice's COMMIT. A concurrent rescan then either lands
-- before the lock -- and is read by the plan as the live truth it is -- or
-- waits behind it and applies to a repaired group. lock_timeout bounds that
-- wait: losing the race aborts the run rather than blocking the deploy, and the
-- committed queue makes the retry cheap.
--
-- NO TRANSACTION is required: the batching procedure COMMITs between slices,
-- which is only legal when the migration runner is not already holding an
-- explicit transaction open around it.
--
-- projection-review: producer = audit_log (resource_type=
-- 'weighing_observation', action='weighing.observation_accepted',
-- resource_id=observation_id, created_at), aggregated to MAX(created_at) per
-- observation so many audit rows per observation cannot fan out; joined 1:1 on
-- observation_id. Consumer = weighing_observations (observation_id,
-- submitted_at). Grouping key = (tenant_id, campaign_shed_id,
-- lower(btrim(scanned_identifier))), byte-identical to the grain of the index
-- (weighing_observations_one_open_tag_uidx) being protected and to 000073's
-- own partition, so ranking here and uniqueness there can never disagree.

CREATE OR REPLACE PROCEDURE public.weighing_repair_duplicate_open_tag_residual(
  batch_size int DEFAULT 200,
  max_slices int DEFAULT 100000
)
LANGUAGE plpgsql
AS $$
DECLARE
  slice_no int := 0;
  changed bigint := 0;
  slice_groups bigint;
  slice_changed bigint;
  slice_total bigint;
BEGIN
  INSERT INTO public.weighing_repair_batch_progress (repair_key)
  VALUES ('000087_weighing_duplicate_open_tag_residual_repair')
  ON CONFLICT (repair_key) DO NOTHING;

  -- Armed HERE, not only inside the loop: the candidate scan below is the one
  -- statement that reads the whole table, and it used to be the only statement
  -- in this migration with no ceiling on how long it could run or how long it
  -- could wait for a lock. The scan budget is deliberately larger than a
  -- slice's -- it is a single full pass, not a bounded write -- but it is a
  -- budget, so a pathological plan fails the migration fast instead of holding
  -- a deploy open indefinitely.
  PERFORM set_config('lock_timeout', '2s', true);
  PERFORM set_config('statement_timeout', '15min', true);

  -- CANDIDATE SET, COMPUTED ONCE AND COMMITTED. The damaged groups are a closed
  -- historical set -- the live write path can no longer create one (000073's
  -- index plus the corrected submit path), and this procedure is the only thing
  -- repairing them -- so the ranking scan is paid once here instead of once per
  -- slice, and once per REPAIR rather than once per attempt: the queue is an
  -- ordinary table, so an aborted run leaves the still-unrepaired groups behind
  -- to resume from instead of forcing a full rescan. Existing means a prior
  -- attempt was interrupted mid-drain; its contents are re-planned against live
  -- rows before anything is written, so resuming cannot act on stale facts.
  IF to_regclass('public.weighing_dup_candidates_000087') IS NULL THEN
    CREATE TABLE public.weighing_dup_candidates_000087 AS
    WITH fingerprinted AS (
      -- 000070's signature: submitted_at exactly equals this observation's own
      -- latest capture-acceptance audit timestamp.
      SELECT wo.observation_id
      FROM public.weighing_observations wo
      JOIN LATERAL (
        SELECT max(a.created_at) AS last_accepted_at
        FROM public.audit_log a
        WHERE a.resource_type = 'weighing_observation'
          AND a.action = 'weighing.observation_accepted'
          AND a.resource_id = wo.observation_id
      ) evidence ON TRUE
      WHERE wo.campaign_shed_id IS NOT NULL
        AND wo.submitted_at IS NOT NULL
        AND wo.submitted_at = evidence.last_accepted_at
    ),
    -- projection-review: membership=weighing_observations rows of a duplicate open-tag group, ranked within their own group; group_key=(tenant_id, campaign_shed_id, tag_key), byte-identical to weighing_observations_one_open_tag_uidx's grain so the repair partitions exactly as the constraint it is restoring; join_cardinality=the evidence LATERAL is a scalar per observation (0..1) and adds no rows, so the count per group is the true row count; pagination=BATCHED, the candidate set is drained in committed slices rather than paged for display; scope=tenant_id, carried on every row and in the partition key
    members AS (
      SELECT wo.tenant_id,
             wo.campaign_shed_id,
             lower(btrim(wo.scanned_identifier)) AS tag_key,
             wo.accepted_at,
             wo.submitted_at,
             (f.observation_id IS NOT NULL) AS is_fingerprinted,
             row_number() OVER (
               PARTITION BY wo.tenant_id, wo.campaign_shed_id, lower(btrim(wo.scanned_identifier))
               ORDER BY wo.accepted_at DESC, wo.observation_id DESC
             ) AS rn
      FROM public.weighing_observations wo
      LEFT JOIN fingerprinted f ON f.observation_id = wo.observation_id
      WHERE wo.campaign_shed_id IS NOT NULL
        AND btrim(wo.scanned_identifier) <> ''
    )
    SELECT tenant_id, campaign_shed_id, tag_key
    FROM members
    GROUP BY tenant_id, campaign_shed_id, tag_key
    HAVING count(*) > 1
       AND (
            -- the exact 000074-skipped shape: the tie-break winner is the
            -- mis-stamped row while a sibling holds the key open.
            (bool_or(rn = 1 AND is_fingerprinted) AND bool_or(rn > 1 AND submitted_at IS NULL))
            -- or a retired loser still carries 000070's fabricated capture
            -- timestamp instead of the 000073 "superseded" marker.
         OR bool_or(rn > 1 AND is_fingerprinted AND submitted_at IS DISTINCT FROM accepted_at)
       );
    -- Make the scan durable before a single group is repaired. Without this the
    -- work list would live and die with the transaction that built it, which is
    -- the whole reason a killed run used to start over.
    COMMIT;
  END IF;

  LOOP
    slice_no := slice_no + 1;
    slice_total := 0;
    IF slice_no > max_slices THEN
      RAISE EXCEPTION 'weighing duplicate-open-tag repair exceeded % slices; predicate is not draining', max_slices;
    END IF;

    -- Transaction-scoped, so they MUST be re-armed after each COMMIT below.
    PERFORM set_config('lock_timeout', '2s', true);
    PERFORM set_config('statement_timeout', '30s', true);

    -- Pop one slice of candidate groups. Popping (DELETE ... RETURNING) is what
    -- bounds the loop: the queue strictly shrinks, so the loop cannot spin. The
    -- pop and the repair commit together, so an abort returns the groups to the
    -- queue rather than dropping them unrepaired.
    CREATE TEMP TABLE weighing_dup_slice_groups ON COMMIT DROP AS
    WITH popped AS (
      DELETE FROM public.weighing_dup_candidates_000087 c
      WHERE c.ctid IN (
        SELECT ctid FROM public.weighing_dup_candidates_000087
        ORDER BY tenant_id, campaign_shed_id, tag_key
        LIMIT batch_size
      )
      RETURNING c.tenant_id, c.campaign_shed_id, c.tag_key
    )
    SELECT tenant_id, campaign_shed_id, tag_key FROM popped;

    -- LOCK BEFORE PLANNING. Everything below reads these rows and then writes
    -- them, and phase 1 writes a value (accepted_at) it read here -- so an
    -- operator rescan committing in between would be overwritten with the
    -- pre-rescan capture time and its fresh evidence retired. Taking the lock
    -- first turns that race into an ordering: a rescan is either already
    -- visible to the plan or waits until the slice commits. observation_id
    -- order keeps two slices from deadlocking each other; lock_timeout keeps a
    -- lost race from blocking the deploy, and the committed queue above makes
    -- the resulting retry cheap.
    PERFORM 1
    FROM public.weighing_observations wo
    JOIN weighing_dup_slice_groups g
      ON g.tenant_id = wo.tenant_id
     AND g.campaign_shed_id = wo.campaign_shed_id
     AND g.tag_key = lower(btrim(wo.scanned_identifier))
    ORDER BY wo.observation_id
    FOR UPDATE OF wo;

    -- Plan both phases in ONE materialised read of the now-locked rows, so
    -- phase 1 and phase 2 act on the same group set and the same winner choice.
    CREATE TEMP TABLE weighing_dup_slice_plan ON COMMIT DROP AS
    WITH popped AS (
      SELECT tenant_id, campaign_shed_id, tag_key FROM weighing_dup_slice_groups
    ),
    fingerprinted AS (
      SELECT wo.observation_id
      FROM public.weighing_observations wo
      JOIN popped p
        ON p.tenant_id = wo.tenant_id
       AND p.campaign_shed_id = wo.campaign_shed_id
       AND p.tag_key = lower(btrim(wo.scanned_identifier))
      JOIN LATERAL (
        SELECT max(a.created_at) AS last_accepted_at
        FROM public.audit_log a
        WHERE a.resource_type = 'weighing_observation'
          AND a.action = 'weighing.observation_accepted'
          AND a.resource_id = wo.observation_id
      ) evidence ON TRUE
      WHERE wo.submitted_at IS NOT NULL
        AND wo.submitted_at = evidence.last_accepted_at
    ),
    -- projection-review: membership=weighing_observations rows of a duplicate open-tag group, ranked within their own group; group_key=(tenant_id, campaign_shed_id, tag_key), byte-identical to weighing_observations_one_open_tag_uidx's grain so the repair partitions exactly as the constraint it is restoring; join_cardinality=the evidence LATERAL is a scalar per observation (0..1) and adds no rows, so the count per group is the true row count; pagination=BATCHED, the candidate set is drained in committed slices rather than paged for display; scope=tenant_id, carried on every row and in the partition key
    members AS (
      SELECT wo.observation_id,
             wo.tenant_id,
             wo.campaign_shed_id,
             p.tag_key,
             wo.accepted_at,
             wo.submitted_at,
             (f.observation_id IS NOT NULL) AS is_fingerprinted,
             row_number() OVER (
               PARTITION BY wo.tenant_id, wo.campaign_shed_id, p.tag_key
               ORDER BY wo.accepted_at DESC, wo.observation_id DESC
             ) AS rn
      FROM public.weighing_observations wo
      JOIN popped p
        ON p.tenant_id = wo.tenant_id
       AND p.campaign_shed_id = wo.campaign_shed_id
       AND p.tag_key = lower(btrim(wo.scanned_identifier))
      LEFT JOIN fingerprinted f ON f.observation_id = wo.observation_id
    ),
    decided AS (
      SELECT tenant_id, campaign_shed_id, tag_key,
             bool_or(rn = 1 AND is_fingerprinted)
               AND bool_or(rn > 1 AND submitted_at IS NULL) AS reopen_winner
      FROM members
      GROUP BY tenant_id, campaign_shed_id, tag_key
    )
    SELECT m.observation_id,
           m.rn,
           m.accepted_at,
           d.reopen_winner
    FROM members m
    JOIN decided d
      ON d.tenant_id = m.tenant_id
     AND d.campaign_shed_id = m.campaign_shed_id
     AND d.tag_key = m.tag_key
    WHERE (m.rn > 1 AND d.reopen_winner AND m.submitted_at IS NULL)
       OR (m.rn > 1 AND m.is_fingerprinted AND m.submitted_at IS DISTINCT FROM m.accepted_at)
       OR (m.rn = 1 AND d.reopen_winner);

    -- PHASE 1: retire/normalise the losers, vacating the open key. This MUST
    -- commit-order before phase 2 within the slice or the reopen below would
    -- hit weighing_observations_one_open_tag_uidx -- the very 23505 that forced
    -- 000074 to skip these groups.
    UPDATE public.weighing_observations wo
    SET submitted_at = p.accepted_at
    FROM weighing_dup_slice_plan p
    WHERE wo.observation_id = p.observation_id
      AND p.rn > 1
      AND wo.submitted_at IS DISTINCT FROM p.accepted_at;
    GET DIAGNOSTICS slice_changed = ROW_COUNT;
    changed := changed + slice_changed;
    slice_total := slice_total + slice_changed;

    -- PHASE 2: reopen the rightful winner, now that the key is free.
    UPDATE public.weighing_observations wo
    SET submitted_at = NULL
    FROM weighing_dup_slice_plan p
    WHERE wo.observation_id = p.observation_id
      AND p.rn = 1
      AND p.reopen_winner
      AND wo.submitted_at IS NOT NULL;
    GET DIAGNOSTICS slice_changed = ROW_COUNT;
    changed := changed + slice_changed;
    slice_total := slice_total + slice_changed;

    -- Loop control is the candidate table draining, NOT the number of rows a
    -- slice happened to change: a popped group that no longer qualifies (repaired
    -- out of band between the scan and now) legitimately plans zero rows, and
    -- exiting on that would abandon the candidates still queued behind it.
    SELECT count(*) INTO slice_groups FROM public.weighing_dup_candidates_000087;

    -- Accumulated on the ROW, not assigned from this call's counter: now that a
    -- killed run resumes, the counter restarts at zero while the repair does
    -- not, and assigning it would report the resumed attempt's rows as the
    -- whole repair's.
    UPDATE public.weighing_repair_batch_progress
    SET batches_run = batches_run + 1,
        rows_repaired = rows_repaired + slice_total,
        last_batch_at = now(),
        completed_at = CASE WHEN slice_groups = 0 THEN now() ELSE NULL END
    WHERE repair_key = '000087_weighing_duplicate_open_tag_residual_repair';

    COMMIT;

    EXIT WHEN slice_groups = 0;
  END LOOP;

  -- The queue exists only to survive an aborted run. Once it is drained the
  -- repair is done, and leaving an empty work list on a live schema would only
  -- invite a future reader to mistake it for state something depends on.
  DROP TABLE IF EXISTS public.weighing_dup_candidates_000087;
END;
$$;

CALL public.weighing_repair_duplicate_open_tag_residual();

-- The procedure is a one-shot migration tool, not an API. Dropping it keeps
-- the schema free of a callable that could be re-run out of band against a
-- database whose duplicate groups have since been resolved by real operator
-- work.
DROP PROCEDURE IF EXISTS public.weighing_repair_duplicate_open_tag_residual(int, int);

-- +goose Down
-- +goose NO TRANSACTION
-- DOWN is a no-op, for the same reason 000074's is. This migration moved
-- submitted_at inside duplicate groups to restore 000073's documented winner;
-- a DOWN cannot distinguish a value this migration wrote from one the live
-- submit path wrote afterwards, and re-freezing the winner (or re-opening a
-- loser) would deliberately hand the animal's current proof back to a
-- superseded capture and could violate weighing_observations_one_open_tag_uidx
-- on the way. Recovery from an incorrect run is a fresh capture, which the
-- live write path already makes safe.
SELECT 1;
