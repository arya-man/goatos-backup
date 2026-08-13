-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing observations are written by the Weighing mobile/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- ROOT-CAUSE FIX: B02 — Historical evidence can be silently overwritten.
--
-- 000061_weighing_observations_submitted_at.sql:33 backfilled submitted_at by reading
-- the bucket's status (cs.status IN ('completed','closed')). But ReopenScope sets
-- completed_at=NULL and status='in_progress', so a bucket that was completed-then-reopened
-- matches NOTHING in the backfill, and already-submitted rows keep submitted_at IS NULL forever.
--
-- The overwrite risk is real: repository.go ~940-957 UPDATE weight_kg/proof_artifact_id in place
-- whenever submitted_at IS NULL, so a rescan after reopen silently DESTROYS the prior
-- accepted weight and proof with no new row and no audit trail.
--
-- A durable source EXISTS and was ignored: audit_log carries 'weighing.observation_accepted'
-- and 'weighing.scope_closed' rows that survive a reopen and prove when a bucket and its
-- observations were actually completed and accepted.
--
-- FIX: idempotent backfill of still-NULL submitted_at from audit history.
-- Per observation_id, find the latest 'weighing.observation_accepted' audit row.
-- This timestamp is the authoritative submitted moment.
--
-- projection-review: producer = audit_log (resource_type='weighing_observation',
-- action IN ('weighing.observation_accepted', 'weighing.scope_closed'), resource_id=observation_id,
-- created_at=timestamp). Consumer = weighing_observations (observation_id, submitted_at).
-- Row multiplicity: audit_log may have many rows per observation (multiple accepts/updates),
-- so we take MAX(created_at) for the final acceptance. 1:1 join on observation_id with ON CONFLICT.
UPDATE weighing_observations wo
SET submitted_at = audit_evidence.last_accepted_at
FROM (
  SELECT DISTINCT ON (resource_id)
         resource_id,
         created_at AS last_accepted_at
  FROM audit_log
  WHERE resource_type = 'weighing_observation'
    AND action IN ('weighing.observation_accepted', 'weighing.scope_closed')
  ORDER BY resource_id, created_at DESC
) audit_evidence
WHERE wo.observation_id = audit_evidence.resource_id
  AND wo.submitted_at IS NULL;

-- +goose Down
-- Removal is not possible without risking data integrity: removing submitted_at makes
-- the observations vulnerable to the same silent overwrite again. A DOWN cannot know
-- which rows were backfilled vs. filled by the app, so DOWN is a no-op.
