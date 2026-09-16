-- +goose Up
-- 000316_weighing_removal_proof_slots.sql
--
-- WEIGHING SOP, second decision the same day (2026-09-15, docs/decisions/weighing-sop.md):
-- the removal card's CAPTURES are authored, not only their wording. A slot may be a live-camera
-- VIDEO, a PHOTO or EITHER, compulsory or optional, and slots may be added, removed or renamed.
--
-- The evidence row therefore stores {slot key: proof ref} in `sop_proofs`; the two legacy
-- columns `feed_proof_ref` / `water_proof_ref` stay as MIRRORS of the seeded feed_video /
-- water_video slots (older readers, the STG rows already written) and are NULL when the document
-- has no such slot. The pair CHECK that forced both or neither is dropped for the same reason:
-- a card may now owe one capture, or five. The midnight gate reads the pen's STATUS, never the
-- presence of a particular column (fastingRoundFullyCoveredSQL).
--
-- Every existing row's sop_proofs is backfilled from its legacy pair so the verifier item and
-- the phone read one shape.
--
-- seed-fixture-guard:ignore: one jsonb column + a dropped CHECK on a weighing-owned table;
-- no vaccination / HRMS / goats schema moves.

ALTER TABLE public.weighing_fasting_shed_proofs
  ADD COLUMN IF NOT EXISTS sop_proofs jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.weighing_fasting_shed_proofs.sop_proofs IS
  '{slot key: proof artifact ref} for every capture the weighing SOP''s removal card asked for on this pen (WEIGHING SOP, 2026-09-15). feed_proof_ref / water_proof_ref mirror the seeded feed_video / water_video slots.';

-- Rows written before slots existed: the legacy pair IS the seeded two slots.
-- seed-migration-guard:ignore owner=claude issue=weighing-sop reason=in-place backfill of a weighing-owned evidence column from the same row's legacy pair; no read-model or clean-slate change expiry=2026-10-31
UPDATE public.weighing_fasting_shed_proofs
SET sop_proofs = (
  CASE WHEN feed_proof_ref IS NOT NULL THEN jsonb_build_object('feed_video', feed_proof_ref::text) ELSE '{}'::jsonb END
  || CASE WHEN water_proof_ref IS NOT NULL THEN jsonb_build_object('water_video', water_proof_ref::text) ELSE '{}'::jsonb END
)
WHERE sop_proofs = '{}'::jsonb AND (feed_proof_ref IS NOT NULL OR water_proof_ref IS NOT NULL);

ALTER TABLE public.weighing_fasting_shed_proofs
  DROP CONSTRAINT IF EXISTS weighing_fasting_shed_proofs_pair;

-- +goose Down
ALTER TABLE public.weighing_fasting_shed_proofs DROP COLUMN IF EXISTS sop_proofs;
