-- +goose Up
-- High-priority shifting embeds feed packing and feeding inside the movement task.
-- Existing rows and low-priority movements retain the single shifting-video contract.
ALTER TABLE shifting_events
    ADD COLUMN feed_packing_proof_ref text,
    ADD COLUMN feed_given_proof_ref text,
    ADD COLUMN feed_config_fingerprint text,
    ADD COLUMN feed_requirement_snapshot jsonb;

ALTER TABLE shifting_events
    ADD CONSTRAINT shifting_events_high_priority_feed_evidence_consistent_check
    CHECK (
        (feed_packing_proof_ref IS NULL AND feed_given_proof_ref IS NULL
            AND feed_config_fingerprint IS NULL AND feed_requirement_snapshot IS NULL)
        OR
        (btrim(feed_packing_proof_ref) <> '' AND btrim(feed_given_proof_ref) <> ''
            AND btrim(feed_config_fingerprint) <> ''
            AND jsonb_typeof(feed_requirement_snapshot) = 'object')
    ) NOT VALID;

ALTER TABLE shifting_events
    VALIDATE CONSTRAINT shifting_events_high_priority_feed_evidence_consistent_check;

COMMENT ON COLUMN shifting_events.feed_packing_proof_ref IS
  'High-priority shifting-only live-camera proof of its embedded feed-packing step; does not complete the separate Feed Packing workflow.';
COMMENT ON COLUMN shifting_events.feed_given_proof_ref IS
  'High-priority shifting-only live-camera proof of the configured feed being given to the moved animal(s).';
COMMENT ON COLUMN shifting_events.feed_config_fingerprint IS
  'Semantic fingerprint of the destination feed configuration shown to the operator and revalidated under the completion row lock.';
COMMENT ON COLUMN shifting_events.feed_requirement_snapshot IS
  'Immutable configured feed requirement accepted with the high-priority completion evidence.';

-- +goose Down
-- Intentionally irreversible: these columns carry audit evidence already used by verification.
SELECT 1;
