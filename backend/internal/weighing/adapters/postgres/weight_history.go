package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

const (
	MaxWeightHistoryUniqueTags = 1000
	MaxWeightHistoryWeighDays  = 90
	MaxWeightHistoryPoints     = 10000
)

// GetWeightHistory returns weight observations (both individual and lump-sum) per RFID or shed.
// Results include parks and sheds actually present in the data for filter chips.
func (r *Repository) GetWeightHistory(ctx context.Context, tenantID string, authorizedParkIDs []string, parkID, campaignShedID string) (domain.WeightHistory, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	// EVERY caller-supplied value is BOUND, never formatted into the SQL text.
	//
	// park_id and campaign_shed_id arrive from the query string. Interpolating them with
	// fmt.Sprintf put arbitrary caller text inside the statement: a crafted campaign_shed_id could
	// close the literal and append its own predicate, defeating the park scoping built into the
	// very same string and reaching another tenant's rows. Binding removes the class entirely --
	// a bound value is data, and can never become SQL.
	//
	// NULL means "no filter" for the two optional predicates, so one statement serves every
	// combination without rebuilding the text per request.
	var parkFilter, shedFilter any
	if strings.TrimSpace(parkID) != "" {
		parkFilter = parkID
	}
	if strings.TrimSpace(campaignShedID) != "" {
		shedFilter = campaignShedID
	}
	authorized := authorizedParkIDs
	if len(authorized) == 0 {
		// nil, not an empty array: an empty ARRAY[] would match nothing and silently return an
		// empty chart to a tenant-wide monitor.
		authorized = nil
	}

	// Query UNION of individual and lump-sum observations.
	q := fmt.Sprintf(`
(SELECT
  'individual'::text AS capture_kind,
  wo.scanned_identifier,
  DATE(TIMEZONE('Asia/Kolkata', wo.accepted_at)::date) AS weigh_date,
  wo.accepted_at AS accepted_at,
  wo.weight_kg,
  NULL::float8 AS total_weight_kg,
  NULL::float8 AS average_weight_kg,
  NULL::int AS animal_count,
  wo.verification_status,
  wo.campaign_shed_id,
  wcs.display_name AS shed_display_name,
  wcs.location_id AS location_id,
  wc.park_id
FROM weighing_observations wo
JOIN weighing_campaign_sheds wcs ON wo.campaign_shed_id = wcs.campaign_shed_id
JOIN weighing_campaigns wc ON wcs.campaign_id = wc.campaign_id
WHERE wo.tenant_id = $1::uuid
  AND wcs.tenant_id = $1::uuid
  AND wc.tenant_id = $1::uuid
  AND ($2::uuid IS NULL OR wc.park_id = $2::uuid)
  AND ($3::uuid IS NULL OR wo.campaign_shed_id = $3::uuid)
  AND ($4::uuid[] IS NULL OR wc.park_id = ANY($4::uuid[]))
  -- Weights AWAITING VERIFICATION are included, and their status travels to the client so the
  -- chart can mark them. Excluding them made a weight captured this morning invisible to
  -- leadership until a verifier happened to act, so the honest answer to "what did we weigh
  -- today" was an empty chart. A pending weight is a real measurement; it is just not yet
  -- checked, which is a caption, not a reason to hide it.
  )
UNION ALL
(SELECT
  'lump_sum'::text AS capture_kind,
  NULL::text AS scanned_identifier,
  DATE(TIMEZONE('Asia/Kolkata', wso.accepted_at)::date) AS weigh_date,
  wso.accepted_at AS accepted_at,
  NULL::float8 AS weight_kg,
  wso.weight_kg AS total_weight_kg,
  wso.average_weight_kg,
  wso.animal_count,
  wso.verification_status,
  wso.campaign_shed_id,
  wcs.display_name AS shed_display_name,
  wcs.location_id AS location_id,
  wc.park_id
FROM weighing_shed_observations wso
JOIN weighing_campaign_sheds wcs ON wso.campaign_shed_id = wcs.campaign_shed_id
JOIN weighing_campaigns wc ON wcs.campaign_id = wc.campaign_id
WHERE wso.tenant_id = $1::uuid
  AND wcs.tenant_id = $1::uuid
  AND wc.tenant_id = $1::uuid
  AND ($2::uuid IS NULL OR wc.park_id = $2::uuid)
  AND ($3::uuid IS NULL OR wso.campaign_shed_id = $3::uuid)
  AND ($4::uuid[] IS NULL OR wc.park_id = ANY($4::uuid[]))
  -- Weights AWAITING VERIFICATION are included, and their status travels to the client so the
  -- chart can mark them. Excluding them made a weight captured this morning invisible to
  -- leadership until a verifier happened to act, so the honest answer to "what did we weigh
  -- today" was an empty chart. A pending weight is a real measurement; it is just not yet
  -- checked, which is a caption, not a reason to hide it.
  )
-- accepted_at, not just weigh_date: weigh_date is a DATE truncation, so an animal weighed
-- twice in one day has two rows with an IDENTICAL sort key and Postgres is free to return
-- them in any order. The series takes its CURRENT SHED from the last point, so an arbitrary
-- tie would silently pick the wrong shed for exactly the animals that moved that day.
ORDER BY weigh_date DESC, accepted_at DESC
LIMIT %d
`, MaxWeightHistoryPoints)

	rows, err := r.pool.Query(ctx, q, tenantID, parkFilter, shedFilter, authorized)
	if err != nil {
		return domain.WeightHistory{}, err
	}
	defer rows.Close()

	// Collect observations by series key.
	seriesByKey := make(map[string][]domain.WeightHistoryPoint)
	parkSet := make(map[string]string) // parkID -> just set marker
	shedSet := make(map[string]*domain.WeighingParkShed)
	cappedAt := ""

	for rows.Next() {
		var (
			captureKind        string
			scannedIdentifier  *string
			weighDate          time.Time
			weightKg           *float64
			totalWeightKg      *float64
			averageWeightKg    *float64
			animalCount        *int
			verificationStatus string
			campaignShedID     string
			shedDisplayName    string
			parkID             string
			locationID         string
		)
		var acceptedAt time.Time
		if err := rows.Scan(
			&captureKind, &scannedIdentifier, &weighDate, &acceptedAt, &weightKg, &totalWeightKg, &averageWeightKg, &animalCount,
			&verificationStatus, &campaignShedID, &shedDisplayName, &locationID, &parkID,
		); err != nil {
			return domain.WeightHistory{}, err
		}

		// Track parks and sheds.
		parkSet[parkID] = ""
		if _, ok := shedSet[campaignShedID]; !ok {
			shedSet[campaignShedID] = &domain.WeighingParkShed{
				CampaignShedID: campaignShedID,
				DisplayName:    shedDisplayName,
				ParkID:         parkID,
				// The PHYSICAL shed. A weighing task mints a new campaign_shed_id per shed per
				// weigh day, so one shed appears once per time it has been weighed. Clients group
				// the shed filter on this id; left empty (as it was), every shed in a park
				// collapsed into a single indistinguishable option and the filter did nothing.
				LocationID: locationID,
			}
		}

		// Build the series key based on capture kind.
		seriesKey := ""
		if captureKind == "individual" && scannedIdentifier != nil {
			seriesKey = fmt.Sprintf("individual:%s", *scannedIdentifier)
		} else if captureKind == "lump_sum" {
			seriesKey = fmt.Sprintf("lumpsum:%s", campaignShedID)
		} else {
			continue // Skip malformed rows.
		}

		// Build the point.
		point := domain.WeightHistoryPoint{
			CaptureKind:        captureKind,
			WeighDate:          weighDate.Format("2006-01-02"),
			VerificationStatus: verificationStatus,
			CampaignShedID:     campaignShedID,
			ShedDisplayName:    shedDisplayName,
		}

		if scannedIdentifier != nil {
			point.ScannedIdentifier = *scannedIdentifier
		}
		if weightKg != nil {
			point.WeightKg = *weightKg
		}
		if totalWeightKg != nil {
			point.TotalWeightKg = *totalWeightKg
		}
		if averageWeightKg != nil {
			point.AverageWeightKg = *averageWeightKg
		}
		if animalCount != nil {
			point.AnimalCount = *animalCount
		}

		seriesByKey[seriesKey] = append(seriesByKey[seriesKey], point)
	}
	if err := rows.Err(); err != nil {
		return domain.WeightHistory{}, err
	}

	// Check truncation.
	if len(seriesByKey) > MaxWeightHistoryUniqueTags {
		cappedAt = "max_unique_tags"
	}

	var totalDates map[string]struct{}
	var totalPoints int
	for _, series := range seriesByKey {
		totalPoints += len(series)
		for _, p := range series {
			if totalDates == nil {
				totalDates = make(map[string]struct{})
			}
			totalDates[p.WeighDate] = struct{}{}
		}
	}

	if len(totalDates) > MaxWeightHistoryWeighDays {
		cappedAt = "max_weigh_days"
	}
	if totalPoints > MaxWeightHistoryPoints {
		cappedAt = "max_points"
	}

	// Build final series (sorted, points reversed to oldest-first).
	var sortedKeys []string
	for key := range seriesByKey {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	seriesList := make([]domain.WeightHistorySeries, len(sortedKeys))
	for i, key := range sortedKeys {
		points := seriesByKey[key]
		// Reverse to get oldest first (query returns newest first).
		for j := 0; j < len(points)/2; j++ {
			points[j], points[len(points)-1-j] = points[len(points)-1-j], points[j]
		}

		// Determine series metadata.
		seriesKind := ""
		seriesID := ""
		shedName := ""
		if len(points) > 0 {
			seriesKind = points[0].CaptureKind
			shedName = points[0].ShedDisplayName
			if seriesKind == "individual" {
				seriesID = points[0].ScannedIdentifier
			} else {
				seriesID = points[0].CampaignShedID
			}
		}

		// CampaignShedID must be the SHED, never the series id. For an individual series the
		// series id is the scanned tag, and assigning it here shipped a tag in the shed field --
		// so a client filtering "show me Godel 1" compared a shed id against an RFID and matched
		// nothing. The chart looked empty for every shed that had data.
		// The LAST point, not the first: a series spans weigh days, and an animal weighed in
		// Godel 1 last month may sit in a different shed today. Pinning the series to its OLDEST
		// weigh meant "show me Godel 1" hid every animal that had ever been weighed elsewhere --
		// exactly the animals with the longest history. The shed the reader means is the current
		// one; the per-point shed name still travels with each bar.
		shedID := ""
		shedName2 := ""
		if len(points) > 0 {
			shedID = points[len(points)-1].CampaignShedID
			shedName2 = points[len(points)-1].ShedDisplayName
		}
		if shedName2 != "" {
			shedName = shedName2
		}
		seriesList[i] = domain.WeightHistorySeries{
			ScannedIdentifier: seriesID,
			CampaignShedID:    shedID,
			ShedDisplayName:   shedName,
			CaptureKind:       seriesKind,
			Points:            points,
		}
	}

	// Build park list from the parks actually in the data.
	parks := []domain.WeighingPark{}
	for parkID := range parkSet {
		// Look up the park name.
		var parkName string
		if err := r.pool.QueryRow(ctx, "SELECT name FROM locations WHERE location_id = $1::uuid", parkID).Scan(&parkName); err == nil {
			parks = append(parks, domain.WeighingPark{
				ParkID: parkID,
				Name:   parkName,
			})
		}
	}
	sort.Slice(parks, func(i, j int) bool { return parks[i].Name < parks[j].Name })

	// Build shed list from the sheds in the data.
	sheds := make([]domain.WeighingParkShed, 0, len(shedSet))
	for _, shed := range shedSet {
		sheds = append(sheds, *shed)
	}
	sort.Slice(sheds, func(i, j int) bool { return sheds[i].DisplayName < sheds[j].DisplayName })

	return domain.WeightHistory{
		Parks:     parks,
		Sheds:     sheds,
		Series:    seriesList,
		Truncated: cappedAt != "",
		CappedAt:  cappedAt,
	}, nil
}
