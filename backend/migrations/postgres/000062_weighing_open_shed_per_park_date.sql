-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: operational Weighing planner tables only; no Vaccination HRMS seed contract change
--
-- ONE OPEN WEIGHING ROW PER (park, weigh_date, shed).
--
-- A weighing task is one PARK on one WEIGH DATE holding N shed buckets. The
-- weigh date is the campaign's existing `start_business_date` (an Asia/Kolkata
-- business DATE, never an instant). Duplicate work -- the same shed weighed
-- twice on the same date under two different tasks -- must be impossible, not
-- merely discouraged by the planner UI.
--
-- Uniqueness cannot span tables, so the two identity columns that live on
-- weighing_campaigns (park_id, start_business_date) are denormalized onto
-- weighing_campaign_sheds. They are written by every campaign write path AND
-- defended here by a NOT VALID check constraint, so a future write path that
-- forgets them fails loudly instead of silently switching the guard off.
--
-- Lock safety:
--   * ADD COLUMN ... NULL is metadata-only on Postgres (no rewrite).
--   * The backfill is bounded by the number of campaign shed rows, which is
--     planner-grain (parks x sheds x dates), not observation-grain.
--   * ADD CONSTRAINT ... NOT VALID takes a brief ACCESS EXCLUSIVE lock and
--     performs NO table scan; existing rows are not re-checked (they were just
--     backfilled).
--   * CREATE UNIQUE INDEX CONCURRENTLY takes only SHARE UPDATE EXCLUSIVE, so
--     operator capture keeps writing while the index builds. That is also why
--     this whole file runs with NO TRANSACTION.
--
-- Free-flow is preserved: nothing here touches goats, rosters, scanned
-- identifiers, or vaccination.

ALTER TABLE public.weighing_campaign_sheds
  ADD COLUMN IF NOT EXISTS park_id uuid NULL,
  ADD COLUMN IF NOT EXISTS start_business_date date NULL;

-- BACKFILL -- required. Without it every pre-existing bucket carries NULL in the
-- index key columns, and a partial unique index over NULLs enforces nothing, so
-- the guard would appear to be installed while blocking nothing.
UPDATE public.weighing_campaign_sheds shed
SET park_id = campaign.park_id,
    start_business_date = campaign.start_business_date
FROM public.weighing_campaigns campaign
WHERE campaign.tenant_id = shed.tenant_id
  AND campaign.campaign_id = shed.campaign_id
  AND (shed.park_id IS NULL OR shed.start_business_date IS NULL);

-- KEEPING THE COPY HONEST.
--
-- park_id/start_business_date are a SECOND copy of task identity, and a second
-- copy that drifts is worse than no copy: the unique index would quietly stop
-- defending the real slot. Rather than trusting every present and future write
-- path to remember two columns, the row derives them from its own campaign
-- whenever they are not supplied. Application writes still set them explicitly;
-- this is the floor, not the mechanism.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.weighing_campaign_sheds_fill_task_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.park_id IS NULL OR NEW.start_business_date IS NULL THEN
    SELECT COALESCE(NEW.park_id, wc.park_id),
           COALESCE(NEW.start_business_date, wc.start_business_date)
      INTO NEW.park_id, NEW.start_business_date
    FROM public.weighing_campaigns wc
    WHERE wc.tenant_id = NEW.tenant_id
      AND wc.campaign_id = NEW.campaign_id;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS weighing_campaign_sheds_fill_task_identity_trg ON public.weighing_campaign_sheds;
CREATE TRIGGER weighing_campaign_sheds_fill_task_identity_trg
  BEFORE INSERT OR UPDATE OF campaign_id, park_id, start_business_date
  ON public.weighing_campaign_sheds
  FOR EACH ROW
  EXECUTE FUNCTION public.weighing_campaign_sheds_fill_task_identity();

ALTER TABLE public.weighing_campaign_sheds
  DROP CONSTRAINT IF EXISTS weighing_campaign_sheds_task_identity_check;
ALTER TABLE public.weighing_campaign_sheds
  ADD CONSTRAINT weighing_campaign_sheds_task_identity_check
  CHECK (park_id IS NOT NULL AND start_business_date IS NOT NULL) NOT VALID;

-- PRE-EXISTING DOUBLE BOOKINGS.
--
-- A unique index cannot be created over data that already violates it: the
-- CONCURRENTLY build would simply fail and leave an INVALID index behind. Two
-- kinds of pre-existing duplicate exist and they are NOT the same problem:
--
--   * Planning-only duplicates (no proof captured in the younger row) are safe
--     to retire automatically -- nobody's work is lost, only a duplicate plan.
--     They are canceled here, deterministically keeping the OLDEST row.
--   * Duplicates where BOTH rows hold captured evidence are a real data
--     question (which weighing is the truth?) and must not be resolved by a
--     migration. The deploy is failed with the conflicting ids so a human
--     decides.
-- +goose StatementBegin
DO $$
DECLARE
  conflicting text;
BEGIN
  -- ANY duplicate group in which ANY member already holds captured evidence is a
  -- real data question and is refused here rather than silently resolved.
  --
  -- The earlier form of this guard applied `HAVING count(*) > 1` to rows ALREADY
  -- filtered down to the evidenced ones, so a group holding exactly ONE evidenced
  -- row fell through to the automatic repair below -- and that repair keeps the
  -- OLDEST row, which may well be the empty draft. The younger row carrying the
  -- real weights was then canceled out of every open-bucket predicate, in a
  -- forward-only NO TRANSACTION migration. Group first, then ask whether the
  -- group contains evidence at all.
  SELECT string_agg(cs.campaign_shed_id::text, ', ' ORDER BY cs.campaign_shed_id)
  INTO conflicting
  FROM public.weighing_campaign_sheds cs
  WHERE cs.status NOT IN ('canceled', 'closed')
    AND (cs.tenant_id, cs.park_id, cs.start_business_date, cs.location_id) IN (
      SELECT dup.tenant_id, dup.park_id, dup.start_business_date, dup.location_id
      FROM public.weighing_campaign_sheds dup
      WHERE dup.status NOT IN ('canceled', 'closed')
      GROUP BY dup.tenant_id, dup.park_id, dup.start_business_date, dup.location_id
      HAVING count(*) > 1
         AND bool_or(
           EXISTS (SELECT 1 FROM public.weighing_observations wo
                    WHERE wo.tenant_id = dup.tenant_id AND wo.campaign_shed_id = dup.campaign_shed_id)
           OR EXISTS (SELECT 1 FROM public.weighing_shed_observations wso
                    WHERE wso.tenant_id = dup.tenant_id AND wso.campaign_shed_id = dup.campaign_shed_id)
         )
    );

  IF conflicting IS NOT NULL THEN
    RAISE EXCEPTION 'weighing: cannot enforce one open shed per park/weigh date -- these buckets are double-booked and at least one already holds captured weights, so which weighing is the truth must be resolved by hand first: %', conflicting;
  END IF;

  -- Planning-only duplicates: every surviving duplicate group is now provably
  -- evidence-free on every member, so retiring all but one loses no captured
  -- work. Keep the OLDEST claim. `has_evidence DESC` is kept as the leading
  -- ORDER BY term anyway so that if this block is ever reached with evidence
  -- present (a future edit to the guard above), the evidenced row is the keeper
  -- rather than the accident of creation order.
  UPDATE public.weighing_campaign_sheds cs
  SET status = 'canceled', updated_at = now()
  WHERE cs.status NOT IN ('canceled', 'closed')
    AND cs.campaign_shed_id NOT IN (
      SELECT DISTINCT ON (keep.tenant_id, keep.park_id, keep.start_business_date, keep.location_id)
             keep.campaign_shed_id
      FROM public.weighing_campaign_sheds keep
      WHERE keep.status NOT IN ('canceled', 'closed')
      ORDER BY keep.tenant_id, keep.park_id, keep.start_business_date, keep.location_id,
               (
                 EXISTS (SELECT 1 FROM public.weighing_observations wo
                          WHERE wo.tenant_id = keep.tenant_id AND wo.campaign_shed_id = keep.campaign_shed_id)
                 OR EXISTS (SELECT 1 FROM public.weighing_shed_observations wso
                          WHERE wso.tenant_id = keep.tenant_id AND wso.campaign_shed_id = keep.campaign_shed_id)
               ) DESC,
               keep.created_at, keep.campaign_shed_id
    );
END
$$;
-- +goose StatementEnd

-- THE GUARD. Partial on the OPEN statuses only: a canceled plan and a closed
-- task are finished history and must never block scheduling that shed again on
-- a later date, nor keep a mistaken plan permanently occupying the slot.
-- 'completed' IS open for this purpose -- the shed was already weighed on that
-- date, so scheduling it a second time on the same date is the exact duplicate
-- work this index exists to stop.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_weighing_open_shed_per_park_date
  ON public.weighing_campaign_sheds (tenant_id, park_id, start_business_date, location_id)
  WHERE status NOT IN ('canceled', 'closed');

-- Serves the planner catalog's "is this shed already taken on this date?"
-- lookup, which is scoped by (tenant, weigh date) across ALL parks and so
-- cannot use the unique index above (park_id sits between the two predicate
-- columns and would have to be skipped).
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaign_sheds_open_date_idx
  ON public.weighing_campaign_sheds (tenant_id, start_business_date, location_id)
  INCLUDE (campaign_id, park_id, operator_user_id, weighing_category, status)
  WHERE status NOT IN ('canceled', 'closed');

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_open_date_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.uq_weighing_open_shed_per_park_date;
DROP TRIGGER IF EXISTS weighing_campaign_sheds_fill_task_identity_trg ON public.weighing_campaign_sheds;
DROP FUNCTION IF EXISTS public.weighing_campaign_sheds_fill_task_identity();
ALTER TABLE public.weighing_campaign_sheds
  DROP CONSTRAINT IF EXISTS weighing_campaign_sheds_task_identity_check;
ALTER TABLE public.weighing_campaign_sheds
  DROP COLUMN IF EXISTS start_business_date,
  DROP COLUMN IF EXISTS park_id;
