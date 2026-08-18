-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: operational Weighing free-flow tables are written by the Weighing planner/mobile/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- VERIFIER WEIGHT CORRECTION (maintainer decision 2026-08-17).
--
-- The verifier watches a weighing proof video and can now fix the number the
-- operator typed, in kg, on the same screen where she watches it. Individual
-- capture: the corrected value REPLACES that one animal's weight. Lump-sum: the
-- corrected total REPLACES the shed total, and she may fix the head count in the
-- same act, because a miscounted shed makes the average wrong too.
--
-- WHY weight_kg IS OVERWRITTEN IN PLACE rather than shadowed by a second column:
-- the corrected number IS the weight from that moment on. Every read model, ADG
-- series, demographic average, CSV export and leadership card must show it
-- WITHOUT each of them learning about corrections -- a "corrected_weight_kg
-- overrides weight_kg" column would need every one of those ~20 read sites to
-- COALESCE, and the first one that forgot would silently report the wrong
-- weight. So the live column stays the live truth and the OPERATOR'S ORIGINAL
-- moves into operator_weight_kg / operator_animal_count.
--
-- operator_weight_kg is set ONLY on the FIRST correction and never overwritten
-- afterwards, so a second correction cannot erase what the operator actually
-- entered. NULL therefore means exactly one thing: never corrected.
--
-- average_weight_kg is a STORED derived column on the lump-sum row, so it is
-- recomputed in the same statement that writes the corrected total/count.
-- Leaving it stale would make the shed's average disagree with its own total.
--
-- Free-flow is preserved: nothing here references goats, herd rosters, or
-- vaccination. A correction changes a number and who changed it; it never
-- resolves a scanned tag to an animal.

ALTER TABLE public.weighing_observations
  ADD COLUMN IF NOT EXISTS operator_weight_kg numeric(8,3),
  ADD COLUMN IF NOT EXISTS weight_corrected_by uuid,
  ADD COLUMN IF NOT EXISTS weight_corrected_at timestamptz,
  ADD COLUMN IF NOT EXISTS weight_correction_reason text;

-- The three correction facts travel together or not at all. A row carrying a
-- corrected_at with no operator_weight_kg has lost the original weight, which is
-- the one thing this table exists to keep.
ALTER TABLE public.weighing_observations
  DROP CONSTRAINT IF EXISTS weighing_observations_weight_correction_check;
ALTER TABLE public.weighing_observations
  ADD CONSTRAINT weighing_observations_weight_correction_check
  CHECK (
    (operator_weight_kg IS NULL AND weight_corrected_by IS NULL AND weight_corrected_at IS NULL)
    OR (operator_weight_kg IS NOT NULL AND weight_corrected_by IS NOT NULL AND weight_corrected_at IS NOT NULL)
  );

ALTER TABLE public.weighing_observations
  DROP CONSTRAINT IF EXISTS weighing_observations_operator_weight_positive_check;
ALTER TABLE public.weighing_observations
  ADD CONSTRAINT weighing_observations_operator_weight_positive_check
  CHECK (operator_weight_kg IS NULL OR operator_weight_kg > 0);

ALTER TABLE public.weighing_shed_observations
  ADD COLUMN IF NOT EXISTS operator_weight_kg numeric(10,3),
  ADD COLUMN IF NOT EXISTS operator_animal_count integer,
  ADD COLUMN IF NOT EXISTS weight_corrected_by uuid,
  ADD COLUMN IF NOT EXISTS weight_corrected_at timestamptz,
  ADD COLUMN IF NOT EXISTS weight_correction_reason text;

-- Same all-or-nothing rule as the individual grain. operator_animal_count is
-- part of the group because a lump-sum correction may move the head count, and
-- the original count is as much of the operator's record as the original total.
ALTER TABLE public.weighing_shed_observations
  DROP CONSTRAINT IF EXISTS weighing_shed_observations_weight_correction_check;
ALTER TABLE public.weighing_shed_observations
  ADD CONSTRAINT weighing_shed_observations_weight_correction_check
  CHECK (
    (operator_weight_kg IS NULL AND operator_animal_count IS NULL AND weight_corrected_by IS NULL AND weight_corrected_at IS NULL)
    OR (operator_weight_kg IS NOT NULL AND operator_animal_count IS NOT NULL AND weight_corrected_by IS NOT NULL AND weight_corrected_at IS NOT NULL)
  );

ALTER TABLE public.weighing_shed_observations
  DROP CONSTRAINT IF EXISTS weighing_shed_observations_operator_weight_positive_check;
ALTER TABLE public.weighing_shed_observations
  ADD CONSTRAINT weighing_shed_observations_operator_weight_positive_check
  CHECK (
    (operator_weight_kg IS NULL OR operator_weight_kg > 0)
    AND (operator_animal_count IS NULL OR operator_animal_count > 0)
  );

-- "Which weights did a verifier change, most recent first" is the leadership
-- read this feature owes an auditor, and it is a small partial index because
-- corrections are rare against the volume of captures.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_observations_weight_corrected_idx
  ON public.weighing_observations (tenant_id, weight_corrected_at DESC)
  WHERE weight_corrected_at IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_shed_observations_weight_corrected_idx
  ON public.weighing_shed_observations (tenant_id, weight_corrected_at DESC)
  WHERE weight_corrected_at IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_shed_observations_weight_corrected_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_observations_weight_corrected_idx;

ALTER TABLE public.weighing_shed_observations
  DROP CONSTRAINT IF EXISTS weighing_shed_observations_operator_weight_positive_check;
ALTER TABLE public.weighing_shed_observations
  DROP CONSTRAINT IF EXISTS weighing_shed_observations_weight_correction_check;
ALTER TABLE public.weighing_shed_observations
  DROP COLUMN IF EXISTS weight_correction_reason,
  DROP COLUMN IF EXISTS weight_corrected_at,
  DROP COLUMN IF EXISTS weight_corrected_by,
  DROP COLUMN IF EXISTS operator_animal_count,
  DROP COLUMN IF EXISTS operator_weight_kg;

ALTER TABLE public.weighing_observations
  DROP CONSTRAINT IF EXISTS weighing_observations_operator_weight_positive_check;
ALTER TABLE public.weighing_observations
  DROP CONSTRAINT IF EXISTS weighing_observations_weight_correction_check;
ALTER TABLE public.weighing_observations
  DROP COLUMN IF EXISTS weight_correction_reason,
  DROP COLUMN IF EXISTS weight_corrected_at,
  DROP COLUMN IF EXISTS weight_corrected_by,
  DROP COLUMN IF EXISTS operator_weight_kg;
