package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// milkPreparationGroupedCTE is the single definition of the page's canonical membership and row
// grain. Producer unique columns are goats(tenant_id, goat_id); consumer group columns are
// (park_id, shed_id, management_stage). The locations joins are both 1:0..1 label lookups on the
// tenant/location key and cannot multiply goats. Page and summary consume the identical grouped
// key set; numerator and denominator for every quantity both range over those K1/K2/K3 live-goat
// rows.
const milkPreparationGroupedCTE = `
WITH grouped AS MATERIALIZED (
  SELECT
	 g.park_id AS park_uuid,
    COALESCE(g.park_id::text, '') AS park_id,
    COALESCE(NULLIF(park.location_code, ''), park.name, '') AS park_label,
    COALESCE(g.shed_id::text, '') AS shed_id,
    COALESCE(NULLIF(shed.name, ''), shed.location_code, '') AS shed_label,
    g.management_stage,
    count(*)::integer AS head_count
  FROM goats g
  LEFT JOIN locations park
    ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
  LEFT JOIN locations shed
    ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND g.lifecycle_status = 'alive'
    AND g.management_stage IN ('K1', 'K2', 'K3')
    AND ($2 = '' OR g.park_id = NULLIF($2, '')::uuid)
  GROUP BY g.park_id, park.location_code, park.name,
           g.shed_id, shed.name, shed.location_code, g.management_stage
)`

// One indexed canonical live-herd aggregate, bounded after grouping to physical sheds x three milk
// cohorts. OFFSET walks only this grouped set (not goats), is handler-capped, and the query asks for
// limit+1 solely to produce has_more.
// scale-guard:ignore: bounded physical-shed x three-cohort worklist; offset is capped at 5000
const milkPreparationPageSQL = milkPreparationGroupedCTE + `,
park_verification AS MATERIALIZED (
  SELECT parks.park_uuid,
         COALESCE(c.status, 'not_submitted') AS verification_status,
         COALESCE(c.completion_id::text, '') AS completion_id,
         COALESCE(c.current_attempt_no, 0)::integer AS attempt_no,
         COALESCE(c.rework_reason, '') AS rework_reason
  FROM (SELECT DISTINCT park_uuid FROM grouped WHERE park_uuid IS NOT NULL) parks
  LEFT JOIN milk_preparation_completions c
    ON c.tenant_id = $1::uuid AND c.park_id = parks.park_uuid AND c.preparation_date = $3::date
),
page_window AS (
  SELECT park_uuid, park_id, park_label, shed_id, shed_label, management_stage, head_count
  FROM grouped
  ORDER BY park_label, shed_label, management_stage, park_id, shed_id
  LIMIT $4 OFFSET $5
),
summary AS (
  SELECT
    count(DISTINCT NULLIF(shed_id, ''))::integer AS shed_count,
    count(*)::integer AS cohort_count,
    COALESCE(sum(head_count), 0)::integer AS head_count,
    COALESCE(sum(head_count) FILTER (WHERE management_stage = 'K1'), 0)::integer AS k1_heads,
    COALESCE(sum(head_count) FILTER (WHERE management_stage = 'K2'), 0)::integer AS k2_heads,
    COALESCE(sum(head_count) FILTER (WHERE management_stage = 'K3'), 0)::integer AS k3_heads,
    count(*) FILTER (WHERE shed_id = '')::integer AS blocked_row_count,
    (SELECT count(*)::integer FROM park_verification) AS park_count,
    (SELECT count(*) FILTER (WHERE verification_status = 'not_submitted')::integer FROM park_verification) AS not_submitted_park_count,
    (SELECT count(*) FILTER (WHERE verification_status = 'pending_verification')::integer FROM park_verification) AS pending_verification_park_count,
    (SELECT count(*) FILTER (WHERE verification_status = 'completed')::integer FROM park_verification) AS completed_park_count,
    (SELECT count(*) FILTER (WHERE verification_status = 'rework')::integer FROM park_verification) AS rework_park_count
  FROM grouped
)
SELECT
  page_window.management_stage IS NOT NULL AS has_item,
  COALESCE(page_window.park_id, ''),
  COALESCE(page_window.park_label, ''),
  COALESCE(page_window.shed_id, ''),
  COALESCE(page_window.shed_label, ''),
  COALESCE(page_window.management_stage, ''),
  COALESCE(page_window.head_count, 0),
	COALESCE(park_verification.verification_status, 'not_submitted'),
	COALESCE(park_verification.completion_id, ''),
	COALESCE(park_verification.attempt_no, 0),
	COALESCE(park_verification.rework_reason, ''),
  summary.shed_count,
  summary.cohort_count,
  summary.head_count,
  summary.k1_heads,
  summary.k2_heads,
  summary.k3_heads,
  summary.blocked_row_count,
	summary.park_count,
	summary.not_submitted_park_count,
	summary.pending_verification_park_count,
	summary.completed_park_count,
	summary.rework_park_count
FROM summary
LEFT JOIN page_window ON true
LEFT JOIN park_verification ON park_verification.park_uuid = page_window.park_uuid
ORDER BY page_window.park_label, page_window.shed_label, page_window.management_stage,
         page_window.park_id, page_window.shed_id`

// GetMilkPreparation serves a bounded page plus whole-scope summary from the current canonical
// herd. It is deliberately a live current-day read; no request date is accepted, so historical
// rows can never be fabricated from today's animal locations.
func (r *Repository) GetMilkPreparation(ctx context.Context, req domain.MilkPreparationQuery) (domain.MilkPreparationPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	offset := req.Offset
	if offset < 0 || offset > 5000 {
		offset = 0
	}
	asOf := req.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	parkID := ptrValue(req.ParkID)
	preparationDay := biztime.BusinessDayStart(asOf)

	rows, err := r.pool.Query(ctx, milkPreparationPageSQL, req.TenantID, parkID, preparationDay.Format("2006-01-02"), limit+1, offset)
	if err != nil {
		return domain.MilkPreparationPage{}, fmt.Errorf("milk preparation: query rows: %w", err)
	}
	defer rows.Close()

	items := make([]domain.MilkPreparationRow, 0, limit+1)
	var summary domain.MilkPreparationSummary
	var k1Heads, k2Heads, k3Heads int
	summarySeen := false
	for rows.Next() {
		var hasItem bool
		var count domain.MilkPreparationCohortCount
		var verificationStatus, completionID, reworkReason string
		var attemptNo int32
		if err := rows.Scan(
			&hasItem,
			&count.ParkID, &count.ParkLabel, &count.ShedID, &count.ShedLabel,
			&count.ManagementStage, &count.HeadCount,
			&verificationStatus, &completionID, &attemptNo, &reworkReason,
			&summary.ShedCount, &summary.CohortCount, &summary.HeadCount,
			&k1Heads, &k2Heads, &k3Heads, &summary.BlockedRowCount,
			&summary.ParkCount, &summary.NotSubmittedParkCount, &summary.PendingVerificationParkCount,
			&summary.CompletedParkCount, &summary.ReworkParkCount,
		); err != nil {
			return domain.MilkPreparationPage{}, fmt.Errorf("milk preparation: scan row: %w", err)
		}
		summarySeen = true
		if !hasItem {
			continue
		}
		row, ok := domain.BuildMilkPreparationRow(count)
		if !ok {
			return domain.MilkPreparationPage{}, fmt.Errorf("milk preparation: unsupported stage %q returned by constrained query", count.ManagementStage)
		}
		row.VerificationStatus = verificationStatus
		row.CompletionID = completionID
		row.AttemptNo = attemptNo
		row.ReworkReason = reworkReason
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return domain.MilkPreparationPage{}, fmt.Errorf("milk preparation: iterate rows: %w", err)
	}
	if !summarySeen {
		return domain.MilkPreparationPage{}, fmt.Errorf("milk preparation: query returned no summary row")
	}

	hasMore := len(items) > int(limit)
	if hasMore {
		items = items[:limit]
	}

	summary.Scope = "filtered"
	for stage, heads := range map[string]int{"K1": k1Heads, "K2": k2Heads, "K3": k3Heads} {
		dailyML, _ := domain.MilkPreparationDailyMLPerHead(stage)
		summary.TotalRequiredML += int64(heads) * dailyML
	}
	summary.CitricAcidGrams = domain.MilkPreparationCitricAcidGrams(summary.TotalRequiredML)

	return domain.MilkPreparationPage{
		PreparationDate: preparationDay.Format("2006-01-02"),
		FeedingDate:     preparationDay.AddDate(0, 0, 1).Format("2006-01-02"),
		GeneratedAt:     asOf,
		Items:           items,
		Summary:         summary,
		Limit:           limit,
		Offset:          offset,
		HasMore:         hasMore,
	}, nil
}
