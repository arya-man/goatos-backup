package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// campaignShedCursor is the task-detail bucket keyset. display_name is the order
// the detail screen shows buckets in, and campaign_shed_id breaks ties so two
// buckets with the same display name cannot loop or skip.
type campaignShedCursor struct {
	DisplayName    string `json:"display_name"`
	CampaignShedID string `json:"campaign_shed_id"`
}

func encodeCampaignShedCursor(cursor campaignShedCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCampaignShedCursor(value string) (campaignShedCursor, error) {
	if strings.TrimSpace(value) == "" {
		return campaignShedCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return campaignShedCursor{}, err
	}
	var cursor campaignShedCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return campaignShedCursor{}, err
	}
	if cursor.CampaignShedID == "" {
		return campaignShedCursor{}, ports.ErrInvalidArgument
	}
	return cursor, nil
}

// ListCampaignSheds pages ONE task's buckets.
//
// projection-review: membership=weighing_campaign_sheds rows of one campaign,
// optionally narrowed to one operator; group_key=(tenant_id, campaign_id,
// campaign_shed_id); join_cardinality=nothing is joined into the row path — the
// per-bucket verification tallies are CORRELATED SUBQUERIES evaluated once per
// returned row by the planner, and workforce_members is filtered to status='active'
// (its only (tenant_id,user_id) uniqueness is the partial active index), so neither
// can fan the bucket row out;
// pagination=keyset on (display_name, campaign_shed_id) ASC with a ~20 default;
// scope=tenant_id plus campaign_id, plus the optional operator predicate.
//
// Grain proof:
//
//	producer weighing_campaign_sheds unique: (campaign_shed_id) PK; tenant/campaign
//	  scoped (tenant_id, campaign_id, campaign_shed_id)
//	consumer page row                match:  the same triple — one row out per row in
//	weighing_observations are 1..N per bucket and weighing_shed_observations is
//	  0..1 OPEN per bucket (plus any number of withdrawn ones, since a rejected or
//	  reopened proof is kept as history). Both are read ONLY inside scalar count subqueries, so they
//	  contribute zero extra rows either way.
//
// TotalCount ranges over the SAME key set as the rows (tenant, campaign, and the
// same operator predicate), so the header count and the pages can never describe
// different bucket sets.
//
// Index-backed by weighing_campaign_sheds_detail_keyset_idx
// (tenant_id, campaign_id, display_name, campaign_shed_id) — migration 000063.
func (r *Repository) ListCampaignSheds(ctx context.Context, tenantID, campaignID, operatorUserID, cursor string, limit int) (domain.CampaignShedPage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = domain.CampaignShedPageSize
	}
	if limit > domain.MaxCampaignShedPageSize {
		limit = domain.MaxCampaignShedPageSize
	}
	cur, err := decodeCampaignShedCursor(cursor)
	if err != nil {
		return domain.CampaignShedPage{}, ports.ErrInvalidArgument
	}
	operatorFilter := strings.TrimSpace(operatorUserID)
	rows, err := r.pool.Query(ctx, `
SELECT cs.campaign_shed_id::text, cs.campaign_id::text, cs.location_id::text, cs.location_type, cs.display_name,
  cs.expected_animal_count, cs.weighing_category, cs.operator_user_id::text, COALESCE(op.display_name, ''), cs.status,
  `+readyToCloseCountsSQL+`
FROM weighing_campaign_sheds cs
LEFT JOIN workforce_members op
  ON op.tenant_id=cs.tenant_id AND op.user_id=cs.operator_user_id AND op.status='active'
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND ($3::uuid IS NULL OR cs.operator_user_id=$3::uuid)
  AND (
    $4::text IS NULL
    OR (cs.display_name, cs.campaign_shed_id) > ($4::text, $5::uuid)
  )
ORDER BY cs.display_name, cs.campaign_shed_id
LIMIT $6`, tenantID, campaignID, nullableString(operatorFilter),
		nullableString(cur.DisplayName), nullableString(cur.CampaignShedID), limit+1)
	if err != nil {
		return domain.CampaignShedPage{}, err
	}
	defer rows.Close()
	page := domain.CampaignShedPage{CampaignID: campaignID, Items: make([]domain.CampaignShed, 0, limit)}
	for rows.Next() {
		var shed domain.CampaignShed
		var submitted int
		if err := rows.Scan(&shed.CampaignShedID, &shed.CampaignID, &shed.LocationID, &shed.LocationType, &shed.DisplayName,
			&shed.ExpectedAnimalCount, &shed.WeighingCategory, &shed.OperatorUserID, &shed.OperatorDisplayName, &shed.Status,
			&submitted, &shed.PendingVerificationCount, &shed.ReworkCount); err != nil {
			return domain.CampaignShedPage{}, err
		}
		shed.ReadyToClose = shed.Status == domain.StatusCompleted && submitted > 0 && shed.PendingVerificationCount == 0
		page.Items = append(page.Items, shed)
	}
	if err := rows.Err(); err != nil {
		return domain.CampaignShedPage{}, err
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.NextCursor = encodeCampaignShedCursor(campaignShedCursor{DisplayName: last.DisplayName, CampaignShedID: last.CampaignShedID})
		page.Items = page.Items[:limit]
	}
	// Whole-task count, deliberately a SEPARATE count-only query: folding it into
	// the paged query as a window function would force the LIMIT off and turn a
	// bounded keyset page into a full scan of the task's buckets on every page.
	if err := r.pool.QueryRow(ctx, `
SELECT count(*)::int
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND ($3::uuid IS NULL OR cs.operator_user_id=$3::uuid)`,
		tenantID, campaignID, nullableString(operatorFilter)).Scan(&page.TotalCount); err != nil {
		return domain.CampaignShedPage{}, err
	}
	return page, nil
}
