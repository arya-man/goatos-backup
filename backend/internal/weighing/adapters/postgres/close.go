package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// Explicit close for Weighing. Until this file existed a bucket or a campaign
// could only end automatically, when every expected animal resolved
// (completeIndividualScopeIfDone / completeCampaignIfDone). Leadership needs to
// end work that will never finish on its own.
//
// Both methods follow the ReopenScope transaction pattern EXACTLY: build a
// semantic request fingerprint, replay-read the idempotency record first (exact
// replay returns the original result with no new side effects, same key with a
// different payload conflicts), then state change + audit + idempotency record +
// outbox enqueue, all in ONE transaction.
//
// Closing with work that was never accepted is ALLOWED and is the whole point.
// It must never read back as accepted work, so:
//   - the terminal status is 'closed', a distinct status from 'completed';
//   - weighing_expected_animals rows are NOT touched, so nothing flips to
//     'weighed'/'closed_by_override';
//   - the reason, the actor, and the exact not-accepted count (plus a bounded
//     identifier sample) are written into the audit row, the idempotency result
//     snapshot, and the outbox payload.
const (
	eventTypeScopeClosed = "weighing.shed.closed"
	// Abandon is a DISTINCT event, never a flavour of closed: "ended without
	// verification" must not be mistakable downstream for "verified and closed".
	eventTypeScopeAbandoned = "weighing.shed.abandoned"
	eventTypeCampaignClosed = "weighing.campaign.closed"
)

// readyToCloseCountsSQL is a correlated-subquery fragment for a `cs` alias over
// weighing_campaign_sheds. It returns (submitted_count, pending_verification_count)
// for that row, using the SAME definition as pendingVerificationCount below: a
// submitted individual observation is submitted_at IS NOT NULL, a lump-sum shed
// observation IS the submission, and 'rework' counts as pending on purpose. It is
// evaluated by the planner as part of ONE query (no per-row application loop), so
// this is not the banned N+1 shape.
const readyToCloseCountsSQL = `(
  (SELECT count(*) FROM weighing_observations wo WHERE wo.tenant_id=cs.tenant_id AND wo.campaign_shed_id=cs.campaign_shed_id AND wo.submitted_at IS NOT NULL)
  + (SELECT count(*) FROM weighing_shed_observations wso WHERE wso.tenant_id=cs.tenant_id AND wso.campaign_shed_id=cs.campaign_shed_id)
) AS submitted_count,
(
  (SELECT count(*) FROM weighing_observations wo WHERE wo.tenant_id=cs.tenant_id AND wo.campaign_shed_id=cs.campaign_shed_id AND wo.submitted_at IS NOT NULL AND wo.verification_status <> 'verified')
  + (SELECT count(*) FROM weighing_shed_observations wso WHERE wso.tenant_id=cs.tenant_id AND wso.campaign_shed_id=cs.campaign_shed_id AND wso.verification_status <> 'verified')
) AS pending_verification_count`

// pendingVerificationCount counts submitted evidence in this bucket that still has
// no verdict.
//
// A bucket is "ready to close" only when every video an operator submitted has been
// looked at. `rework` counts as PENDING on purpose: a bounced video is unfinished
// work the operator still owes, so closing on it would bury the rework request.
//
// Weighing-owned tables only (weighing_observations + weighing_shed_observations) —
// no goats, no roster, no vaccination. Individual rows count only once submitted
// (submitted_at NOT NULL); a lump-sum shed observation IS the submission, so it
// counts as soon as it exists.
func (r *Repository) pendingVerificationCount(ctx context.Context, tx pgx.Tx, tenantID, campaignShedID string) (int, int, error) {
	var submitted, pending int
	if err := tx.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM weighing_observations
     WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid AND submitted_at IS NOT NULL)
  + (SELECT count(*) FROM weighing_shed_observations
     WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid),
  (SELECT count(*) FROM weighing_observations
     WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid AND submitted_at IS NOT NULL
       AND verification_status <> 'verified')
  + (SELECT count(*) FROM weighing_shed_observations
     WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid
       AND verification_status <> 'verified')`,
		tenantID, campaignShedID).Scan(&submitted, &pending); err != nil {
		return 0, 0, err
	}
	return submitted, pending, nil
}

// CloseScope closes exactly one weighing bucket (campaign shed).
//
// NORMAL close is GATED (maintainer decision 2026-07-31): leadership may not close
// a bucket while any submitted video is still waiting on the verifier. The gate is
// only about closing EARLY — it never blocks the operator scanning or submitting,
// and never blocks the verifier reviewing. Work that will genuinely never finish
// ends through AbandonScope instead, which is explicit and reason-bearing.
func (r *Repository) CloseScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
	return r.closeScope(ctx, cmd, false)
}

// AbandonScope ends a bucket whose work will never finish, WITHOUT the verification
// gate. It is a separate primitive rather than a flag on close so the distinction
// survives in the audit trail and on the bus: a reason is mandatory, the audit action
// is weighing.scope_abandoned, and the event is weighing.shed.abandoned.
func (r *Repository) AbandonScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
	if strings.TrimSpace(cmd.Reason) == "" {
		return domain.CloseResult{}, ports.ErrInvalidArgument
	}
	return r.closeScope(ctx, cmd, true)
}

func (r *Repository) closeScope(ctx context.Context, cmd domain.CloseCommand, abandon bool) (domain.CloseResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.CloseResult{}, err
	}
	defer tx.Rollback(ctx)

	fingerprint := idempotencyFingerprint(map[string]any{
		"campaign_id":      cmd.CampaignID,
		"campaign_shed_id": cmd.CampaignShedID,
		"closed_by":        cmd.ClosedBy,
		"reason":           cmd.Reason,
	})
	eventType := eventTypeScopeClosed
	auditAction := "weighing.scope_closed"
	if abandon {
		eventType = eventTypeScopeAbandoned
		auditAction = "weighing.scope_abandoned"
	}
	if result, ok, err := r.closeByIdempotency(ctx, tx, cmd.TenantID, eventType, cmd.IdempotencyKey, fingerprint, "weighing_campaign_shed"); err != nil || ok {
		if err != nil {
			return domain.CloseResult{}, err
		}
		return result, tx.Commit(ctx)
	}

	// Lock the bucket first so the not-accepted snapshot and the status flip
	// cannot straddle a concurrent scan/submit.
	var category, status string
	if err := tx.QueryRow(ctx, `
SELECT cs.weighing_category, cs.status
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns campaign
  ON campaign.tenant_id=cs.tenant_id
 AND campaign.campaign_id=cs.campaign_id
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND cs.campaign_shed_id=$3::uuid
FOR UPDATE OF cs`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID).Scan(&category, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CloseResult{}, ports.ErrNotFound
		}
		return domain.CloseResult{}, err
	}
	if status == domain.StatusClosed || status == "canceled" {
		return domain.CloseResult{}, ports.ErrImmutable
	}

	// THE CLOSE GATE. Checked under the same row lock taken above, so a verdict
	// landing concurrently cannot slip between the check and the status flip.
	if !abandon {
		_, pending, err := r.pendingVerificationCount(ctx, tx, cmd.TenantID, cmd.CampaignShedID)
		if err != nil {
			return domain.CloseResult{}, err
		}
		if pending > 0 {
			return domain.CloseResult{}, ports.ErrVerificationPending
		}
	}

	notAcceptedCount, notAccepted, err := r.scopeNotAcceptedWork(ctx, tx, cmd, category)
	if err != nil {
		return domain.CloseResult{}, err
	}

	var closedAt time.Time
	if err := tx.QueryRow(ctx, `
UPDATE weighing_campaign_sheds
SET status='closed',
  closed_at=now(),
  closed_by=$4::uuid,
  close_reason=$5,
  closed_not_accepted_count=$6,
  updated_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND campaign_shed_id=$3::uuid
  AND status NOT IN ('closed','canceled')
RETURNING closed_at`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.ClosedBy, cmd.Reason, notAcceptedCount).Scan(&closedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CloseResult{}, ports.ErrImmutable
		}
		return domain.CloseResult{}, err
	}

	result := domain.CloseResult{
		CampaignID:       cmd.CampaignID,
		CampaignShedID:   cmd.CampaignShedID,
		Status:           domain.StatusClosed,
		Reason:           cmd.Reason,
		ClosedBy:         cmd.ClosedBy,
		ClosedAt:         closedAt,
		NotAcceptedCount: notAcceptedCount,
		NotAccepted:      notAccepted,
	}
	if err := r.auditClose(ctx, tx, cmd, auditAction, "weighing_campaign_shed", cmd.CampaignShedID, result); err != nil {
		return domain.CloseResult{}, err
	}
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, eventType, cmd.IdempotencyKey, fingerprint, "weighing_campaign_shed", cmd.CampaignShedID, result); err != nil {
		return domain.CloseResult{}, err
	}
	if err := r.enqueueScopeClosed(ctx, tx, cmd, result, eventType); err != nil {
		return domain.CloseResult{}, err
	}
	return result, tx.Commit(ctx)
}

// CloseCampaign closes a whole weighing campaign plus every bucket still open
// under it. The buckets are closed with ONE set-based UPDATE (never a per-bucket
// loop), and the campaign-closed event carries the bounded affected-bucket list
// so the notifier can reach each bucket's assigned operator from one event.
func (r *Repository) CloseCampaign(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.CloseResult{}, err
	}
	defer tx.Rollback(ctx)

	fingerprint := idempotencyFingerprint(map[string]any{
		"campaign_id": cmd.CampaignID,
		"closed_by":   cmd.ClosedBy,
		"reason":      cmd.Reason,
	})
	if result, ok, err := r.closeByIdempotency(ctx, tx, cmd.TenantID, eventTypeCampaignClosed, cmd.IdempotencyKey, fingerprint, "weighing_campaign"); err != nil || ok {
		if err != nil {
			return domain.CloseResult{}, err
		}
		return result, tx.Commit(ctx)
	}

	var status string
	if err := tx.QueryRow(ctx, `
SELECT status
FROM weighing_campaigns
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid
FOR UPDATE`, cmd.TenantID, cmd.CampaignID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CloseResult{}, ports.ErrNotFound
		}
		return domain.CloseResult{}, err
	}
	if status == domain.StatusClosed || status == "canceled" {
		return domain.CloseResult{}, ports.ErrImmutable
	}

	// THE SAME CLOSE GATE, at campaign grain.
	//
	// CloseScope refuses to close one bucket with unreviewed videos, but this
	// cascade was a SECOND, ungated door to status='closed': it required no
	// reason and is not the explicit abandon path, so it is a normal close and
	// must obey the same rule. Without this, leadership could sweep the exact
	// bucket the per-bucket gate just refused.
	//
	// Note the cascade below deliberately skips buckets already at 'completed'
	// (accepted work is never rewritten), so the only buckets it can close are
	// pending/in_progress ones — but those can still hold submitted, unverified
	// evidence after a partial submit, which is exactly the case being guarded.
	var campaignPending int
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND cs.status NOT IN ('completed','closed','canceled')
  AND (
    EXISTS (
      SELECT 1 FROM weighing_observations o
      WHERE o.tenant_id=cs.tenant_id AND o.campaign_shed_id=cs.campaign_shed_id
        AND o.submitted_at IS NOT NULL AND o.verification_status <> 'verified'
    )
    OR EXISTS (
      SELECT 1 FROM weighing_shed_observations so
      WHERE so.tenant_id=cs.tenant_id AND so.campaign_shed_id=cs.campaign_shed_id
        AND so.verification_status <> 'verified'
    )
  )`, cmd.TenantID, cmd.CampaignID).Scan(&campaignPending); err != nil {
		return domain.CloseResult{}, err
	}
	if campaignPending > 0 {
		return domain.CloseResult{}, ports.ErrVerificationPending
	}

	buckets, notAcceptedCount, err := r.campaignNotAcceptedBuckets(ctx, tx, cmd)
	if err != nil {
		return domain.CloseResult{}, err
	}

	// One set-based cascade. Buckets that already reached 'completed' keep that
	// status: a completed bucket is accepted work and close must not rewrite it.
	// Buckets whose work was never accepted go to 'closed' and STAY not accepted.
	if _, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds
SET status='closed',
  closed_at=now(),
  closed_by=$3::uuid,
  close_reason=$4,
  updated_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status NOT IN ('completed','closed','canceled')`,
		cmd.TenantID, cmd.CampaignID, cmd.ClosedBy, cmd.Reason); err != nil {
		return domain.CloseResult{}, err
	}

	var closedAt time.Time
	if err := tx.QueryRow(ctx, `
UPDATE weighing_campaigns
SET status='closed',
  closed_at=now(),
  closed_by=$3::uuid,
  close_reason=$4,
  closed_not_accepted_count=$5,
  updated_at=now(),
  row_version=row_version+1
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status NOT IN ('closed','canceled')
RETURNING closed_at`, cmd.TenantID, cmd.CampaignID, cmd.ClosedBy, cmd.Reason, notAcceptedCount).Scan(&closedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CloseResult{}, ports.ErrImmutable
		}
		return domain.CloseResult{}, err
	}

	labels := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		labels = append(labels, bucket.ShedLabel)
	}
	result := domain.CloseResult{
		CampaignID:       cmd.CampaignID,
		Status:           domain.StatusClosed,
		Reason:           cmd.Reason,
		ClosedBy:         cmd.ClosedBy,
		ClosedAt:         closedAt,
		NotAcceptedCount: notAcceptedCount,
		NotAccepted:      labels,
	}
	if err := r.auditClose(ctx, tx, cmd, "weighing.campaign_closed", "weighing_campaign", cmd.CampaignID, result); err != nil {
		return domain.CloseResult{}, err
	}
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, eventTypeCampaignClosed, cmd.IdempotencyKey, fingerprint, "weighing_campaign", cmd.CampaignID, result); err != nil {
		return domain.CloseResult{}, err
	}
	if err := r.enqueueCampaignClosed(ctx, tx, cmd, result, buckets); err != nil {
		return domain.CloseResult{}, err
	}
	return result, tx.Commit(ctx)
}

// closeByIdempotency is the exact-replay read. A stored record with a different
// request fingerprint surfaces ErrIdempotencyConflict from idempotencyResource; a
// stored record pointing at a different resource type is also a conflict.
func (r *Repository) closeByIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, eventType, idempotencyKey, fingerprint, wantResourceType string,
) (domain.CloseResult, bool, error) {
	_, resourceType, snapshot, ok, err := r.idempotencyResource(ctx, tx, tenantID, eventType, idempotencyKey, fingerprint)
	if err != nil || !ok {
		return domain.CloseResult{}, ok, err
	}
	if resourceType != wantResourceType {
		return domain.CloseResult{}, true, ports.ErrIdempotencyConflict
	}
	var result domain.CloseResult
	if len(snapshot) > 0 {
		if err := json.Unmarshal(snapshot, &result); err != nil {
			return domain.CloseResult{}, true, err
		}
	}
	return result, true, nil
}

// scopeNotAcceptedWork counts the work in one bucket that was never accepted and
// returns a bounded identifier sample. The count is the WHOLE-bucket total; the
// sample is capped at domain.CloseNotAcceptedSampleLimit so a large roster cannot
// blow up the audit/idempotency payload.
//
// Free-flow: the individual branch reads the campaign's own roster table and
// prefers the raw scanned identifier. It never joins goats/vaccination and never
// requires animal_id to be present.
func (r *Repository) scopeNotAcceptedWork(ctx context.Context, tx pgx.Tx, cmd domain.CloseCommand, category string) (int, []string, error) {
	if category == domain.CategoryPerShedPartition {
		// A lump-sum bucket holds exactly one unit of work: the shed weight.
		var accepted bool
		if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM weighing_shed_observations
  WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid
)`, cmd.TenantID, cmd.CampaignShedID).Scan(&accepted); err != nil {
			return 0, nil, err
		}
		if accepted {
			return 0, nil, nil
		}
		var label string
		if err := tx.QueryRow(ctx, `
SELECT display_name FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, cmd.TenantID, cmd.CampaignShedID).Scan(&label); err != nil {
			return 0, nil, err
		}
		return 1, []string{label}, nil
	}

	var count int
	var sample []string
	if err := tx.QueryRow(ctx, `
WITH open_work AS (
  SELECT COALESCE(NULLIF(btrim(ea.scanned_identifier), ''), ea.animal_id::text) AS label
  FROM weighing_expected_animals ea
  WHERE ea.tenant_id=$1::uuid
    AND ea.campaign_id=$2::uuid
    AND ea.campaign_shed_id=$3::uuid
    AND ea.status NOT IN ('weighed','unavailable','canceled','closed_by_override')
)
SELECT
  (SELECT count(*)::int FROM open_work),
  COALESCE((SELECT array_agg(label ORDER BY label) FROM (SELECT label FROM open_work ORDER BY label LIMIT $4) capped), '{}')`,
		cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, domain.CloseNotAcceptedSampleLimit,
	).Scan(&count, &sample); err != nil {
		return 0, nil, err
	}
	return count, sample, nil
}

// closedBucket is one campaign shed that a campaign close ends with work that was
// never accepted, together with its single assigned operator. One bucket has
// exactly ONE operator, so this is the DOWNWARD routing key for the notifier.
type closedBucket struct {
	CampaignShedID string `json:"campaign_shed_id"`
	ShedID         string `json:"shed_id"`
	ShedLabel      string `json:"shed_label"`
	OperatorID     string `json:"operator_id"`
	Status         string `json:"previous_status"`
}

// campaignNotAcceptedBuckets returns the bounded affected-bucket list plus the
// exact whole-campaign not-accepted bucket count. Buckets already 'completed' are
// accepted work and are excluded.
func (r *Repository) campaignNotAcceptedBuckets(ctx context.Context, tx pgx.Tx, cmd domain.CloseCommand) ([]closedBucket, int, error) {
	var total int
	if err := tx.QueryRow(ctx, `
SELECT count(*)::int
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status NOT IN ('completed','closed','canceled')`, cmd.TenantID, cmd.CampaignID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `
SELECT cs.campaign_shed_id::text, cs.location_id::text, cs.display_name, cs.operator_user_id::text, cs.status
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND cs.status NOT IN ('completed','closed','canceled')
ORDER BY cs.display_name, cs.campaign_shed_id
LIMIT $3`, cmd.TenantID, cmd.CampaignID, domain.CloseNotAcceptedSampleLimit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	buckets := make([]closedBucket, 0, 8)
	for rows.Next() {
		var bucket closedBucket
		if err := rows.Scan(&bucket.CampaignShedID, &bucket.ShedID, &bucket.ShedLabel, &bucket.OperatorID, &bucket.Status); err != nil {
			return nil, 0, err
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return buckets, total, nil
}

func (r *Repository) auditClose(
	ctx context.Context,
	tx pgx.Tx,
	cmd domain.CloseCommand,
	action, resourceType, resourceID string,
	result domain.CloseResult,
) error {
	return audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     cmd.TenantID,
		ActorID:      cmd.ClosedBy,
		ActorType:    "user",
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ScopeType:    "weighing.campaign",
		ScopeID:      cmd.CampaignID,
		AfterState:   result,
		Metadata: map[string]any{
			"campaign_id":            cmd.CampaignID,
			"campaign_shed_id":       cmd.CampaignShedID,
			"reason":                 cmd.Reason,
			"closed_by":              cmd.ClosedBy,
			"not_accepted_count":     result.NotAcceptedCount,
			"not_accepted":           result.NotAccepted,
			"client_idempotency_key": cmd.IdempotencyKey,
		},
	})
}
