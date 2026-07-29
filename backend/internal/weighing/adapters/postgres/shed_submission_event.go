package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const eventWeighingShedSubmissionCompleted = "weighing.shed_submission.completed"

type weighingShedSubmissionCompletedPayload struct {
	TenantID       string    `json:"tenant_id"`
	CampaignID     string    `json:"campaign_id"`
	CampaignShedID string    `json:"campaign_shed_id"`
	ParkID         string    `json:"park_id"`
	ShedID         string    `json:"shed_id"`
	ShedLabel      string    `json:"shed_label"`
	CompletedAt    time.Time `json:"completed_at"`
}

func (r *Repository) enqueueShedSubmissionCompleted(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	campaignShedID string,
) error {
	payload := weighingShedSubmissionCompletedPayload{
		TenantID:       tenantID,
		CampaignShedID: campaignShedID,
	}
	if err := tx.QueryRow(ctx, `
SELECT
  cs.campaign_id::text,
  wc.park_id::text,
  cs.location_id::text,
  cs.display_name,
  cs.completed_at
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns wc
  ON wc.tenant_id=cs.tenant_id
 AND wc.campaign_id=cs.campaign_id
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_shed_id=$2::uuid
  AND cs.status='completed'`,
		tenantID, campaignShedID,
	).Scan(
		&payload.CampaignID,
		&payload.ParkID,
		&payload.ShedID,
		&payload.ShedLabel,
		&payload.CompletedAt,
	); err != nil {
		return err
	}
	return r.enqueue(
		ctx,
		tx,
		tenantID,
		eventWeighingShedSubmissionCompleted,
		payload.CampaignID,
		eventWeighingShedSubmissionCompleted+":"+campaignShedID,
		"",
		payload,
	)
}
