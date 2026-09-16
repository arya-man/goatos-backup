-- +goose Up
-- SOP CAPTURE CARD on the Add birth / Add death forms (maintainer decisions 4 and 7, 2026-09-16,
-- docs/decisions/sop-driven-herd-operations.md → "Phase 2: capture forms").
--
-- A report may carry the card's extras: proofs under authored slot keys and answers to authored
-- questions, pinned to the counts.birth / counts.death version in force when the report was
-- raised. They live in their OWN columns, never inside `payload`: the payload is replayed through
-- identity's strict decoder on approval and must stay byte-for-byte what it was.
--
--   capture_sop_version_id  the pinned version (NULL = the card was never authored / older row)
--   capture_proofs          {slot key: proof ref} as accepted
--   capture_answers         {question id: answer} as accepted (inapplicable conditionals dropped)
--   capture_evidence        the SNAPSHOT the approver and the verifier read:
--                           {version_label, media:[{ref,kind,label}], rows:[{label,value,group}],
--                            missing_note}; the older-app note names what the card asked for and
--                           the app never sent (decision 7)
--   capture_review_status   the verifier's verdict on the report's own proof (birth_capture item)
--   capture_review_reason   the verifier's words on a rework
--
-- Additive; every existing row reads as "the form asked nothing".

ALTER TABLE public.counts_approval_requests
  ADD COLUMN IF NOT EXISTS capture_sop_version_id uuid,
  ADD COLUMN IF NOT EXISTS capture_proofs jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS capture_answers jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS capture_evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS capture_review_status text,
  ADD COLUMN IF NOT EXISTS capture_review_reason text;

ALTER TABLE public.counts_approval_requests
  DROP CONSTRAINT IF EXISTS counts_approval_requests_capture_review_status_check;
ALTER TABLE public.counts_approval_requests
  ADD CONSTRAINT counts_approval_requests_capture_review_status_check
  CHECK (capture_review_status IS NULL OR capture_review_status IN ('pending', 'approved', 'rework'));

COMMENT ON COLUMN public.counts_approval_requests.capture_sop_version_id IS
  'counts.birth / counts.death sop_versions row the report''s capture card was judged by (SOP capture card, 2026-09-16). NULL = never authored / pre-feature row.';
COMMENT ON COLUMN public.counts_approval_requests.capture_proofs IS
  '{slot key: proof artifact ref} for every capture the card asked for, as accepted at raise.';
COMMENT ON COLUMN public.counts_approval_requests.capture_answers IS
  'The operator''s answers to the card''s questions, keyed by question id, validated against the pinned version at raise.';
COMMENT ON COLUMN public.counts_approval_requests.capture_evidence IS
  'Snapshot {version_label, media:[{ref,kind,label}], rows:[{label,value,group}], missing_note} the approver''s row and the verifier''s item render verbatim. {} = the form asked nothing.';
COMMENT ON COLUMN public.counts_approval_requests.capture_review_status IS
  'Verifier verdict on the report''s own proof: pending (item queued), approved, rework (re-shoot requested). NULL = no proof to review.';

-- +goose Down
ALTER TABLE public.counts_approval_requests
  DROP CONSTRAINT IF EXISTS counts_approval_requests_capture_review_status_check,
  DROP COLUMN IF EXISTS capture_review_reason,
  DROP COLUMN IF EXISTS capture_review_status,
  DROP COLUMN IF EXISTS capture_evidence,
  DROP COLUMN IF EXISTS capture_answers,
  DROP COLUMN IF EXISTS capture_proofs,
  DROP COLUMN IF EXISTS capture_sop_version_id;
