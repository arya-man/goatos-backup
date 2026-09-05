-- +goose Up
-- seed-fixture-guard:ignore: brand-new operational table plus one additive boolean on
-- health_cases. No seed, config or SOP contract changes: a cause of death is recorded by the
-- death workflow at runtime and is never seeded, so no seed command, fixture, projector or
-- closeout step has anything to populate.
--
-- CAUSE OF DEATH becomes a recorded fact (maintainer decision 2026-09-05).
--
-- Until now nothing in the product recorded WHY an animal died. The death workflow captured
-- a written account and two videos; `exit_reason` on the animal is the MANNER of exit
-- (sold/died/culled/transferred/lost), never a diagnosis; and the post-mortem was filmed and
-- never read back into a field. The Health Analytics mortality read could therefore only
-- report whether a case happened to be OPEN when the animal died -- temporal co-incidence,
-- not causation -- and a co-morbid animal's death could not be attributed to one disease at
-- all.
--
-- The operator now answers one question on the death form: NORMAL death, or DUE TO DISEASE.
-- Disease deaths carry a coded cause; normal deaths carry the written account exactly as
-- they always have.
--
-- WHY THIS IS A HEALTH TABLE AND NOT TWO COLUMNS BESIDE `exit_reason`.
--
-- Those columns were the first design and were wrong twice over. A cause of death is a
-- CLINICAL judgement, and the identity module owns the animal's lifecycle, not the disease
-- list -- putting the diagnosis on identity's row would make identity the keeper of a fact
-- only Health can validate. And that row belongs to a SEEDED canonical table whose schema is
-- coupled to the vaccination/HRMS source fixtures; altering it demands that whole companion
-- set move, which for a column no seed ever populates would have been churn standing in for
-- review. The clinical fact lives with the clinical module.
--
-- WHY THE KEY IS SHAPED (kind, key) AND NOT A SINGLE COLUMN. A case reaches Health two ways,
-- and they name a disease in two different vocabularies:
--
--   register_rule  the DIAGNOSIS rule the engine named ('MASTITIS', 'BLOAT') -- 34 adult
--                  rules and 27 per kid class. The precise vocabulary, and the one
--                  `health_cases.register_rule_id` and the analytics disease board key on.
--   disease_key    the TREATMENT CARD a pre-engine, direct-pick case was opened against
--                  ('mastitis', 'supportive'). Many-to-one -- PPR, POX and UNDIFFERENTIATED
--                  all route to 'supportive' -- so it cannot say which illness was named,
--                  and it is stored ONLY where no rule id exists.
--
-- The pair mirrors `HealthAnalyticsDisease.key` / `.key_kind` exactly, so a cause of death
-- lands in the same key space the incidence board counts and the two can be read against
-- each other without a translation table.
--
-- LOCK NOTE. The dropdown's vocabulary is the diagnosis register, EMBEDDED YAML validated at
-- process start, not a table. It is deliberately NOT copied into a lookup table here: two
-- copies of a clinical rule list drift, and the register is already the single source the
-- engine diagnoses from. `cause_key` is therefore free text validated in Go against the
-- loaded register, exactly as `health_cases.register_rule_id` already is.

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. The recorded cause, one row per animal that died of something
-- ---------------------------------------------------------------------------
--
-- ONE ROW PER ANIMAL, by primary key. An animal dies once, so a second cause is a bug and
-- the key is what stops it rather than a check somewhere upstream.
--
-- Absent means a NORMAL death -- a complete answer, not missing data -- and it is also the
-- state of every death recorded before this table existed, which is why the mortality read
-- keeps its open-case inference for a row that is not here.
--
-- NO FOREIGN KEY to the animal table, deliberately. Naming it in this migration would couple
-- the change to the vaccination/HRMS seed fixture contract, and integrity here does not need
-- it: the only writer is Health's own approved-death consumer, which is handed an id the
-- identity module has already exited, and every read joins the animal anyway.
CREATE TABLE IF NOT EXISTS public.health_death_causes (
  tenant_id  uuid NOT NULL,
  goat_id    uuid NOT NULL,
  cause_key  text NOT NULL CHECK (btrim(cause_key) <> ''),
  cause_kind text NOT NULL CHECK (cause_kind IN ('register_rule', 'disease_key')),
  -- The case the cause was matched to, when the animal actually had one open. NULL when the
  -- operator named a disease the animal was never opened a case for, which is the normal
  -- shape for a death recorded on the Counts form.
  health_case_id uuid,
  recorded_by uuid,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, goat_id)
);

COMMENT ON TABLE public.health_death_causes IS
  'What an animal died OF, as the operator named it on the death form. One row per animal; absent means a normal death with no disease established. Written only by the approved-death consumer.';
COMMENT ON COLUMN public.health_death_causes.cause_kind IS
  'Which vocabulary cause_key belongs to: register_rule (the diagnosis rule, precise) or disease_key (the treatment card, many-to-one across diseases).';

-- The mortality read arrives with a set of animal ids, which the primary key already serves.
-- This index serves the other direction -- "how many died of MASTITIS" -- without walking
-- every row.
CREATE INDEX IF NOT EXISTS health_death_causes_by_disease_idx
  ON public.health_death_causes (tenant_id, cause_key);

-- ---------------------------------------------------------------------------
-- 2. Which case was named as the cause
-- ---------------------------------------------------------------------------
--
-- An animal can be under treatment for several diseases at once. When it dies, EVERY open
-- case closes -- unchanged, and what `CloseForApprovedDeath` has always done -- but exactly
-- ONE of them may be marked as the cause the operator named. The rest close as ordinary
-- dead-closed history.
--
-- This is the flag that makes "close it under one disease and the others go with it, and it
-- is counted under that one only" true in the data rather than only on the screen.

ALTER TABLE public.health_cases
  ADD COLUMN IF NOT EXISTS is_death_cause boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN public.health_cases.is_death_cause IS
  'TRUE on the one case the operator named as the cause of the animal''s death. Every other open case still closes as closed_dead when the animal dies; only this one is counted as the cause.';

-- AT MOST ONE per animal. Without this a second write could mark two causes and the
-- mortality board would count one death twice.
CREATE UNIQUE INDEX IF NOT EXISTS health_cases_one_death_cause_per_goat_uq
  ON public.health_cases (tenant_id, goat_id)
  WHERE is_death_cause;

-- A case may only be the cause of a death if it is itself dead-closed. A recovered or
-- cancelled case cannot have killed the animal.
ALTER TABLE public.health_cases
  ADD CONSTRAINT health_cases_death_cause_is_closed_dead_check
    CHECK (NOT is_death_cause OR status = 'closed_dead');

COMMIT;

-- +goose Down
-- Forward-only: dropping a recorded cause of death destroys clinical history.
SELECT 1;
