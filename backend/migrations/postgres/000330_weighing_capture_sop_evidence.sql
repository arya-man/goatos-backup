-- +goose Up
-- 000330_weighing_capture_sop_evidence.sql
--
-- THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16, docs/decisions/weighing-sop.md
-- -> "The weigh captures are authored"). The weighing SOP now carries TWO separate capture
-- sections -- per animal and whole pen -- each with its own proof slots (video / photo / either)
-- and its own questions. The evidence rows therefore store what the operator captured and
-- answered, keyed by the pinned SOP's slot / question ids:
--
--   weighing_observations.sop_proofs        {slot key: proof ref} for a per-animal capture; '{}' on
--                                           every existing row = the seeded animal_video slot,
--                                           whose ref is the row's proof_artifact_id (readers map
--                                           '{}' onto that; no backfill, the legacy column stays
--                                           the primary and the verdict applier's comparison key).
--   weighing_observations.sop_answers       the per-animal answers; '{}' (the seed asks none).
--   weighing_shed_observations.sop_answers  the whole-pen answers; '{}'.
--   weighing_shed_observation_proofs.slot_key  which whole-pen slot each capture proves; NULL on
--                                           every existing row = the seeded pen_video slot.
--
-- Rollout-safe: every column is defaulted or nullable, no row is rewritten, the flat proof list
-- and proof_artifact_id stay exactly what every existing reader reads. The per-slot ceiling on a
-- whole-pen row is widened by the next migration (000331).
--
-- seed-fixture-guard:ignore: three defaulted jsonb / nullable text columns on weighing-owned
-- evidence tables; no vaccination / HRMS / goats schema moves.

ALTER TABLE public.weighing_observations
  ADD COLUMN IF NOT EXISTS sop_proofs jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.weighing_observations.sop_proofs IS
  '{slot key: proof artifact ref} for every capture the weighing SOP''s PER-ANIMAL section asked for (2026-09-16). ''{}'' = the seeded animal_video slot = proof_artifact_id.';
COMMENT ON COLUMN public.weighing_observations.sop_answers IS
  '{question id: answer} for the weighing SOP''s PER-ANIMAL questions (2026-09-16).';

ALTER TABLE public.weighing_shed_observations
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.weighing_shed_observations.sop_answers IS
  '{question id: answer} for the weighing SOP''s WHOLE-PEN questions (2026-09-16).';

ALTER TABLE public.weighing_shed_observation_proofs
  ADD COLUMN IF NOT EXISTS slot_key text;
COMMENT ON COLUMN public.weighing_shed_observation_proofs.slot_key IS
  'The whole-pen capture slot this proof proves (weighing SOP, 2026-09-16). NULL = the seeded pen_video slot.';

-- +goose Down
ALTER TABLE public.weighing_shed_observation_proofs DROP COLUMN IF EXISTS slot_key;
ALTER TABLE public.weighing_shed_observations DROP COLUMN IF EXISTS sop_answers;
ALTER TABLE public.weighing_observations DROP COLUMN IF EXISTS sop_answers;
ALTER TABLE public.weighing_observations DROP COLUMN IF EXISTS sop_proofs;
