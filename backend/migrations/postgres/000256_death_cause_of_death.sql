-- +goose Up
-- CAUSE OF DEATH becomes a recorded fact (maintainer decision 2026-09-05).
--
-- Until now nothing in the product recorded WHY an animal died. The death workflow
-- captured a written account and two videos; `goats.exit_reason` is the MANNER of exit
-- (sold/died/culled/transferred/lost), never a diagnosis; and the post-mortem was filmed
-- and never read back into a field. The Health Analytics mortality read could therefore
-- only report whether a case happened to be OPEN when the animal died -- temporal
-- co-incidence, not causation -- and a co-morbid animal's death could not be attributed
-- to one disease at all.
--
-- The operator now answers one question on the death form: NORMAL death, or DUE TO
-- DISEASE. Disease deaths carry a coded cause; normal deaths carry the written account
-- exactly as they always have.
--
-- WHY THE KEY IS SHAPED (kind, key) AND NOT A SINGLE COLUMN. A case reaches Health two
-- ways, and they name a disease in two different vocabularies:
--
--   register_rule  the DIAGNOSIS rule the engine named ('MASTITIS', 'BLOAT') --
--                  34 adult rules and 27 per kid class. This is the precise vocabulary
--                  and the one `health_cases.register_rule_id` and the analytics disease
--                  board already key on.
--   disease_key    the TREATMENT CARD a pre-engine, direct-pick case was opened against
--                  ('mastitis', 'supportive'). It is many-to-one -- PPR, POX and
--                  UNDIFFERENTIATED all route to 'supportive' -- so it cannot say which
--                  illness was named, and it is stored ONLY where no rule id exists.
--
-- The pair mirrors `HealthAnalyticsDisease.key` / `.key_kind` exactly, so a cause of
-- death lands in the same key space the incidence board counts and the two can be read
-- against each other without a translation table.
--
-- LOCK NOTE. The dropdown's vocabulary is the diagnosis register, which is EMBEDDED YAML
-- validated at process start, not a table. It is deliberately NOT copied into a lookup
-- table here: two copies of a clinical rule list drift, and the register is already the
-- single source the engine diagnoses from. The columns below are therefore free text
-- validated in Go against the loaded register, exactly as `health_cases.register_rule_id`
-- already is.

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. The cause, on the animal's own exit
-- ---------------------------------------------------------------------------

ALTER TABLE public.goats
  ADD COLUMN IF NOT EXISTS death_cause_key text,
  ADD COLUMN IF NOT EXISTS death_cause_kind text;

-- Both or neither. A kind with no key names nothing; a key with no kind cannot be read,
-- because the same string can be a rule id in one vocabulary and a card key in the other.
ALTER TABLE public.goats
  ADD CONSTRAINT goats_death_cause_pair_check
    CHECK ((death_cause_key IS NULL) = (death_cause_kind IS NULL));

ALTER TABLE public.goats
  ADD CONSTRAINT goats_death_cause_kind_check
    CHECK (death_cause_kind IS NULL OR death_cause_kind IN ('register_rule', 'disease_key'));

-- A cause of death belongs ONLY to a death. Attaching one to a sale or a transfer would
-- put a clinical claim on an exit nobody examined -- and a cull is deliberately included
-- in that refusal: a cull is a DECISION, a death is an OUTCOME, and the two are counted
-- apart everywhere else in the product.
ALTER TABLE public.goats
  ADD CONSTRAINT goats_death_cause_only_on_death_check
    CHECK (death_cause_key IS NULL OR exit_reason = 'died');

COMMENT ON COLUMN public.goats.death_cause_key IS
  'Coded cause of death: a diagnosis register rule id, or a treatment card key where the case predates the engine. NULL for a normal death, which carries only its written account. Read with death_cause_kind -- the same string can exist in both vocabularies.';
COMMENT ON COLUMN public.goats.death_cause_kind IS
  'Which vocabulary death_cause_key belongs to: register_rule (the diagnosis rule, precise) or disease_key (the treatment card, many-to-one across diseases).';

-- The mortality read groups deaths by cause over an exit-date window, so the index leads
-- with the tenant and the exit instant it filters on.
CREATE INDEX IF NOT EXISTS goats_death_cause_idx
  ON public.goats (tenant_id, exited_at DESC, death_cause_key)
  WHERE exit_reason = 'died';

-- ---------------------------------------------------------------------------
-- 2. Which case was named as the cause
-- ---------------------------------------------------------------------------
--
-- An animal can be under treatment for several diseases at once. When it dies, EVERY open
-- case closes -- that behaviour is unchanged and is what `CloseForApprovedDeath` has always
-- done -- but exactly ONE of them may be marked as the cause the operator named. The rest
-- close as ordinary dead-closed history.
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
