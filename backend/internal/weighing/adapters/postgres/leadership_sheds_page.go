package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// leadershipShedCursor is the BUCKET-grain leadership keyset.
//
// It extends the task-list keyset (period_start_date, created_at, campaign_id)
// with campaign_shed_id, so the gallery walks the same task order leadership
// already reads tasks in, and a task whose buckets straddle a page boundary
// resumes inside itself instead of restarting.
type leadershipShedCursor struct {
	PeriodStartDate string    `json:"period_start_date"`
	CreatedAt       time.Time `json:"created_at"`
	CampaignID      string    `json:"campaign_id"`
	CampaignShedID  string    `json:"campaign_shed_id"`
}

func encodeLeadershipShedCursor(cursor leadershipShedCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeLeadershipShedCursor(value string) (leadershipShedCursor, error) {
	if strings.TrimSpace(value) == "" {
		return leadershipShedCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return leadershipShedCursor{}, err
	}
	var cursor leadershipShedCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return leadershipShedCursor{}, err
	}
	if cursor.CampaignID == "" || cursor.CampaignShedID == "" {
		return leadershipShedCursor{}, ports.ErrInvalidArgument
	}
	return cursor, nil
}

// ListLeadershipSheds is the leadership gallery read: ONE keyset page of shed
// buckets across tasks, each carrying its own context and its FIRST page of
// captured evidence.
//
// It exists because the gallery previously had no bucket-grain read at all: the
// client fetched a page of TASKS, expanded every embedded bucket, and then issued
// one shed-videos HTTP call per bucket — roughly 1,500 sequential round trips on a
// 76-shed park, on every screen resume. A per-call limit bounds each response, not
// the number of calls; the only fix is a page at the grain the screen renders.
//
// projection-review: membership=weighing_campaign_sheds rows of one tenant, walked
// in task order; group_key=(tenant_id, campaign_id, campaign_shed_id);
// join_cardinality=weighing_campaigns is 1:1 on campaign_id, locations park is 0..1
// on its primary key, and workforce_members is filtered to status='active' (its only
// (tenant_id,user_id) uniqueness is the partial active index) — none can multiply the
// bucket row; pagination=keyset on
// (period_start_date, created_at, campaign_id, campaign_shed_id) DESC;
// scope=tenant_id, narrowed by the caller's capability-scoped parkIDs when it has any.
//
// Every evidence read below is bounded on BOTH axes: at most `limit` buckets, and at
// most one page of observations per bucket, fetched with a LATERAL that the planner
// walks through weighing_observations_shed_keyset_idx (migration 000060).
func (r *Repository) ListLeadershipSheds(ctx context.Context, tenantID string, parkIDs []string, cursor string, limit, perShedLimit int) (domain.LeadershipShedPage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = domain.LeadershipShedPageSize
	}
	if limit > domain.MaxLeadershipShedPageSize {
		limit = domain.MaxLeadershipShedPageSize
	}
	if perShedLimit <= 0 {
		perShedLimit = domain.LeadershipShedVideosPageSize
	}
	if perShedLimit > domain.MaxLeadershipShedVideosPageSize {
		perShedLimit = domain.MaxLeadershipShedVideosPageSize
	}
	cur, err := decodeLeadershipShedCursor(cursor)
	if err != nil {
		return domain.LeadershipShedPage{}, ports.ErrInvalidArgument
	}

	type keyRow struct {
		periodStartDate string
		createdAt       time.Time
	}
	rows, err := r.pool.Query(ctx, `
SELECT wc.campaign_id::text, cs.campaign_shed_id::text, cs.display_name, COALESCE(cs.partition_label, ''),
       COALESCE(cs.operator_user_id::text, ''), cs.weighing_category, cs.status,
       COALESCE(cs.expected_animal_count, 0),
       COALESCE(park.name, ''),
       COALESCE(cs.start_business_date::text, wc.start_business_date::text, ''),
       COALESCE(op.display_name, ''),
       wc.period_start_date::text, COALESCE(wc.period_end_date::text, ''),
       wc.created_at
FROM weighing_campaigns wc
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=wc.tenant_id AND cs.campaign_id=wc.campaign_id
LEFT JOIN locations park
  ON park.tenant_id=wc.tenant_id AND park.location_id=wc.park_id
LEFT JOIN workforce_members op
  ON op.tenant_id=cs.tenant_id AND op.user_id=cs.operator_user_id AND op.status='active'
WHERE wc.tenant_id=$1::uuid
  AND cs.status <> 'canceled'
  -- The park filter is IN the keyset walk, not applied to its result: a page cut before
  -- authorization came back short or empty for a park-scoped monitor while their own
  -- buckets sat further down the order, unreachable. NULL means unrestricted.
  AND ($7::uuid[] IS NULL OR wc.park_id = ANY($7::uuid[]))
  AND (
    $2::date IS NULL
    OR (wc.period_start_date, wc.created_at, wc.campaign_id, cs.campaign_shed_id)
       < ($2::date, $3::timestamptz, $4::uuid, $5::uuid)
  )
ORDER BY wc.period_start_date DESC, wc.created_at DESC, wc.campaign_id DESC, cs.campaign_shed_id DESC
LIMIT $6`,
		tenantID, nullableString(cur.PeriodStartDate), nullableTime(cur.CreatedAt),
		nullableString(cur.CampaignID), nullableString(cur.CampaignShedID), limit+1,
		nullableStrings(parkIDs))
	if err != nil {
		return domain.LeadershipShedPage{}, err
	}
	defer rows.Close()

	page := domain.LeadershipShedPage{Items: make([]domain.LeadershipShedVideos, 0, limit)}
	keys := make([]keyRow, 0, limit+1)
	for rows.Next() {
		var item domain.LeadershipShedVideos
		var key keyRow
		var periodStart, periodEnd string
		if err := rows.Scan(&item.CampaignID, &item.CampaignShedID, &item.ShedName, &item.PartitionLabel,
			&item.OperatorUserID, &item.WeighingCategory, &item.Status, &item.EstimatedAnimalCount,
			&item.ParkName, &item.WeighDate, &item.OperatorDisplayName,
			&periodStart, &periodEnd, &key.createdAt); err != nil {
			return domain.LeadershipShedPage{}, err
		}
		applyLeadershipShedPartitionDisplay(&item)
		item.PeriodLabel = periodLabel(periodStart, periodEnd)
		item.Individual = []domain.Observation{}
		item.MaxShedVideos = domain.MaxShedProofArtifacts
		key.periodStartDate = periodStart
		page.Items = append(page.Items, item)
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return domain.LeadershipShedPage{}, err
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		lastKey := keys[limit-1]
		page.NextCursor = encodeLeadershipShedCursor(leadershipShedCursor{
			PeriodStartDate: lastKey.periodStartDate,
			CreatedAt:       lastKey.createdAt,
			CampaignID:      last.CampaignID,
			CampaignShedID:  last.CampaignShedID,
		})
		page.Items = page.Items[:limit]
	}
	if len(page.Items) == 0 {
		return page, nil
	}

	byShed := make(map[string]int, len(page.Items))
	individualCampaigns := make([]string, 0, len(page.Items))
	individualSheds := make([]string, 0, len(page.Items))
	lumpCampaigns := make([]string, 0, len(page.Items))
	lumpSheds := make([]string, 0, len(page.Items))
	for i := range page.Items {
		byShed[page.Items[i].CampaignID+"/"+page.Items[i].CampaignShedID] = i
		if page.Items[i].WeighingCategory == domain.CategoryPerShedPartition {
			lumpCampaigns = append(lumpCampaigns, page.Items[i].CampaignID)
			lumpSheds = append(lumpSheds, page.Items[i].CampaignShedID)
			continue
		}
		individualCampaigns = append(individualCampaigns, page.Items[i].CampaignID)
		individualSheds = append(individualSheds, page.Items[i].CampaignShedID)
	}

	if len(individualSheds) > 0 {
		obsRows, err := r.pool.Query(ctx, `
SELECT o.campaign_id::text, o.campaign_shed_id::text, o.observation_id::text,
       o.scanned_identifier,
       o.weight_kg::float8, o.proof_artifact_id::text, o.accepted_at
FROM unnest($2::uuid[], $3::uuid[]) AS k(campaign_id, campaign_shed_id)
CROSS JOIN LATERAL (
  SELECT w.campaign_id, w.campaign_shed_id, w.observation_id, w.scanned_identifier,
         w.weight_kg, w.proof_artifact_id, w.accepted_at
  FROM weighing_observations w
  WHERE w.tenant_id=$1::uuid AND w.campaign_id=k.campaign_id AND w.campaign_shed_id=k.campaign_shed_id
  ORDER BY w.accepted_at, w.observation_id
  LIMIT $4
) o`, tenantID, individualCampaigns, individualSheds, perShedLimit+1)
		if err != nil {
			return domain.LeadershipShedPage{}, err
		}
		defer obsRows.Close()
		for obsRows.Next() {
			var observation domain.Observation
			if err := obsRows.Scan(&observation.CampaignID, &observation.CampaignShedID,
				&observation.ObservationID, &observation.ScannedIdentifier, &observation.WeightKg,
				&observation.ProofArtifactID, &observation.AcceptedAt); err != nil {
				return domain.LeadershipShedPage{}, err
			}
			observation.ProofArtifactIDs = []string{observation.ProofArtifactID}
			if idx, ok := byShed[observation.CampaignID+"/"+observation.CampaignShedID]; ok {
				page.Items[idx].Individual = append(page.Items[idx].Individual, observation)
			}
		}
		if err := obsRows.Err(); err != nil {
			return domain.LeadershipShedPage{}, err
		}
		// The LATERAL asked for one row of lookahead per bucket, exactly as the
		// single-shed read does, so each bucket states its own next cursor.
		for i := range page.Items {
			if len(page.Items[i].Individual) > perShedLimit {
				last := page.Items[i].Individual[perShedLimit-1]
				page.Items[i].NextIndividualCursor = encodeObservationsCursor(observationsCursor{
					AcceptedAt: last.AcceptedAt, ObservationID: last.ObservationID,
				})
				page.Items[i].Individual = page.Items[i].Individual[:perShedLimit]
			}
		}
	}

	if len(lumpSheds) > 0 {
		lumpRows, err := r.pool.Query(ctx, `
SELECT l.campaign_id::text, l.campaign_shed_id::text, l.shed_observation_id::text,
       l.weight_kg::float8, l.average_weight_kg::float8, l.animal_count,
       l.proof_artifact_id::text, l.proof_ids, l.accepted_at
FROM unnest($2::uuid[], $3::uuid[]) AS k(campaign_id, campaign_shed_id)
CROSS JOIN LATERAL (
  SELECT wso.campaign_id, wso.campaign_shed_id, wso.shed_observation_id, wso.weight_kg,
         wso.average_weight_kg, wso.animal_count, wso.proof_artifact_id, wso.accepted_at,
         COALESCE(
           (SELECT array_agg(p.proof_artifact_id::text ORDER BY p.proof_position)
              FROM weighing_shed_observation_proofs p
             WHERE p.tenant_id=wso.tenant_id AND p.shed_observation_id=wso.shed_observation_id),
           ARRAY[wso.proof_artifact_id::text]
         ) AS proof_ids
  FROM weighing_shed_observations wso
  WHERE wso.tenant_id=$1::uuid AND wso.campaign_id=k.campaign_id AND wso.campaign_shed_id=k.campaign_shed_id
    AND wso.withdrawn_at IS NULL
  ORDER BY wso.accepted_at DESC
  LIMIT 1
) l`, tenantID, lumpCampaigns, lumpSheds)
		if err != nil {
			return domain.LeadershipShedPage{}, err
		}
		defer lumpRows.Close()
		for lumpRows.Next() {
			var lump domain.Observation
			if err := lumpRows.Scan(&lump.CampaignID, &lump.CampaignShedID, &lump.ObservationID,
				&lump.WeightKg, &lump.AverageWeightKg, &lump.AnimalCount, &lump.ProofArtifactID,
				&lump.ProofArtifactIDs, &lump.AcceptedAt); err != nil {
				return domain.LeadershipShedPage{}, err
			}
			if idx, ok := byShed[lump.CampaignID+"/"+lump.CampaignShedID]; ok {
				row := lump
				page.Items[idx].LumpSum = &row
			}
		}
		if err := lumpRows.Err(); err != nil {
			return domain.LeadershipShedPage{}, err
		}
	}

	return page, nil
}

// nullableStrings sends an EMPTY id set to Postgres as NULL rather than as `{}`.
//
// The two are opposite answers to the park filter: `= ANY('{}')` is false for every row, which
// would silently blank the gallery for callers the service means to leave unrestricted (a
// tenant-wide monitor, a CLI/test context with no grants at all). NULL is the "no restriction"
// arm the query is written around.
func nullableStrings(values []string) any {
	if len(values) == 0 {
		return nil
	}
	return values
}

// periodLabel is backend-owned copy: the client must not invent the weigh-period
// sentence by concatenating two dates itself.
func periodLabel(start, end string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(start) != "" {
		parts = append(parts, start)
	}
	if strings.TrimSpace(end) != "" && end != start {
		parts = append(parts, end)
	}
	return strings.Join(parts, " - ")
}
