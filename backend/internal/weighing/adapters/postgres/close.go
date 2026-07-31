package postgres

import (
	"context"
	"encoding/json"
	"errors"
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
	eventTypeScopeClosed    = "weighing.shed.closed"
	eventTypeCampaignClosed = "weighing.campaign.closed"
)

// CloseScope closes exactly one weighing bucket (campaign shed).
func (r *Repository) CloseScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
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
	if result, ok, err := r.closeByIdempotency(ctx, tx, cmd.TenantID, eventTypeScopeClosed, cmd.IdempotencyKey, fingerprint, "weighing_campaign_shed"); err != nil || ok {
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
	if err := r.auditClose(ctx, tx, cmd, "weighing.scope_closed", "weighing_campaign_shed", cmd.CampaignShedID, result); err != nil {
		return domain.CloseResult{}, err
	}
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, eventTypeScopeClosed, cmd.IdempotencyKey, fingerprint, "weighing_campaign_shed", cmd.CampaignShedID, result); err != nil {
		return domain.CloseResult{}, err
	}
	if err := r.enqueueScopeClosed(ctx, tx, cmd, result); err != nil {
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
