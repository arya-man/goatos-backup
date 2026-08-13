package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// assertNoCategoryFlipOverCapturedWork refuses to change a bucket's weighing category once that
// bucket already holds captures of the OTHER kind.
//
// The two categories write to different tables -- individual animals to weighing_observations,
// lump-sum to weighing_shed_observations -- and every bucket count sums BOTH. Flipping a bucket
// that already holds work therefore does not migrate or discard anything: it hides the old
// captures behind a screen that cannot render them, then adds the new ones on top. A shed weighed
// animal-by-animal (4 rows) and then flipped and lump-summed at 10 head reports 14 animals
// weighed, and closes as completed on that number -- the same animals counted twice, with the
// operator seeing an empty form and no indication their earlier work is still there.
//
// The category is a property of how the shed WAS weighed, so it may only change while the bucket
// holds nothing. Planners can still re-categorise an untouched shed, which is the real use case.
func (r *Repository) assertNoCategoryFlipOverCapturedWork(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, campaignID string,
	sheds []domain.CreateCampaignShed,
) error {
	if len(sheds) == 0 {
		return nil
	}
	wanted := make(map[string]string, len(sheds))
	for _, shed := range sheds {
		wanted[shed.LocationID] = shed.WeighingCategory
	}
	rows, err := tx.Query(ctx, `
SELECT cs.location_id,
       cs.weighing_category,
       cs.display_name,
       (SELECT count(*) FROM weighing_observations wo
         WHERE wo.tenant_id=cs.tenant_id AND wo.campaign_shed_id=cs.campaign_shed_id),
       (SELECT count(*) FROM weighing_shed_observations wso
         WHERE wso.tenant_id=cs.tenant_id AND wso.campaign_shed_id=cs.campaign_shed_id)
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id=$1::uuid AND cs.campaign_id=$2::uuid`, tenantID, campaignID)
	if err != nil {
		return err
	}
	defer rows.Close()
	blocked := []string{}
	for rows.Next() {
		var locationID, current, displayName string
		var individualCount, lumpSumCount int
		if err := rows.Scan(&locationID, &current, &displayName, &individualCount, &lumpSumCount); err != nil {
			return err
		}
		next, present := wanted[locationID]
		if !present || next == "" || next == current {
			continue
		}
		if individualCount > 0 || lumpSumCount > 0 {
			blocked = append(blocked, displayName)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(blocked) == 0 {
		return nil
	}
	return &ports.CategoryFlipConflict{Sheds: blocked}
}
