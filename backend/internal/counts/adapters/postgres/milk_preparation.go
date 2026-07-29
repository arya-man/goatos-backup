package postgres

import (
	"context"
	"encoding/json"
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
    g.shed_id AS shed_uuid,
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
farm_verification AS MATERIALIZED (
  SELECT farms.park_uuid,
         COALESCE(c.status, 'not_submitted') AS verification_status,
         COALESCE(c.completion_id::text, '') AS completion_id,
         COALESCE(c.current_attempt_no, 0)::integer AS attempt_no,
         COALESCE(c.rework_reason, '') AS rework_reason
  FROM (SELECT DISTINCT park_uuid FROM grouped WHERE park_uuid IS NOT NULL) farms
  LEFT JOIN milk_preparation_completions c
    ON c.tenant_id = $1::uuid AND c.park_id = farms.park_uuid AND c.shed_id IS NULL
   AND c.preparation_date = $3::date AND c.status <> 'retired'
),
page_window AS (
  SELECT park_uuid, park_id, park_label, shed_uuid, shed_id, shed_label, management_stage, head_count
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
    count(DISTINCT NULLIF(park_id, ''))::integer AS park_count,
    (SELECT count(*) FILTER (WHERE verification_status = 'not_submitted')::integer FROM farm_verification) AS not_submitted_farm_count,
    (SELECT count(*) FILTER (WHERE verification_status = 'pending_verification')::integer FROM farm_verification) AS pending_verification_farm_count,
    (SELECT count(*) FILTER (WHERE verification_status = 'completed')::integer FROM farm_verification) AS completed_farm_count,
    (SELECT count(*) FILTER (WHERE verification_status = 'rework')::integer FROM farm_verification) AS rework_farm_count
  FROM grouped
),
farm_direction AS (
  SELECT
    park_id,
    management_stage,
    sum(head_count)::integer AS head_count,
    CASE management_stage WHEN 'K1' THEN 200 WHEN 'K2' THEN 300 WHEN 'K3' THEN 200 END::integer AS per_head_ml,
    CASE management_stage WHEN 'K1' THEN 4 WHEN 'K2' THEN 4 WHEN 'K3' THEN 2 END::integer AS session_count,
    sum(CASE management_stage
      WHEN 'K1' THEN head_count * 800
      WHEN 'K2' THEN head_count * 1200
      WHEN 'K3' THEN head_count * 400
      ELSE 0 END)::bigint AS required_ml
  FROM grouped
  GROUP BY park_id, management_stage
),
farm_tasks AS (
  SELECT
    grouped.park_id,
    grouped.park_label,
    count(*)::integer AS cohort_count,
    COALESCE(sum(grouped.head_count), 0)::integer AS head_count,
    COALESCE(sum(CASE grouped.management_stage
      WHEN 'K1' THEN grouped.head_count * 800
      WHEN 'K2' THEN grouped.head_count * 1200
      WHEN 'K3' THEN grouped.head_count * 400
      ELSE 0 END), 0)::bigint AS total_required_ml,
    farm_verification.verification_status,
    farm_verification.completion_id,
    farm_verification.attempt_no,
    farm_verification.rework_reason
  FROM grouped
  JOIN farm_verification ON farm_verification.park_uuid = grouped.park_uuid
  GROUP BY grouped.park_id, grouped.park_label,
           farm_verification.verification_status, farm_verification.completion_id,
           farm_verification.attempt_no, farm_verification.rework_reason
),
farm_tasks_json AS (
  SELECT COALESCE(jsonb_agg(jsonb_build_object(
    'park_id', park_id,
    'park_label', park_label,
    'cohort_count', cohort_count,
    'head_count', head_count,
    'total_required_ml', total_required_ml,
    'milk_direction', COALESCE((
      SELECT jsonb_agg(jsonb_build_object(
        'management_stage', direction.management_stage,
        'head_count', direction.head_count,
        'per_head_ml', direction.per_head_ml,
        'session_count', direction.session_count,
        'required_ml', direction.required_ml
      ) ORDER BY direction.management_stage)
      FROM farm_direction direction
      WHERE direction.park_id = farm_tasks.park_id
    ), '[]'::jsonb),
    'citric_acid_grams_per_litre', 5.5,
    'citric_acid_grams', round((total_required_ml::numeric / 1000) * 5.5, 1),
    'verification_status', verification_status,
    'completion_id', completion_id,
    'attempt_no', attempt_no,
    'rework_reason', rework_reason
  ) ORDER BY park_label, park_id), '[]'::jsonb)::text AS payload
  FROM farm_tasks
)
SELECT
  page_window.management_stage IS NOT NULL AS has_item,
  COALESCE(page_window.park_id, ''),
  COALESCE(page_window.park_label, ''),
  COALESCE(page_window.shed_id, ''),
  COALESCE(page_window.shed_label, ''),
  COALESCE(page_window.management_stage, ''),
  COALESCE(page_window.head_count, 0),
	COALESCE(farm_verification.verification_status, 'not_submitted'),
	COALESCE(farm_verification.completion_id, ''),
	COALESCE(farm_verification.attempt_no, 0),
	COALESCE(farm_verification.rework_reason, ''),
  summary.shed_count,
  summary.cohort_count,
  summary.head_count,
  summary.k1_heads,
  summary.k2_heads,
  summary.k3_heads,
  summary.blocked_row_count,
	summary.park_count,
	summary.not_submitted_farm_count,
	summary.pending_verification_farm_count,
	summary.completed_farm_count,
	summary.rework_farm_count,
	farm_tasks_json.payload
FROM summary
CROSS JOIN farm_tasks_json
LEFT JOIN page_window ON true
LEFT JOIN farm_verification ON farm_verification.park_uuid = page_window.park_uuid
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
	var farmTasksJSON string
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
			&summary.ParkCount, &summary.NotSubmittedFarmCount, &summary.PendingVerificationFarmCount,
			&summary.CompletedFarmCount, &summary.ReworkFarmCount,
			&farmTasksJSON,
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
	farmTasks := make([]domain.MilkPreparationFarmTask, 0)
	if err := json.Unmarshal([]byte(farmTasksJSON), &farmTasks); err != nil {
		return domain.MilkPreparationPage{}, fmt.Errorf("milk preparation: decode farm tasks: %w", err)
	}

	return domain.MilkPreparationPage{
		PreparationDate: preparationDay.Format("2006-01-02"),
		FeedingDate:     preparationDay.AddDate(0, 0, 1).Format("2006-01-02"),
		GeneratedAt:     asOf,
		Items:           items,
		FarmTasks:       farmTasks,
		Summary:         summary,
		Limit:           limit,
		Offset:          offset,
		HasMore:         hasMore,
	}, nil
}
