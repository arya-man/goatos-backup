-- +goose Up
-- K3 is a SEVEN DAY weaning window, not a standing cohort.
--
-- Maintainer rule 2026-08-15: an animal shifted into K3 draws milk for 7 days, then stops. This
-- matches the source ladder, where K3 spans days 78-84 (goats-and-parks-source-findings) -- the
-- rule was always 7 days; nothing enforced it. Until now a K3 animal drew 400ml/day forever,
-- because no code path in backend/internal/** ever advances management_stage on its own: the tag
-- changes only when a human moves or re-tags the animal.
--
-- k3_milk_started_on is that clock. It is a DATE, not a timestamp, because a feed day is a business
-- day in Asia/Kolkata and a kid does not start weaning at 14:32.
--
-- NULL MEANS NO MILK, and that is the whole design:
--
--   * Every future entry INTO K3 sets it, in the same statement that writes the tag (relocation and
--     the admin stage edit both). So a K3 animal with no clock is one that never entered K3 through
--     this system.
--   * The animals already sitting in K3 today are exactly that population, and the maintainer's
--     rule for them is explicit: count them as 0 days remaining, they have already had their week.
--     Leaving them NULL IS that answer -- no backfill row, no milk.
--   * It also fails safe in the other direction. A future write path that sets K3 without setting
--     the clock stops that animal's milk rather than feeding it forever, which is the error a
--     human notices.
--
-- The one exception is an animal whose K3 entry is on RECORD. If a shifting moved it into K3 three
-- days ago, its goat.stage_changed event says so, and "already completed 7 days" is factually wrong
-- for it -- it is a new arrival in the pen with four days left, which is the case the maintainer's
-- first sentence covers. Those get their real start date; everything else stays NULL.
--
-- Leaving K3 clears the clock (see applyRelocation), so an animal that returns to K3 later starts a
-- fresh week rather than inheriting a spent one.

ALTER TABLE public.goats
  ADD COLUMN IF NOT EXISTS k3_milk_started_on date;

-- Backfill ONLY the on-record entries. occurred_at is an instant; the clock is a business day, so
-- it converts through Asia/Kolkata rather than UTC -- an 02:00 IST move belongs to that day, not
-- the one before.
--
-- Bounded to live K3 animals of one tenant (tens to low hundreds), one index seek each on
-- goat_identity_events_tenant_goat_timeline_keyset_idx. Runs once, here, never on a request path.
WITH current_k3 AS (
  SELECT g.tenant_id, g.goat_id
  FROM public.goats g
  WHERE g.merged_into_goat_id IS NULL
    AND g.lifecycle_status = 'alive'
    AND upper(regexp_replace(trim(coalesce(g.management_stage, '')), '[^A-Za-z0-9]+', '', 'g')) = 'K3'
),
entered AS (
  SELECT
    k.tenant_id,
    k.goat_id,
    (
      SELECT (e.occurred_at AT TIME ZONE 'Asia/Kolkata')::date
      FROM public.goat_identity_events e
      WHERE e.tenant_id = k.tenant_id
        AND e.goat_id = k.goat_id
        AND e.event_type = 'goat.stage_changed'
        AND upper(regexp_replace(trim(coalesce(e.payload->>'management_stage', '')), '[^A-Za-z0-9]+', '', 'g')) = 'K3'
      ORDER BY e.occurred_at DESC, e.identity_event_id DESC
      LIMIT 1
    ) AS started_on
  FROM current_k3 k
)
UPDATE public.goats g
   SET k3_milk_started_on = e.started_on,
       updated_at = now()
  FROM entered e
 WHERE g.tenant_id = e.tenant_id
   AND g.goat_id = e.goat_id
   AND e.started_on IS NOT NULL
   AND g.k3_milk_started_on IS DISTINCT FROM e.started_on;

-- +goose Down
ALTER TABLE public.goats
  DROP COLUMN IF EXISTS k3_milk_started_on;
