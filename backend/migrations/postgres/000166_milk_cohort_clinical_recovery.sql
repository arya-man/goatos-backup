-- +goose Up
-- A kid in ICU still drinks milk. Recover WHICH milk it drinks from its own stage history.
--
-- goats.management_stage carries TWO facts at once: the operational cohort (K1/K2/K3 = the milk
-- ladder) and, for imported animals, a clinical placement tag (ICU-Kid, Quarantine kids). When the
-- clinical tag wins, the K-band is not hidden -- it is OVERWRITTEN. Milk Preparation filters
-- `management_stage IN ('K1','K2','K3')` and prices each band separately (K1 800ml, K2 1200ml,
-- K3 400ml), so an ICU-housed kid contributes ZERO ml and the ICU shed's milk is never prepared.
-- Milk FEEDING already counts those animals ('ICUKID' is in its eligibility list since 000096), so
-- the two surfaces disagree about who is on milk today.
--
-- milk_cohort is the recovered band, and ONLY the recovered band. It is deliberately narrow:
--
--   * It is populated ONLY for animals whose CURRENT stage is a clinical kid tag. An ordinary K2
--     kid keeps answering from management_stage and gets no row here, so there is exactly one
--     source of truth per animal and the two can never drift.
--   * A clinical kid with NO recoverable K band stays NULL and is EXCLUDED from milk preparation
--     (maintainer decision: "if icu-goat doesn't have prior tag ignore it"). NULL means "we do not
--     know", and a guessed band is a guessed milk volume for a sick kid -- the worst place in this
--     system to guess. Those animals are handled by hand, not by a default.
--   * It is resolved ON WRITE and stored. Reading it costs the milk query one already-selected
--     column; deriving it per request would put a per-animal history scan on a paged operator
--     screen, which is the compute-on-read shape scale-guard blocks.
--
-- WHY HISTORY IS A SAFE SOURCE HERE. The stage event records the transition itself
-- (previous_management_stage -> management_stage, written by insertStageChangeIdentityEvents), so
-- "the band this animal carried before it became clinical" is a stored fact, not an inference. That
-- is the whole reason this is recoverable without dates of birth -- ages are unreliable across much
-- of this herd and cannot drive the milk ladder.
--
-- WHAT THIS DOES NOT REPAIR. Animals that arrived from the source import ALREADY tagged ICU-Kid
-- never transitioned through the system, so they have no stage event and stay NULL. That is the
-- expected outcome, not a failure of the backfill.

ALTER TABLE public.goats
  ADD COLUMN IF NOT EXISTS milk_cohort text;

ALTER TABLE public.goats
  DROP CONSTRAINT IF EXISTS goats_milk_cohort_check;

-- NOT VALID keeps the existing-row check off the ACCESS EXCLUSIVE lock. Validation is split into
-- 000169 so the table scan does not run while this DDL transaction still holds the add-constraint
-- lock.
ALTER TABLE public.goats
  ADD CONSTRAINT goats_milk_cohort_check
  CHECK (milk_cohort IS NULL OR milk_cohort IN ('K1', 'K2', 'K3')) NOT VALID;

-- Backfill from stage history.
--
-- Clinical kid tags are matched through the SAME normalization the milk feeding materializer uses
-- (000096/000097), because the imported herd genuinely carries both 'ICU- kid' and 'ICU-Kid'. Both
-- 'Quarantine kids' (the spelling in the live herd and in animal_stage_lookup) and
-- 'Quarantine milk kid' (the spelling the feeding list matches) are accepted here, so the recovery
-- does not silently depend on which of the two a given tenant seeded.
--
-- The correlated lookup is an index seek per animal on
-- goat_identity_events_tenant_goat_timeline_keyset_idx (tenant_id, goat_id, occurred_at DESC,
-- identity_event_id DESC), over a set bounded to the clinically-housed kids of one tenant --
-- tens of animals, not the herd. It runs once, here, and never on a request path.
WITH clinical_kids AS (
  SELECT g.tenant_id, g.goat_id
  FROM public.goats g
  WHERE g.merged_into_goat_id IS NULL
    AND g.lifecycle_status = 'alive'
    AND upper(regexp_replace(trim(coalesce(g.management_stage, '')), '[^A-Za-z0-9]+', '', 'g'))
        IN ('ICUKID', 'QUARANTINEKIDS', 'QUARANTINEKID', 'QUARANTINEMILKKID')
),
recovered AS (
  SELECT
    ck.tenant_id,
    ck.goat_id,
    (
      -- Newest transition OUT OF a milk band. For K2 -> ICU-Kid this reads 'K2' straight off the
      -- event that destroyed it. ORDER BY is on the full keyset (occurred_at, identity_event_id)
      -- so two events in the same instant still resolve deterministically rather than arbitrarily.
      SELECT upper(regexp_replace(trim(e.payload->>'previous_management_stage'), '[^A-Za-z0-9]+', '', 'g'))
      FROM public.goat_identity_events e
      WHERE e.tenant_id = ck.tenant_id
        AND e.goat_id = ck.goat_id
        AND e.event_type = 'goat.stage_changed'
        AND upper(regexp_replace(trim(coalesce(e.payload->>'previous_management_stage', '')), '[^A-Za-z0-9]+', '', 'g'))
            IN ('K1', 'K2', 'K3')
      ORDER BY e.occurred_at DESC, e.identity_event_id DESC
      LIMIT 1
    ) AS milk_cohort
  FROM clinical_kids ck
)
UPDATE public.goats g
   SET milk_cohort = r.milk_cohort,
       updated_at = now()
  FROM recovered r
 WHERE g.tenant_id = r.tenant_id
   AND g.goat_id = r.goat_id
   AND r.milk_cohort IS NOT NULL
   AND g.milk_cohort IS DISTINCT FROM r.milk_cohort;

-- +goose Down
-- Dropping the column drops goats_milk_cohort_check with it, so this needs no separate DROP
-- CONSTRAINT -- which on a hot table would be an unreviewed ACCESS EXCLUSIVE of its own.
ALTER TABLE public.goats
  DROP COLUMN IF EXISTS milk_cohort;
