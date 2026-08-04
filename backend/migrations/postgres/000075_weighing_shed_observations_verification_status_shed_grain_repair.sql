-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing observations are written by the Weighing mobile/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- FORWARD-REPAIR (M6, P1) for 000071_weighing_backfill_verification_status_from_items.sql.
--
-- 000071 IS NOT EDITED HERE, for the same checksum-drift reason documented in
-- 000074/000072: it is already merged to main and may already be applied.
--
-- ROOT CAUSE: 000071's shed-level backfill joined
-- `weighing_observations wo JOIN verification_items vi ON wo.observation_id =
-- vi.source_ref_id` and rolled the result up to weighing_shed_observations by
-- (tenant_id, campaign_shed_id). That join is correct ONLY for the
-- individual-animal category, where verification_items.source_ref_type =
-- 'weighing_observation' and source_ref_id really is
-- weighing_observations.observation_id (see enqueueVerification /
-- domain.VerificationRefTypeAnimal, weighing/domain/types.go).
--
-- For lump-sum / per-shed-partition weighing (RecordShedObservation,
-- service.go ~line 500-547), the verification item is raised with
-- source_ref_type = 'weighing_shed_observation' and source_ref_id =
-- weighing_shed_observations.shed_observation_id -- the shed observation's
-- OWN id, not any weighing_observations row (per-shed-partition sheds have
-- ZERO rows in weighing_observations at all; see domain.VerificationRefTypeShed).
-- 000071's join can never match those verdicts, so every per-shed-partition
-- shed's verification_status stays 'pending' forever, regardless of what a
-- verifier actually decided.
--
-- close.go's ready-to-close gate (pending_verification_count, ~line 57/86)
-- reads weighing_shed_observations.verification_status directly, so this is
-- a live production gate, not a future concern: a verified per-shed-partition
-- submission can never let its bucket close.
--
-- FIX: idempotent backfill of weighing_shed_observations.verification_status
-- keyed on the correct id -- shed_observation_id = verification_items.source_ref_id,
-- restricted to source_ref_type = 'weighing_shed_observation' so this can never
-- collide with the individual-animal verdicts 000071 already handles correctly.
-- This is a plain 1:1 join (weighing_shed_observations.shed_observation_id is
-- its primary key), so -- unlike 000071's second block -- no aggregation is
-- needed: one verification_items row decides one shed_observation row.
--
-- LOCK SAFETY: weighing_shed_observations is shed-grain (bounded by the
-- number of weighing buckets, not by animal count), several orders of
-- magnitude smaller than weighing_observations. SET LOCAL lock_timeout /
-- statement_timeout are still set as defense-in-depth against contending with
-- the live close/submit paths, matching the standing rule for any UPDATE
-- against a table on the hot-table list.
--
-- IDEMPOTENT / RE-RUNNABLE: only rows currently 'pending' are touched, and a
-- fresh database with no verification_items backlog updates zero rows.
--
-- projection-review: producer = verification_items (source_ref_type=
-- 'weighing_shed_observation', source_ref_id=shed_observation_id, status,
-- verified_by, verified_at, verdict_reason). Consumer =
-- weighing_shed_observations (shed_observation_id). Row multiplicity: 1:1 --
-- shed_observation_id is the verification_items source_ref_id AND the
-- weighing_shed_observations primary key, so no fan-out and no aggregation.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

UPDATE weighing_shed_observations wso
SET verification_status = CASE vi.status
      WHEN 'approved' THEN 'verified'
      WHEN 'rejected' THEN 'rework'
      ELSE 'pending'
    END,
    verified_by = vi.verified_by,
    verified_at = vi.verified_at,
    rework_reason = CASE WHEN vi.status = 'rejected' THEN vi.verdict_reason ELSE NULL END
FROM verification_items vi
WHERE vi.source_ref_type = 'weighing_shed_observation'
  AND vi.source_ref_id = wso.shed_observation_id
  AND vi.status IN ('approved', 'rejected')
  AND wso.verification_status = 'pending';

-- +goose Down
-- Removal is not possible without losing the record of what a verifier
-- decided (same rationale as 000071's Down). DOWN is a no-op.
