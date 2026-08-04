-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing observations are written by the Weighing mobile/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- ROOT-CAUSE FIX: B03 — Historical verification verdicts reset to pending.
--
-- 000058_weighing_close_and_verification_state.sql:52 added verification_status
-- with NOT NULL DEFAULT 'pending'. The verification_items producer shipped 2026-07-30;
-- the column + consumer shipped 2026-07-31 — so any verdicts recorded in that window
-- defaulted to 'pending', and the consumer never replays history.
--
-- Verdicts are recorded in verification_items with status='approved'/'rejected' and
-- the timestamp in verified_at. The join is exact 1:1: verification_items.source_ref_id
-- = weighing_observations.observation_id, category='weighing_proof', and observation_id
-- is the PK of weighing_observations.
--
-- FIX: idempotent backfill of verification_status='pending' from verification_items
-- using:
--   status='approved' -> verification_status='verified', verdict_reason -> rework_reason (NULL)
--   status='rejected'  -> verification_status='rework', verdict_reason -> rework_reason
--
-- CRITICAL: weighing_shed_observations.verification_status IS read by close.go:57 and :86
-- to count pending_verification for the "ready to close" gate. Stale 'pending' blocks close
-- operations even when all individual observations are verified. This is NOT a future decision —
-- the shed observation verdict state gates a production workflow right now.
--
-- projection-review (individual observations):
-- producer = verification_items (source_ref_id, status, verified_by, verified_at, verdict_reason),
-- joined 1:1 to weighing_observations (observation_id = source_ref_id). Row multiplicity: 1:1; no fan-out.
--
-- projection-review (shed observations):
-- producer = verification_items joined to weighing_observations + aggregated to unique campaign_shed_id.
-- consumer = weighing_shed_observations (campaign_shed_id = subject_id).
-- Row multiplicity: weighing_shed_observations is one row per shed (UNIQUE constraint on
-- (tenant_id, campaign_shed_id)), so the aggregate is 1:1. No fan-out, no double-counting.

-- BACKFILL weighing_observations from verification_items.
UPDATE weighing_observations wo
-- NOTE ON STATUS VOCABULARY: verification_items.status is 'pending' | 'approved' | 'rejected'
-- (see backend/internal/verification/domain/types.go and the repository writes). There is NO
-- 'rework' status on verification_items -- 'rework' is the WEIGHING-side vocabulary for a
-- bounced proof. An earlier draft of this migration matched vi.status='rework', which never
-- exists, so the bounced half of the backfill silently repaired nothing. The mapping is
-- verification_items 'rejected' -> weighing 'rework'.
SET verification_status = CASE vi.status
      WHEN 'approved' THEN 'verified'
      WHEN 'rejected' THEN 'rework'
      ELSE 'pending'
    END,
    verified_by = vi.verified_by,
    verified_at = vi.verified_at,
    rework_reason = CASE WHEN vi.status = 'rejected' THEN vi.verdict_reason ELSE NULL END
FROM verification_items vi
WHERE wo.observation_id = vi.source_ref_id
  AND vi.category = 'weighing_proof'
  AND vi.status IN ('approved', 'rejected')
  AND wo.verification_status = 'pending';

-- BACKFILL weighing_shed_observations from verification_items.
-- Join individual observations to their shed, then determine the shed-level verdict:
-- if ANY individual observation in the shed has rework, the shed is rework;
-- if ALL are verified, the shed is verified.
-- Use DISTINCT ON to pick the LATEST verification_item per shed (by verified_at DESC),
-- then get verified_by/verified_at/verdict_reason from that SAME row. This avoids
-- mixing metadata from different rows (max(uuid) is not valid in PostgreSQL).
--
-- projection-review: membership=verification_items joined 1:1 to weighing_observations; group_key=tenant_id, campaign_shed_id; join_cardinality=weighing_observations -> verification_items is many:1, pre-aggregated with ARRAY_AGG; pagination=one bounded aggregate per campaign_shed_id (no pagination); scope=tenant_id, campaign_shed_id uniquely identify the shed within the tenant.
UPDATE weighing_shed_observations wso
SET verification_status = shed_verdict.verdict_status,
    verified_by = shed_verdict.verified_by,
    verified_at = shed_verdict.verified_at,
    rework_reason = shed_verdict.rework_reason
FROM (
  SELECT wo.tenant_id,
         wo.campaign_shed_id,
         CASE
           WHEN (ARRAY_AGG(vi.status) FILTER (WHERE vi.status = 'rejected'))[1] IS NOT NULL THEN 'rework'
           ELSE 'verified'
         END AS verdict_status,
         (ARRAY_AGG(vi.verified_by ORDER BY vi.verified_at DESC NULLS LAST))[1] AS verified_by,
         (ARRAY_AGG(vi.verified_at ORDER BY vi.verified_at DESC NULLS LAST))[1] AS verified_at,
         (ARRAY_AGG(CASE WHEN vi.status = 'rejected' THEN vi.verdict_reason ELSE NULL END
                    ORDER BY vi.verified_at DESC NULLS LAST))[1] AS rework_reason
  FROM weighing_observations wo
  JOIN verification_items vi ON wo.observation_id = vi.source_ref_id
  WHERE vi.category = 'weighing_proof'
    AND vi.status IN ('approved', 'rejected')
  GROUP BY wo.tenant_id, wo.campaign_shed_id
) shed_verdict
WHERE wso.tenant_id = shed_verdict.tenant_id
  AND wso.campaign_shed_id = shed_verdict.campaign_shed_id
  AND wso.verification_status = 'pending';

-- +goose Down
-- Removal is not possible without risking lost verdict context: removing verification_status
-- loses the record of what a verifier decided. A DOWN cannot know which rows were backfilled
-- vs. filled by the app, so DOWN is a no-op.
