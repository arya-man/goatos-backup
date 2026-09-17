-- +goose Up
-- +goose NO TRANSACTION
-- SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md): the high-priority
-- feed evidence is now the AUTHORED high_priority card, so the legacy CHECK from 000053 -- which
-- demanded BOTH fixed feed refs whenever a fingerprint is stored -- would refuse a farm that renamed
-- or replaced the two seeded clips (a single "feed given" capture, a packing PHOTO). The same-named
-- constraint is re-added STRICTLY WEAKER: either no feed evidence at all, or a non-blank fingerprint
-- with an object snapshot and each legacy feed ref NULL-or-non-blank. The fingerprint rule itself
-- is untouched (the completion still refuses feed_config_changed); the authored slots decide which
-- captures exist, and sop_proofs carries them.
--
-- Lock safety: DROP + ADD NOT VALID are metadata-only; VALIDATE runs in its own autocommit step
-- under lock_timeout so the write lock never spans the scan. Every row satisfying the old CHECK
-- satisfies the new one, so VALIDATE cannot fail on existing data.
SET lock_timeout = '2s';
SET statement_timeout = '30s';
ALTER TABLE public.shifting_events
  DROP CONSTRAINT IF EXISTS shifting_events_high_priority_feed_evidence_consistent_check;
ALTER TABLE public.shifting_events
  ADD CONSTRAINT shifting_events_high_priority_feed_evidence_consistent_check
  CHECK (
    (feed_packing_proof_ref IS NULL AND feed_given_proof_ref IS NULL
      AND feed_config_fingerprint IS NULL AND feed_requirement_snapshot IS NULL)
    OR
    (btrim(feed_config_fingerprint) <> ''
      AND jsonb_typeof(feed_requirement_snapshot) = 'object'
      AND (feed_packing_proof_ref IS NULL OR btrim(feed_packing_proof_ref) <> '')
      AND (feed_given_proof_ref IS NULL OR btrim(feed_given_proof_ref) <> ''))
  ) NOT VALID;
RESET lock_timeout;
RESET statement_timeout;

SET lock_timeout = '2s';
SET statement_timeout = '30s';
ALTER TABLE public.shifting_events
  VALIDATE CONSTRAINT shifting_events_high_priority_feed_evidence_consistent_check;
RESET lock_timeout;
RESET statement_timeout;

-- +goose Down
-- +goose NO TRANSACTION
-- Restores the 000053 shape NOT VALID only: rows written under the authored card (one feed ref)
-- may not satisfy it, and a VALIDATE here would block the rollback.
SET lock_timeout = '2s';
SET statement_timeout = '30s';
ALTER TABLE public.shifting_events
  DROP CONSTRAINT IF EXISTS shifting_events_high_priority_feed_evidence_consistent_check;
ALTER TABLE public.shifting_events
  ADD CONSTRAINT shifting_events_high_priority_feed_evidence_consistent_check
  CHECK (
    (feed_packing_proof_ref IS NULL AND feed_given_proof_ref IS NULL
      AND feed_config_fingerprint IS NULL AND feed_requirement_snapshot IS NULL)
    OR
    (btrim(feed_packing_proof_ref) <> '' AND btrim(feed_given_proof_ref) <> ''
      AND btrim(feed_config_fingerprint) <> ''
      AND jsonb_typeof(feed_requirement_snapshot) = 'object')
  ) NOT VALID;
RESET lock_timeout;
RESET statement_timeout;
