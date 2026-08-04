-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing observations are written by the Weighing mobile/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- FORWARD-REPAIR (M5, P1) for 000070_weighing_backfill_submitted_at_from_audit.sql.
--
-- 000070 IS NOT EDITED HERE. It is already merged to main and may already be
-- applied in dev/stg (cmd/migrate/main.go tracks migrations by a SHA-256
-- checksum of file content; rewriting an already-applied migration's SQL
-- changes its checksum and breaks every environment that already ran it --
-- "migration %s was already applied with checksum %s, current %s"). This is
-- the forward-only correction.
--
-- ROOT CAUSE: 000070 backfilled submitted_at from audit_log rows with
-- action IN ('weighing.observation_accepted', 'weighing.scope_closed'),
-- resource_type='weighing_observation'. Both clauses are wrong:
--   1. 'weighing.observation_accepted' is written by auditAnimalObservation
--      at CAPTURE time (repository.go's auditAnimalObservation / ~line 3005),
--      every time an operator scans and accepts a weight -- not at submit.
--      Backfilling submitted_at from it freezes a still-draft, never-submitted
--      capture as if it had been submitted.
--   2. 'weighing.scope_closed' is written by auditClose (close.go) against
--      resource_type='weighing_campaign_shed', so it can never match 000070's
--      `resource_type = 'weighing_observation'` predicate -- a dead clause
--      that backfilled nothing.
--   The real submit action for the individual-scope path is
--   'weighing.individual_scope_submitted', recorded not in audit_log but as
--   a durable row in weighing_idempotency_records (event_type=
--   'weighing.individual_scope_submitted', resource_type=
--   'weighing_campaign_shed', resource_id=campaign_shed_id, result_snapshot
--   carrying the submitted scanned_identifiers array) -- see
--   SubmitIndividualScope, repository.go ~line 1998-2103. That same code path
--   ALSO stamps weighing_observations.submitted_at=now() directly in-app at
--   submit time (repository.go ~line 2079-2086), so this repair only matters
--   for HISTORICAL rows 000070 mis-stamped before/around its own backfill --
--   the live write path has been correct all along.
--
-- STEP 1 -- UNDO the incorrect freeze.
-- A row is "incorrectly frozen" when its submitted_at exactly equals the
-- latest 'weighing.observation_accepted' audit_log timestamp for that
-- observation_id. The in-app submit path stamps submitted_at=now() strictly
-- AFTER the capture that produced the audit row (capture necessarily precedes
-- its own submission), so an exact equality between submitted_at and the
-- capture-acceptance audit timestamp is 000070's fingerprint, not a
-- coincidence of real submit timing.
--
-- Clearing back to NULL must not collide with the 000073 partial unique
-- index (one open (submitted_at IS NULL) row per (tenant_id,
-- campaign_shed_id, lower(btrim(scanned_identifier)))): if another row for
-- the same key is already open, this row is left as-is (still incorrectly
-- frozen) rather than risk a uniqueness violation or silently picking a
-- winner outside 000073's own documented tie-break. Any such leftover is a
-- narrow, identifiable residual (WHERE submitted_at = capture evidence) that
-- a follow-up can target explicitly; it is not silently swept under this one.
--
-- STEP 2 -- RE-DERIVE from the correct submit evidence.
-- For rows left NULL after step 1 (or already NULL), re-apply the real
-- submit timestamp from weighing_idempotency_records: unnest each
-- 'weighing.individual_scope_submitted' record's result_snapshot ->
-- 'scanned_identifiers' against observations in that record's
-- campaign_shed_id, matching on the normalised tag, and only for
-- observations accepted at or before the submit was recorded (an
-- observation cannot have been included in a submit that predates its own
-- capture).
--
-- LOCK SAFETY: weighing_observations is animal-grain and hot. This repair
-- targets only the narrow anomaly set produced by 000070's incorrect
-- predicate, not the whole table, but it is still an UPDATE against a table
-- the live SERIALIZABLE capture path writes continuously. SET LOCAL
-- lock_timeout/statement_timeout bound how long this migration can contend
-- for row locks or run before giving up, instead of stalling behind (or
-- blocking) live capture traffic indefinitely.
--
-- IDEMPOTENT / RE-RUNNABLE: step 1's predicate only matches rows still
-- bearing 000070's exact fingerprint, and step 2 only fills rows still NULL,
-- so a re-run (including on a fresh database that never had the bug) is a
-- no-op.
--
-- projection-review: producer (step 1) = audit_log (resource_type=
-- 'weighing_observation', action='weighing.observation_accepted',
-- resource_id=observation_id, created_at). Consumer = weighing_observations
-- (observation_id, submitted_at). Row multiplicity: audit_log may carry many
-- accept/update rows per observation, so MAX(created_at) is taken; join is
-- 1:1 on observation_id. producer (step 2) = weighing_idempotency_records
-- (event_type='weighing.individual_scope_submitted', resource_id=
-- campaign_shed_id, result_snapshot->'scanned_identifiers'). Consumer =
-- weighing_observations (tenant_id, campaign_shed_id, scanned_identifier).
-- Row multiplicity: one submit record fans out to every observation whose
-- normalised tag appears in its scanned_identifiers array -- a plain
-- membership test, no double counting since each observation is only
-- updated while its own submitted_at is still NULL.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- STEP 1: undo the incorrect freeze, guarded against the 000073 one-open-tag index.
WITH mis_stamped AS (
  SELECT wo.observation_id, wo.tenant_id, wo.campaign_shed_id, wo.scanned_identifier
  FROM weighing_observations wo
  JOIN (
    SELECT DISTINCT ON (resource_id)
           resource_id AS observation_id,
           created_at AS last_accepted_at
    FROM audit_log
    WHERE resource_type = 'weighing_observation'
      AND action = 'weighing.observation_accepted'
    ORDER BY resource_id, created_at DESC
  ) evidence ON evidence.observation_id = wo.observation_id
  WHERE wo.submitted_at = evidence.last_accepted_at
    AND wo.campaign_shed_id IS NOT NULL
)
UPDATE weighing_observations wo
SET submitted_at = NULL
FROM mis_stamped m
WHERE wo.observation_id = m.observation_id
  AND NOT EXISTS (
    SELECT 1
    FROM weighing_observations other
    WHERE other.tenant_id = m.tenant_id
      AND other.campaign_shed_id = m.campaign_shed_id
      AND lower(btrim(other.scanned_identifier)) = lower(btrim(m.scanned_identifier))
      AND other.observation_id <> m.observation_id
      AND other.submitted_at IS NULL
  );

-- STEP 2: re-derive submitted_at from the real submit evidence.
WITH submit_events AS (
  SELECT resource_id AS campaign_shed_id,
         created_at AS submitted_at,
         jsonb_array_elements_text(result_snapshot -> 'scanned_identifiers') AS scanned_identifier
  FROM weighing_idempotency_records
  WHERE event_type = 'weighing.individual_scope_submitted'
    AND resource_type = 'weighing_campaign_shed'
)
UPDATE weighing_observations wo
SET submitted_at = ev.submitted_at
FROM submit_events ev
WHERE wo.campaign_shed_id = ev.campaign_shed_id
  AND lower(btrim(wo.scanned_identifier)) = lower(btrim(ev.scanned_identifier))
  AND wo.submitted_at IS NULL
  AND wo.accepted_at <= ev.submitted_at;

-- +goose Down
-- Removal is not possible without risking the same silent-overwrite exposure
-- 000070/000061 exist to prevent: a DOWN cannot know which submitted_at
-- values were corrected here versus set by the live application, so clearing
-- them would re-open already-submitted rows to the overwrite bug. DOWN is a
-- no-op.
