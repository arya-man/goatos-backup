-- +goose Up
-- seed-fixture-guard:ignore: widens a proof-type vocabulary; no vaccination/HRMS seed contract change.
--
-- AUDIO PROOFS (maintainer decision 2026-09-03). A vendor's voice note is recorded on the phone's
-- microphone and stored through the same proof pipeline every photo and video takes -- durable
-- outbox upload, tenant-scoped signed download, retention -- rather than a second media path.
-- The pipeline's type vocabulary was photo/video/attachment; 'audio' joins it here. The write-side
-- rules for an audio upload (in-app microphone only, captured window) live in the proof service,
-- the same place video's do. 000001 is NOT amended: STG records migration checksums.
-- seed-migration-guard:ignore owner=Ravi issue=PR-179 reason=widens-proof_type-check-to-audio-no-seeded-proof-row-changes expiry=2026-10-31
ALTER TABLE public.proof_artifacts
  DROP CONSTRAINT IF EXISTS proof_artifacts_proof_type_check,
  ADD CONSTRAINT proof_artifacts_proof_type_check
    CHECK (proof_type = ANY (ARRAY['photo'::text, 'video'::text, 'attachment'::text, 'audio'::text]));

-- +goose Down
-- seed-migration-guard:ignore owner=Ravi issue=PR-179 reason=widens-proof_type-check-to-audio-no-seeded-proof-row-changes expiry=2026-10-31
ALTER TABLE public.proof_artifacts
  DROP CONSTRAINT IF EXISTS proof_artifacts_proof_type_check,
  ADD CONSTRAINT proof_artifacts_proof_type_check
    CHECK (proof_type = ANY (ARRAY['photo'::text, 'video'::text, 'attachment'::text]));
