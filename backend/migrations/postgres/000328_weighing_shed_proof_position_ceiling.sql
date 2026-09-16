-- +goose Up
-- 000328_weighing_shed_proof_position_ceiling.sql
--
-- THE WEIGH CAPTURES ARE AUTHORED (2026-09-16): a whole-pen weigh may carry up to four counted
-- slots of at most five captures each, ten in total (domain.MaxLumpSumProofsTotal). The 000008
-- CHECK bounded proof_position to 1..5 -- one slot's worth. Widen it to 1..10.
--
-- Lock-safe: the new constraint is added NOT VALID (no table scan under the ACCESS EXCLUSIVE
-- lock), validated separately (SHARE UPDATE EXCLUSIVE), and only then is the old one dropped.
-- The old constraint's auto-generated name is resolved from the catalog rather than assumed.
--
-- seed-fixture-guard:ignore: a CHECK bound on a weighing-owned child table; no data moves.

ALTER TABLE public.weighing_shed_observation_proofs
  ADD CONSTRAINT weighing_shed_observation_proofs_proof_position_1_10_check
  CHECK (proof_position BETWEEN 1 AND 10) NOT VALID;
ALTER TABLE public.weighing_shed_observation_proofs
  VALIDATE CONSTRAINT weighing_shed_observation_proofs_proof_position_1_10_check;

-- +goose StatementBegin
DO $$
DECLARE
  old_name text;
BEGIN
  FOR old_name IN
    SELECT con.conname
    FROM pg_constraint con
    JOIN pg_class rel ON rel.oid = con.conrelid
    JOIN pg_namespace nsp ON nsp.oid = rel.relnamespace
    WHERE nsp.nspname = 'public'
      AND rel.relname = 'weighing_shed_observation_proofs'
      AND con.contype = 'c'
      AND con.conname <> 'weighing_shed_observation_proofs_proof_position_1_10_check'
      AND pg_get_constraintdef(con.oid) ILIKE '%proof_position%'
  LOOP
    EXECUTE format('ALTER TABLE public.weighing_shed_observation_proofs DROP CONSTRAINT %I', old_name);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE public.weighing_shed_observation_proofs
  ADD CONSTRAINT weighing_shed_observation_proofs_proof_position_check
  CHECK (proof_position BETWEEN 1 AND 5) NOT VALID;
ALTER TABLE public.weighing_shed_observation_proofs
  DROP CONSTRAINT IF EXISTS weighing_shed_observation_proofs_proof_position_1_10_check;
