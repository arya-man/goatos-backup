-- +goose Up
-- ANIMAL PURCHASES: THE PROCUREMENT SOP QUESTIONNAIRE (maintainer decision 2026-09-13, from the
-- farm's "Procurement SOP" Google Form).
--
-- The per-animal form is no longer a handful of basic facts; it is the farm's own inspection SOP
-- -- teeth, weight, height, rectal temperature, a face check, a body check, an udder/testicle
-- check, each with its own media, and the inspector's field verdict (Selected / On Hold). The
-- questions are a VERSIONED CATALOG served by the backend (animalpurchase/domain.Questionnaire);
-- the answers land here keyed by question id, and every media slot is a row of its own so the
-- CEO's web review shows each clip beside the question it answers.
--
-- The three original columns (condition, temp_tag, video_proof_ref) stay for the rows recorded
-- before this change and become nullable; new rows write the questionnaire instead.
ALTER TABLE public.animal_purchase_candidates
  ALTER COLUMN condition DROP NOT NULL,
  ALTER COLUMN video_proof_ref DROP NOT NULL,
  DROP CONSTRAINT IF EXISTS animal_purchase_candidates_video_check,
  DROP CONSTRAINT IF EXISTS animal_purchase_candidates_condition_check,
  ADD CONSTRAINT animal_purchase_candidates_condition_check
    CHECK (condition IS NULL OR condition IN ('healthy', 'minor_concern', 'unwell')),
  -- The questionnaire version the answers were recorded against, so a later question change
  -- never re-reads an old row as a new answer.
  ADD COLUMN questionnaire_version integer NOT NULL DEFAULT 0,
  -- {question_id: answer}. Choice answers hold the option value (or an "other" free text under
  -- the option key), text answers the text, numbers as JSON numbers.
  ADD COLUMN sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb,
  -- The inspector's OWN verdict on the farm: 'selected' or 'on_hold'. A recommendation only --
  -- the CEO/CXO's decision column stays the decision.
  ADD COLUMN field_verdict text,
  ADD COLUMN height_cm numeric(6,1),
  ADD COLUMN rectal_temp_c numeric(4,1),
  ADD CONSTRAINT animal_purchase_candidates_field_verdict_check
    CHECK (field_verdict IS NULL OR field_verdict IN ('selected', 'on_hold'));

-- One row per uploaded proof per media slot. A slot may hold several files (the SOP allows up
-- to five for the face/body set and the suspicious-area set), ordered by position.
CREATE TABLE public.animal_purchase_candidate_media (
  tenant_id     uuid NOT NULL,
  candidate_id  uuid NOT NULL,
  slot          text NOT NULL,
  position      integer NOT NULL DEFAULT 0,
  proof_ref     text NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, candidate_id, slot, position),
  CONSTRAINT animal_purchase_candidate_media_candidate_fk
    FOREIGN KEY (candidate_id) REFERENCES public.animal_purchase_candidates (candidate_id) ON DELETE CASCADE,
  CONSTRAINT animal_purchase_candidate_media_slot_check
    CHECK (slot IN ('teeth', 'weight', 'temperature', 'animal', 'suspicious', 'udder')),
  CONSTRAINT animal_purchase_candidate_media_proof_check CHECK (btrim(proof_ref) <> ''),
  CONSTRAINT animal_purchase_candidate_media_proof_uq UNIQUE (tenant_id, proof_ref)
);

-- Backfill: the rows recorded before this change had one walk-around video; it is the 'animal'
-- slot of the questionnaire.
INSERT INTO public.animal_purchase_candidate_media (tenant_id, candidate_id, slot, position, proof_ref, created_at)
SELECT tenant_id, candidate_id, 'animal', 0, video_proof_ref, created_at
FROM public.animal_purchase_candidates
WHERE video_proof_ref IS NOT NULL AND btrim(video_proof_ref) <> ''
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS public.animal_purchase_candidate_media;
ALTER TABLE public.animal_purchase_candidates
  DROP CONSTRAINT IF EXISTS animal_purchase_candidates_field_verdict_check,
  DROP COLUMN IF EXISTS rectal_temp_c,
  DROP COLUMN IF EXISTS height_cm,
  DROP COLUMN IF EXISTS field_verdict,
  DROP COLUMN IF EXISTS sop_answers,
  DROP COLUMN IF EXISTS questionnaire_version;
