package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// milkPreparationGroupedCTE is the single definition of the page's canonical membership and row
// grain. The row grain is OperationalLocation-complete: physical shed x management stage x
// partition. partitionKeyExpr (defined once in repository.go, shared byte-for-byte with the
// Counts Breakdown and operator-execution normalizers, and with oploc.NormalizePartition on the
// Go side) collapses NULL/”/'whole' to the same 'whole' bucket so a non-partitioned shed is one
// row, never fragmented by encoding.
//
// projection-review: membership=goats, unique on (tenant_id,goat_id), filtered to live unmerged
// animals in management_stage K1/K2/K3; group_key=(park_id, shed_id, management_stage,
// partition_key) for page rows read directly off this CTE at full OperationalLocation grain,
// re-rolled to (park_id, shed_id, management_stage) in grouped_by_shed for cohort summary and
// farm_tasks BEFORE aggregating, so those whole-scope numbers are byte-identical to the
// pre-partition grain and independent of how many partition rows a shed has; join_cardinality=both
// locations joins are 1:0..1 label lookups on (tenant_id, location_id) that cannot multiply goats,
// goat_shed_partitions is 1:{0,1} per animal on its (tenant_id, goat_id) primary key so it cannot
// fan out the count either, and farm_verification is pre-aggregated to one row per park_uuid
// before farm_tasks joins it, so head_count and required_ml stay one-row-per-goat sums;
// pagination=OFFSET walks only the page-grain GROUPED set (physical sheds x three milk cohorts x
// partition), never goats, and every summary is a whole-filter aggregate over grouped_by_shed
// rather than a rollup of the returned page; scope=park, applied inside the CTE from the caller's
// clamped park filter so page and summary share one scope
//
// Numerator and denominator for every quantity both range over those same K1/K2/K3 live-goat rows.
const milkPreparationGroupedCTE = `
WITH grouped AS MATERIALIZED (
  SELECT
	 g.park_id AS park_uuid,
    COALESCE(g.park_id::text, '') AS park_id,
    COALESCE(NULLIF(park.location_code, ''), park.name, '') AS park_label,
    g.shed_id AS shed_uuid,
    COALESCE(g.shed_id::text, '') AS shed_id,
    COALESCE(
      CASE
        WHEN exact_sp.operational_location_id IS NOT NULL THEN NULL
        WHEN lower(btrim(COALESCE(gsp.source_shed_name, ''))) IN ('', 'seed') THEN NULL
        WHEN btrim(COALESCE(gsp.source_shed_name, '')) = btrim(COALESCE(gsp.partition_label, '')) THEN NULL
        ELSE btrim(gsp.source_shed_name)
      END,
      NULLIF(shed.name, ''),
      shed.location_code,
      ''
    ) AS shed_label,
    g.management_stage,
    ` + partitionKeyExpr + ` AS partition_key,
    -- Raw label as stored (or NULL for non-partitioned), kept alongside the normalized key so the
    -- display preserves each shed's own 'N' vs 'Part N' convention. min() picks a deterministic
    -- representative among rows sharing the same normalized key.
	    min(CASE WHEN exact_sp.operational_location_id IS NOT NULL THEN NULL ELSE gsp.partition_label END) AS partition_label_raw,
    count(*)::integer AS head_count
  FROM goats g
  LEFT JOIN locations park
    ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
  LEFT JOIN locations shed
    ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
  -- 1:{0,1} per animal (goat_shed_partitions PK is (tenant_id, goat_id)) -- no fan-out. A goat
  -- with no row here is not partitioned and normalizes to 'whole'.
	  LEFT JOIN goat_shed_partitions gsp
	    ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
	  LEFT JOIN shed_partitions exact_sp
	    ON exact_sp.tenant_id = g.tenant_id
	   AND exact_sp.operational_location_id = g.shed_id
	   AND exact_sp.status = 'active'
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND g.lifecycle_status = 'alive'
    AND g.management_stage IN ('K1', 'K2', 'K3')
    AND ($2 = '' OR g.park_id = NULLIF($2, '')::uuid)
  GROUP BY g.park_id, park.location_code, park.name,
           g.shed_id, shed.name, shed.location_code, g.management_stage,
           CASE
             WHEN exact_sp.operational_location_id IS NOT NULL THEN NULL
             WHEN lower(btrim(COALESCE(gsp.source_shed_name, ''))) IN ('', 'seed') THEN NULL
             WHEN btrim(COALESCE(gsp.source_shed_name, '')) = btrim(COALESCE(gsp.partition_label, '')) THEN NULL
             ELSE btrim(gsp.source_shed_name)
           END,
           ` + partitionKeyExpr + `
),
-- grouped_by_shed re-rolls the partition grain back up to the pre-partition (shed, stage) grain.
-- Every whole-scope consumer below (farm_verification's park set, summary, farm_direction,
-- farm_tasks) reads THIS, not grouped, so head_count/required_ml/cohort_count are byte-identical
-- to what they were before partitions existed -- summing head_count is exact because it is already
-- a per-row count(*), never re-derived from goats.
grouped_by_shed AS (
  SELECT park_uuid, park_id, park_label, shed_uuid, shed_id, min(shed_label) AS shed_label, management_stage,
         sum(head_count)::integer AS head_count
  FROM grouped
  GROUP BY park_uuid, park_id, park_label, shed_uuid, shed_id, management_stage
  -- Deliberately re-rolled up ACROSS partition_key: this GROUP BY reads the already
  -- partition-complete grouped CTE and sums its head_count back to the pre-partition shed grain
  -- for whole-scope summary/farm_task consumers only; page_window (the visible row grain) reads
  -- grouped directly and keeps the partition dimension.
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
  SELECT park_uuid, park_id, park_label, shed_uuid, shed_id, shed_label, partition_key,
         -- '' when partition_key is 'whole' (non-partitioned): never surface the sentinel to a
         -- client. Matches the Counts Breakdown contract byte-for-byte.
         CASE WHEN partition_key = 'whole' THEN '' ELSE partition_label_raw END AS partition_label,
         management_stage, head_count
  FROM grouped
  ORDER BY park_label, shed_label, partition_key, management_stage, park_id, shed_id
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
  FROM grouped_by_shed
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
  FROM grouped_by_shed
  GROUP BY park_id, management_stage
),
farm_tasks AS (
  SELECT
    grouped_by_shed.park_id,
    grouped_by_shed.park_label,
    count(*)::integer AS cohort_count,
    COALESCE(sum(grouped_by_shed.head_count), 0)::integer AS head_count,
    COALESCE(sum(CASE grouped_by_shed.management_stage
      WHEN 'K1' THEN grouped_by_shed.head_count * 800
      WHEN 'K2' THEN grouped_by_shed.head_count * 1200
      WHEN 'K3' THEN grouped_by_shed.head_count * 400
      ELSE 0 END), 0)::bigint AS total_required_ml,
    farm_verification.verification_status,
    farm_verification.completion_id,
    farm_verification.attempt_no,
    farm_verification.rework_reason
  FROM grouped_by_shed
  JOIN farm_verification ON farm_verification.park_uuid = grouped_by_shed.park_uuid
  GROUP BY grouped_by_shed.park_id, grouped_by_shed.park_label,
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
  COALESCE(page_window.partition_label, ''),
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
ORDER BY page_window.park_label, page_window.shed_label, page_window.partition_key,
         page_window.management_stage, page_window.park_id, page_window.shed_id`

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
			&count.ParkID, &count.ParkLabel, &count.ShedID, &count.ShedLabel, &count.PartitionLabel,
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
		row.OperationalLocationDisplay = oploc.OperationalLocation{
			ShedName:       row.ShedLabel,
			PartitionLabel: row.PartitionLabel,
		}.Display()
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
