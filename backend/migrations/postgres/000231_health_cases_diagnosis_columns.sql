-- +goose Up
-- Health diagnosis engine, part 2: the columns the engine stamps on health_cases.
--
-- Split from 000229 so that migration stays CREATE-only (its seed-fixture-guard
-- marker covers brand-new tables); everything here alters health's own
-- health_cases table and touches no seeded canonical table.

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. Closure model on the case
-- ---------------------------------------------------------------------------

ALTER TABLE public.health_cases
  ALTER COLUMN duration_days DROP NOT NULL;

ALTER TABLE public.health_cases
  ADD COLUMN IF NOT EXISTS exit_type text NOT NULL DEFAULT 'F'
    CHECK (exit_type IN ('F', 'T', 'V', 'Supportive'));

-- Fixed-length courses carry a day count; the other three do not, and must not
-- acquire one by default. Written as an equivalence so neither half can drift:
-- a Type F with no duration cannot close, and a Type V with one would close on a
-- calendar the Director never agreed to.
--
-- Every pre-existing row has a duration and defaults to 'F', so this holds on
-- the existing data without a backfill.
ALTER TABLE public.health_cases
  ADD CONSTRAINT health_cases_duration_matches_exit_type
    CHECK ((exit_type = 'F') = (duration_days IS NOT NULL));

COMMENT ON COLUMN public.health_cases.duration_days IS
  'Closure day count. NON-NULL only for exit_type=F. NULL for T (closes on a test), V (Director looks) and Supportive (daily, ongoing). This is NOT the protocol step count -- health_protocol_versions.duration_days still says how many days of steps were authored.';

COMMENT ON COLUMN public.health_cases.exit_type IS
  'How this course closes: F fixed days + criteria, T on a test result, V on Director review, Supportive daily until cleared. Snapshotted from the register rule at diagnosis.';

-- Which observation produced this course. NULL for a case opened through the
-- pre-engine path, which stays supported: the direct disease-pick route is not
-- retired by this migration.
ALTER TABLE public.health_cases
  ADD COLUMN IF NOT EXISTS health_diagnosis_run_id uuid;

-- Which REGISTER RULE diagnosed this course, e.g. 'MASTITIS'.
--
-- This is not the same fact as disease_key, and reconcile needs exactly this
-- one. On the next observation the engine is handed the animal's open problems
-- as register ids so it can tell ongoing from new, propose a close, or spot a
-- relapse. disease_key cannot answer that: it names the treatment CARD, and the
-- mapping is many-to-one -- PPR, POX and UNDIFFERENTIATED all route to the
-- 'supportive' card, so a card key cannot say which of the three is open.
--
-- NULL for a pre-engine case, which simply does not participate in reconcile.
ALTER TABLE public.health_cases
  ADD COLUMN IF NOT EXISTS register_rule_id text;

COMMENT ON COLUMN public.health_cases.register_rule_id IS
  'The register rule that diagnosed this course (MASTITIS, PPR, ...). Fed back to the engine as an open problem on the next observation. NOT interchangeable with disease_key, which names the treatment card and is many-to-one.';

CREATE INDEX IF NOT EXISTS health_cases_open_register_rule_idx
  ON public.health_cases (tenant_id, goat_id, register_rule_id)
  WHERE register_rule_id IS NOT NULL AND status = 'active';

ALTER TABLE public.health_cases
  ADD CONSTRAINT health_cases_diagnosis_run_fk
    FOREIGN KEY (tenant_id, health_diagnosis_run_id)
    REFERENCES public.health_diagnosis_runs (tenant_id, health_diagnosis_run_id);

CREATE INDEX IF NOT EXISTS health_cases_diagnosis_run_idx
  ON public.health_cases (tenant_id, health_diagnosis_run_id)
  WHERE health_diagnosis_run_id IS NOT NULL;

-- A confirmed run opens at most one case per disease. Re-confirming is an
-- idempotent replay, not a second course on the same animal for the same
-- illness.
CREATE UNIQUE INDEX IF NOT EXISTS health_cases_run_disease_uq
  ON public.health_cases (tenant_id, health_diagnosis_run_id, disease_key)
  WHERE health_diagnosis_run_id IS NOT NULL;

COMMIT;
