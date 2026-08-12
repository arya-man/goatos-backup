package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Outbox payloads for the two explicit close events. Both are enqueued in the
// SAME transaction as the status flip (see close.go), so a close can never be
// visible without its event and the event can never fire for a rolled-back close.
//
// Both payloads carry the assigned operator(s) so the notification consumer can
// route DOWNWARD from role/assignment truth in the payload instead of resolving
// names anywhere.

type weighingShedClosedPayload struct {
	TenantID         string    `json:"tenant_id"`
	CampaignID       string    `json:"campaign_id"`
	CampaignShedID   string    `json:"campaign_shed_id"`
	ParkID           string    `json:"park_id"`
	ShedID           string    `json:"shed_id"`
	ShedLabel        string    `json:"shed_label"`
	OperatorID       string    `json:"operator_id"`
	ClosedBy         string    `json:"closed_by"`
	Reason           string    `json:"reason,omitempty"`
	NotAcceptedCount int       `json:"not_accepted_count"`
	NotAccepted      []string  `json:"not_accepted,omitempty"`
	ClosedAt         time.Time `json:"closed_at"`
}

type weighingCampaignClosedPayload struct {
	TenantID         string                  `json:"tenant_id"`
	CampaignID       string                  `json:"campaign_id"`
	ParkID           string                  `json:"park_id"`
	ClosedBy         string                  `json:"closed_by"`
	Reason           string                  `json:"reason,omitempty"`
	NotAcceptedCount int                     `json:"not_accepted_count"`
	Operators        []closedOperatorSummary `json:"operators,omitempty"`
	ClosedAt         time.Time               `json:"closed_at"`
}

// A per-bucket close has exactly ONE event type, eventTypeScopeClosed. The type
// threaded back through.
func (r *Repository) enqueueScopeClosed(ctx context.Context, tx pgx.Tx, cmd domain.CloseCommand, result domain.CloseResult, eventType string) error {
	if eventType == "" {
		eventType = eventTypeScopeClosed
	}
	payload := weighingShedClosedPayload{
		TenantID:         cmd.TenantID,
		CampaignID:       cmd.CampaignID,
		CampaignShedID:   cmd.CampaignShedID,
		ClosedBy:         cmd.ClosedBy,
		Reason:           cmd.Reason,
		NotAcceptedCount: result.NotAcceptedCount,
		NotAccepted:      result.NotAccepted,
		ClosedAt:         result.ClosedAt,
	}
	if err := tx.QueryRow(ctx, `
SELECT wc.park_id::text, cs.location_id::text, cs.display_name, cs.operator_user_id::text
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns wc
  ON wc.tenant_id=cs.tenant_id
 AND wc.campaign_id=cs.campaign_id
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_shed_id=$2::uuid`,
		cmd.TenantID, cmd.CampaignShedID,
	).Scan(&payload.ParkID, &payload.ShedID, &payload.ShedLabel, &payload.OperatorID); err != nil {
		return err
	}
	return r.enqueue(
		ctx,
		tx,
		cmd.TenantID,
		eventType,
		cmd.CampaignID,
		eventType+":"+cmd.CampaignShedID+":"+cmd.IdempotencyKey,
		"",
		payload,
	)
}

func (r *Repository) enqueueCampaignClosed(
	ctx context.Context,
	tx pgx.Tx,
	cmd domain.CloseCommand,
	result domain.CloseResult,
	buckets []closedBucket,
) error {
	payload := weighingCampaignClosedPayload{
		TenantID:         cmd.TenantID,
		CampaignID:       cmd.CampaignID,
		ClosedBy:         cmd.ClosedBy,
		Reason:           cmd.Reason,
		NotAcceptedCount: result.NotAcceptedCount,
		Operators:        summarizeClosedBucketsByOperator(buckets),
		ClosedAt:         result.ClosedAt,
	}
	if err := tx.QueryRow(ctx, `
SELECT park_id::text
FROM weighing_campaigns
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, cmd.TenantID, cmd.CampaignID).Scan(&payload.ParkID); err != nil {
		return err
	}
	return r.enqueue(
		ctx,
		tx,
		cmd.TenantID,
		eventTypeCampaignClosed,
		cmd.CampaignID,
		eventTypeCampaignClosed+":"+cmd.CampaignID+":"+cmd.IdempotencyKey,
		"",
		payload,
	)
}

// publishedBucket is one assigned weighing bucket in the campaign-published
// payload. One bucket has exactly ONE operator, so OperatorID is the DOWNWARD
// routing key and an operator must only ever be told about their own buckets.
type publishedBucket struct {
	CampaignShedID      string `json:"campaign_shed_id"`
	ShedID              string `json:"shed_id"`
	ShedLabel           string `json:"shed_label"`
	OperatorID          string `json:"operator_id"`
	WeighingCategory    string `json:"weighing_category"`
	ExpectedAnimalCount int    `json:"expected_animal_count"`
}

// weighingCampaignPublishedPayload replaces the previous two-key
// {campaign_id, published_by} publish payload. Every key here is read by the
// notification consumer; nothing is accepted-and-discarded.
type weighingCampaignPublishedPayload struct {
	TenantID          string            `json:"tenant_id"`
	CampaignID        string            `json:"campaign_id"`
	ParkID            string            `json:"park_id"`
	PublishedBy       string            `json:"published_by"`
	PeriodStartDate   string            `json:"period_start_date"`
	PeriodEndDate     string            `json:"period_end_date"`
	StartBusinessDate string            `json:"start_business_date"`
	Buckets           []publishedBucket `json:"buckets"`
}
