-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: operational Weighing free-flow kernel work items are written by the Weighing publish transaction and the kernel worker; they do not change the Vaccination HRMS seed contract
--
-- FORWARD-REPAIR (M9, P1) for the damage 000076 could only PREVENT, not undo.
--
-- 000069, 000072 and 000076 ARE NOT EDITED HERE (checksum drift; see 000081's
-- header).
--
-- ROOT CAUSE -- the hole 000076 leaves open.
--
-- 000072 deleted every weighing_work_items row whose parent campaign read
-- status='draft', with no lock on the campaign row. PublishCampaign flips
-- weighing_campaigns.status to 'published' and inserts the campaign's real
-- work items via ON CONFLICT DO NOTHING in the SAME transaction
-- (createWorkItemsForPublishTx). A conflict-path ON CONFLICT DO NOTHING does
-- not bump the row's xmax, so Postgres has no EPQ re-check to fire: if 000072's
-- DELETE and a concurrent publish overlapped, the DELETE could remove rows the
-- publish was relying on and the publish would still commit "successfully".
--
-- 000076 correctly closed the RACE going forward -- it re-runs the same delete
-- under an explicit FOR UPDATE on weighing_campaigns, so a concurrent publish
-- must serialise against it. But 000076 is a DELETE. It cannot resurrect rows
-- 000072 already destroyed. Its own scope is campaigns that are draft RIGHT
-- NOW; a campaign that lost its work items to the race is, by definition,
-- PUBLISHED now -- outside 000076's predicate entirely, and outside 000069's
-- too (000069 has already run and is recorded as applied, so it will never
-- backfill again).
--
-- CONSEQUENCE: a published campaign can sit with zero (or partial) work items.
-- weighing_work_items IS the durable obligation for weighing -- no row means no
-- kernel sweep, no roll-forward, no delayed/escalation pass, no day-start
-- surfacing to the assigned operator, and no Calendar/Control Tower presence.
-- Leadership authorised the work, the campaign shows as published, and the
-- operational kernel simply does not know it exists. Nothing self-heals this:
-- createWorkItemsForPublishTx only runs on publish, which has already
-- happened, and the kernel sweeper only ever advances rows that exist.
--
-- FIX: re-materialise the MISSING work items for campaigns that are published
-- (any non-draft, non-canceled status), ANCHORED ON THE SURVIVING SIBLINGS'
-- ACTUAL planned_business_date.
--
-- A previous revision re-derived createWorkItemsForPublishTx's day-offset window
-- from scratch at repair time. That is wrong, and measurably so: the window ranged
-- over the bucket set AS IT STANDS NOW, while the rows it had to line up with were
-- dated from the set as it stood AT PUBLISH. Any post-publish edit slid the two
-- apart. With cap 100 and buckets A/B/C/D of 100 animals each, publish gives
-- D = start+3; cancel B afterwards and the recomputed window regenerated D at
-- start+2, on top of its surviving sibling C -- 200 animals booked on one
-- operator-day against a cap of 100.
--
-- The surviving work items ARE the record of what publish decided, so they are the
-- anchor rather than something to be re-derived around. Buckets are walked in the
-- publish planner's own order (display_name, campaign_shed_id) within an operator
-- partition, and each missing bucket is placed relative to the nearest EARLIER
-- bucket that still has a work item:
--
--   date = anchor.planned_business_date + (animals already booked on the anchor's
--          operator-day) / GREATEST(planned_cap_per_day, 1)
--
-- That integer division IS the publish planner's `floor(running_sum / cap)`,
-- re-expressed as the carry out of a day whose load is read from the surviving
-- rows instead of recomputed. Where the campaign has not changed the two agree
-- exactly, because the animals standing on the anchor's day are precisely the
-- publish-time running sum's remainder within that day's band -- including the
-- deliberate overflow publish allows when a bucket straddles the cap. Where the
-- campaign HAS changed, the survivors still win, so a regenerated bucket lands
-- after the day its siblings occupy instead of on top of it.
--
-- The anchor search deliberately does NOT skip canceled buckets. A bucket canceled
-- after publish keeps its work item, and that row is still holding the operator-day
-- it was given; stepping over it would drop its animals from the carry and place
-- the regenerated bucket straight onto it.
--
-- WHAT THIS NOW GUARANTEES: a regenerated bucket never lands on an operator-day
-- whose surviving load already meets planned_cap_per_day. Cap overflow can still
-- appear on a day, but only in the one shape publish itself produces -- a single
-- bucket larger than the remaining room, placed on a day that was under cap.
--
-- RESIDUAL, honestly stated. When NO earlier bucket in the operator partition has a
-- surviving work item there is nothing to anchor on, and the walk falls back to
-- start_business_date and packs forward -- which reproduces the publish window over
-- the CURRENT bucket set, carrying exactly the divergence described above if the
-- campaign changed after publish. A wholly destroyed campaign has no better
-- evidence available anywhere in the database. Anchoring also cannot recover a
-- publish-time date that no surviving row ever witnessed: if the buckets flanking a
-- gap were themselves edited after publish, the repair reproduces the schedule the
-- survivors now describe, not the one publish wrote.
--
-- WORK STATE is inferred from the bucket's CURRENT status rather than blindly
-- 'scheduled' (which is all publish-time knows): a bucket that has since been
-- completed/closed/canceled must not be resurrected as live work an operator is
-- chased for. This mirrors 000069's mapping exactly. Canceled buckets are
-- skipped entirely, matching createWorkItemsForPublishTx's own
-- `cs.status NOT IN ('canceled')`.
--
-- RACE SAFETY: the candidate campaigns are locked FOR UPDATE before their work
-- items are read/inserted, the same discipline 000076 introduced and for the
-- same reason -- a concurrent publish, cancel or status transition must
-- serialise against this repair rather than interleave with it. On top of that
-- the insert is ON CONFLICT (tenant_id, campaign_shed_id) DO NOTHING, so even a
-- publish that wins the lock and inserts first simply makes this a no-op for
-- that bucket. This migration can therefore never create a duplicate work item
-- for a bucket, which is the one failure mode that would be worse than the
-- missing row it is fixing.
--
-- BATCHED / LOCK-SAFE / RESUMABLE / IDEMPOTENT: per the contract documented
-- once in 000081. Campaigns are repaired in committed slices, so the FOR UPDATE
-- locks are held for one small slice at a time instead of across every damaged
-- campaign in the database; lock_timeout and statement_timeout are re-armed
-- inside each slice; the predicate is self-draining (a campaign with no missing
-- buckets stops being selected), so a killed run keeps its completed slices and
-- resumes, and a re-run -- or a healthy database that never hit the race --
-- selects zero campaigns and exits on the first slice.
--
-- NO TRANSACTION is required because the batching procedure COMMITs between
-- slices.
--
-- projection-review: producer = weighing_campaign_sheds (tenant_id,
-- campaign_id, campaign_shed_id PK, location_id, status, operator_user_id,
-- expected_animal_count) joined many:1 to weighing_campaigns (tenant_id,
-- campaign_id, start_business_date, planned_cap_per_day, park_id, status),
-- which contributes no rows of its own -- the join is 1:1 per bucket and
-- nothing can fan out. Consumer = weighing_work_items (tenant_id,
-- campaign_shed_id), the exact unique key of weighing_work_items_bucket_uidx.
-- The occupancy sum joins weighing_work_items back to its bucket on that same
-- unique key, so a day's load counts each bucket once. Carry numerator (animals
-- standing on the anchor's day) and denominator (planned_cap_per_day) both range
-- over the same (tenant_id, campaign_id, operator_user_id) key set, so no ratio is
-- compared across mismatched grains.

CREATE OR REPLACE PROCEDURE public.weighing_repair_published_missing_work_items(
  batch_size int DEFAULT 50,
  max_slices int DEFAULT 100000
)
LANGUAGE plpgsql
AS $$
DECLARE
  slice_no int := 0;
  repaired bigint := 0;
  slice_campaigns bigint;
  slice_inserted bigint;
  bucket record;
  anchor_date date;
  booked_animals bigint;
  placed_date date;
  bucket_inserted bigint;
BEGIN
  INSERT INTO public.weighing_repair_batch_progress (repair_key)
  VALUES ('000083_weighing_published_campaign_missing_work_items_repair')
  ON CONFLICT (repair_key) DO NOTHING;

  LOOP
    slice_no := slice_no + 1;
    IF slice_no > max_slices THEN
      RAISE EXCEPTION 'weighing published-campaign work-item repair exceeded % slices; predicate is not draining', max_slices;
    END IF;

    -- Transaction-scoped, so they MUST be re-armed after each COMMIT below.
    PERFORM set_config('lock_timeout', '5s', true);
    PERFORM set_config('statement_timeout', '60s', true);

    -- Claim + lock one slice of damaged campaigns. The FOR UPDATE is what makes
    -- this safe against a concurrent publish/cancel of the same campaign; the
    -- lock is held only until this slice's COMMIT below.
    CREATE TEMP TABLE weighing_missing_wi_slice ON COMMIT DROP AS
    SELECT c.tenant_id, c.campaign_id
    FROM public.weighing_campaigns c
    WHERE c.status NOT IN ('draft', 'canceled')
      AND EXISTS (
        SELECT 1
        FROM public.weighing_campaign_sheds cs
        WHERE cs.tenant_id = c.tenant_id
          AND cs.campaign_id = c.campaign_id
          AND cs.status <> 'canceled'
          -- An unassigned bucket is work nobody has been given yet: publish could
          -- not have written a work item for it (operator_user_id is NOT NULL
          -- there), so it is not damage. It MUST also stay out of this predicate
          -- or the slice would re-select the same campaign forever and trip the
          -- max_slices guard.
          AND cs.operator_user_id IS NOT NULL
          AND NOT EXISTS (
            SELECT 1
            FROM public.weighing_work_items wi
            WHERE wi.tenant_id = cs.tenant_id
              AND wi.campaign_shed_id = cs.campaign_shed_id
          )
      )
    ORDER BY c.tenant_id, c.campaign_id
    LIMIT batch_size
    FOR UPDATE OF c;

    GET DIAGNOSTICS slice_campaigns = ROW_COUNT;

    slice_inserted := 0;

    -- Bucket-at-a-time ON PURPOSE, in the publish planner's own order. Each
    -- placement reads the operator-day load that the placements before it have
    -- already written, so a run of consecutive missing buckets packs forward off
    -- one another exactly as publish packed them. A single set-based statement
    -- cannot see its own inserts, which is what forced the earlier revision to
    -- recompute a window instead of anchoring on one.
    FOR bucket IN
      SELECT cs.tenant_id,
             cs.campaign_id,
             cs.campaign_shed_id,
             c.park_id,
             cs.operator_user_id,
             cs.weighing_category,
             cs.display_name,
             cs.location_id,
             cs.status,
             c.start_business_date,
             GREATEST(c.planned_cap_per_day, 1) AS cap_per_day
      FROM public.weighing_campaign_sheds cs
      JOIN weighing_missing_wi_slice s
        ON s.tenant_id = cs.tenant_id
       AND s.campaign_id = cs.campaign_id
      JOIN public.weighing_campaigns c
        ON c.tenant_id = cs.tenant_id
       AND c.campaign_id = cs.campaign_id
      WHERE cs.status <> 'canceled'
        AND cs.operator_user_id IS NOT NULL
        AND NOT EXISTS (
          SELECT 1
          FROM public.weighing_work_items wi
          WHERE wi.tenant_id = cs.tenant_id
            AND wi.campaign_shed_id = cs.campaign_shed_id
        )
      ORDER BY cs.tenant_id, cs.campaign_id, cs.operator_user_id,
               cs.display_name, cs.campaign_shed_id
    LOOP
      -- The nearest EARLIER bucket of this operator that still holds a work item.
      -- Canceled buckets count here: their work item survives and is still
      -- standing on the operator-day publish gave it.
      SELECT wi.planned_business_date
        INTO anchor_date
      FROM public.weighing_campaign_sheds p
      JOIN public.weighing_work_items wi
        ON wi.tenant_id = p.tenant_id
       AND wi.campaign_shed_id = p.campaign_shed_id
      WHERE p.tenant_id = bucket.tenant_id
        AND p.campaign_id = bucket.campaign_id
        AND p.operator_user_id = bucket.operator_user_id
        AND (p.display_name, p.campaign_shed_id) < (bucket.display_name, bucket.campaign_shed_id)
      ORDER BY p.display_name DESC, p.campaign_shed_id DESC
      LIMIT 1;

      -- Nothing earlier survived, so there is no witness to what publish decided
      -- and start_business_date is the only floor left. See the header's RESIDUAL.
      IF anchor_date IS NULL THEN
        anchor_date := bucket.start_business_date;
      END IF;

      SELECT COALESCE(sum(GREATEST(p.expected_animal_count, 1)), 0)
        INTO booked_animals
      FROM public.weighing_work_items wi
      JOIN public.weighing_campaign_sheds p
        ON p.tenant_id = wi.tenant_id
       AND p.campaign_shed_id = wi.campaign_shed_id
      WHERE wi.tenant_id = bucket.tenant_id
        AND wi.campaign_id = bucket.campaign_id
        AND wi.operator_user_id = bucket.operator_user_id
        AND wi.planned_business_date = anchor_date;

      -- This integer division IS createWorkItemsForPublishTx's
      -- floor(running_sum / cap), read off the surviving rows rather than
      -- recomputed: it is the carry out of the anchor's day.
      placed_date := anchor_date + (booked_animals / bucket.cap_per_day)::int;

      INSERT INTO public.weighing_work_items (
        tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id,
        weighing_category, shed_label, shed_location_id,
        planned_business_date, due_business_date, work_state
      )
      VALUES (
        bucket.tenant_id,
        bucket.campaign_id,
        bucket.campaign_shed_id,
        bucket.park_id,
        bucket.operator_user_id,
        bucket.weighing_category,
        bucket.display_name,
        bucket.location_id,
        placed_date,
        placed_date,
        CASE bucket.status
          WHEN 'completed' THEN 'completed'
          WHEN 'closed' THEN 'closed'
          ELSE 'scheduled'
        END
      )
      ON CONFLICT (tenant_id, campaign_shed_id) DO NOTHING;

      GET DIAGNOSTICS bucket_inserted = ROW_COUNT;
      slice_inserted := slice_inserted + bucket_inserted;
    END LOOP;
    repaired := repaired + slice_inserted;

    UPDATE public.weighing_repair_batch_progress
    SET batches_run = batches_run + 1,
        -- ACCUMULATE, do not assign. `repaired` is call-local, so on a RESUMED run it
        -- restarts at zero: a run that repaired 5,000 rows, was killed, then resumed and
        -- repaired 200 would overwrite the true total with 200. That is precisely the
        -- under-reporting 000081's progress table exists to prevent, and 000082 already
        -- accumulates for this reason -- this file kept the wrong form.
        rows_repaired = weighing_repair_batch_progress.rows_repaired + slice_inserted,
        last_batch_at = now(),
        completed_at = CASE WHEN slice_campaigns = 0 THEN now() ELSE NULL END
    WHERE repair_key = '000083_weighing_published_campaign_missing_work_items_repair';

    COMMIT;

    EXIT WHEN slice_campaigns = 0;
  END LOOP;
END;
$$;

CALL public.weighing_repair_published_missing_work_items();

-- One-shot migration tool, not an API: dropping it prevents an out-of-band
-- re-run against a campaign whose work items were legitimately removed later.
DROP PROCEDURE IF EXISTS public.weighing_repair_published_missing_work_items(int, int);

-- +goose Down
-- +goose NO TRANSACTION
-- DOWN is a no-op, same rationale as 000069/000072/000076: by the time a DOWN
-- ran, the regenerated work items may already have been claimed, rolled
-- forward, completed or closed by the kernel and by real operators, and a DOWN
-- cannot distinguish a row this migration recreated from one a subsequent
-- legitimate publish wrote. Deleting them would re-open the exact
-- invisible-obligation hole this migration exists to close.
SELECT 1;
