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
-- (any non-draft, non-canceled status), using the byte-identical planner that
-- createWorkItemsForPublishTx uses -- the same greedy day-offset window
-- (running EXCLUSIVE bucket size over GREATEST(expected_animal_count, 1),
-- partitioned per (tenant_id, campaign_id, operator_user_id) because capacity
-- is operator-business-date grain, divided by GREATEST(planned_cap_per_day, 1),
-- ordered by display_name then campaign_shed_id). The window is computed over
-- the campaign's FULL non-canceled bucket set and only THEN filtered to the
-- missing buckets, rather than restarting the offset from zero.
--
-- HONEST LIMIT, measured rather than assumed. That window is the bucket set as it
-- stands AT REPAIR TIME, not as it stood at publish time, so it reproduces the
-- publish planner only for a campaign whose shape has not changed since. A
-- post-publish cancel shrinks the running sum and pulls every later bucket one day
-- earlier: with cap 100 and buckets A/B/C/D of 100 animals each, publish gives
-- D = start+3; cancel B afterwards and this repair regenerates D at start+2, where
-- its surviving sibling C already sits -- 200 animals booked on one operator-day
-- against a cap of 100. The same divergence follows any post-publish change to
-- planned_cap_per_day, start_business_date, expected_animal_count, or the bucket
-- set.
--
-- An earlier version of this header claimed the regenerated dates 'line up with the
-- dates its surviving siblings already carry'. That claim was FALSE and is
-- withdrawn: this migration never reads a surviving sibling's actual
-- planned_business_date, so alignment is not something it can guarantee. Anchoring
-- on a surviving sibling is the real fix and is deliberately NOT attempted here --
-- it changes what the repair computes, and this file is meant to restore rows, not
-- to re-plan a campaign. A regenerated date that collides is visible and
-- correctable; the missing row it replaces was not.
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
-- Day-offset numerator (running bucket size) and denominator
-- (planned_cap_per_day) both range over the same
-- (tenant_id, campaign_id, operator_user_id) key set, so no ratio is compared
-- across mismatched grains.

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

    INSERT INTO public.weighing_work_items (
      tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id,
      weighing_category, shed_label, shed_location_id,
      planned_business_date, due_business_date, work_state
    )
    SELECT planned.tenant_id,
           planned.campaign_id,
           planned.campaign_shed_id,
           planned.park_id,
           planned.operator_user_id,
           planned.weighing_category,
           planned.display_name,
           planned.location_id,
           planned.start_business_date + planned.day_offset,
           planned.start_business_date + planned.day_offset,
           CASE planned.status
             WHEN 'completed' THEN 'completed'
             WHEN 'closed' THEN 'closed'
             ELSE 'scheduled'
           END
    FROM (
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
             floor(
               COALESCE(
                 sum(GREATEST(cs.expected_animal_count, 1)) OVER (
                   PARTITION BY cs.tenant_id, cs.campaign_id, cs.operator_user_id
                   ORDER BY cs.display_name, cs.campaign_shed_id
                   ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
                 ), 0
               )::numeric / GREATEST(c.planned_cap_per_day, 1)::numeric
             )::int AS day_offset
      FROM public.weighing_campaign_sheds cs
      JOIN weighing_missing_wi_slice s
        ON s.tenant_id = cs.tenant_id
       AND s.campaign_id = cs.campaign_id
      JOIN public.weighing_campaigns c
        ON c.tenant_id = cs.tenant_id
       AND c.campaign_id = cs.campaign_id
      -- The window MUST see every non-canceled bucket of the campaign, not just
      -- the missing ones, or a partial repair would re-plan surviving siblings'
      -- days on top of itself.
      WHERE cs.status <> 'canceled'
    ) planned
    WHERE NOT EXISTS (
      SELECT 1
      FROM public.weighing_work_items wi
      WHERE wi.tenant_id = planned.tenant_id
        AND wi.campaign_shed_id = planned.campaign_shed_id
    )
    ON CONFLICT (tenant_id, campaign_shed_id) DO NOTHING;

    GET DIAGNOSTICS slice_inserted = ROW_COUNT;
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
