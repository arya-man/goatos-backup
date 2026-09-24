package app

// Reference implementation of the RETIRED per-request risk classification (whole filtered
// cohort walk + enrichment + cohort pen medians), kept only so tests can prove the persisted
// classifier is equivalent to it. Never used by production code.

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

const (
	liveSignalCohortPageSize = 5000
	liveSignalCohortMaxRows  = 50000
)

// classifyCohortInMemory is the old ListLive risk path's classification of the whole tenant.
func (s *Service) classifyCohortInMemory(ctx context.Context, tenantID string) ([]domain.LiveItem, error) {
	tags, err := s.listAllTagsLatest(ctx, tenantID, nil, nil, nil, nil, nil, nil, nil, domain.LiveSort{})
	if err != nil {
		return nil, err
	}
	items := s.enrichTagsBatch(ctx, tenantID, tags, nil, false)
	applyRiskSignals(items, riskGroupStatsFromItems(items))
	return items, nil
}

func (s *Service) listAllTagsLatest(ctx context.Context, tenantID string, parkID, shedID, movementState, liveState, mappingState, pattern, q *string, sort domain.LiveSort) ([]domain.TagLatest, error) {
	var all []domain.TagLatest
	cursor := ""
	for {
		// scale-guard:ignore: bounded keyset page walk owner=herd-signals issue=PR-251 reason=risk_state is computed after batched enrichment and must see the whole filtered live cohort; the loop is capped by liveSignalCohortMaxRows and advances by opaque repository cursor expiry=2026-12-31
		tags, _, nextCursor, err := s.repo.ListTagsLatest(ctx, tenantID, parkID, shedID, movementState, liveState, mappingState, pattern, q, cursor, liveSignalCohortPageSize, sort)
		if err != nil {
			return nil, err
		}
		all = append(all, tags...)
		if nextCursor == nil || *nextCursor == "" {
			return all, nil
		}
		if len(all) >= liveSignalCohortMaxRows {
			return nil, fmt.Errorf("live signal cohort exceeds %d rows", liveSignalCohortMaxRows)
		}
		if *nextCursor == cursor {
			return nil, fmt.Errorf("list tags cursor did not advance")
		}
		cursor = *nextCursor
	}
}

func riskGroupStatsFromItems(items []domain.LiveItem) map[string]riskGroupStats {
	groups := make(map[string][]domain.LiveItem)
	for _, item := range items {
		if item.PenKey == "" {
			continue
		}
		groups[item.PenKey] = append(groups[item.PenKey], item)
	}

	groupStats := make(map[string]riskGroupStats, len(groups))
	for shedID, groupItems := range groups {
		var motions []float64
		var temps []float64
		for _, item := range groupItems {
			if item.MotionDelta != nil && !item.GapDelta {
				motions = append(motions, float64(*item.MotionDelta))
			}
			if item.TagTemperatureC != nil {
				temps = append(temps, *item.TagTemperatureC)
			}
		}
		groupStats[shedID] = riskGroupStats{
			motionMedian: medianFloat(motions),
			tempMedian:   medianFloat(temps),
		}
	}
	return groupStats
}
