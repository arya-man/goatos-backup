package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// ComboDriveAligner aligns planned combo-session batches across protocol versions onto one shed visit date.
type ComboDriveAligner interface {
	ListPlannedComboBatches(ctx context.Context, tenantID string, dueBefore time.Time, limit int32) ([]domain.ComboDriveBatch, error)
	UpdateBatchPlannedDate(ctx context.Context, tenantID, batchID string, plannedDate time.Time) error
}

// AlignComboDrives harmonizes planned dates for approved combo bundles (FMD+HS, etc.) after per-version sweeps.
func (s *SweeperService) AlignComboDrives(ctx context.Context, tenantID string, alignWindowDays int32, dueBefore time.Time) (int, error) {
	aligner, ok := s.repo.(ComboDriveAligner)
	if !ok || alignWindowDays <= 0 {
		return 0, nil
	}
	rows, err := aligner.ListPlannedComboBatches(ctx, tenantID, dueBefore, s.page)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	type groupKey struct {
		scopeType string
		scopeID   string
		session   string
	}
	groups := make(map[groupKey][]domain.ComboDriveBatch)
	for _, row := range rows {
		if !strings.HasPrefix(row.Session, "combo:") {
			continue
		}
		key := groupKey{scopeType: row.ScopeType, scopeID: row.ScopeID, session: row.Session}
		groups[key] = append(groups[key], row)
	}
	aligned := 0
	window := time.Duration(alignWindowDays) * 24 * time.Hour
	for _, batches := range groups {
		if len(batches) < 2 {
			continue
		}
		target := pickComboAlignDate(batches, dueBefore, window)
		if target == nil {
			continue
		}
		for _, batch := range batches {
			if batch.PlannedDate != nil && businessDate(*batch.PlannedDate).Equal(*target) {
				continue
			}
			if err := aligner.UpdateBatchPlannedDate(ctx, tenantID, batch.BatchID, *target); err != nil {
				return aligned, err
			}
			aligned++
		}
	}
	return aligned, nil
}

func pickComboAlignDate(batches []domain.ComboDriveBatch, dueBefore time.Time, window time.Duration) *time.Time {
	if len(batches) == 0 {
		return nil
	}
	var minDate, maxDate *time.Time
	for _, batch := range batches {
		if batch.PlannedDate == nil || batch.PlannedDate.IsZero() {
			continue
		}
		day := businessDate(*batch.PlannedDate)
		if minDate == nil || day.Before(*minDate) {
			minDate = &day
		}
		if maxDate == nil || day.After(*maxDate) {
			maxDate = &day
		}
	}
	if minDate == nil || maxDate == nil {
		return nil
	}
	if maxDate.Sub(*minDate) > window {
		return nil
	}
	target := *maxDate
	nowDay := businessDate(dueBefore)
	if target.Before(nowDay) {
		target = nowDay
	}
	return &target
}
