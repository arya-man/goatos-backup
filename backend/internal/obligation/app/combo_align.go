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

// AlignComboDrives harmonizes planned dates for approved combo bundles (FMD+HS, etc.) after
// per-version sweeps. maxShotsPerAnimalPerDrive (0 = no cap) and session together guard against
// co-locating combo batches onto one shared date pushing a member animal past
// MaxShotsPerAnimalPerDrive: a batch that would push any of its animals over the cap on the
// target date is left on its own (already-safe) planned date instead of being aligned. Pass the
// same SweepSession used for the preceding SweepVersionWithSession calls so the check also
// counts shots already claimed by those sweeps, not just by the combo batches being aligned.
func (s *SweeperService) AlignComboDrives(ctx context.Context, tenantID string, alignWindowDays int32, dueBefore time.Time, maxShotsPerAnimalPerDrive int32, session *SweepSession) (int, error) {
	aligner, ok := s.repo.(ComboDriveAligner)
	if !ok || alignWindowDays <= 0 {
		return 0, nil
	}
	session = sessionOrNew(session)
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
			// Seed session with the persisted, cross-pass shot count for this batch's animals on
			// the target date BEFORE checking the cap (VAX-REV-01): without this, a fresh session
			// (a new sweep pass, or AlignComboDrives running standalone) would wrongly assume 0
			// shots already exist on target for these animals and could align a batch onto a visit
			// that a PRIOR pass already filled, exceeding MaxShotsPerAnimalPerDrive.
			if maxShotsPerAnimalPerDrive > 0 {
				if err := s.seedVisitShotCounts(ctx, tenantID, batch.TargetIDs, target, maxShotsPerAnimalPerDrive, session); err != nil {
					return aligned, err
				}
			}
			if maxShotsPerAnimalPerDrive > 0 && comboBatchExceedsShotCapAtDate(batch, *target, maxShotsPerAnimalPerDrive, session) {
				// Aligning would push a member animal past the shot cap on the target date;
				// leave this batch on its own already-safe planned date (overflow).
				continue
			}
			if err := aligner.UpdateBatchPlannedDate(ctx, tenantID, batch.BatchID, *target); err != nil {
				return aligned, err
			}
			session.claimComboBatchTargets(batch.TargetIDs, *target)
			aligned++
		}
	}
	return aligned, nil
}

// comboBatchExceedsShotCapAtDate reports whether moving batch onto target would push any of its
// member animals past maxShots, given shots already claimed for that date in session (by prior
// sweeps and/or earlier-aligned batches in this same pass).
func comboBatchExceedsShotCapAtDate(batch domain.ComboDriveBatch, target time.Time, maxShots int32, session *SweepSession) bool {
	for _, targetID := range batch.TargetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		if session.visitShotCounts[visitShotCountKey(target, targetID)] >= maxShots {
			return true
		}
	}
	return false
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
